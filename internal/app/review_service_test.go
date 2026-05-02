package app

import (
	"os"
	"path/filepath"
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
