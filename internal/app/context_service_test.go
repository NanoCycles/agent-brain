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
	p := domain.ContextPack{TaskID: "TASK-1", TaskSummary: "Fix thing", DetectedTopics: []string{"graphql"}, LikelyRelevantFiles: []string{"internal/adapters/graphql/resolver.go"}}
	md := RenderMarkdown(p)
	if md == "" || !contains(md, "# Agent Context Pack") || !contains(md, "## Likely Relevant Files") {
		t.Fatalf("unexpected markdown: %s", md)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
