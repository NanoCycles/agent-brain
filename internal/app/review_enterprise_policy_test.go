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

func TestReviewPlanRequiresBoundaryAuthAndValidation(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(dir, "plan.md")
	plan := "Implement new REST handler for customer billing."
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := NewReviewService().ReviewPlan(planPath, rulesDir)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFindingTitle(report.Findings, "Boundary validation missing") {
		t.Fatalf("expected boundary validation finding, got %#v", report.Findings)
	}
	if !hasFindingTitle(report.Findings, "Boundary authorization missing") {
		t.Fatalf("expected boundary authorization finding, got %#v", report.Findings)
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

func TestEnterpriseDiffFindingsIgnoresTestContextBackground(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "app", "index_service_test.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package app

import "context"

func TestRun() {
	_ = context.Background()
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := enterpriseDiffFindings(root, path, "_ = context.Background()")
	if len(findings) != 0 {
		t.Fatalf("expected test context helper to be allowed, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsIgnoresPolicyStringLiterals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "app", "review_service.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package app

import "strings"

func Review(text string) bool {
	return strings.Contains(text, "fmt.println(") || strings.Contains(text, "context.todo()")
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := enterpriseDiffFindings(root, path, `return strings.Contains(text, "fmt.println(") || strings.Contains(text, "context.todo()")`)
	if len(findings) != 0 {
		t.Fatalf("expected policy string literals to be ignored, got %#v", findings)
	}
}

func TestContractDiffFindingsIgnoresRoutePolicyStringLiterals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "app", "review_service.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package app

func Review(text string) bool {
	return text == ".get(" || text == ".post("
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := contractDiffFindings(root, path)
	if len(findings) != 0 {
		t.Fatalf("expected route policy string literals to be ignored, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsAllowsCLIOutputWriters(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "adapters", "cli", "root.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `package cli

import "fmt"

func Print(out any) {
	fmt.Fprintln(out, "done")
}
`
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	findings := enterpriseDiffFindings(root, path, `fmt.Fprintln(out, "done")`)
	if len(findings) != 0 {
		t.Fatalf("expected CLI output writer to be allowed, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsBlockDomainInfrastructureImport(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "domain", "service.go")
	full := writeReviewFixture(t, root, path, `package domain

import "github.com/acme/app/internal/adapters/postgres"

func Run() { _ = postgres.New }
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if !hasFindingTitle(findings, "Domain layer depends on framework/infrastructure") {
		t.Fatalf("expected domain architecture finding, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsPublicBoundaryRequiresAuthAndValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "adapters", "http", "users_handler.go")
	full := writeReviewFixture(t, root, path, `package http

func Register(r Router) {
	r.Get("/users", listUsers)
}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if !hasFindingTitle(findings, "Boundary authorization not evident") {
		t.Fatalf("expected authorization finding, got %#v", findings)
	}
	if !hasFindingTitle(findings, "Boundary input validation not evident") {
		t.Fatalf("expected validation finding, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsDetectInjectionAndResourceRisks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "adapters", "postgres", "repo.go")
	full := writeReviewFixture(t, root, path, `package postgres

import (
	"database/sql"
	"fmt"
	"net/http"
	"os/exec"
)

func Find(db *sql.DB, table string) {
	db.Query("select * from " + table)
	_ = fmt.Sprintf("select * from %s", table)
	http.Get("https://example.com")
	exec.Command("sh", "-c", "echo " + table)
}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	for _, title := range []string{"Possible SQL injection", "HTTP call lacks context", "Command execution lacks context", "Possible command injection", "Database call lacks context"} {
		if !hasFindingTitle(findings, title) {
			t.Fatalf("expected %q finding, got %#v", title, findings)
		}
	}
}

func TestEnterpriseDiffFindingsDetectSecretsAndSensitiveLogs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "application", "service.go")
	full := writeReviewFixture(t, root, path, `package application

import "log"

const apiKey = "abc123"

func Run(token string) {
	log.Printf("token=%s", token)
}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if !hasFindingTitle(findings, "Possible hardcoded secret") {
		t.Fatalf("expected hardcoded secret finding, got %#v", findings)
	}
	if !hasFindingTitle(findings, "Sensitive data logging risk") {
		t.Fatalf("expected sensitive logging finding, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsDoesNotFlagPolicyKeywordListsAsSecrets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "app", "review_service.go")
	full := writeReviewFixture(t, root, path, `package app

func Review(text string) bool {
	return reviewTextContainsAny(text, "api_key", "secret", "password", "token")
}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if hasFindingTitle(findings, "Possible hardcoded secret") {
		t.Fatalf("expected policy keyword list to be allowed, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsEventChangesRequireIdempotency(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "adapters", "events", "consumer.go")
	full := writeReviewFixture(t, root, path, `package events

func Consume(event Event) error {
	return publish(event)
}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if !hasFindingTitle(findings, "Event idempotency not evident") {
		t.Fatalf("expected idempotency finding, got %#v", findings)
	}
}

func TestEnterpriseDiffFindingsGraphQLPublicExecutionRequiresDoSControls(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("internal", "adapters", "graphql", "server.go")
	full := writeReviewFixture(t, root, path, `package graphql

func QueryResolver() {}
`)

	findings := enterpriseDiffFindings(root, path, readFixture(t, full))
	if !hasFindingTitle(findings, "GraphQL DoS controls not evident") {
		t.Fatalf("expected GraphQL DoS finding, got %#v", findings)
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

func writeReviewFixture(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hasFindingTitle(findings []domain.Finding, title string) bool {
	for _, finding := range findings {
		if finding.Title == title {
			return true
		}
	}
	return false
}
