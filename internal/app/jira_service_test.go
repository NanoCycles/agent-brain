package app

import (
	"strings"
	"testing"
)

func TestParseJiraKeyAndBaseFromURL(t *testing.T) {
	key, base := parseJiraKeyAndBase("https://archie-dev.atlassian.net/browse/AK-1183")
	if key != "AK-1183" {
		t.Fatalf("unexpected key %q", key)
	}
	if base != "https://archie-dev.atlassian.net" {
		t.Fatalf("unexpected base %q", base)
	}
}

func TestRenderJiraSkeletonIsAgentTask(t *testing.T) {
	got := renderJiraSkeleton("AK-123", "https://example.atlassian.net")
	for _, want := range []string{"# AK-123", "## Context", "## Problem", "## Acceptance Criteria"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}
