package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/rules"
	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/ports"
)

type ContextService struct {
	meta       ports.MetadataStore
	graph      ports.GraphStore
	ruleLoader rules.Loader
}

func NewContextService(meta ports.MetadataStore, graph ports.GraphStore) *ContextService {
	return &ContextService{meta: meta, graph: graph, ruleLoader: rules.Loader{}}
}

func (s *ContextService) Generate(ctx context.Context, taskPath, rulesDir, outputDir string) (domain.ContextPack, string, string, error) {
	task, err := ReadTask(taskPath)
	if err != nil {
		return domain.ContextPack{}, "", "", err
	}
	sets, err := s.ruleLoader.Load(rulesDir)
	if err != nil {
		return domain.ContextPack{}, "", "", err
	}
	allRules := rules.Flatten(sets)
	relevantRules := rules.Match(allRules, task.Topics)
	files, _ := s.meta.RelevantFiles(ctx, task.Topics, 8)
	layers := inferLayers(task.Topics, files)
	pack := domain.ContextPack{
		TaskID: task.ID, GeneratedAt: time.Now().UTC(), TaskSummary: summarize(task.Content),
		DetectedTopics: task.Topics, RelevantArchitectureRules: filterRulePrefix(relevantRules, "architecture."),
		RelevantBusinessTechnicalRules: filterRuleNotPrefix(relevantRules, "architecture."),
		LikelyAffectedLayers:           layers, LikelyRelevantFiles: files,
		PublicContractImpact: inferContracts(task.Topics),
		SecurityRisks:        risksFor(task.Topics, "security"), ConcurrencyRisks: risksFor(task.Topics, "concurrency"),
		MemoryPerformanceRisks:       risksFor(task.Topics, "performance"),
		SuggestedTests:               []string{"Add or update unit tests for changed behavior.", "Add integration tests when touching persistence or transports."},
		KnownPitfalls:                []string{"Keep adapters thin.", "Do not include secrets or full sensitive file contents in context."},
		RecommendedAgentInstructions: []string{"Read this pack before editing.", "Prefer smallest safe change.", "Do not run commit, push, merge, or destructive commands.", "Verify public contracts and required tests before final answer."},
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return domain.ContextPack{}, "", "", err
	}
	mdPath := filepath.Join(outputDir, task.ID+".agent.md")
	jsonPath := filepath.Join(outputDir, task.ID+".agent.json")
	j, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return domain.ContextPack{}, "", "", err
	}
	if err := os.WriteFile(jsonPath, j, 0o644); err != nil {
		return domain.ContextPack{}, "", "", err
	}
	if err := os.WriteFile(mdPath, []byte(RenderMarkdown(pack)), 0o644); err != nil {
		return domain.ContextPack{}, "", "", err
	}
	_ = s.meta.SaveContextPack(ctx, pack, mdPath, jsonPath)
	return pack, mdPath, jsonPath, nil
}

func ReadTask(path string) (domain.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Task{}, err
	}
	id := TaskIDFromPath(path)
	content := string(data)
	lines := strings.Split(content, "\n")
	title := id
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			title = line
			break
		}
	}
	return domain.Task{ID: id, Path: path, Title: title, Content: content, Topics: DetectTopics(content)}, nil
}

func TaskIDFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	id := strings.TrimSuffix(base, ext)
	re := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	id = re.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		return "task"
	}
	return id
}

func DetectTopics(text string) []string {
	words := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_/-]{2,}`).FindAllString(strings.ToLower(text), -1)
	stop := map[string]struct{}{"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {}, "debe": {}, "para": {}, "con": {}, "una": {}, "los": {}, "las": {}}
	seen := map[string]struct{}{}
	var out []string
	for _, w := range words {
		if _, ok := stop[w]; ok {
			continue
		}
		if _, ok := seen[w]; !ok {
			out = append(out, w)
			seen[w] = struct{}{}
		}
		if len(out) >= 16 {
			break
		}
	}
	return out
}

func RenderMarkdown(p domain.ContextPack) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent Context Pack\n\n## Task Summary\n%s\n\n", p.TaskSummary)
	writeList(&b, "Detected Topics", p.DetectedTopics)
	writeRules(&b, "Relevant Architecture Rules", p.RelevantArchitectureRules)
	writeRules(&b, "Relevant Business / Technical Rules", p.RelevantBusinessTechnicalRules)
	writeList(&b, "Likely Affected Layers", p.LikelyAffectedLayers)
	writeList(&b, "Likely Relevant Files", p.LikelyRelevantFiles)
	writeList(&b, "Public Contract Impact", p.PublicContractImpact)
	writeList(&b, "Security Risks", p.SecurityRisks)
	writeList(&b, "Concurrency Risks", p.ConcurrencyRisks)
	writeList(&b, "Memory / Performance Risks", p.MemoryPerformanceRisks)
	writeList(&b, "Suggested Tests", p.SuggestedTests)
	writeList(&b, "Known Pitfalls", p.KnownPitfalls)
	writeList(&b, "Recommended Agent Instructions", p.RecommendedAgentInstructions)
	return b.String()
}

func writeList(b *strings.Builder, title string, xs []string) {
	fmt.Fprintf(b, "## %s\n", title)
	if len(xs) == 0 {
		b.WriteString("- None detected.\n\n")
		return
	}
	for _, x := range xs {
		fmt.Fprintf(b, "- %s\n", x)
	}
	b.WriteString("\n")
}

func writeRules(b *strings.Builder, title string, rs []domain.Rule) {
	fmt.Fprintf(b, "## %s\n", title)
	if len(rs) == 0 {
		b.WriteString("- None matched.\n\n")
		return
	}
	for _, r := range rs {
		fmt.Fprintf(b, "- [%s] %s\n", r.Severity, r.Title)
	}
	b.WriteString("\n")
}

func summarize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

func filterRulePrefix(rs []domain.Rule, prefix string) []domain.Rule {
	var out []domain.Rule
	for _, r := range rs {
		if strings.HasPrefix(r.ID, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func filterRuleNotPrefix(rs []domain.Rule, prefix string) []domain.Rule {
	var out []domain.Rule
	for _, r := range rs {
		if !strings.HasPrefix(r.ID, prefix) {
			out = append(out, r)
		}
	}
	return out
}

func inferLayers(topics, files []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, x := range append(topics, files...) {
		for _, layer := range []string{"domain", "application", "adapter_rest", "adapter_graphql", "adapter_grpc", "adapter_events", "adapter_persistence", "entrypoint"} {
			if strings.Contains(strings.ToLower(x), strings.TrimPrefix(layer, "adapter_")) || strings.Contains(strings.ToLower(x), strings.ReplaceAll(layer, "_", "/")) {
				if _, ok := seen[layer]; !ok {
					out = append(out, layer)
					seen[layer] = struct{}{}
				}
			}
		}
	}
	return out
}

func inferContracts(topics []string) []string {
	var out []string
	for _, t := range topics {
		switch {
		case strings.Contains(t, "graphql"):
			out = append(out, "GraphQL")
		case strings.Contains(t, "grpc"), strings.Contains(t, "protobuf"):
			out = append(out, "gRPC")
		case strings.Contains(t, "event"):
			out = append(out, "Events")
		case strings.Contains(t, "rest"), strings.Contains(t, "http"):
			out = append(out, "REST")
		case strings.Contains(t, "db"), strings.Contains(t, "postgres"), strings.Contains(t, "sql"):
			out = append(out, "DB")
		}
	}
	if len(out) == 0 {
		return []string{"None detected"}
	}
	return out
}

func risksFor(topics []string, kind string) []string {
	for _, t := range topics {
		if strings.Contains(t, kind) || (kind == "performance" && strings.Contains(t, "n+1")) {
			return []string{"Review " + kind + " impact before implementation."}
		}
	}
	return []string{"None detected"}
}
