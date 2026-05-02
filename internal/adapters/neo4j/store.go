package neo4j

import (
	"context"
	"fmt"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Store struct {
	driver neo4j.DriverWithContext
}

func New(uri, user, password string) (*Store, error) {
	d, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return nil, err
	}
	return &Store{driver: d}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.driver.VerifyConnectivity(ctx)
}

func (s *Store) SaveIndex(ctx context.Context, index domain.CodeIndex) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		if _, err := tx.Run(ctx, `match (n {repo:$repo}) detach delete n`, map[string]any{"repo": index.Repository.Root}); err != nil {
			return nil, err
		}
		if _, err := tx.Run(ctx, `merge (r:Repository {path:$path}) set r.name=$name, r.repo=$repo, r.commit_sha=$commit, r.indexed_at=$indexed_at, r.source='agent-brain', r.confidence=1.0`,
			map[string]any{"path": index.Repository.Root, "name": index.Repository.Name, "repo": index.Repository.Root, "commit": index.Repository.CommitSHA, "indexed_at": index.Repository.IndexedAt}); err != nil {
			return nil, err
		}
		for _, n := range index.Nodes {
			q := fmt.Sprintf(`merge (n:%s {repo:$repo, name:$name, path:$path}) set n.package=$package, n.layer=$layer, n.commit_sha=$commit, n.indexed_at=$indexed_at, n.source=$source, n.confidence=$confidence, n.operation=$operation, n.evidence=$evidence`, n.Label)
			params := map[string]any{
				"repo": n.Repo, "name": n.Name, "path": n.Path, "package": n.Package, "layer": n.Layer,
				"commit": n.CommitSHA, "indexed_at": n.IndexedAt, "source": n.Source, "confidence": n.Confidence,
				"operation": "", "evidence": "",
			}
			if n.Properties != nil {
				if op, ok := n.Properties["operation"].(string); ok {
					params["operation"] = op
				}
				if evidence, ok := n.Properties["evidence"].(string); ok {
					params["evidence"] = evidence
				}
			}
			if _, err := tx.Run(ctx, q, params); err != nil {
				return nil, err
			}
		}
		for _, rel := range index.Relations {
			q := fmt.Sprintf(`match (a:%s {repo:$repo, path:$from}), (b:%s {repo:$repo, path:$to}) merge (a)-[:%s]->(b)`, rel.FromLabel, rel.ToLabel, rel.Type)
			if _, err := tx.Run(ctx, q, map[string]any{"repo": rel.Repo, "from": rel.FromKey, "to": rel.ToKey}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (s *Store) Stats(ctx context.Context) (domain.GraphStats, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	res, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		row, err := tx.Run(ctx, `match (n) with count(n) as nodes optional match ()-[r]->() return nodes, count(r) as rels`, nil)
		if err != nil {
			return domain.GraphStats{}, err
		}
		if row.Next(ctx) {
			return domain.GraphStats{Nodes: row.Record().Values[0].(int64), Relationships: row.Record().Values[1].(int64)}, nil
		}
		return domain.GraphStats{}, row.Err()
	})
	if err != nil {
		return domain.GraphStats{}, err
	}
	return res.(domain.GraphStats), nil
}

func (s *Store) SearchImpact(ctx context.Context, topic string, limit int) ([]domain.GraphNode, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	res, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		rows, err := tx.Run(ctx, `match (n) where toLower(coalesce(n.name,'') + ' ' + coalesce(n.path,'') + ' ' + coalesce(n.layer,'')) contains toLower($topic) return labels(n)[0], n.name, n.path, n.repo, n.package, n.layer limit $limit`,
			map[string]any{"topic": topic, "limit": limit})
		if err != nil {
			return nil, err
		}
		var out []domain.GraphNode
		for rows.Next(ctx) {
			rec := rows.Record()
			out = append(out, domain.GraphNode{
				Label:   rec.Values[0].(string),
				Name:    stringValue(rec.Values[1]),
				Path:    stringValue(rec.Values[2]),
				Repo:    stringValue(rec.Values[3]),
				Package: stringValue(rec.Values[4]),
				Layer:   stringValue(rec.Values[5]),
			})
		}
		return out, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return res.([]domain.GraphNode), nil
}

func (s *Store) ExpandImpact(ctx context.Context, repoRoot string, topics []string, limit int) ([]domain.GraphNode, error) {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	query := strings.Join(topics, " ")
	res, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		rows, err := tx.Run(ctx, `
			match (seed {repo:$repo})
			where any(t in $topics where toLower(coalesce(seed.name,'') + ' ' + coalesce(seed.path,'') + ' ' + coalesce(seed.layer,'') + ' ' + coalesce(seed.evidence,'')) contains toLower(t))
			optional match p=(seed)-[*1..2]-(n)
			where n.repo=$repo and any(label in labels(n) where label in ['File','Function','Method','Test','RESTEndpoint','GraphQLField','GRPCMethod','EventType','Contract'])
			with distinct coalesce(n, seed) as node
			return labels(node)[0], node.name, node.path, node.repo, node.package, node.layer
			limit $limit`,
			map[string]any{"repo": repoRoot, "topics": topics, "q": query, "limit": limit})
		if err != nil {
			return nil, err
		}
		var out []domain.GraphNode
		for rows.Next(ctx) {
			rec := rows.Record()
			out = append(out, domain.GraphNode{
				Label:   rec.Values[0].(string),
				Name:    stringValue(rec.Values[1]),
				Path:    stringValue(rec.Values[2]),
				Repo:    stringValue(rec.Values[3]),
				Package: stringValue(rec.Values[4]),
				Layer:   stringValue(rec.Values[5]),
			})
		}
		return out, rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return res.([]domain.GraphNode), nil
}

func (s *Store) SaveMemory(ctx context.Context, memory domain.MemoryRecord) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		params := map[string]any{
			"repo": memory.RepoRoot, "task_id": memory.TaskID, "source_path": memory.SourcePath,
			"applied_at": memory.AppliedAt, "lessons": memory.LessonsLearned, "rules": memory.SuggestedRules,
			"bugs": memory.RelatedBugs, "tests": memory.TestsAdded, "files": memory.FilesModified, "risks": memory.RisksDetected,
		}
		if _, err := tx.Run(ctx, `
			merge (t:Task {repo:$repo, name:$task_id, path:$source_path})
			set t.source='agent-brain-memory', t.indexed_at=$applied_at, t.confidence=1.0
			with t
			unwind $lessons as lesson
			merge (m:Memory {repo:$repo, name:lesson, path:$task_id + '#lesson:' + lesson})
			set m.source='agent-brain-memory', m.indexed_at=$applied_at, m.confidence=0.9
			merge (t)-[:RELATED_TO]->(m)`,
			params); err != nil {
			return nil, err
		}
		if _, err := tx.Run(ctx, `
			match (t:Task {repo:$repo, name:$task_id})
			unwind $rules as rule
			merge (r:Rule {repo:$repo, name:rule, path:$task_id + '#rule:' + rule})
			set r.source='agent-brain-memory', r.indexed_at=$applied_at, r.confidence=0.8
			merge (t)-[:GOVERNED_BY]->(r)`,
			params); err != nil {
			return nil, err
		}
		if _, err := tx.Run(ctx, `
			match (t:Task {repo:$repo, name:$task_id})
			unwind $risks as risk
			merge (r:Risk {repo:$repo, name:risk, path:$task_id + '#risk:' + risk})
			set r.source='agent-brain-memory', r.indexed_at=$applied_at, r.confidence=0.8
			merge (t)-[:AFFECTS]->(r)`,
			params); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (s *Store) SaveDomainMemory(ctx context.Context, repoRoot string, memory domain.DomainMemory) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		params := map[string]any{"repo": repoRoot, "indexed_at": memory.GeneratedAt}
		if _, err := tx.Run(ctx, `match (n {repo:$repo, source:'agent-brain-domain-memory'}) detach delete n`, params); err != nil {
			return nil, err
		}
		for _, c := range memory.DomainConcepts {
			if _, err := tx.Run(ctx, `
				merge (n:DomainConcept {repo:$repo, name:$name})
				set n.path=$path, n.area=$area, n.source='agent-brain-domain-memory', n.confidence=$confidence, n.description=$description, n.evidence=$evidence, n.indexed_at=$indexed_at`,
				map[string]any{"repo": repoRoot, "name": c.Name, "area": c.Area, "path": "domain://" + c.Name, "confidence": c.Confidence, "description": c.Description, "evidence": strings.Join(c.Evidence, "\n"), "indexed_at": memory.GeneratedAt}); err != nil {
				return nil, err
			}
		}
		for _, c := range memory.SystemComponents {
			if _, err := tx.Run(ctx, `
				merge (n:SystemComponent {repo:$repo, name:$name})
				set n.path=$path, n.area=$area, n.source='agent-brain-domain-memory', n.confidence=$confidence, n.kind=$kind, n.description=$responsibility, n.evidence=$evidence, n.indexed_at=$indexed_at`,
				map[string]any{"repo": repoRoot, "name": c.Name, "area": c.Area, "path": "component://" + c.Name, "confidence": c.Confidence, "kind": c.Kind, "responsibility": c.Responsibility, "evidence": strings.Join(c.Files, "\n"), "indexed_at": memory.GeneratedAt}); err != nil {
				return nil, err
			}
			for _, file := range c.Files {
				if _, err := tx.Run(ctx, `
					match (component:SystemComponent {repo:$repo, name:$name})
					optional match (file:File {repo:$repo, path:$file})
					with component, file
					where file is not null
					merge (file)-[:DEFINES]->(component)`,
					map[string]any{"repo": repoRoot, "name": c.Name, "file": file}); err != nil {
					return nil, err
				}
			}
		}
		for _, r := range memory.BusinessRules {
			if _, err := tx.Run(ctx, `
				merge (n:BusinessRule {repo:$repo, name:$name})
				set n.path=$path, n.area=$area, n.source='agent-brain-domain-memory', n.confidence=$confidence, n.description=$statement, n.evidence=$evidence, n.indexed_at=$indexed_at`,
				map[string]any{"repo": repoRoot, "name": r.Name, "area": r.Area, "path": "rule://" + r.Name, "confidence": r.Confidence, "statement": r.Statement, "evidence": strings.Join(r.Evidence, "\n"), "indexed_at": memory.GeneratedAt}); err != nil {
				return nil, err
			}
		}
		for _, inv := range memory.Invariants {
			if _, err := tx.Run(ctx, `
				merge (n:Invariant {repo:$repo, name:$name})
				set n.path=$path, n.area=$area, n.source='agent-brain-domain-memory', n.confidence=$confidence, n.description=$statement, n.evidence=$evidence, n.indexed_at=$indexed_at`,
				map[string]any{"repo": repoRoot, "name": inv.Name, "area": inv.Area, "path": "invariant://" + inv.Name, "confidence": inv.Confidence, "statement": inv.Statement, "evidence": strings.Join(inv.Evidence, "\n"), "indexed_at": memory.GeneratedAt}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

func (s *Store) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
