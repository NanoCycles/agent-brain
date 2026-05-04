package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexerIndexesNodeTypeScriptRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"node-service"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(root, "src", "routes")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := `import express from "express";
export interface CreateUserInput { name: string }
export class UserController {
  async listUsers() { return [] }
}
export function registerRoutes(app) {
  app.get("/users", listUsers)
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "users.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "users.test.ts"), []byte(`test("lists users", () => {})`), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := Indexer{}.Index(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Repository.GoModule != "node-service" {
		t.Fatalf("expected package name, got %q", idx.Repository.GoModule)
	}
	var foundRoute, foundTest, foundInterface bool
	for _, f := range idx.Files {
		for _, c := range f.Contracts {
			if c.Kind == "RESTEndpoint" && c.Name == "GET /users" {
				foundRoute = true
			}
		}
		if len(f.Tests) > 0 {
			foundTest = true
		}
		if len(f.Interfaces) > 0 {
			foundInterface = true
		}
	}
	if !foundRoute || !foundTest || !foundInterface {
		t.Fatalf("missing JS/TS index data: route=%t test=%t interface=%t files=%#v", foundRoute, foundTest, foundInterface, idx.Files)
	}
}
