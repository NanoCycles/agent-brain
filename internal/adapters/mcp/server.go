package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	in  io.Reader
	out io.Writer
}

func NewServer(in io.Reader, out io.Writer) *Server {
	return &Server{in: in, out: out}
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
			"serverInfo": map[string]any{"name": "agent-brain", "version": "0.1.0"},
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
		text, err := callTool(ctx, params.Name, params.Arguments)
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
	return []map[string]any{
		tool("prepare_context", "Initialize local runtime if needed, index the current repo unless skipped, generate an agent context pack, and return a compact handoff. Safe: does not modify source code.", map[string]any{
			"task_path": map[string]any{"type": "string", "description": "Path to .ai/tasks/<TASK>.md"},
			"topic":     map[string]any{"type": "string", "description": "Free text task/topic when no task file exists"},
			"budget":    map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
			"fast":      map[string]any{"type": "boolean", "description": "Skip reindex if the last index is recent"},
			"no_index":  map[string]any{"type": "boolean", "description": "Generate context from existing metadata without indexing"},
		}),
		tool("get_context_pack", "Read an existing generated .agent.md context pack for a task or explicit context path.", map[string]any{
			"task_path":    map[string]any{"type": "string"},
			"context_path": map[string]any{"type": "string"},
		}),
		tool("impact", "Analyze likely impact for a topic using SQLite metadata and Neo4j graph expansion.", map[string]any{
			"topic":  map[string]any{"type": "string"},
			"budget": map[string]any{"type": "string", "description": "Token budget: cavernicola, compact, standard, or deep"},
		}),
		tool("review_diff", "Review current git diff for risks, contracts, tests, forbidden files, and rule violations. Read-only.", map[string]any{}),
		tool("status", "Return project runtime, graph, SQLite, and capability status.", map[string]any{}),
		tool("memory_proposal", "Generate a structured memory proposal after a task is implemented. Does not apply memory automatically.", map[string]any{
			"task_path": map[string]any{"type": "string"},
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

func callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	p, err := paths.Discover(".")
	if err != nil {
		return "", err
	}
	switch name {
	case "prepare_context":
		return prepareContext(ctx, p, args)
	case "get_context_pack":
		return getContextPack(p, args)
	case "impact":
		return impact(ctx, p, stringArg(args, "topic"), stringArg(args, "budget"))
	case "review_diff":
		report, summary, err := app.NewReviewService().ReviewDiff(ctx, p.Root, p.RulesDir)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s\nDecision: %s", summary, report.Decision), nil
	case "status":
		return status(ctx, p)
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
		Fast:     boolArg(args, "fast"),
		NoIndex:  boolArg(args, "no_index"),
		Budget:   stringArg(args, "budget"),
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Agent context ready: %s\nJSON: %s\nQuality: %s %.2f\nIndexed: %t\nRuntime started: %t\nTop files: %d\n\nSuggested prompt:\n%s",
		result.MarkdownPath, result.JSONPath, result.Pack.ContextQuality.Level, result.Pack.ContextQuality.Score, result.Indexed, result.RuntimeUp, len(result.Pack.LikelyRelevantFiles), result.Handoff), nil
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

func boolArg(args map[string]any, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
