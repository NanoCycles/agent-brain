package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	dockerruntime "github.com/NanoCycles/agent-brain/internal/adapters/docker"
	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/adapters/golang"
	neo "github.com/NanoCycles/agent-brain/internal/adapters/neo4j"
	sqlstore "github.com/NanoCycles/agent-brain/internal/adapters/sqlite"
	"github.com/NanoCycles/agent-brain/internal/app"
	"github.com/NanoCycles/agent-brain/internal/domain"
	"github.com/NanoCycles/agent-brain/internal/platform/paths"
)

type Server struct {
	in         io.Reader
	out        io.Writer
	operations *operationStore
}

const serverVersion = "0.1.18"

func NewServer(in io.Reader, out io.Writer) *Server {
	return &Server{in: in, out: out, operations: newOperationStore()}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type operation struct {
	ID          string           `json:"operation_id"`
	Name        string           `json:"name"`
	RepoRoot    string           `json:"repo_root,omitempty"`
	Status      string           `json:"status"`
	Progress    int              `json:"progress"`
	Stage       string           `json:"stage"`
	Events      []operationEvent `json:"events,omitempty"`
	Result      string           `json:"result,omitempty"`
	Error       string           `json:"error,omitempty"`
	StartedAt   time.Time        `json:"started_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	CompletedAt time.Time        `json:"completed_at,omitempty"`
	cancel      context.CancelFunc
}

type operationEvent struct {
	At       time.Time `json:"at"`
	Progress int       `json:"progress"`
	Stage    string    `json:"stage"`
}

type operationStore struct {
	mu  sync.Mutex
	seq int64
	ops map[string]*operation
	max int
	ttl time.Duration
}

func newOperationStore() *operationStore {
	return &operationStore{ops: map[string]*operation{}, max: 64, ttl: 2 * time.Hour}
}

func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 8*1024*1024)
	enc := json.NewEncoder(s.out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(errorResponse(nil, -32700, "parse error"))
			continue
		}
		resp, ok := s.handle(ctx, req)
		if !ok {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req request) (response, bool) {
	switch req.Method {
	case "initialize":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": requestedProtocol(req.Params),
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": "agent-brain", "version": serverVersion},
		}}, true
	case "notifications/initialized":
		return response{}, false
	case "ping":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}, true
	case "tools/list":
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefinitions()}}, true
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, -32602, "invalid tool call params"), true
		}
		text, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			return response{JSONRPC: "2.0", ID: req.ID, Result: toolError(err.Error())}, true
		}
		return response{JSONRPC: "2.0", ID: req.ID, Result: toolText(text)}, true
	default:
		if req.ID == nil {
			return response{}, false
		}
		return errorResponse(req.ID, -32601, "method not found"), true
	}
}

func requestedProtocol(raw json.RawMessage) string {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(raw, &params)
	if params.ProtocolVersion == "" {
		return "2025-06-18"
	}
	return params.ProtocolVersion
}

func errorResponse(id any, code int, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func toolText(text string) map[string]any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}
}

func toolError(text string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": text}}}
}

func toolDefinitions() []map[string]any {
	repoRoot := map[string]any{"type": "string", "description": "Optional repository root when the MCP host launches agent-brain outside the project directory."}
	return []map[string]any{
		tool("start_task", "Agent-first workflow: prepare context, include system memory, and return next actions before editing code.", map[string]any{
			"task_path": map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":     map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"fast":      map[string]any{"type": "boolean"},
			"sync":      map[string]any{"type": "boolean", "description": "Run synchronously. Default false because IDE MCP hosts often have short deadlines."},
			"repo_root": repoRoot,
		}),
		tool("finish_task", "Agent-first workflow: review current diff and generate implementation/domain memory proposals for human approval.", map[string]any{
			"task_path": map[string]any{"type": "string"},
			"repo_root": repoRoot,
		}),
		tool("prepare_context", "Initialize local runtime if needed, index the current repo unless skipped, generate an agent context pack, and return a compact handoff. Safe: does not modify source code.", map[string]any{
			"task_path": map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":     map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"budget":    map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
			"fast":      map[string]any{"type": "boolean", "description": "Skip reindex if the last index is recent"},
			"no_index":  map[string]any{"type": "boolean", "description": "Generate context from existing metadata without indexing"},
			"sync":      map[string]any{"type": "boolean", "description": "Run synchronously. Default false because IDE MCP hosts often have short deadlines."},
			"repo_root": repoRoot,
		}),
		tool("start_task_async", "Start agent-first context preparation in the background and return an operation_id immediately. Use operation_status to poll progress/result.", map[string]any{
			"task_path": map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":     map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"fast":      map[string]any{"type": "boolean"},
			"repo_root": repoRoot,
		}),
		tool("prepare_context_async", "Start context pack generation in the background and return an operation_id immediately. Use operation_status to poll progress/result.", map[string]any{
			"task_path": map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":     map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"budget":    map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
			"fast":      map[string]any{"type": "boolean", "description": "Skip reindex if the last index is recent"},
			"no_index":  map[string]any{"type": "boolean", "description": "Generate context from existing metadata without indexing"},
			"repo_root": repoRoot,
		}),
		tool("agent_start", "Run the complete agent startup workflow: init/up/index/context and domain-memory proposal when needed.", map[string]any{
			"task_path":        map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":            map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"budget":           map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
			"fast":             map[string]any{"type": "boolean"},
			"no_index":         map[string]any{"type": "boolean"},
			"bootstrap_memory": map[string]any{"type": "boolean", "description": "Generate a domain-memory proposal"},
			"memory_area":      map[string]any{"type": "string", "description": "Memory area: all, graphql, auth, billing, events, persistence, application, domain"},
			"sync":             map[string]any{"type": "boolean", "description": "Run synchronously. Default false because IDE MCP hosts often have short deadlines."},
			"repo_root":        repoRoot,
		}),
		tool("agent_start_async", "Run the complete agent startup workflow in the background. Use operation_status to poll progress/result.", map[string]any{
			"task_path":        map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":            map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"budget":           map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
			"fast":             map[string]any{"type": "boolean"},
			"no_index":         map[string]any{"type": "boolean"},
			"bootstrap_memory": map[string]any{"type": "boolean"},
			"memory_area":      map[string]any{"type": "string"},
			"repo_root":        repoRoot,
		}),
		tool("finish_task_async", "Run final diff review and memory proposal generation in the background. Use operation_status to poll progress/result.", map[string]any{
			"task_path": map[string]any{"type": "string"},
			"repo_root": repoRoot,
		}),
		tool("review_diff_async", "Run review_diff in the background. Use operation_status to poll progress/result.", map[string]any{"repo_root": repoRoot}),
		tool("operation_status", "Return status, progress, result, or error for a background MCP operation.", map[string]any{
			"operation_id": map[string]any{"type": "string"},
		}),
		tool("operation_cancel", "Cancel a running background MCP operation.", map[string]any{
			"operation_id": map[string]any{"type": "string"},
		}),
		tool("operation_list", "List recent background MCP operations for this server process.", map[string]any{
			"repo_root": repoRoot,
		}),
		tool("clean_context", "Remove generated .agent.md/.agent.json context packs for this project. Does not touch source, memory, SQLite, or Neo4j.", map[string]any{
			"confirmed": map[string]any{"type": "boolean", "description": "Required true confirmation"},
			"repo_root": repoRoot,
		}),
		tool("get_context_pack", "Read an existing generated .agent.md context pack for a task or explicit context path.", map[string]any{
			"task_path":    map[string]any{"type": "string"},
			"context_path": map[string]any{"type": "string"},
		}),
		tool("impact", "Analyze likely impact for a topic using SQLite metadata and Neo4j graph expansion.", map[string]any{
			"topic":  map[string]any{"type": "string"},
			"budget": map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
		}),
		tool("review_diff", "Review current git diff for risks, contracts, tests, forbidden files, and rule violations. Read-only.", map[string]any{"repo_root": repoRoot}),
		tool("review_comments", "Turn external code review comments into a prioritized agent repair plan. Read-only.", map[string]any{
			"comments_path": map[string]any{"type": "string", "description": "Markdown/text file with review comments"},
		}),
		tool("github_pr_comments", "Import GitHub PR review comments into .ai/reviews and return an agent repair plan. Requires GITHUB_TOKEN/GH_TOKEN or authenticated gh CLI.", map[string]any{
			"pr":           map[string]any{"type": "string", "description": "PR number or GitHub pull request URL"},
			"repo":         map[string]any{"type": "string", "description": "Optional owner/repo; defaults to git origin"},
			"token":        map[string]any{"type": "string", "description": "Optional GitHub token; defaults to GITHUB_TOKEN/GH_TOKEN"},
			"review":       map[string]any{"type": "boolean", "description": "Run review_comments after import"},
			"post_summary": map[string]any{"type": "boolean", "description": "Post an agent-brain import summary comment back to the PR"},
			"repo_root":    repoRoot,
		}),
		tool("github_pr_comments_async", "Import GitHub PR review comments in the background. Use operation_status to poll progress/result.", map[string]any{
			"pr":           map[string]any{"type": "string", "description": "PR number or GitHub pull request URL"},
			"repo":         map[string]any{"type": "string", "description": "Optional owner/repo; defaults to git origin"},
			"token":        map[string]any{"type": "string", "description": "Optional GitHub token; defaults to GITHUB_TOKEN/GH_TOKEN"},
			"review":       map[string]any{"type": "boolean", "description": "Run review_comments after import"},
			"post_summary": map[string]any{"type": "boolean", "description": "Post an agent-brain import summary comment back to the PR"},
			"repo_root":    repoRoot,
		}),
		tool("jira_import_from_mcp", "Create .ai/tasks/<KEY>.md from Jira fields already read by an Atlassian/Jira MCP tool. Use when CLI Jira credentials are unavailable.", map[string]any{
			"key":                 map[string]any{"type": "string", "description": "Jira issue key, e.g. AK-1184"},
			"source_url":          map[string]any{"type": "string", "description": "Jira browse URL"},
			"summary":             map[string]any{"type": "string"},
			"description":         map[string]any{"type": "string"},
			"acceptance_criteria": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"comments":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"issue_type":          map[string]any{"type": "string"},
			"status":              map[string]any{"type": "string"},
			"priority":            map[string]any{"type": "string"},
			"labels":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}),
		tool("status", "Return project runtime, graph, SQLite, and capability status.", map[string]any{"repo_root": repoRoot}),
		tool("doctor", "Diagnose local prerequisites and project wiring for agent-brain.", map[string]any{"repo_root": repoRoot}),
		tool("mcp_health", "Return MCP-focused health diagnostics including cwd, repo root binding, runtime, and recommended recovery actions.", map[string]any{"repo_root": repoRoot}),
		tool("memory_proposal", "Generate a structured memory proposal after a task is implemented. Does not apply memory automatically.", map[string]any{
			"task_path": map[string]any{"type": "string"},
		}),
		tool("propose_domain_memory", "Generate a proposed system/domain memory update with evidence. Does not apply automatically.", map[string]any{
			"task_path": map[string]any{"type": "string"},
			"area":      map[string]any{"type": "string", "description": "Optional area: graphql, auth, billing, events, persistence, or all"},
		}),
		tool("bootstrap_domain_memory", "Create an initial system/domain memory proposal from indexed code. Does not apply automatically.", map[string]any{
			"area":      map[string]any{"type": "string", "description": "Optional area: all, graphql, auth, billing, events, persistence, application, domain"},
			"repo_root": repoRoot,
		}),
		tool("apply_domain_memory", "Apply approved system/domain memory to local file, SQLite, and Neo4j. Call only after human approval.", map[string]any{
			"proposal_path": map[string]any{"type": "string"},
			"confirmed":     map[string]any{"type": "boolean"},
		}),
		tool("get_system_memory", "Return compact approved system/domain memory relevant to a topic.", map[string]any{
			"topic": map[string]any{"type": "string"},
			"area":  map[string]any{"type": "string", "description": "Optional area filter"},
		}),
		tool("handoff", "Return the short prompt an agent should follow for a generated context pack.", map[string]any{
			"task_path":    map[string]any{"type": "string"},
			"context_path": map[string]any{"type": "string"},
		}),
	}
}

func tool(name, description string, props map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": props,
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	p, err := discoverProjectPaths(args)
	if err != nil {
		return "", err
	}
	switch name {
	case "start_task":
		if !boolArg(args, "sync") {
			return s.startAsync(ctx, "start_task", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
				args["progress"] = progress
				return startTask(ctx, p, args)
			}), nil
		}
		return startTask(ctx, p, args)
	case "start_task_async":
		return s.startAsync(ctx, "start_task", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			args["progress"] = progress
			return startTask(ctx, p, args)
		}), nil
	case "finish_task":
		return finishTask(ctx, p, args)
	case "finish_task_async":
		return s.startAsync(ctx, "finish_task", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			progress(20, "reviewing git diff")
			return finishTask(ctx, p, args)
		}), nil
	case "prepare_context":
		if !boolArg(args, "sync") {
			return s.startAsync(ctx, "prepare_context", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
				args["progress"] = progress
				return prepareContext(ctx, p, args)
			}), nil
		}
		return prepareContext(ctx, p, args)
	case "prepare_context_async":
		return s.startAsync(ctx, "prepare_context", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			args["progress"] = progress
			return prepareContext(ctx, p, args)
		}), nil
	case "agent_start":
		if !boolArg(args, "sync") {
			return s.startAsync(ctx, "agent_start", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
				args["progress"] = progress
				return agentStart(ctx, p, args)
			}), nil
		}
		return agentStart(ctx, p, args)
	case "agent_start_async":
		return s.startAsync(ctx, "agent_start", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			args["progress"] = progress
			return agentStart(ctx, p, args)
		}), nil
	case "get_context_pack":
		return getContextPack(p, args)
	case "impact":
		return impact(ctx, p, stringArg(args, "topic"), stringArg(args, "budget"))
	case "review_diff":
		reviewCtx, cancel := reviewContext(ctx)
		defer cancel()
		report, summary, err := app.NewReviewService().ReviewDiff(reviewCtx, p.Root, p.RulesDir)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s\nDecision: %s", summary, report.Decision), nil
	case "review_diff_async":
		return s.startAsync(ctx, "review_diff", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			progress(25, "reading git diff")
			report, summary, err := app.NewReviewService().ReviewDiff(ctx, p.Root, p.RulesDir)
			if err != nil {
				return "", err
			}
			progress(85, "rendering review findings")
			return fmt.Sprintf("%s\nDecision: %s", summary, report.Decision), nil
		}), nil
	case "operation_status":
		return s.operationStatus(stringArg(args, "operation_id"))
	case "operation_cancel":
		return s.operationCancel(stringArg(args, "operation_id"))
	case "operation_list":
		return s.operationList(p.Root)
	case "clean_context":
		result, err := app.CleanGeneratedContext(p, boolArg(args, "confirmed"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Removed generated context packs: %d\nSkipped unsafe paths: %d\nRepo root: %s", len(result.Removed), len(result.Skipped), p.Root), nil
	case "review_comments":
		path := stringArg(args, "comments_path")
		if path == "" {
			return "", fmt.Errorf("comments_path is required")
		}
		_, summary, err := app.NewReviewService().ReviewComments(path)
		return summary, err
	case "github_pr_comments":
		return githubPRComments(ctx, p, args)
	case "github_pr_comments_async":
		return s.startAsync(ctx, "github_pr_comments", p.Root, args, func(ctx context.Context, progress app.ProgressFunc) (string, error) {
			progress(20, "fetching GitHub PR review comments")
			text, err := githubPRComments(ctx, p, args)
			if err != nil {
				return "", err
			}
			progress(85, "review comments imported")
			return text, nil
		}), nil
	case "jira_import_from_mcp":
		return jiraImportFromMCP(p, args)
	case "status":
		return status(ctx, p)
	case "doctor":
		return doctor(ctx, p)
	case "mcp_health":
		return mcpHealth(ctx, p)
	case "memory_proposal":
		taskPath := stringArg(args, "task_path")
		if taskPath == "" {
			return "", fmt.Errorf("task_path is required")
		}
		store, err := sqlstore.New(p.SQLitePath)
		if err != nil {
			return "", err
		}
		defer store.Close()
		_ = store.Init(ctx)
		path, err := app.NewMemoryService(store).GenerateProposal(ctx, p.Root, taskPath, p.AIMemoryProposals)
		if err != nil {
			return "", err
		}
		return "Memory proposal generated: " + path, nil
	case "propose_domain_memory":
		return proposeDomainMemory(ctx, p, stringArg(args, "task_path"), stringArg(args, "area"))
	case "bootstrap_domain_memory":
		return proposeDomainMemory(ctx, p, "", stringArg(args, "area"))
	case "apply_domain_memory":
		return applyDomainMemory(ctx, p, stringArg(args, "proposal_path"), boolArg(args, "confirmed"))
	case "get_system_memory":
		return getSystemMemory(ctx, p, stringArg(args, "topic"), stringArg(args, "area"))
	case "handoff":
		contextPath := contextPathFromArgs(p, args)
		if contextPath == "" {
			return "", fmt.Errorf("task_path or context_path is required")
		}
		return app.HandoffPrompt(contextPath), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func discoverProjectPaths(args map[string]any) (paths.ProjectPaths, error) {
	root := strings.TrimSpace(stringArg(args, "repo_root"))
	if root == "" {
		root = strings.TrimSpace(stringArg(args, "cwd"))
	}
	if root == "" {
		if cwd, err := os.Getwd(); err == nil && looksLikeProjectRoot(cwd) {
			root = cwd
		}
	}
	if root == "" {
		root = strings.TrimSpace(os.Getenv("AGENT_BRAIN_REPO_ROOT"))
	}
	if root == "" {
		root = "."
	}
	p, err := paths.Discover(root)
	if err != nil {
		return paths.ProjectPaths{}, err
	}
	if _, err := os.Stat(p.Root); err != nil {
		return paths.ProjectPaths{}, fmt.Errorf("repository root is not accessible: %s: %w", p.Root, err)
	}
	return p, nil
}

func looksLikeProjectRoot(root string) bool {
	for _, marker := range []string{paths.DirName, ".git", "go.mod", "package.json"} {
		if _, err := os.Stat(filepath.Join(root, marker)); err == nil {
			return true
		}
	}
	return false
}

func reviewContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(parent)
	return context.WithTimeout(base, 2*time.Minute)
}

func (s *Server) startAsync(parent context.Context, name, repoRoot string, args map[string]any, run func(context.Context, app.ProgressFunc) (string, error)) string {
	op, ctx := s.operations.start(parent, name, repoRoot)
	go func() {
		s.operations.update(op.ID, "running", 10, "started", "", "")
		progress := func(progress int, stage string) {
			s.operations.update(op.ID, "running", progress, stage, "", "")
		}
		result, err := run(ctx, progress)
		if err != nil {
			s.operations.update(op.ID, "failed", 100, "failed", "", asyncErrorMessage(err, repoRoot))
			return
		}
		s.operations.update(op.ID, "completed", 100, "completed", result, "")
	}()
	_ = args
	return fmt.Sprintf("Operation started: %s\nName: %s\nRepo root: %s\nStatus: running\nProgress: 10\nNext: call operation_status with operation_id=%q", op.ID, name, repoRoot, op.ID)
}

func asyncErrorMessage(err error, repoRoot string) string {
	msg := err.Error()
	if strings.Contains(msg, "exit status 129") {
		return msg + "\nHint: git returned usage/error 129. Verify the MCP server is bound to the target repository root and that git is available in the IDE environment. Repo root: " + repoRoot
	}
	if strings.Contains(msg, "context deadline exceeded") {
		return msg + "\nHint: the operation exceeded its MCP-safe timeout. Retry with the async tool and poll operation_status, or pass no_index=true for context preparation after an index exists."
	}
	return msg
}

func (s *Server) operationStatus(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("operation_id is required")
	}
	op, ok := s.operations.get(id)
	if !ok {
		return "", fmt.Errorf("operation not found: %s", id)
	}
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) operationCancel(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("operation_id is required")
	}
	if !s.operations.cancel(id) {
		return "", fmt.Errorf("operation not found or already completed: %s", id)
	}
	return "Operation cancelled: " + id, nil
}

func (s *Server) operationList(repoRoot string) (string, error) {
	ops := s.operations.list(repoRoot)
	data, err := json.MarshalIndent(ops, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *operationStore) start(parent context.Context, name, repoRoot string) (*operation, context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked(time.Now())
	s.seq++
	base := context.WithoutCancel(parent)
	ctx, cancel := context.WithTimeout(base, 30*time.Minute)
	now := time.Now().UTC()
	op := &operation{
		ID:        fmt.Sprintf("op-%d-%d", now.UnixNano(), s.seq),
		Name:      name,
		RepoRoot:  repoRoot,
		Status:    "queued",
		Progress:  0,
		Stage:     "queued",
		StartedAt: now,
		UpdatedAt: now,
		cancel:    cancel,
	}
	s.ops[op.ID] = op
	return cloneOperation(op), ctx
}

func (s *operationStore) update(id, status string, progress int, stage, result, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.ops[id]
	if !ok {
		return
	}
	if op.Status == "completed" || op.Status == "failed" || op.Status == "cancelled" {
		return
	}
	if progress < op.Progress {
		progress = op.Progress
	}
	if progress > 100 {
		progress = 100
	}
	op.Status = status
	op.Progress = progress
	op.Stage = stage
	op.Result = result
	op.Error = errText
	op.UpdatedAt = time.Now().UTC()
	if stage != "" {
		op.Events = append(op.Events, operationEvent{At: op.UpdatedAt, Progress: progress, Stage: stage})
		if len(op.Events) > 40 {
			op.Events = op.Events[len(op.Events)-40:]
		}
	}
	if status == "completed" || status == "failed" || status == "cancelled" {
		op.CompletedAt = op.UpdatedAt
		if op.cancel != nil {
			op.cancel()
			op.cancel = nil
		}
	}
}

func (s *operationStore) get(id string) (operation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.ops[id]
	if !ok {
		return operation{}, false
	}
	return *cloneOperation(op), true
}

func (s *operationStore) list(repoRoot string) []operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops := make([]operation, 0, len(s.ops))
	for _, op := range s.ops {
		if repoRoot != "" && op.RepoRoot != repoRoot {
			continue
		}
		ops = append(ops, *cloneOperation(op))
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].UpdatedAt.After(ops[j].UpdatedAt)
	})
	if len(ops) > 20 {
		ops = ops[:20]
	}
	return ops
}

func (s *operationStore) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.ops[id]
	if !ok || op.Status == "completed" || op.Status == "failed" || op.Status == "cancelled" {
		return false
	}
	if op.cancel != nil {
		op.cancel()
		op.cancel = nil
	}
	now := time.Now().UTC()
	op.Status = "cancelled"
	op.Progress = 100
	op.Stage = "cancelled"
	op.UpdatedAt = now
	op.CompletedAt = now
	return true
}

func (s *operationStore) gcLocked(now time.Time) {
	if len(s.ops) <= s.max {
		return
	}
	for id, op := range s.ops {
		if op.CompletedAt.IsZero() {
			continue
		}
		if now.Sub(op.CompletedAt) > s.ttl {
			delete(s.ops, id)
		}
	}
	for len(s.ops) > s.max {
		var oldestID string
		var oldest time.Time
		for id, op := range s.ops {
			if oldestID == "" || op.UpdatedAt.Before(oldest) {
				oldestID = id
				oldest = op.UpdatedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.ops, oldestID)
	}
}

func cloneOperation(op *operation) *operation {
	if op == nil {
		return nil
	}
	cp := *op
	cp.cancel = nil
	return &cp
}

func startTask(ctx context.Context, p paths.ProjectPaths, args map[string]any) (string, error) {
	args["budget"] = app.BudgetCavernicola
	text, err := prepareContext(ctx, p, args)
	if err != nil {
		return "", err
	}
	memory, _ := getSystemMemory(ctx, p, stringArg(args, "topic"), stringArg(args, "area"))
	return text + "\n\nSystem memory:\n" + memory + "\nNext agent actions:\n- Read the generated context pack.\n- Open only top-ranked files first.\n- Implement the smallest safe change.\n- Run focused tests.\n- Call review_diff before final response.\n- Call finish_task after validation.", nil
}

func finishTask(ctx context.Context, p paths.ProjectPaths, args map[string]any) (string, error) {
	report, summary, err := app.NewReviewService().ReviewDiff(ctx, p.Root, p.RulesDir)
	if err != nil {
		return "", err
	}
	taskPath := stringArg(args, "task_path")
	var memPath, domainPath string
	if taskPath != "" {
		store, err := sqlstore.New(p.SQLitePath)
		if err != nil {
			return "", err
		}
		defer store.Close()
		_ = store.Init(ctx)
		memPath, _ = app.NewMemoryService(store).GenerateProposal(ctx, p.Root, taskPath, p.AIMemoryProposals)
		graph := graphOrNil(p.ConfigPath)
		if graph != nil {
			defer graph.Close(ctx)
		}
		domainPath, _, _ = app.NewDomainMemoryService(store, graph).Propose(ctx, p.Root, taskPath, p.AIMemoryProposals, "")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nDecision: %s\n", summary, report.Decision)
	if memPath != "" {
		fmt.Fprintf(&b, "Implementation memory proposal: %s\n", memPath)
	}
	if domainPath != "" {
		fmt.Fprintf(&b, "Domain memory proposal: %s\nHuman approval required before apply_domain_memory.\n", domainPath)
		fmt.Fprintf(&b, "Human message: I found possible system/domain memory from this change. Do you approve saving it for future agents?\n")
	}
	return b.String(), nil
}

func prepareContext(ctx context.Context, p paths.ProjectPaths, args map[string]any) (string, error) {
	taskPath := stringArg(args, "task_path")
	topic := stringArg(args, "topic")
	initSvc := app.NewInitService(filesystem.LocalFS{})
	if err := initSvc.Init(p.Root); err != nil {
		return "", err
	}
	cfg, _ := app.LoadConfig(p.ConfigPath)
	if cfg.ProjectID == "" {
		cfg = app.DefaultConfig(p.Root)
	}
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	result, err := app.NewPrepareService(
		initSvc,
		dockerruntime.Runtime{},
		golang.Indexer{},
		store,
		graph,
	).Prepare(ctx, p, cfg, app.PrepareOptions{
		TaskPath: taskPath,
		Topic:    topic,
		Fast:     boolArgDefault(args, "fast", true),
		NoIndex:  boolArg(args, "no_index"),
		Budget:   stringArg(args, "budget"),
		Progress: progressArg(args),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Agent context ready: %s\nJSON: %s\nQuality: %s %.2f\nIndexed: %t\nRuntime started: %t\nTop files: %d\n\nSuggested prompt:\n%s",
		result.MarkdownPath, result.JSONPath, result.Pack.ContextQuality.Level, result.Pack.ContextQuality.Score, result.Indexed, result.RuntimeUp, len(result.Pack.LikelyRelevantFiles), result.Handoff), nil
}

func agentStart(ctx context.Context, p paths.ProjectPaths, args map[string]any) (string, error) {
	cfg, _ := app.LoadConfig(p.ConfigPath)
	if cfg.ProjectID == "" {
		cfg = app.DefaultConfig(p.Root)
	}
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	prepare := app.NewPrepareService(app.NewInitService(filesystem.LocalFS{}), dockerruntime.Runtime{}, golang.Indexer{}, store, graph)
	result, err := app.NewAgentWorkflowService(prepare, store, graph).Start(ctx, p, cfg, app.AgentWorkflowOptions{
		TaskPath:        stringArg(args, "task_path"),
		Topic:           stringArg(args, "topic"),
		Budget:          stringArg(args, "budget"),
		Fast:            boolArgDefault(args, "fast", true),
		NoIndex:         boolArg(args, "no_index"),
		BootstrapMemory: boolArgDefault(args, "bootstrap_memory", true),
		MemoryArea:      stringArgDefault(args, "memory_area", "all"),
		Progress:        progressArg(args),
	})
	if err != nil {
		return "", err
	}
	return app.RenderAgentWorkflowResult(result), nil
}

func proposeDomainMemory(ctx context.Context, p paths.ProjectPaths, taskPath, area string) (string, error) {
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	_ = store.Init(ctx)
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	path, memory, err := app.NewDomainMemoryService(store, graph).Propose(ctx, p.Root, taskPath, p.AIMemoryProposals, area)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Domain memory proposal: %s\nConcepts: %d\nComponents: %d\nRules: %d\nInvariants: %d\nHuman approval required before apply_domain_memory.", path, len(memory.DomainConcepts), len(memory.SystemComponents), len(memory.BusinessRules), len(memory.Invariants)), nil
}

func applyDomainMemory(ctx context.Context, p paths.ProjectPaths, proposalPath string, confirmed bool) (string, error) {
	if proposalPath == "" {
		return "", fmt.Errorf("proposal_path is required")
	}
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	_ = store.Init(ctx)
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	memory, err := app.NewDomainMemoryService(store, graph).Apply(ctx, p.Root, proposalPath, domainMemoryPath(p), confirmed)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Domain memory applied: %s\nConcepts: %d\nComponents: %d\nRules: %d\nInvariants: %d", domainMemoryPath(p), len(memory.DomainConcepts), len(memory.SystemComponents), len(memory.BusinessRules), len(memory.Invariants)), nil
}

func githubPRComments(ctx context.Context, p paths.ProjectPaths, args map[string]any) (string, error) {
	pr := stringArg(args, "pr")
	if pr == "" {
		return "", fmt.Errorf("pr is required")
	}
	result, err := app.ImportGitHubPRComments(ctx, app.GitHubPRCommentsOptions{
		RepoRoot:    p.Root,
		PR:          pr,
		OwnerRepo:   stringArg(args, "repo"),
		OutputDir:   filepath.Join(p.Root, ".ai", "reviews"),
		Token:       stringArg(args, "token"),
		PostSummary: boolArg(args, "post_summary"),
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GitHub PR comments imported: %s\nRepository: %s\nPR: #%s\nComments: %d\nSource: %s\n", result.Path, result.OwnerRepo, result.PR, result.CommentCount, result.Source)
	if result.PostedURL != "" {
		fmt.Fprintf(&b, "Posted summary: %s\n", result.PostedURL)
	}
	if boolArgDefault(args, "review", true) {
		_, summary, err := app.NewReviewService().ReviewComments(result.Path)
		if err != nil {
			return "", err
		}
		b.WriteString("\n")
		b.WriteString(summary)
	}
	return b.String(), nil
}

func jiraImportFromMCP(p paths.ProjectPaths, args map[string]any) (string, error) {
	key := stringArg(args, "key")
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	result, err := app.ImportJiraIssueContent(app.JiraIssueContentOptions{
		Key:                key,
		SourceURL:          stringArg(args, "source_url"),
		Summary:            stringArg(args, "summary"),
		Description:        stringArg(args, "description"),
		AcceptanceCriteria: stringSliceArg(args, "acceptance_criteria"),
		Comments:           stringSliceArg(args, "comments"),
		IssueType:          stringArg(args, "issue_type"),
		Status:             stringArg(args, "status"),
		Priority:           stringArg(args, "priority"),
		Labels:             stringSliceArg(args, "labels"),
		OutputDir:          filepath.Join(p.Root, ".ai", "tasks"),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Jira task imported from MCP content: %s\nTask ID: %s\nNext: call prepare_context_async with task_path=%q and budget=%q.", result.Path, result.TaskID, result.Path, app.BudgetCavernicola), nil
}

func getSystemMemory(ctx context.Context, p paths.ProjectPaths, topic, area string) (string, error) {
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	_ = store.Init(ctx)
	memory, err := app.NewDomainMemoryService(store, nil).Load(ctx, p.Root, domainMemoryPath(p))
	if err != nil {
		return "", err
	}
	return app.RenderDomainMemory(memory, topic, area, 8), nil
}

func getContextPack(p paths.ProjectPaths, args map[string]any) (string, error) {
	contextPath := contextPathFromArgs(p, args)
	if contextPath == "" {
		return "", fmt.Errorf("task_path or context_path is required")
	}
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func impact(ctx context.Context, p paths.ProjectPaths, topic, budget string) (string, error) {
	if topic == "" {
		return "", fmt.Errorf("topic is required")
	}
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	_ = store.Init(ctx)
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	pack, err := app.NewContextServiceWithBudget(store, graph, budget).GenerateForText(ctx, p.Root, "impact", topic, p.RulesDir)
	if err != nil {
		return "", err
	}
	return renderImpact(pack, topic), nil
}

func renderImpact(pack domain.ContextPack, topic string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Impact for %q\n", topic)
	fmt.Fprintf(&b, "Context quality: %s %.2f\n", pack.ContextQuality.Level, pack.ContextQuality.Score)
	fmt.Fprintf(&b, "Main capability: %s\n", pack.TaskAnalysis.MainCapability)
	b.WriteString("Likely files:\n")
	if len(pack.LikelyRelevantFiles) == 0 {
		b.WriteString("- No strong relevant files detected.\n")
	}
	for _, c := range pack.LikelyRelevantFiles {
		fmt.Fprintf(&b, "- %s [%s %.2f] %s\n", c.Path, c.Category, c.Confidence, c.Reason)
	}
	b.WriteString("Risks:\n")
	for _, r := range append(append(pack.Risks.Security, pack.Risks.Concurrency...), pack.Risks.MemoryPerformance...) {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	b.WriteString("Suggested tests:\n")
	for _, t := range pack.SuggestedTests {
		fmt.Fprintf(&b, "- %s\n", t)
	}
	return b.String()
}

func status(ctx context.Context, p paths.ProjectPaths) (string, error) {
	cfg, err := app.LoadConfig(p.ConfigPath)
	if err != nil {
		cfg = app.DefaultConfig(p.Root)
	}
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
	report := app.NewRuntimeService(dockerruntime.Runtime{}, graph).Status(ctx, spec)
	store, err := sqlstore.New(p.SQLitePath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	_ = store.Init(ctx)
	files, _ := store.IndexedFiles(ctx, p.Root)
	caps := app.DetectRepoCapabilities(p.Root, files)
	return fmt.Sprintf("Project ID: %s\nNeo4j: %s\nSQLite: %s\nGraph nodes: %d\nGraph relationships: %d\nCapabilities: GraphQL=%t REST=%t gRPC=%t Events=%t Persistence=%t Tests=%t",
		cfg.ProjectID, yesNo(report.Neo4jRunning), p.SQLitePath, report.GraphStats.Nodes, report.GraphStats.Relationships, caps.HasGraphQL, caps.HasREST, caps.HasGRPC, caps.HasEvents, caps.HasPersistence, caps.HasTests), nil
}

func doctor(ctx context.Context, p paths.ProjectPaths) (string, error) {
	cfg, err := app.LoadConfig(p.ConfigPath)
	if err != nil {
		cfg = app.DefaultConfig(p.Root)
	}
	graph := graphOrNil(p.ConfigPath)
	if graph != nil {
		defer graph.Close(ctx)
	}
	spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
	report := app.NewRuntimeService(dockerruntime.Runtime{}, graph).Status(ctx, spec)
	store, _ := sqlstore.New(p.SQLitePath)
	var fileCount int
	var lastIndex string
	var caps domain.RepoCapabilities
	if store != nil {
		defer store.Close()
		_ = store.Init(ctx)
		files, _ := store.IndexedFiles(ctx, p.Root)
		fileCount = len(files)
		caps = app.DetectRepoCapabilities(p.Root, files)
		if run, _ := store.LastIndexRun(ctx, p.Root); run != nil {
			lastIndex = run.CompletedAt.Format(time.RFC3339)
		}
	}
	contextStats := app.CountGeneratedContext(p)
	var b strings.Builder
	fmt.Fprintf(&b, "agent-brain doctor\n")
	fmt.Fprintf(&b, "Repo root: %s\nProject ID: %s\nRuntime namespace: %s\n", p.Root, cfg.ProjectID, cfg.RuntimeNamespace)
	fmt.Fprintf(&b, "Config: %s\nDocker: %s\nDocker CLI: %s\nNeo4j: %s (%s)\nSQLite: %s\n", yesNo(fileExists(p.ConfigPath)), yesNo(report.DockerAvailable), dockerCLIPathForDisplay(), yesNo(report.Neo4jRunning), cfg.Neo4jURI, p.SQLitePath)
	fmt.Fprintf(&b, "Indexed files: %d\nLast index: %s\nGraph: %d nodes / %d relationships\nContext packs: %d\n", fileCount, valueOr(lastIndex, "none"), report.GraphStats.Nodes, report.GraphStats.Relationships, contextStats.GeneratedPacks)
	fmt.Fprintf(&b, "Capabilities: GraphQL=%s REST=%s gRPC=%s Events=%s Persistence=%s Tests=%s MainLanguage=%s\n", yesNo(caps.HasGraphQL), yesNo(caps.HasREST), yesNo(caps.HasGRPC), yesNo(caps.HasEvents), yesNo(caps.HasPersistence), yesNo(caps.HasTests), caps.MainLanguage)
	for _, action := range doctorActions(report, fileCount, caps, contextStats) {
		fmt.Fprintf(&b, "Next: %s\n", action)
	}
	return b.String(), nil
}

func mcpHealth(ctx context.Context, p paths.ProjectPaths) (string, error) {
	cwd, _ := os.Getwd()
	envRoot := os.Getenv("AGENT_BRAIN_REPO_ROOT")
	diag, err := doctor(ctx, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("agent-brain MCP health\nServer version: %s\nProcess cwd: %s\nAGENT_BRAIN_REPO_ROOT: %s\nResolved repo root: %s\n\n%s", serverVersion, cwd, valueOr(envRoot, "not set"), p.Root, diag), nil
}

func doctorActions(report app.StatusReport, fileCount int, caps domain.RepoCapabilities, contextStats app.ContextOutputStats) []string {
	var actions []string
	if !report.DockerAvailable {
		actions = append(actions, "Start Docker Desktop, then restart the IDE/agent MCP host or rerun agent-brain mcp install-* from the target repo.")
	}
	if report.DockerAvailable && !report.Neo4jRunning {
		actions = append(actions, "Run agent-brain up.")
	}
	if fileCount == 0 {
		actions = append(actions, "Run agent-brain index --repo . or prepare_context_async.")
	}
	if fileCount > 0 && report.Neo4jRunning && report.GraphStats.Nodes == 0 {
		actions = append(actions, "Re-run agent-brain index --repo . to sync Neo4j.")
	}
	if fileCount > 0 && !caps.HasTests {
		actions = append(actions, "Add or identify tests; indexed repo has no detected tests.")
	}
	if contextStats.GeneratedPacks == 0 && fileCount > 0 {
		actions = append(actions, "Run prepare_context_async for the current task.")
	}
	if len(actions) == 0 {
		actions = append(actions, "Environment looks ready. Use start_task_async or prepare_context_async.")
	}
	return actions
}

func contextPathFromArgs(p paths.ProjectPaths, args map[string]any) string {
	if explicit := stringArg(args, "context_path"); explicit != "" {
		return explicit
	}
	taskPath := stringArg(args, "task_path")
	if taskPath == "" {
		return ""
	}
	return filepath.Join(p.AIContextDir, app.TaskIDFromPath(taskPath)+".agent.md")
}

func domainMemoryPath(p paths.ProjectPaths) string {
	return filepath.Join(p.AgentBrainDir, "memory", "domain.yml")
}

func graphOrNil(configPath string) *neo.Store {
	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		return nil
	}
	graph, err := neo.New(cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		return nil
	}
	return graph
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

func stringArgDefault(args map[string]any, key, fallback string) string {
	if v := strings.TrimSpace(stringArg(args, key)); v != "" {
		return v
	}
	return fallback
}

func boolArg(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func boolArgDefault(args map[string]any, key string, fallback bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return fallback
}

func progressArg(args map[string]any) app.ProgressFunc {
	if v, ok := args["progress"].(app.ProgressFunc); ok {
		return v
	}
	return nil
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(values) == "" {
			return nil
		}
		return []string{values}
	default:
		return nil
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func dockerCLIPathForDisplay() string {
	path, err := dockerruntime.DockerCLIPath()
	if err != nil {
		return "not found"
	}
	return path
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
