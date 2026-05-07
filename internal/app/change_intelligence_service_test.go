package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestClassifyChangeScopeCriticalForAuthTenantContract(t *testing.T) {
	task := domain.Task{
		ID:      "SEC-1",
		Title:   "Fix tenant isolation in GraphQL resolver",
		Content: "Security bug: authorization bypass in GraphQL resolver leaks tenant data through nested count.",
	}
	analysis := AnalyzeTask(task)
	scope := ClassifyChangeScope(analysis, domain.RepoCapabilities{HasGraphQL: true}, nil, task.Content)
	if scope.Level != domain.ChangeScopeCritical {
		t.Fatalf("expected critical scope, got %s (%d): %#v", scope.Level, scope.Score, scope.Reasons)
	}
	if len(scope.HumanApprovals) == 0 {
		t.Fatalf("expected human approval requirements")
	}
}

func TestImplementationChecklistIncludesGraphQLReviewPrevention(t *testing.T) {
	analysis := AnalyzeTask(domain.Task{
		ID:      "BUG-1",
		Title:   "GraphQL nested count returns null",
		Content: "Bug: GraphQL nested relationship count returns null and must avoid N+1.",
	})
	checklist := BuildImplementationChecklist(analysis, domain.ChangeScope{Level: domain.ChangeScopeLarge})
	text := strings.ToLower(strings.Join(checklist, "\n"))
	for _, want := range []string{"regression", "non-null", "n+1", "schema", "tenant/project"} {
		if !strings.Contains(text, want) {
			t.Fatalf("checklist missing %q:\n%s", want, text)
		}
	}
}

func TestPlanGateRejectsGraphQLPlanWithoutBatching(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.md")
	plan := "Fix GraphQL nested relationship count in resolver. Add tests and run review-diff."
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	report, summary, err := NewChangeIntelligenceService().PlanGate(context.Background(), dir, rulesDir, planPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != domain.Rejected {
		t.Fatalf("expected rejected, got %s\n%s", report.Decision, summary)
	}
	if !strings.Contains(summary, "GraphQL N+1 prevention missing") {
		t.Fatalf("missing N+1 finding:\n%s", summary)
	}
}

func TestReviewCommentsLearningProposalCreatesDomainMemory(t *testing.T) {
	dir := t.TempDir()
	comments := filepath.Join(dir, "comments.md")
	body := "- changes requested: GraphQL relation resolver introduces N+1 risk in internal/graphql/resolver.go"
	if err := os.WriteFile(comments, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	path, memory, err := ReviewCommentsLearningProposal(comments, dir)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || len(memory.BusinessRules) == 0 || len(memory.Invariants) == 0 {
		t.Fatalf("expected memory proposal, got path=%q rules=%d invariants=%d", path, len(memory.BusinessRules), len(memory.Invariants))
	}
	if memory.BusinessRules[0].Area != "graphql" {
		t.Fatalf("expected graphql area, got %q", memory.BusinessRules[0].Area)
	}
}
