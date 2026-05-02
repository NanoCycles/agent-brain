package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
	"gopkg.in/yaml.v3"
)

type DomainMemoryService struct {
	meta  ports.MetadataStore
	graph ports.GraphStore
}

func NewDomainMemoryService(meta ports.MetadataStore, graph ports.GraphStore) *DomainMemoryService {
	return &DomainMemoryService{meta: meta, graph: graph}
}

func (s *DomainMemoryService) Propose(ctx context.Context, repoRoot, taskPath, outputDir, area string) (string, domain.DomainMemory, error) {
	var task domain.Task
	var err error
	if taskPath != "" {
		task, err = ReadTask(taskPath)
		if err != nil {
			return "", domain.DomainMemory{}, err
		}
	} else {
		task = AnalyzeTextAsTask("domain-initial", "initial domain memory scan")
	}
	files, _ := s.meta.IndexedFiles(ctx, repoRoot)
	caps := DetectRepoCapabilities(repoRoot, files)
	memory := inferDomainMemory(task, caps, files, area)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", domain.DomainMemory{}, err
	}
	id := task.ID
	if id == "" || id == "task" {
		id = "domain-initial"
	}
	path := filepath.Join(outputDir, id+".domain.yml")
	data, err := yaml.Marshal(memory)
	if err != nil {
		return "", domain.DomainMemory{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", domain.DomainMemory{}, err
	}
	return path, memory, nil
}

func (s *DomainMemoryService) Apply(ctx context.Context, repoRoot, proposalPath, domainPath string, confirmed bool) (domain.DomainMemory, error) {
	if !confirmed {
		return domain.DomainMemory{}, os.ErrPermission
	}
	data, err := os.ReadFile(proposalPath)
	if err != nil {
		return domain.DomainMemory{}, err
	}
	var incoming domain.DomainMemory
	if err := yaml.Unmarshal(data, &incoming); err != nil {
		return domain.DomainMemory{}, err
	}
	current, _ := s.Load(ctx, repoRoot, domainPath)
	merged := mergeDomainMemory(current, incoming)
	merged.GeneratedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(domainPath), 0o755); err != nil {
		return domain.DomainMemory{}, err
	}
	out, err := yaml.Marshal(merged)
	if err != nil {
		return domain.DomainMemory{}, err
	}
	if err := os.WriteFile(domainPath, out, 0o644); err != nil {
		return domain.DomainMemory{}, err
	}
	if s.meta != nil {
		_ = s.meta.SaveDomainMemory(ctx, repoRoot, merged)
	}
	if s.graph != nil {
		_ = s.graph.SaveDomainMemory(ctx, repoRoot, merged)
	}
	return merged, nil
}

func (s *DomainMemoryService) Load(ctx context.Context, repoRoot, domainPath string) (domain.DomainMemory, error) {
	if data, err := os.ReadFile(domainPath); err == nil {
		var memory domain.DomainMemory
		if err := yaml.Unmarshal(data, &memory); err == nil {
			return memory, nil
		}
	}
	if s.meta != nil {
		return s.meta.DomainMemory(ctx, repoRoot)
	}
	return domain.DomainMemory{}, nil
}

func RenderDomainMemory(memory domain.DomainMemory, topic, area string, limit int) string {
	var b strings.Builder
	if limit <= 0 {
		limit = 8
	}
	fmt.Fprintf(&b, "Relevant System Memory")
	if topic != "" {
		fmt.Fprintf(&b, " for %q", topic)
	}
	if area != "" && area != "all" {
		fmt.Fprintf(&b, " in area %q", area)
	}
	b.WriteString("\n")
	writeDomainItems(&b, "Concepts", domainConceptLines(memory.DomainConcepts, topic, area), limit)
	writeDomainItems(&b, "Components", componentLines(memory.SystemComponents, topic, area), limit)
	writeDomainItems(&b, "Business Rules", ruleLines(memory.BusinessRules, topic, area), limit)
	writeDomainItems(&b, "Invariants", invariantLines(memory.Invariants, topic, area), limit)
	return b.String()
}

func inferDomainMemory(task domain.Task, caps domain.RepoCapabilities, files []domain.IndexedFile, requestedArea string) domain.DomainMemory {
	m := domain.DomainMemory{GeneratedAt: time.Now().UTC()}
	taskText := strings.ToLower(task.Content + " " + task.Title)
	byLayer := map[string][]string{}
	for _, f := range files {
		if f.Layer != "" {
			byLayer[f.Layer] = append(byLayer[f.Layer], f.Path)
		}
	}
	if shouldInferArea(requestedArea, "graphql") && caps.HasGraphQL {
		evidence := selectEvidence(files, "graphql", "resolver", "schema", "relationship", "nested")
		m.DomainConcepts = append(m.DomainConcepts, domain.DomainConcept{
			Name: "GraphQL API", Area: "graphql", Description: "Exposes application data and relationships through GraphQL contracts and resolvers.",
			Evidence: evidence, ImplementedBy: []string{"GraphQL adapter"}, Confidence: 0.78,
			OpenQuestions: []string{"Confirm domain-specific semantics for counts, permissions, and pagination before changing public behavior."},
		})
		m.SystemComponents = append(m.SystemComponents, domain.SystemComponent{
			Name: "GraphQL adapter", Area: "graphql", Kind: "adapter", Responsibility: "Builds GraphQL schema fields, resolvers, nested relationship behavior, and response shaping.",
			Files: evidence, Contracts: []string{"GraphQL"}, Confidence: 0.78,
		})
	}
	if shouldInferArea(requestedArea, "persistence") && caps.HasPersistence {
		evidence := selectEvidence(files, "repository", "store", "postgres", "mysql", "sqlite")
		m.SystemComponents = append(m.SystemComponents, domain.SystemComponent{
			Name: "Persistence adapter", Area: "persistence", Kind: "adapter", Responsibility: "Loads and persists domain/application data through repository/store implementations.",
			Files: evidence, Contracts: []string{"DB"}, Confidence: 0.65,
		})
	}
	if shouldInferArea(requestedArea, "graphql") && strings.Contains(taskText, "count") {
		evidence := selectEvidence(files, "count", "graphql", "relationship", "nested")
		m.BusinessRules = append(m.BusinessRules, domain.BusinessRule{
			Name: "Nested relationship count semantics", Area: "graphql", Statement: "Nested relationship counts should follow existing API semantics and must not become null when authoritative relation data is available.",
			Evidence: evidence, Confidence: 0.7,
		})
		m.Invariants = append(m.Invariants, domain.BusinessInvariant{
			Name: "Preserve public GraphQL response shape", Area: "graphql", Statement: "GraphQL changes must preserve public response shape unless explicitly approved.",
			Evidence: evidence, Confidence: 0.75,
		})
	}
	if shouldInferArea(requestedArea, "auth") && (mentionsAny(taskText, "auth", "authorization", "permission", "tenant", "project", "token") || hasEvidence(files, "auth", "authorization", "permission", "tenant", "project", "token")) {
		evidence := selectEvidence(files, "auth", "authorization", "permission", "tenant", "project", "token")
		m.DomainConcepts = append(m.DomainConcepts, domain.DomainConcept{
			Name: "Authorization boundary", Area: "auth", Description: "Controls who can access or mutate project, tenant, and user-scoped data.",
			Evidence: evidence, Confidence: 0.68,
			OpenQuestions: []string{"Confirm whether authorization is enforced in middleware, use cases, repositories, or all three."},
		})
		m.BusinessRules = append(m.BusinessRules, domain.BusinessRule{
			Name: "Tenant/project isolation", Area: "auth", Statement: "All reads and writes must preserve tenant/project isolation and avoid exposing hidden records through counts or errors.",
			Evidence: evidence, Confidence: 0.72,
		})
	}
	if shouldInferArea(requestedArea, "billing") && (mentionsAny(taskText, "billing", "invoice", "subscription", "payment", "plan", "quota") || hasEvidence(files, "billing", "invoice", "subscription", "payment", "plan", "quota")) {
		evidence := selectEvidence(files, "billing", "invoice", "subscription", "payment", "plan", "quota")
		m.DomainConcepts = append(m.DomainConcepts, domain.DomainConcept{
			Name: "Billing lifecycle", Area: "billing", Description: "Represents subscription, invoice, plan, quota, or payment behavior inferred from code and task context.",
			Evidence: evidence, Confidence: 0.62,
			OpenQuestions: []string{"Confirm source of truth for billing state and external payment provider contracts before changing behavior."},
		})
		m.Invariants = append(m.Invariants, domain.BusinessInvariant{
			Name: "Billing state must stay auditable", Area: "billing", Statement: "Billing changes should preserve traceability and avoid silent state transitions.",
			Evidence: evidence, Confidence: 0.65,
		})
	}
	if shouldInferArea(requestedArea, "events") && (caps.HasEvents || mentionsAny(taskText, "event", "consumer", "producer", "kafka", "pubsub", "webhook", "idempotent") || hasEvidence(files, "event", "consumer", "producer", "kafka", "pubsub", "webhook")) {
		evidence := selectEvidence(files, "event", "consumer", "producer", "kafka", "pubsub", "webhook")
		m.DomainConcepts = append(m.DomainConcepts, domain.DomainConcept{
			Name: "Event processing", Area: "events", Description: "Handles asynchronous messages, consumers, producers, retries, and delivery semantics.",
			Evidence: evidence, Confidence: 0.7,
		})
		m.BusinessRules = append(m.BusinessRules, domain.BusinessRule{
			Name: "Consumers are idempotent", Area: "events", Statement: "Event consumers must tolerate duplicate delivery and should not apply side effects twice.",
			Evidence: evidence, Confidence: 0.78,
		})
	}
	for layer, paths := range byLayer {
		sort.Strings(paths)
		if len(paths) > 3 {
			paths = paths[:3]
		}
		if layer == "application" || layer == "domain" {
			m.SystemComponents = append(m.SystemComponents, domain.SystemComponent{
				Name: titleWords(strings.ReplaceAll(layer, "_", " ")) + " layer", Area: layer, Kind: "layer", Responsibility: "Owns " + layer + " behavior inferred from repository structure.",
				Files: paths, Confidence: 0.55,
			})
		}
	}
	return m
}

func selectEvidence(files []domain.IndexedFile, terms ...string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, f := range files {
		hay := strings.ToLower(filepath.ToSlash(f.Path) + " " + f.Package)
		for _, term := range terms {
			if strings.Contains(hay, strings.ToLower(term)) {
				if _, ok := seen[f.Path]; !ok {
					out = append(out, f.Path)
					seen[f.Path] = struct{}{}
				}
				break
			}
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func shouldInferArea(requested, area string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	return requested == "" || requested == "all" || requested == strings.ToLower(area)
}

func hasEvidence(files []domain.IndexedFile, terms ...string) bool {
	return len(selectEvidence(files, terms...)) > 0
}

func mentionsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func titleWords(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func mergeDomainMemory(a, b domain.DomainMemory) domain.DomainMemory {
	out := a
	out.DomainConcepts = mergeByName(out.DomainConcepts, b.DomainConcepts)
	out.SystemComponents = mergeByName(out.SystemComponents, b.SystemComponents)
	out.BusinessRules = mergeByName(out.BusinessRules, b.BusinessRules)
	out.Invariants = mergeByName(out.Invariants, b.Invariants)
	return out
}

func mergeByName[T any](a, b []T) []T {
	nameOf := func(v any) string {
		switch x := v.(type) {
		case domain.DomainConcept:
			return x.Name
		case domain.SystemComponent:
			return x.Name
		case domain.BusinessRule:
			return x.Name
		case domain.BusinessInvariant:
			return x.Name
		default:
			return ""
		}
	}
	seen := map[string]int{}
	out := append([]T{}, a...)
	for i, item := range out {
		seen[strings.ToLower(nameOf(item))] = i
	}
	for _, item := range b {
		name := strings.ToLower(nameOf(item))
		if idx, ok := seen[name]; ok && name != "" {
			out[idx] = item
			continue
		}
		out = append(out, item)
	}
	return out
}

func writeDomainItems(b *strings.Builder, title string, xs []string, limit int) {
	if len(xs) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	if len(xs) > limit {
		xs = xs[:limit]
	}
	for _, x := range xs {
		fmt.Fprintf(b, "- %s\n", x)
	}
}

func domainConceptLines(xs []domain.DomainConcept, topic, area string) []string {
	var out []string
	for _, x := range xs {
		if matchesMemoryArea(area, x.Area) && matchesMemoryTopic(topic, x.Name+" "+x.Area, x.Description, x.Evidence) {
			out = append(out, fmt.Sprintf("%s: %s", x.Name, x.Description))
		}
	}
	return out
}

func componentLines(xs []domain.SystemComponent, topic, area string) []string {
	var out []string
	for _, x := range xs {
		if matchesMemoryArea(area, x.Area) && matchesMemoryTopic(topic, x.Name+" "+x.Area, x.Responsibility, x.Files) {
			out = append(out, fmt.Sprintf("%s: %s", x.Name, x.Responsibility))
		}
	}
	return out
}

func ruleLines(xs []domain.BusinessRule, topic, area string) []string {
	var out []string
	for _, x := range xs {
		if matchesMemoryArea(area, x.Area) && matchesMemoryTopic(topic, x.Name+" "+x.Area, x.Statement, x.Evidence) {
			out = append(out, fmt.Sprintf("%s: %s", x.Name, x.Statement))
		}
	}
	return out
}

func invariantLines(xs []domain.BusinessInvariant, topic, area string) []string {
	var out []string
	for _, x := range xs {
		if matchesMemoryArea(area, x.Area) && matchesMemoryTopic(topic, x.Name+" "+x.Area, x.Statement, x.Evidence) {
			out = append(out, fmt.Sprintf("%s: %s", x.Name, x.Statement))
		}
	}
	return out
}

func matchesMemoryArea(filter, area string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" || filter == "all" {
		return true
	}
	if strings.TrimSpace(area) == "" {
		return true
	}
	return strings.EqualFold(filter, area)
}

func matchesMemoryTopic(topic, name, description string, evidence []string) bool {
	if topic == "" {
		return true
	}
	hay := strings.ToLower(name + " " + description + " " + strings.Join(evidence, " "))
	for _, part := range strings.Fields(strings.ToLower(topic)) {
		if len(part) > 2 && strings.Contains(hay, part) {
			return true
		}
	}
	return false
}
