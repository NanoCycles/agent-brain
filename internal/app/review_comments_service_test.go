package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NanoCycles/agent-brain/internal/domain"
)

func TestReviewCommentsPrioritizesCriticalGraphQLRisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.md")
	err := os.WriteFile(path, []byte(`
- N+1 queries in internal/infrastructure/adapters/secondary/graphql/resolvers.go must use DataLoader.
- Resolver-level authorization bypass can break tenant isolation.
- nit: rename local variable.
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	report, summary, err := NewReviewService().ReviewComments(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision != domain.Rejected {
		t.Fatalf("expected rejected for auth/tenant critical risk, got %s", report.Decision)
	}
	if len(report.Findings) < 2 {
		t.Fatalf("expected findings, got %#v", report.Findings)
	}
	if !strings.Contains(summary, "DataLoader") {
		t.Fatalf("expected batching guidance, got %s", summary)
	}
	if !strings.Contains(summary, "tenant/project isolation") {
		t.Fatalf("expected isolation action, got %s", summary)
	}
}
