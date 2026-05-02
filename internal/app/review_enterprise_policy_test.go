package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestReviewPlanRequiresGraphQLRelationBatching(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.md")
	plan := "Fix GraphQL nested relationship resolver count bug with regression tests."
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := NewReviewService().ReviewPlan(planPath, rulesDir)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFindingTitle(report.Findings, "GraphQL relation batching missing") {
		t.Fatalf("expected batching finding, got %#v", report.Findings)
	}
}

func TestEnterpriseDiffFindingsFlagDebugPrintAndContextTODO(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "application", "services", "foo.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package services

import (
	"context"
	"fmt"
)

func Run() {
	fmt.Println("debug")
	_ = context.TODO()
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := enterpriseDiffFindings(root, path)
	var joined []string
	for _, finding := range findings {
		joined = append(joined, finding.Title)
	}
	got := strings.Join(joined, "\n")
	if !strings.Contains(got, "Debug print") {
		t.Fatalf("expected debug print finding, got %#v", findings)
	}
	if !strings.Contains(got, "Context propagation") {
		t.Fatalf("expected context finding, got %#v", findings)
	}
}

func hasFindingTitle(findings []domain.Finding, title string) bool {
	for _, finding := range findings {
		if finding.Title == title {
			return true
		}
	}
	return false
}
