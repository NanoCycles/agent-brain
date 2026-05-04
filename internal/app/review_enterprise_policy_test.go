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

	findings := enterpriseDiffFindings(root, path, "fmt.Println(\"debug\")\n_ = context.TODO()")
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

func TestEnterpriseDiffFindingsIgnoresPreExistingFullFileRisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "application", "services", "foo.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package services

import "context"

func Existing() {
	_ = context.TODO()
}

func Changed() string {
	return "safe"
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := enterpriseDiffFindings(root, path, `return "safe"`)
	if len(findings) != 0 {
		t.Fatalf("expected no findings for pre-existing full-file risk, got %#v", findings)
	}
}

func TestRelatedExistingTestsFindsIntegrationTestsNearChangedCode(t *testing.T) {
	root := t.TempDir()
	changed := filepath.Join("internal", "infrastructure", "adapters", "secondary", "graphql", "transformers", "nested_args_batch.go")
	testPath := filepath.Join(root, "internal", "infrastructure", "adapters", "secondary", "graphql", "transformers", "nested_args_resolver_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte("package transformers\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := relatedExistingTests(root, []string{changed}, 8)
	if len(tests) == 0 || !strings.Contains(tests[0], "nested_args_resolver_test.go") {
		t.Fatalf("expected related existing test, got %#v", tests)
	}
}

func TestNoTestsFindingSkippedWhenRelatedExistingTestsExist(t *testing.T) {
	root := t.TempDir()
	changed := filepath.Join("internal", "foo", "stripe_service.go")
	testPath := filepath.Join(root, "internal", "foo", "stripe_service_integration_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte("package foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := relatedExistingTests(root, []string{changed}, 8)
	if len(tests) == 0 {
		t.Fatal("expected existing related tests")
	}
	if hasTestableChanges([]string{changed}) && len(tests) == 0 {
		t.Fatal("would incorrectly emit no-tests finding")
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
