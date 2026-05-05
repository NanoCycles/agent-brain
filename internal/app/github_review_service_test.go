package app

import (
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestParseGitHubOwnerRepo(t *testing.T) {
	tests := map[string]string{
		"git@github.com:NanoCycles/agent-brain.git":       "NanoCycles/agent-brain",
		"https://github.com/NanoCycles/agent-brain.git":   "NanoCycles/agent-brain",
		"ssh://git@github.com/NanoCycles/agent-brain.git": "NanoCycles/agent-brain",
	}
	for remote, want := range tests {
		if got := parseGitHubOwnerRepo(remote); got != want {
			t.Fatalf("parseGitHubOwnerRepo(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestNormalizePRNumber(t *testing.T) {
	if got := normalizePRNumber("https://github.com/NanoCycles/agent-brain/pull/42"); got != "42" {
		t.Fatalf("expected PR 42, got %q", got)
	}
	if got := normalizePRNumber("17"); got != "17" {
		t.Fatalf("expected PR 17, got %q", got)
	}
	if got := normalizePRNumber("not-a-pr"); got != "" {
		t.Fatalf("expected empty PR, got %q", got)
	}
}

func TestRenderGitHubCommentsMarkdownIsReviewCommentsCompatible(t *testing.T) {
	md := renderGitHubCommentsMarkdown("NanoCycles/agent-brain", "42", []githubComment{
		{Author: "reviewer", Path: "internal/foo.go", Line: 12, Body: "Changes requested: missing test for tenant isolation."},
		{Author: "reviewer", Path: "internal/graphql/resolver.go", Line: 30, Body: "N+1 query risk; use DataLoader."},
	}, "test")
	if !strings.Contains(md, "internal/foo.go") || !strings.Contains(md, "N+1 query risk") {
		t.Fatalf("unexpected markdown: %s", md)
	}
	report, _, err := NewReviewService().ReviewCommentsFromText(md)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != domain.Rejected {
		t.Fatalf("expected rejected for tenant isolation review risk, got %s", report.Decision)
	}
	if len(report.Findings) < 2 {
		t.Fatalf("expected findings, got %#v", report.Findings)
	}
}

func TestRenderGitHubImportSummaryIsActionable(t *testing.T) {
	body := renderGitHubImportSummary("NanoCycles/agent-brain", "42", 3, ".ai/reviews/PR-42-comments.md")
	for _, want := range []string{"Imported comments: 3", "review-comments --file", "review-diff"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in summary: %s", want, body)
		}
	}
}
