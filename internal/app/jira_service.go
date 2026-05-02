package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type JiraImportOptions struct {
	KeyOrURL  string
	BaseURL   string
	Email     string
	Token     string
	OutputDir string
	Offline   bool
}

type JiraImportResult struct {
	TaskID  string
	Path    string
	Offline bool
}

func ImportJiraIssue(ctx context.Context, opts JiraImportOptions) (JiraImportResult, error) {
	key, baseURL := parseJiraKeyAndBase(opts.KeyOrURL)
	if key == "" {
		return JiraImportResult{}, fmt.Errorf("jira issue key or URL is required")
	}
	if opts.BaseURL != "" {
		baseURL = opts.BaseURL
	}
	if baseURL == "" {
		baseURL = os.Getenv("JIRA_BASE_URL")
	}
	email := firstNonEmpty(opts.Email, os.Getenv("JIRA_EMAIL"))
	token := firstNonEmpty(opts.Token, os.Getenv("JIRA_API_TOKEN"))
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(".ai", "tasks")
	}

	var body string
	offline := opts.Offline || baseURL == "" || email == "" || token == ""
	if offline {
		body = renderJiraSkeleton(key, baseURL)
	} else {
		issue, err := fetchJiraIssue(ctx, baseURL, key, email, token)
		if err != nil {
			return JiraImportResult{}, err
		}
		body = renderJiraTask(issue, baseURL)
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return JiraImportResult{}, err
	}
	path := filepath.Join(opts.OutputDir, key+".md")
	if _, err := os.Stat(path); err == nil {
		backup := path + ".bak-agent-brain-" + time.Now().UTC().Format("20060102150405")
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return JiraImportResult{}, readErr
		}
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return JiraImportResult{}, err
		}
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return JiraImportResult{}, err
	}
	return JiraImportResult{TaskID: key, Path: path, Offline: offline}, nil
}

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		IssueType   struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
		Priority struct {
			Name string `json:"name"`
		} `json:"priority"`
		Labels []string `json:"labels"`
	} `json:"fields"`
}

func fetchJiraIssue(ctx context.Context, baseURL, key, email, token string) (jiraIssue, error) {
	u := strings.TrimRight(baseURL, "/") + "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=summary,description,issuetype,status,priority,labels"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return jiraIssue{}, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return jiraIssue{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jiraIssue{}, fmt.Errorf("jira import failed: %s", resp.Status)
	}
	var issue jiraIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return jiraIssue{}, err
	}
	return issue, nil
}

func renderJiraTask(issue jiraIssue, baseURL string) string {
	desc := strings.TrimSpace(adfText(issue.Fields.Description))
	if desc == "" {
		desc = "No Jira description was provided."
	}
	return fmt.Sprintf(`# %s: %s

Source: %s/browse/%s
Type: %s
Status: %s
Priority: %s
Labels: %s

## Context
%s

## Acceptance Criteria
- Confirm expected behavior from Jira and existing business rules.
- Preserve public contracts unless explicit approval exists.
- Add or adjust focused tests for the changed behavior.
`, issue.Key, issue.Fields.Summary, strings.TrimRight(baseURL, "/"), issue.Key, issue.Fields.IssueType.Name, issue.Fields.Status.Name, issue.Fields.Priority.Name, strings.Join(issue.Fields.Labels, ", "), desc)
}

func renderJiraSkeleton(key, baseURL string) string {
	source := key
	if baseURL != "" {
		source = strings.TrimRight(baseURL, "/") + "/browse/" + key
	}
	return fmt.Sprintf(`# %s

Source: %s

## Context
Jira credentials were not available locally, so agent-brain created this task shell.

## Problem
Paste or import the Jira summary and description here.

## Acceptance Criteria
- Preserve public contracts unless explicit approval exists.
- Add or adjust focused tests for the changed behavior.
`, key, source)
}

func adfText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	var out []string
	collectADFText(v, &out)
	return strings.Join(out, " ")
}

func collectADFText(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		if text, ok := x["text"].(string); ok && text != "" {
			*out = append(*out, text)
		}
		if content, ok := x["content"].([]any); ok {
			for _, item := range content {
				collectADFText(item, out)
			}
		}
	case []any:
		for _, item := range x {
			collectADFText(item, out)
		}
	}
}

func parseJiraKeyAndBase(input string) (string, string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}
	if u, err := url.Parse(input); err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, part := range parts {
			if strings.EqualFold(part, "browse") && i+1 < len(parts) {
				return strings.ToUpper(parts[i+1]), u.Scheme + "://" + u.Host
			}
		}
		return strings.ToUpper(parts[len(parts)-1]), u.Scheme + "://" + u.Host
	}
	return strings.ToUpper(input), ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
