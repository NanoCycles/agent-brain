package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexerIndexesGoFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "domain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package domain
type User struct { ID string }
type Repo interface { Save(User) error }
func NewUser() User { return User{} }
func (User) Name() string { return "" }
`
	if err := os.WriteFile(filepath.Join(dir, "user.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Files) != 1 {
		t.Fatalf("expected 1 indexed file, got %d", len(idx.Files))
	}
	f := idx.Files[0]
	if f.Layer != "domain" || len(f.Structs) != 1 || len(f.Interfaces) != 1 || len(f.Functions) != 1 || len(f.Methods) != 1 {
		t.Fatalf("unexpected parsed file: %#v", f)
	}
}

func TestIndexerExtractsGraphQLSchemaContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "graph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	schema := `type Project {
  id: ID!
  items(first: Int): [Item!]!
  count: Int!
}
`
	if err := os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range idx.Files {
		for _, c := range f.Contracts {
			if c.Kind == "GraphQLField" && c.Name == "Project.count" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected Project.count GraphQL field contract: %#v", idx.Files)
	}
}

func TestIndexerExtractsProtoContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "proto")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	proto := `syntax = "proto3";
service ProjectService {
  rpc ListProjects (ListProjectsRequest) returns (ListProjectsResponse);
}
`
	if err := os.WriteFile(filepath.Join(dir, "project.proto"), []byte(proto), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range idx.Files {
		for _, c := range f.Contracts {
			if c.Kind == "GRPCMethod" && c.Name == "ProjectService.ListProjects" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected gRPC method contract: %#v", idx.Files)
	}
}

func TestIndexerExtractsRESTRouteContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "adapters", "http")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package http
import "net/http"
func Register() {
  http.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {})
}
`
	if err := os.WriteFile(filepath.Join(dir, "routes.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range idx.Files {
		for _, c := range f.Contracts {
			if c.Kind == "RESTEndpoint" && c.Name == "HTTP /projects" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected REST endpoint contract: %#v", idx.Files)
	}
}

func TestIndexerInfersCallsAndInterfaceImplementations(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "domain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package domain
type Saver interface { Save() error }
type Repo struct{}
func (Repo) Save() error { return nil }
func Use() error { return Repo{}.Save() }
`
	if err := os.WriteFile(filepath.Join(dir, "repo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var hasCall, hasImpl bool
	for _, rel := range idx.Relations {
		if rel.Type == "CALLS" {
			hasCall = true
		}
		if rel.Type == "IMPLEMENTS" {
			hasImpl = true
		}
	}
	if !hasCall || !hasImpl {
		t.Fatalf("expected CALLS and IMPLEMENTS relationships, got %#v", idx.Relations)
	}
}

func TestIndexerTypedCallResolutionDisambiguatesReceiver(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "domain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package domain
type Repo struct{}
type Cache struct{}
func (Repo) Save() error { return nil }
func (Cache) Save() error { return nil }
func Use(r Repo) error { return r.Save() }
`
	if err := os.WriteFile(filepath.Join(dir, "repo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var repoCall, cacheCall bool
	for _, rel := range idx.Relations {
		if rel.Type == "CALLS" && rel.FromKey == "internal/domain/repo.go#Use" && rel.ToKey == "internal/domain/repo.go#Repo.Save" {
			repoCall = true
		}
		if rel.Type == "CALLS" && rel.FromKey == "internal/domain/repo.go#Use" && rel.ToKey == "internal/domain/repo.go#Cache.Save" {
			cacheCall = true
		}
	}
	if !repoCall || cacheCall {
		t.Fatalf("expected typed call to Repo.Save only, repoCall=%v cacheCall=%v rels=%#v", repoCall, cacheCall, idx.Relations)
	}
}

func TestIndexerTypedImplementationWithPointerReceiver(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "domain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package domain
type Saver interface { Save() error }
type Repo struct{}
func (*Repo) Save() error { return nil }
`
	if err := os.WriteFile(filepath.Join(dir, "repo.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var hasImpl bool
	for _, rel := range idx.Relations {
		if rel.Type == "IMPLEMENTS" && rel.FromKey == "internal/domain/repo.go#Repo" && rel.ToKey == "internal/domain/repo.go#Saver" {
			hasImpl = true
		}
	}
	if !hasImpl {
		t.Fatalf("expected pointer receiver implementation, got %#v", idx.Relations)
	}
}
