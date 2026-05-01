package app

import (
	"strings"
	"testing"
)

func TestDefaultConfigCreatesProjectIsolation(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	if cfg.ProjectID == "" || cfg.RuntimeNamespace == "" {
		t.Fatalf("expected project identity: %#v", cfg)
	}
	if cfg.Neo4jHTTPPort == 7474 || cfg.Neo4jBoltPort == 7687 {
		t.Fatalf("expected project-scoped non-default ports for new config: %#v", cfg)
	}
	if !strings.Contains(cfg.Neo4jURI, ":") {
		t.Fatalf("expected bolt uri with port: %s", cfg.Neo4jURI)
	}
}

func TestNormalizeConfigPreservesLegacyDefaultPorts(t *testing.T) {
	cfg := NormalizeConfig(Config{RepoRoot: ".", Neo4jURI: "bolt://localhost:7687"})
	if cfg.Neo4jBoltPort != 7687 || cfg.Neo4jHTTPPort != 7474 {
		t.Fatalf("expected legacy ports preserved: %#v", cfg)
	}
}

func TestRuntimeSpecFromConfigIsNamespaced(t *testing.T) {
	cfg := NormalizeConfig(Config{ProjectID: "demo-123", RuntimeNamespace: "demo-123", Neo4jHTTPPort: 17777, Neo4jBoltPort: 18888, Neo4jUser: "neo4j", Neo4jPassword: "agentbrain"})
	spec := RuntimeSpecFromConfig(cfg, ".agent-brain/runtime/docker-compose.yml")
	if spec.ContainerName != "agent-brain-neo4j-demo-123" || spec.VolumeName != "agent-brain-neo4j-data-demo-123" {
		t.Fatalf("unexpected runtime spec: %#v", spec)
	}
}
