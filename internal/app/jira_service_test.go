package app

import (
	"os"
	"path/filepath"
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

func TestImportJiraIssueContentCreatesTaskFromMCPFields(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tasks")
	result, err := ImportJiraIssueContent(JiraIssueContentOptions{
		Key:                "AK-1184",
		SourceURL:          "https://archie-dev.atlassian.net/browse/AK-1184",
		Summary:            "REST parity for Stripe Layer",
		Description:        "Expose REST operations matching GraphQL behavior.",
		AcceptanceCriteria: []string{"REST handlers stay thin.", "GraphQL contract remains unchanged."},
		Comments:           []string{"Product confirmed parity scope."},
		IssueType:          "Story",
		Status:             "To Do",
		Priority:           "High",
		Labels:             []string{"stripe", "rest"},
		OutputDir:          out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID != "AK-1184" || result.Offline {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	task := string(data)
	for _, want := range []string{"# AK-1184: REST parity", "REST handlers stay thin.", "GraphQL contract remains unchanged.", "Imported through agent-brain from Jira MCP-provided content"} {
		if !strings.Contains(task, want) {
			t.Fatalf("missing %q in task:\n%s", want, task)
		}
	}
}
