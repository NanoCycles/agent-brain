package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestTaskAnalysisDetectsBUG(t *testing.T) {
	task := domain.Task{ID: "BUG-001", Title: "GraphQL nested count returns null", Content: "Nested count returns null"}
	analysis := AnalyzeTask(task)
	if analysis.Type != domain.TaskTypeBug {
		t.Fatalf("expected bug, got %s", analysis.Type)
	}
}

func TestTaskAnalysisDetectsGraphQLContractImpact(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null and may affect public contract"})
	if analysis.MainCapability != "graphql" || !containsString(analysis.ContractImpact, "GraphQL") {
		t.Fatalf("unexpected analysis: %#v", analysis)
	}
}

func TestTopicExtractorRemovesHeadingsAndNoise(t *testing.T) {
	topics := DetectTopics("# Title\n## Context\nProblem while are the api exposes returns GraphQL nested count null")
	for _, noisy := range []string{"title", "context", "problem", "while", "are", "the"} {
		if containsString(topics, noisy) {
			t.Fatalf("noise topic %q was not removed: %#v", noisy, topics)
		}
	}
	if !containsString(topics, "graphql") {
		t.Fatalf("missing graphql topic: %#v", topics)
	}
}

func TestTopicExtractorDetectsPhrases(t *testing.T) {
	topics := DetectTopics("GraphQL relationship nested count requires public contract review and schema change")
	for _, want := range []string{"nested count", "public contract", "schema change", "graphql relationship"} {
		if !containsString(topics, want) {
			t.Fatalf("missing topic %q in %#v", want, topics)
		}
	}
}

func TestRepoCapabilitiesDetectsGraphQLFromPath(t *testing.T) {
	files := []domain.IndexedFile{{Path: "internal/adapters/graphql/handler.go", Package: "graphql"}}
	caps := DetectRepoCapabilities(t.TempDir(), files)
	if !caps.HasGraphQL {
		t.Fatalf("expected GraphQL capability: %#v", caps)
	}
}

func TestRepoCapabilitiesDetectsGraphQLFromContract(t *testing.T) {
	files := []domain.IndexedFile{{Path: "graph/schema.graphql", Package: "graphql", Contracts: []domain.Contract{{Kind: "GraphQLField", Name: "Project.count"}}}}
	caps := DetectRepoCapabilities(t.TempDir(), files)
	if !caps.HasGraphQL || len(caps.Evidence) == 0 {
		t.Fatalf("expected GraphQL capability from contract: %#v", caps)
	}
}

func TestRepoCapabilitiesNoGraphQLForAgentBrainTooling(t *testing.T) {
	files := []domain.IndexedFile{
		{Path: "cmd/agent-brain/main.go", Package: "main"},
		{Path: "internal/app/context_service.go", Package: "app", Functions: []domain.Function{{Name: "RenderMarkdown"}}},
	}
	caps := DetectRepoCapabilities(t.TempDir(), files)
	if caps.HasGraphQL {
		t.Fatalf("did not expect GraphQL capability: %#v", caps)
	}
}

func TestFileRankingDoesNotRankCLIPrimaryForGraphQLBug(t *testing.T) {
	root := t.TempDir()
	files := []domain.IndexedFile{{Path: "cmd/agent-brain/main.go", Package: "main", Layer: "entrypoint"}}
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	caps := DetectRepoCapabilities(root, files)
	candidates := RankFileCandidates(context.Background(), root, files, analysis, caps, nil)
	for _, c := range candidates {
		if c.Path == "cmd/agent-brain/main.go" && c.Category == "primary_candidate" {
			t.Fatalf("cli main ranked primary: %#v", c)
		}
	}
}

func TestFileRankingRanksGraphQLHandlerHigh(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "adapters", "graphql")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "handler.go"), []byte("package graphql\n// nested count resolver\nfunc ProjectResolver() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []domain.IndexedFile{{Path: filepath.Join("internal", "adapters", "graphql", "handler.go"), Package: "graphql", Layer: "adapter_graphql", Functions: []domain.Function{{Name: "ProjectResolver"}}}}
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	caps := DetectRepoCapabilities(root, files)
	candidates := RankFileCandidates(context.Background(), root, files, analysis, caps, nil)
	if len(candidates) == 0 || candidates[0].Category != "primary_candidate" || candidates[0].Confidence < 0.7 {
		t.Fatalf("expected high GraphQL candidate: %#v", candidates)
	}
}

func TestFileRankingRanksGraphQLContractHigh(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "graph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte("type Project {\n  count: Int!\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []domain.IndexedFile{{Path: filepath.Join("graph", "schema.graphql"), Package: "graphql", Layer: "adapter_graphql", Contracts: []domain.Contract{{Kind: "GraphQLField", Name: "Project.count", Evidence: "GraphQL schema field"}}}}
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	caps := DetectRepoCapabilities(root, files)
	candidates := RankFileCandidates(context.Background(), root, files, analysis, caps, nil)
	if len(candidates) == 0 || candidates[0].Category != "primary_candidate" || !containsString(candidates[0].MatchedCapabilities, "graphql") {
		t.Fatalf("expected GraphQL contract to rank high: %#v", candidates)
	}
}

func TestFileRankingPrioritizesRelatedTests(t *testing.T) {
	root := t.TempDir()
	files := []domain.IndexedFile{
		{Path: filepath.Join("internal", "adapters", "graphql", "resolver.go"), Package: "graphql", Layer: "adapter_graphql", Contracts: []domain.Contract{{Kind: "GraphQLField", Name: "Project.count"}}},
		{Path: filepath.Join("internal", "adapters", "graphql", "resolver_test.go"), Package: "graphql", Layer: "adapter_graphql", Tests: []domain.Test{{Name: "TestProjectNestedCount"}}},
	}
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	caps := DetectRepoCapabilities(root, files)
	candidates := RankFileCandidates(context.Background(), root, files, analysis, caps, nil)
	var found bool
	for _, c := range candidates {
		if strings.HasSuffix(c.Path, "resolver_test.go") && c.Category == "related_test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected related test candidate: %#v", candidates)
	}
}

func TestContextQualityLowWhenDomainMissing(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	q := ComputeContextQuality(analysis, domain.RepoCapabilities{MainLanguage: "go"}, nil, domain.RuleGroups{}, false)
	if q.Level != "low" {
		t.Fatalf("expected low quality, got %#v", q)
	}
}

func TestContextQualityMediumOrHighWhenMatchingFilesAndRulesExist(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	caps := domain.RepoCapabilities{HasGraphQL: true, HasTests: true, MainLanguage: "go"}
	candidates := []domain.FileCandidate{
		{Path: "internal/adapters/graphql/a.go", Category: "primary_candidate"},
		{Path: "internal/adapters/graphql/b.go", Category: "primary_candidate"},
		{Path: "internal/adapters/graphql/c.go", Category: "primary_candidate"},
		{Path: "internal/adapters/graphql/a_test.go", Category: "related_test"},
	}
	rules := domain.RuleGroups{Architecture: []domain.Rule{{ID: "architecture.thin_adapters"}}}
	q := ComputeContextQuality(analysis, caps, candidates, rules, true)
	if q.Level == "low" {
		t.Fatalf("expected medium/high quality, got %#v", q)
	}
}

func TestRuleMatchingReturnsArchitectureRulesForGraphQLAdapter(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	groups := MatchRulesV2(flattenDefaultRules(), analysis)
	if len(groups.Architecture) == 0 {
		t.Fatalf("expected architecture rules: %#v", groups)
	}
}

func TestRiskEngineGraphQLNestedCount(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	risks := BuildRisks(analysis)
	if !containsJoined(risks.MemoryPerformance, "N+1") || !containsJoined(risks.Security, "tenant/project isolation") {
		t.Fatalf("unexpected risks: %#v", risks)
	}
}

func TestSuggestionsGraphQLNestedCount(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"})
	tests := SuggestTests(analysis)
	if !containsString(tests, "Nested count is not null.") || !containsString(tests, "Empty relation returns count 0.") {
		t.Fatalf("unexpected tests: %#v", tests)
	}
}

func TestMarkdownIncludesContextQuality(t *testing.T) {
	pack := BuildContextPack(context.Background(), t.TempDir(), nil, domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"}, flattenDefaultRules(), false)
	if !strings.Contains(RenderMarkdown(pack), "## Context Quality") {
		t.Fatal("markdown did not include context quality")
	}
}

func TestJSONIncludesAgentBudgetAndLikelyRelevantFileObjects(t *testing.T) {
	root := t.TempDir()
	files := []domain.IndexedFile{{Path: "internal/adapters/graphql/handler.go", Package: "graphql", Layer: "adapter_graphql"}}
	pack := BuildContextPack(context.Background(), root, files, domain.Task{ID: "BUG-001", Content: "GraphQL nested count returns null"}, flattenDefaultRules(), true)
	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"agent_budget"`) || !strings.Contains(string(data), `"likely_relevant_files":[{`) {
		t.Fatalf("json missing expected fields: %s", data)
	}
}

func flattenDefaultRules() []domain.Rule {
	var out []domain.Rule
	for _, set := range DefaultRuleSets() {
		out = append(out, set.Rules...)
	}
	return out
}

func containsJoined(xs []string, sub string) bool {
	return strings.Contains(strings.Join(xs, " "), sub)
}
