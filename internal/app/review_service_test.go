package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractDiffFindingsDetectGraphQLSchema(t *testing.T) {
	findings := contractDiffFindings(t.TempDir(), "graph/schema.graphql")
	if len(findings) == 0 || findings[0].Title != "GraphQL contract modified" {
		t.Fatalf("expected GraphQL contract finding: %#v", findings)
	}
}

func TestContractDiffFindingsDetectProto(t *testing.T) {
	findings := contractDiffFindings(t.TempDir(), "proto/project.proto")
	if len(findings) == 0 || findings[0].Title != "gRPC/protobuf contract modified" {
		t.Fatalf("expected proto contract finding: %#v", findings)
	}
}

func TestContractDiffFindingsDetectRESTRoute(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "adapters", "http")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("internal", "adapters", "http", "routes.go")
	if err := os.WriteFile(filepath.Join(root, rel), []byte(`package http
func Register(r Router) { r.Get("/projects", handler) }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := contractDiffFindings(root, rel)
	if len(findings) == 0 || findings[0].Title != "REST route registration modified" {
		t.Fatalf("expected REST route finding: %#v", findings)
	}
}

func TestReviewDiffIncludesUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init")
	path := filepath.Join("internal", "app", "new_service.go")
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(`package app

import "fmt"

func Run() {
	fmt.Println("debug")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	report, summary, err := NewReviewService().ReviewDiff(context.Background(), root, filepath.Join(root, ".agent-brain", "rules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, filepath.ToSlash(path)) {
		t.Fatalf("expected untracked file in summary: %s", summary)
	}
	if !hasFindingTitle(report.Findings, "Debug print in production path") {
		t.Fatalf("expected debug print finding for untracked file, got %#v", report.Findings)
	}
}

func TestEnterpriseDiffFindingsDoNotRequireBoundaryProofInTests(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("internal", "adapters", "mcp", "server_test.go")
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`package mcp

import "testing"

func TestToolMentionsGraphQLBoundary(t *testing.T) {
	t.Log("review GraphQL public boundary auth validation event")
}
`)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
	findings := enterpriseDiffFindings(root, rel, string(data))
	for _, finding := range findings {
		switch finding.Title {
		case "Boundary authorization not evident", "Boundary input validation not evident", "Event idempotency not evident":
			t.Fatalf("unexpected production-boundary finding for test file: %#v", finding)
		}
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
