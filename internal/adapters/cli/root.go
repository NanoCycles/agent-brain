package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dockerruntime "github.com/NanoCycles/agent-brain/internal/adapters/docker"
	"github.com/NanoCycles/agent-brain/internal/adapters/filesystem"
	"github.com/NanoCycles/agent-brain/internal/adapters/golang"
	"github.com/NanoCycles/agent-brain/internal/adapters/mcp"
	neo "github.com/NanoCycles/agent-brain/internal/adapters/neo4j"
	sqlstore "github.com/NanoCycles/agent-brain/internal/adapters/sqlite"
	"github.com/NanoCycles/agent-brain/internal/app"
	"github.com/NanoCycles/agent-brain/internal/platform/paths"
	"github.com/spf13/cobra"
)

func NewRootCommand(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-brain",
		Short: "Local knowledge CLI for AI coding agents",
	}
	root.AddCommand(initCmd(ctx), upCmd(ctx), downCmd(ctx), destroyCmd(ctx), statusCmd(ctx), prepareCmd(ctx), handoffCmd(), mcpCmd(ctx), indexCmd(ctx), contextCmd(ctx), impactCmd(ctx), reviewPlanCmd(), reviewDiffCmd(ctx), memoryProposalCmd(ctx), memoryApplyCmd())
	return root
}

func initCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize agent-brain in the current repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			if err := app.NewInitService(filesystem.LocalFS{}).Init(p.Root); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Initialized agent-brain at %s\n", p.AgentBrainDir)
			_ = ctx
			return nil
		},
	}
}

func upCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start local agent-brain services",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultConfig(p)
			if err != nil {
				return err
			}
			spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
			if err := app.NewRuntimeService(dockerruntime.Runtime{}, nil).Up(ctx, spec); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Neo4j is running for project %s: http://localhost:%d (%s / %s)\n", cfg.ProjectID, cfg.Neo4jHTTPPort, cfg.Neo4jUser, cfg.Neo4jPassword)
			return nil
		},
	}
}

func downCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop local agent-brain services without deleting data",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultConfig(p)
			if err != nil {
				return err
			}
			if err := app.NewRuntimeService(dockerruntime.Runtime{}, nil).Down(ctx, app.RuntimeSpecFromConfig(cfg, p.ComposePath)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent-brain services stopped for project %s\n", cfg.ProjectID)
			return nil
		},
	}
}

func destroyCmd(ctx context.Context) *cobra.Command {
	var confirm bool
	c := &cobra.Command{
		Use:   "destroy --confirm",
		Short: "Destroy local agent-brain runtime data for this project without touching source code",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("destroy requires --confirm; this removes this project's Neo4j volume and SQLite metadata, but does not touch source code")
			}
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultConfig(p)
			if err != nil {
				return err
			}
			spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
			if (filesystem.LocalFS{}).Exists(p.ComposePath) {
				if err := app.NewRuntimeService(dockerruntime.Runtime{}, nil).Destroy(ctx, spec); err != nil {
					return err
				}
			}
			for _, path := range []string{p.SQLitePath, p.SQLitePath + "-shm", p.SQLitePath + "-wal"} {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Destroyed local runtime data for project %s. Source code, config, rules, context, and memory proposals were not removed.\n", cfg.ProjectID)
			return nil
		},
	}
	c.Flags().BoolVar(&confirm, "confirm", false, "confirm removal of this project's local runtime data")
	return c
}

func statusCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local runtime and metadata status",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultConfig(p)
			if err != nil {
				return err
			}
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
			report := app.NewRuntimeService(dockerruntime.Runtime{}, graph).Status(ctx, spec)
			repoInitialized := filesystem.LocalFS{}.Exists(p.ConfigPath)
			store, _ := sqlstore.New(p.SQLitePath)
			var last string
			if store != nil {
				_ = store.Init(ctx)
				run, _ := store.LastIndexRun(ctx, p.Root)
				if run != nil {
					last = run.CompletedAt.Format("2006-01-02 15:04:05")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project ID: %s\nRuntime namespace: %s\nNeo4j HTTP URL: http://localhost:%d\nNeo4j Bolt URI: %s\nDocker available: %s\nNeo4j running: %s\nSQLite metadata path: %s\nRepo initialized: %s\nLast index: %s\nGraph nodes: %d\nGraph relationships: %d\n",
				cfg.ProjectID, cfg.RuntimeNamespace, cfg.Neo4jHTTPPort, cfg.Neo4jURI, yesNo(report.DockerAvailable), yesNo(report.Neo4jRunning), p.SQLitePath, yesNo(repoInitialized), valueOr(last, "none"), report.GraphStats.Nodes, report.GraphStats.Relationships)
			if store != nil {
				files, _ := store.IndexedFiles(ctx, p.Root)
				caps := app.DetectRepoCapabilities(p.Root, files)
				fmt.Fprintf(cmd.OutOrStdout(), "Repo capabilities: GraphQL=%s REST=%s gRPC=%s Events=%s Persistence=%s Tests=%s MainLanguage=%s\n",
					yesNo(caps.HasGraphQL), yesNo(caps.HasREST), yesNo(caps.HasGRPC), yesNo(caps.HasEvents), yesNo(caps.HasPersistence), yesNo(caps.HasTests), caps.MainLanguage)
				if len(files) > 0 && len(caps.Evidence) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Warning: repo is indexed but no capabilities were detected")
				}
				if len(files) > 0 && !caps.HasTests {
					fmt.Fprintln(cmd.OutOrStdout(), "Warning: repo is indexed but no tests were detected")
				}
				if report.Neo4jRunning && report.GraphStats.Nodes == 0 && len(files) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Warning: Neo4j is running but graph appears unsynchronized with SQLite metadata")
				}
				_ = store.Close()
			}
			return nil
		},
	}
}

func prepareCmd(ctx context.Context) *cobra.Command {
	var task, topic string
	var fast, noIndex bool
	c := &cobra.Command{
		Use:   "prepare",
		Short: "Initialize, start services, index, and generate an agent context pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, err := loadOrDefaultConfig(p)
			if err != nil {
				return err
			}
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			result, err := app.NewPrepareService(
				app.NewInitService(filesystem.LocalFS{}),
				dockerruntime.Runtime{},
				golang.Indexer{},
				store,
				graph,
			).Prepare(ctx, p, cfg, app.PrepareOptions{TaskPath: task, Topic: topic, Fast: fast, NoIndex: noIndex})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Agent context ready: %s\nJSON: %s\nQuality: %s %.2f\nIndexed: %s\nRuntime started: %s\nTop files: %d\n\nSuggested prompt:\n%s\n",
				result.MarkdownPath, result.JSONPath, result.Pack.ContextQuality.Level, result.Pack.ContextQuality.Score, yesNo(result.Indexed), yesNo(result.RuntimeUp), len(result.Pack.LikelyRelevantFiles), result.Handoff)
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	c.Flags().StringVar(&topic, "topic", "", "topic text")
	c.Flags().BoolVar(&fast, "fast", false, "skip reindex if the last index is recent")
	c.Flags().BoolVar(&noIndex, "no-index", false, "do not index before generating context")
	return c
}

func handoffCmd() *cobra.Command {
	var task, contextPath string
	c := &cobra.Command{
		Use:   "handoff",
		Short: "Print a prompt for Codex/Cursor/Claude to use an agent context pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contextPath == "" {
				if task == "" {
					return fmt.Errorf("--task or --context is required")
				}
				p, err := paths.Discover(".")
				if err != nil {
					return err
				}
				taskID := app.TaskIDFromPath(task)
				contextPath = filepath.Join(p.AIContextDir, taskID+".agent.md")
			}
			fmt.Fprintln(cmd.OutOrStdout(), app.HandoffPrompt(contextPath))
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	c.Flags().StringVar(&contextPath, "context", "", "context pack markdown path")
	return c
}

func mcpCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:   "mcp",
		Short: "Run agent-brain MCP integrations",
	}
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run the local stdio MCP server for AI coding agents",
		Long:  "Run the local stdio MCP server for AI coding agents. The command writes only MCP JSON-RPC messages to stdout.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.NewServer(os.Stdin, os.Stdout).Serve(ctx)
		},
	}
	root.AddCommand(serve)
	return root
}

func indexCmd(ctx context.Context) *cobra.Command {
	var repo string
	c := &cobra.Command{
		Use:   "index",
		Short: "Index a Go repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			repoAbs, err := filepath.Abs(repo)
			if err != nil {
				return err
			}
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			idx, err := app.NewIndexService(golang.Indexer{}, store, graph).Index(ctx, repoAbs)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d files, %d nodes, %d relationships\n", len(idx.Files), len(idx.Nodes), len(idx.Relations))
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", ".", "repository path")
	return c
}

func contextCmd(ctx context.Context) *cobra.Command {
	var task string
	c := &cobra.Command{
		Use:   "context",
		Short: "Generate compact agent context pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if task == "" {
				return fmt.Errorf("--task is required")
			}
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			_ = store.Init(ctx)
			pack, md, js, err := app.NewContextService(store, graphOrNil(p.ConfigPath)).Generate(ctx, p.Root, task, p.RulesDir, p.AIContextDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated context pack %s (%d files)\nJSON: %s\nMarkdown: %s\n", pack.TaskID, len(pack.LikelyRelevantFiles), js, md)
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	return c
}

func impactCmd(ctx context.Context) *cobra.Command {
	var topic string
	c := &cobra.Command{
		Use:   "impact",
		Short: "Search graph impact for a topic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			p, _ := paths.Discover(".")
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			_ = store.Init(ctx)
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			pack, err := app.NewContextService(store, graph).GenerateForText(ctx, p.Root, "impact", topic, p.RulesDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Impact for %q\n", topic)
			fmt.Fprintf(cmd.OutOrStdout(), "Context quality: %s %.2f\n", pack.ContextQuality.Level, pack.ContextQuality.Score)
			fmt.Fprintf(cmd.OutOrStdout(), "Main capability: %s\n", pack.TaskAnalysis.MainCapability)
			fmt.Fprintln(cmd.OutOrStdout(), "Likely files:")
			if len(pack.LikelyRelevantFiles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- No strong relevant files detected.")
			}
			for _, c := range pack.LikelyRelevantFiles {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s [%s %.2f] %s\n", c.Path, c.Category, c.Confidence, c.Reason)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Risks:")
			for _, r := range append(append(pack.Risks.Security, pack.Risks.Concurrency...), pack.Risks.MemoryPerformance...) {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", r)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Suggested tests:")
			for _, t := range pack.SuggestedTests {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", t)
			}
			return nil
		},
	}
	c.Flags().StringVar(&topic, "topic", "", "topic text")
	return c
}

func reviewPlanCmd() *cobra.Command {
	var plan string
	c := &cobra.Command{
		Use:   "review-plan",
		Short: "Review an agent plan against local rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			if plan == "" {
				return fmt.Errorf("--plan is required")
			}
			p, _ := paths.Discover(".")
			report, err := app.NewReviewService().ReviewPlan(plan, p.RulesDir)
			if err != nil {
				return err
			}
			printReview(cmd, report)
			return nil
		},
	}
	c.Flags().StringVar(&plan, "plan", "", "plan markdown path")
	return c
}

func reviewDiffCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "review-diff",
		Short: "Review current git diff without modifying files",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := paths.Discover(".")
			report, summary, err := app.NewReviewService().ReviewDiff(ctx, p.Root, p.RulesDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\nDecision: %s\n", summary, report.Decision)
			return nil
		},
	}
}

func memoryProposalCmd(ctx context.Context) *cobra.Command {
	var task string
	c := &cobra.Command{
		Use:   "memory-proposal",
		Short: "Generate a structured memory proposal",
		RunE: func(cmd *cobra.Command, args []string) error {
			if task == "" {
				return fmt.Errorf("--task is required")
			}
			p, _ := paths.Discover(".")
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			_ = store.Init(ctx)
			path, err := app.NewMemoryService(store).GenerateProposal(ctx, p.Root, task, p.AIMemoryProposals)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated memory proposal: %s\n", path)
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	return c
}

func memoryApplyCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "memory-apply <proposal.yml>",
		Short: "Apply a memory proposal after confirmation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := paths.Discover(".")
			if !yes {
				fmt.Fprint(cmd.OutOrStdout(), "Apply memory proposal? Type 'yes' to continue: ")
				var answer string
				fmt.Fscan(os.Stdin, &answer)
				yes = strings.EqualFold(answer, "yes")
			}
			appliedPath, err := app.NewMemoryService(nil).ApplyProposal(args[0], p.LocalMemoryDir, yes)
			if err != nil {
				return fmt.Errorf("confirmation required or invalid proposal; rerun with --yes after reviewing %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Memory proposal applied locally: %s\n", appliedPath)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm applying memory proposal")
	return c
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

func loadOrDefaultConfig(p paths.ProjectPaths) (app.Config, error) {
	cfg, err := app.LoadConfig(p.ConfigPath)
	if err == nil {
		return cfg, nil
	}
	return app.DefaultConfig(p.Root), nil
}

func printReview(cmd *cobra.Command, report app.ReviewReport) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", report.Decision)
	for _, f := range report.Findings {
		fmt.Fprintf(cmd.OutOrStdout(), "- [%s] %s", f.Severity, f.Title)
		if f.Path != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", f.Path)
		}
		fmt.Fprintf(cmd.OutOrStdout(), ": %s\n", f.Message)
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
