package app

import (
	"strings"
	"testing"
	"time"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestRenderDomainMemoryFiltersRelevantTopic(t *testing.T) {
	memory := domain.DomainMemory{
		GeneratedAt: time.Now(),
		DomainConcepts: []domain.DomainConcept{
			{Name: "GraphQL API", Area: "graphql", Description: "Exposes nested relationship fields.", Evidence: []string{"graphql/relationships.go"}, Confidence: 0.8},
			{Name: "Billing", Area: "billing", Description: "Handles invoices.", Evidence: []string{"billing.go"}, Confidence: 0.8},
		},
		BusinessRules: []domain.BusinessRule{
			{Name: "Nested relationship count semantics", Area: "graphql", Statement: "Nested relationship counts must not become null.", Evidence: []string{"nested_args_batch_test.go"}, Confidence: 0.8},
		},
	}

	got := RenderDomainMemory(memory, "graphql nested count", "graphql", 8)

	if !strings.Contains(got, "GraphQL API") {
		t.Fatalf("expected GraphQL memory, got %s", got)
	}
	if !strings.Contains(got, "Nested relationship count semantics") {
		t.Fatalf("expected nested count rule, got %s", got)
	}
	if strings.Contains(got, "Billing") {
		t.Fatalf("unexpected unrelated memory, got %s", got)
	}
}

func TestRenderDomainMemoryKeepsLegacyAreaBlank(t *testing.T) {
	memory := domain.DomainMemory{
		GeneratedAt: time.Now(),
		BusinessRules: []domain.BusinessRule{
			{Name: "Nested relationship count semantics", Statement: "Nested relationship counts must not become null.", Evidence: []string{"nested_args_batch_test.go"}, Confidence: 0.8},
		},
	}

	got := RenderDomainMemory(memory, "graphql nested count", "graphql", 8)
	if !strings.Contains(got, "Nested relationship count semantics") {
		t.Fatalf("expected legacy area-less memory to match filtered area, got %s", got)
	}
}

func TestInferDomainMemoryBootstrapFindsLayersAndREST(t *testing.T) {
	files := []domain.IndexedFile{
		{Path: "internal/domain/project.go", Layer: "domain"},
		{Path: "internal/application/usecase/create_project.go", Layer: "application"},
		{Path: "internal/adapters/http/routes.go", Layer: "adapter_rest"},
		{Path: "internal/adapters/postgres/project_repository.go", Layer: "adapter_persistence"},
	}
	caps := DetectRepoCapabilities(t.TempDir(), files)
	memory := inferDomainMemory(domain.Task{ID: "domain-initial", Content: "initial scan"}, caps, files, "all")
	joined := RenderDomainMemory(memory, "", "all", 20)
	for _, want := range []string{"Domain model", "Application use cases", "REST/HTTP adapter", "Persistence adapter", "Domain stays framework-free"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in bootstrap memory, got %s", want, joined)
		}
	}
}
