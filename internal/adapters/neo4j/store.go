package neo4j

import (
	"context"
	"fmt"

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
		if _, err := tx.Run(ctx, `merge (r:Repository {path:$path}) set r.name=$name, r.repo=$repo, r.commit_sha=$commit, r.indexed_at=$indexed_at, r.source='agent-brain', r.confidence=1.0`,
			map[string]any{"path": index.Repository.Root, "name": index.Repository.Name, "repo": index.Repository.Root, "commit": index.Repository.CommitSHA, "indexed_at": index.Repository.IndexedAt}); err != nil {
			return nil, err
		}
		for _, n := range index.Nodes {
			q := fmt.Sprintf(`merge (n:%s {repo:$repo, name:$name, path:$path}) set n.package=$package, n.layer=$layer, n.commit_sha=$commit, n.indexed_at=$indexed_at, n.source=$source, n.confidence=$confidence`, n.Label)
			if _, err := tx.Run(ctx, q, map[string]any{
				"repo": n.Repo, "name": n.Name, "path": n.Path, "package": n.Package, "layer": n.Layer,
				"commit": n.CommitSHA, "indexed_at": n.IndexedAt, "source": n.Source, "confidence": n.Confidence,
			}); err != nil {
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

func (s *Store) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
