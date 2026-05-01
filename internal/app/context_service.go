package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

func (s *ContextService) Generate(ctx context.Context, repoRoot, taskPath, rulesDir, outputDir string) (domain.ContextPack, string, string, error) {
	task, err := ReadTask(taskPath)
	if err != nil {
		return domain.ContextPack{}, "", "", err
	}
	return s.generateForTask(ctx, repoRoot, task, rulesDir, outputDir, true)
}

func (s *ContextService) GenerateForText(ctx context.Context, repoRoot, id, text, rulesDir string) (domain.ContextPack, error) {
	task := AnalyzeTextAsTask(id, text)
	pack, _, _, err := s.generateForTask(ctx, repoRoot, task, rulesDir, "", false)
	return pack, err
}

func (s *ContextService) generateForTask(ctx context.Context, repoRoot string, task domain.Task, rulesDir, outputDir string, writeFiles bool) (domain.ContextPack, string, string, error) {
	sets, err := s.ruleLoader.Load(rulesDir)
	if err != nil {
		return domain.ContextPack{}, "", "", err
	}
	allRules := rules.Flatten(sets)
	var indexedFiles []domain.IndexedFile
	if s.meta != nil {
		_ = s.meta.Init(ctx)
		indexedFiles, _ = s.meta.IndexedFiles(ctx, repoRoot)
	}
	graphAvailable := false
	if s.graph != nil && s.graph.Ping(ctx) == nil {
		graphAvailable = true
	}
	pack := BuildContextPack(ctx, repoRoot, indexedFiles, task, allRules, graphAvailable)
	if !writeFiles {
		return pack, "", "", nil
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
	if s.meta != nil {
		_ = s.meta.SaveContextPack(ctx, pack, mdPath, jsonPath)
	}
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

func RenderMarkdown(p domain.ContextPack) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent Context Pack\n\n")
	fmt.Fprintf(&b, "## Context Quality\nLevel: %s\nScore: %.2f\n", p.ContextQuality.Level, p.ContextQuality.Score)
	writeList(&b, "Warnings", p.ContextQuality.Warnings)
	fmt.Fprintf(&b, "Recommended next action: %s\n\n", p.ContextQuality.RecommendedNextAction)
	fmt.Fprintf(&b, "## Task Summary\n%s\n\n", p.TaskSummary)
	fmt.Fprintf(&b, "## Task Analysis\n- Type: %s\n- Main capability: %s\n- Contract impact: %s\n- Affected layers: %s\n\n",
		p.TaskAnalysis.Type, p.TaskAnalysis.MainCapability, strings.Join(p.PublicContractImpact, ", "), strings.Join(p.LikelyAffectedLayers, ", "))
	writeCapabilities(&b, p.RepositoryCapabilities)
	writeList(&b, "Detected Technical Topics", p.DetectedTopics)
	writeRuleGroups(&b, p.RelevantRules)
	writeCandidates(&b, p)
	writeList(&b, "Public Contract Impact", p.PublicContractImpact)
	b.WriteString("## Risks\n")
	writeList(&b, "Security", p.Risks.Security)
	writeList(&b, "Concurrency", p.Risks.Concurrency)
	writeList(&b, "Memory/Performance", p.Risks.MemoryPerformance)
	writeList(&b, "Suggested Tests", p.SuggestedTests)
	writeList(&b, "Recommended Strategy", p.RecommendedStrategy)
	writeList(&b, "Recommended Agent Instructions", p.RecommendedAgentInstructions)
	return b.String()
}

func writeCapabilities(b *strings.Builder, caps domain.RepoCapabilities) {
	b.WriteString("## Repository Capabilities\n")
	writeCapLine(b, "GraphQL", caps.HasGraphQL, caps.Evidence)
	writeCapLine(b, "REST", caps.HasREST, caps.Evidence)
	writeCapLine(b, "gRPC", caps.HasGRPC, caps.Evidence)
	writeCapLine(b, "Events", caps.HasEvents, caps.Evidence)
	writeCapLine(b, "Persistence", caps.HasPersistence, caps.Evidence)
	writeCapLine(b, "Tests", caps.HasTests, caps.Evidence)
	b.WriteString("\n")
}

func writeCapLine(b *strings.Builder, name string, ok bool, evidence []domain.CapabilityEvidence) {
	value := "no"
	if ok {
		value = "yes"
	}
	fmt.Fprintf(b, "- %s: %s", name, value)
	lower := strings.ToLower(name)
	if lower == "grpc" {
		lower = "grpc"
	}
	for _, e := range evidence {
		if e.Capability == lower {
			fmt.Fprintf(b, " (%s: %s)", e.Path, e.Reason)
			break
		}
	}
	b.WriteString("\n")
}

func writeRuleGroups(b *strings.Builder, groups domain.RuleGroups) {
	b.WriteString("## Relevant Rules\n")
	writeInlineRules(b, "Architecture", groups.Architecture)
	writeInlineRules(b, "GraphQL/REST/gRPC/Events", groups.Contracts)
	writeInlineRules(b, "Security", groups.Security)
	writeInlineRules(b, "Testing", groups.Testing)
	b.WriteString("\n")
}

func writeInlineRules(b *strings.Builder, title string, rs []domain.Rule) {
	fmt.Fprintf(b, "- %s:", title)
	if len(rs) == 0 {
		b.WriteString(" none\n")
		return
	}
	for i, r := range rs {
		if i > 0 {
			b.WriteString(";")
		}
		fmt.Fprintf(b, " [%s] %s", r.Severity, r.Title)
	}
	b.WriteString("\n")
}

func writeCandidates(b *strings.Builder, p domain.ContextPack) {
	b.WriteString("## Likely Relevant Files\n")
	if len(p.LikelyRelevantFiles) == 0 {
		b.WriteString("- No strong relevant files detected.\n")
		fmt.Fprintf(b, "- Recommended next action: %s\n\n", p.ContextQuality.RecommendedNextAction)
		return
	}
	for i, c := range p.LikelyRelevantFiles {
		fmt.Fprintf(b, "%d. %s\n   Category: %s\n   Confidence: %.2f\n   Reason: %s\n", i+1, c.Path, c.Category, c.Confidence, c.Reason)
		if len(c.Evidence) > 0 {
			fmt.Fprintf(b, "   Evidence: %s\n", strings.Join(c.Evidence, "; "))
		}
		if len(c.MatchedTopics) > 0 {
			fmt.Fprintf(b, "   Matched topics: %s\n", strings.Join(c.MatchedTopics, ", "))
		}
		if c.Warning != "" {
			fmt.Fprintf(b, "   Warning: %s\n", c.Warning)
		}
	}
	b.WriteString("\n")
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

func summarize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}
