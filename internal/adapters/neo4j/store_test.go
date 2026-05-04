package neo4j

import (
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestGraphChangesForPathsKeepsOnlyChangedFileSubgraph(t *testing.T) {
	now := time.Now()
	index := domain.CodeIndex{
		Repository: domain.Repository{Root: "/repo", Name: "repo", IndexedAt: now},
		Nodes: []domain.GraphNode{
			{Label: "Repository", Path: "/repo", Repo: "/repo"},
			{Label: "Package", Path: "internal/domain", Repo: "/repo"},
			{Label: "Layer", Path: "domain", Repo: "/repo"},
			{Label: "File", Path: "internal/domain/user.go", Repo: "/repo"},
			{Label: "Function", Path: "internal/domain/user.go#FindUser", Repo: "/repo"},
			{Label: "File", Path: "internal/domain/order.go", Repo: "/repo"},
			{Label: "Function", Path: "internal/domain/order.go#FindOrder", Repo: "/repo"},
		},
		Relations: []domain.GraphRelationship{
			{FromLabel: "Repository", FromKey: "/repo", ToLabel: "File", ToKey: "internal/domain/user.go", Type: "CONTAINS", Repo: "/repo"},
			{FromLabel: "File", FromKey: "internal/domain/user.go", ToLabel: "Function", ToKey: "internal/domain/user.go#FindUser", Type: "DEFINES", Repo: "/repo"},
			{FromLabel: "File", FromKey: "internal/domain/order.go", ToLabel: "Function", ToKey: "internal/domain/order.go#FindOrder", Type: "DEFINES", Repo: "/repo"},
			{FromLabel: "Function", FromKey: "internal/domain/order.go#FindOrder", ToLabel: "Function", ToKey: "internal/domain/user.go#FindUser", Type: "CALLS", Repo: "/repo"},
		},
	}

	nodes, rels := graphChangesForPaths(index, []string{"internal/domain/user.go"})

	if hasNode(nodes, "Repository", "/repo") {
		t.Fatal("repository node is written separately and should not be in incremental node batch")
	}
	if !hasNode(nodes, "File", "internal/domain/user.go") || !hasNode(nodes, "Function", "internal/domain/user.go#FindUser") {
		t.Fatalf("expected changed file nodes, got %#v", nodes)
	}
	if hasNode(nodes, "File", "internal/domain/order.go") || hasNode(nodes, "Function", "internal/domain/order.go#FindOrder") {
		t.Fatalf("unchanged file nodes should not be rewritten: %#v", nodes)
	}
	if !hasRel(rels, "Repository", "/repo", "CONTAINS", "File", "internal/domain/user.go") {
		t.Fatalf("expected repo-to-file relationship for changed file: %#v", rels)
	}
	if !hasRel(rels, "Function", "internal/domain/order.go#FindOrder", "CALLS", "Function", "internal/domain/user.go#FindUser") {
		t.Fatalf("expected incoming relationship to changed file node to be recreated: %#v", rels)
	}
	if hasRel(rels, "File", "internal/domain/order.go", "DEFINES", "Function", "internal/domain/order.go#FindOrder") {
		t.Fatalf("unchanged-only relationships should not be rewritten: %#v", rels)
	}
}

func TestNormalizedPathsDedupeAndSlashNormalize(t *testing.T) {
	got := normalizedPaths([]string{`internal\app\service.go`, "internal/app/service.go", "", "  graph/schema.graphql  "})
	want := []string{"internal/app/service.go", "graph/schema.graphql"}
	if len(got) != len(want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	}
}

func hasNode(nodes []domain.GraphNode, label, path string) bool {
	for _, node := range nodes {
		if node.Label == label && node.Path == path {
			return true
		}
	}
	return false
}

func hasRel(rels []domain.GraphRelationship, fromLabel, from, typ, toLabel, to string) bool {
	for _, rel := range rels {
		if rel.FromLabel == fromLabel && rel.FromKey == from && rel.Type == typ && rel.ToLabel == toLabel && rel.ToKey == to {
			return true
		}
	}
	return false
}
