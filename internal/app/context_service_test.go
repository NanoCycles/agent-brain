package app

import (
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestTaskIDFromPath(t *testing.T) {
	got := TaskIDFromPath(".ai/tasks/ABC-123 fix login.md")
	if got != "ABC-123-fix-login" {
		t.Fatalf("unexpected id: %s", got)
	}
}

func TestRenderMarkdownBasic(t *testing.T) {
	p := domain.ContextPack{TaskID: "TASK-1", TaskSummary: "Fix thing", DetectedTopics: []string{"graphql"}, LikelyRelevantFiles: []domain.FileCandidate{{Path: "internal/adapters/graphql/resolver.go"}}}
	md := RenderMarkdown(p)
	if md == "" || !contains(md, "# Agent Context Pack") || !contains(md, "## Files") {
		t.Fatalf("unexpected markdown: %s", md)
	}
}

func TestApplyBudgetDefaultsToCavernicola(t *testing.T) {
	p := domain.ContextPack{
		LikelyRelevantFiles: []domain.FileCandidate{
			{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}, {Path: "d.go"}, {Path: "e.go"},
		},
		DetectedTopics:               []string{"graphql", "nested count", "count", "null", "relationship", "schema", "pagination", "tenant", "n+1"},
		SuggestedTests:               []string{"t1", "t2", "t3", "t4", "t5"},
		RecommendedStrategy:          []string{"s1", "s2", "s3", "s4", "s5", "s6"},
		ContextQuality:               domain.ContextQuality{Level: "medium"},
		RecommendedAgentInstructions: []string{"i1", "i2", "i3", "i4", "i5"},
	}
	ApplyBudget(&p, "")
	if p.AgentBudget.TokenMode != BudgetCavernicola {
		t.Fatalf("expected cavernicola budget, got %s", p.AgentBudget.TokenMode)
	}
	if len(p.LikelyRelevantFiles) != 4 {
		t.Fatalf("expected 4 files, got %d", len(p.LikelyRelevantFiles))
	}
	if p.AgentBudget.OpenTopFilesFirst != 2 {
		t.Fatalf("expected open top 2, got %d", p.AgentBudget.OpenTopFilesFirst)
	}
}

func TestUpdateContextEfficiency(t *testing.T) {
	p := domain.ContextPack{
		LikelyRelevantFiles: []domain.FileCandidate{{Path: "a.go"}, {Path: "b.go"}},
		AgentBudget:         domain.AgentBudget{OpenTopFilesFirst: 1, ExplorationMode: "minimal"},
		ContextQuality:      domain.ContextQuality{Level: "high"},
	}
	UpdateContextEfficiency(&p, 10)
	if p.ContextEfficiency.ReturnedFiles != 2 || p.ContextEfficiency.FilesAvoided != 8 {
		t.Fatalf("unexpected efficiency: %#v", p.ContextEfficiency)
	}
	if p.ContextEfficiency.EstimatedTokensSaved != 6400 {
		t.Fatalf("unexpected token savings: %#v", p.ContextEfficiency)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
