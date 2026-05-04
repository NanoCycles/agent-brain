package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type GitHubPRCommentsOptions struct {
	RepoRoot  string
	PR        string
	OwnerRepo string
	OutputDir string
	Token     string
}

type GitHubPRCommentsResult struct {
	Path         string
	PR           string
	OwnerRepo    string
	CommentCount int
	Source       string
}

type githubComment struct {
	Author    string
	Body      string
	Path      string
	Line      int
	URL       string
	CreatedAt string
}

func ImportGitHubPRComments(ctx context.Context, opts GitHubPRCommentsOptions) (GitHubPRCommentsResult, error) {
	pr := normalizePRNumber(opts.PR)
	if pr == "" {
		return GitHubPRCommentsResult{}, fmt.Errorf("pr is required")
	}
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	ownerRepo := opts.OwnerRepo
	if ownerRepo == "" {
		ownerRepo = detectGitHubOwnerRepo(ctx, opts.RepoRoot)
	}
	if ownerRepo == "" {
		return GitHubPRCommentsResult{}, fmt.Errorf("github owner/repo is required; pass --repo owner/name or configure git remote")
	}
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(opts.RepoRoot, ".ai", "reviews")
	}
	token := opts.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}

	var comments []githubComment
	source := "github_api"
	var err error
	if token != "" {
		comments, err = fetchGitHubCommentsHTTP(ctx, ownerRepo, pr, token)
	} else {
		source = "gh_cli"
		comments, err = fetchGitHubCommentsGH(ctx, ownerRepo, pr)
	}
	if err != nil {
		return GitHubPRCommentsResult{}, err
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return GitHubPRCommentsResult{}, err
	}
	path := filepath.Join(opts.OutputDir, "PR-"+pr+"-comments.md")
	if err := os.WriteFile(path, []byte(renderGitHubCommentsMarkdown(ownerRepo, pr, comments, source)), 0o644); err != nil {
		return GitHubPRCommentsResult{}, err
	}
	return GitHubPRCommentsResult{Path: path, PR: pr, OwnerRepo: ownerRepo, CommentCount: len(comments), Source: source}, nil
}

func normalizePRNumber(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return raw
	}
	re := regexp.MustCompile(`/pull/(\d+)`)
	if match := re.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	return ""
}

func detectGitHubOwnerRepo(ctx context.Context, repoRoot string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return parseGitHubOwnerRepo(strings.TrimSpace(string(out)))
}

func parseGitHubOwnerRepo(remote string) string {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@github.com:") {
		return strings.TrimPrefix(remote, "git@github.com:")
	}
	u, err := url.Parse(remote)
	if err == nil && strings.EqualFold(u.Host, "github.com") {
		return strings.TrimPrefix(u.Path, "/")
	}
	if strings.Contains(remote, "github.com/") {
		parts := strings.SplitN(remote, "github.com/", 2)
		return strings.TrimPrefix(parts[1], "/")
	}
	return ""
}

func fetchGitHubCommentsHTTP(ctx context.Context, ownerRepo, pr, token string) ([]githubComment, error) {
	var comments []githubComment
	for _, endpoint := range []string{
		fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/comments", ownerRepo, pr),
		fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments", ownerRepo, pr),
	} {
		part, err := fetchGitHubCommentEndpointHTTP(ctx, endpoint, token)
		if err != nil {
			return nil, err
		}
		comments = append(comments, part...)
	}
	return comments, nil
}

func fetchGitHubCommentEndpointHTTP(ctx context.Context, endpoint, token string) ([]githubComment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github comments import failed: %s", resp.Status)
	}
	var rows []struct {
		Body      string `json:"body"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	comments := make([]githubComment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, githubComment{Author: row.User.Login, Body: row.Body, Path: row.Path, Line: row.Line, URL: row.HTMLURL, CreatedAt: row.CreatedAt})
	}
	return comments, nil
}

func fetchGitHubCommentsGH(ctx context.Context, ownerRepo, pr string) ([]githubComment, error) {
	var comments []githubComment
	for _, endpoint := range []string{
		fmt.Sprintf("repos/%s/pulls/%s/comments", ownerRepo, pr),
		fmt.Sprintf("repos/%s/issues/%s/comments", ownerRepo, pr),
	} {
		cmd := exec.CommandContext(ctx, "gh", "api", endpoint)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("github comments import requires GITHUB_TOKEN/GH_TOKEN or authenticated gh CLI: %w", err)
		}
		part, err := decodeGitHubComments(out)
		if err != nil {
			return nil, err
		}
		comments = append(comments, part...)
	}
	return comments, nil
}

func decodeGitHubComments(data []byte) ([]githubComment, error) {
	var rows []struct {
		Body      string `json:"body"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&rows); err != nil {
		return nil, err
	}
	comments := make([]githubComment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, githubComment{Author: row.User.Login, Body: row.Body, Path: row.Path, Line: row.Line, URL: row.HTMLURL, CreatedAt: row.CreatedAt})
	}
	return comments, nil
}

func renderGitHubCommentsMarkdown(ownerRepo, pr string, comments []githubComment, source string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# GitHub PR Review Comments\n\n")
	fmt.Fprintf(&b, "- Repository: %s\n", ownerRepo)
	fmt.Fprintf(&b, "- PR: #%s\n", pr)
	fmt.Fprintf(&b, "- Imported at: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- Source: %s\n\n", source)
	if len(comments) == 0 {
		b.WriteString("No review comments found.\n")
		return b.String()
	}
	for i, comment := range comments {
		fmt.Fprintf(&b, "## Comment %d\n\n", i+1)
		if comment.Path != "" {
			fmt.Fprintf(&b, "- File: `%s`", comment.Path)
			if comment.Line > 0 {
				fmt.Fprintf(&b, ":%d", comment.Line)
			}
			b.WriteString("\n")
		}
		if comment.Author != "" {
			fmt.Fprintf(&b, "- Author: %s\n", comment.Author)
		}
		if comment.URL != "" {
			fmt.Fprintf(&b, "- URL: %s\n", comment.URL)
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(comment.Body))
		b.WriteString("\n\n")
	}
	return b.String()
}
