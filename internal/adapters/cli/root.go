package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	root.AddCommand(initCmd(ctx), upCmd(ctx), downCmd(ctx), destroyCmd(ctx), statusCmd(ctx), doctorCmd(ctx), logsCmd(ctx), prepareCmd(ctx), agentStartCmd(ctx), handoffCmd(), cleanContextCmd(), mcpCmd(ctx), jiraCmd(ctx), githubCmd(ctx), indexCmd(ctx), contextCmd(ctx), impactCmd(ctx), reviewPlanCmd(), reviewCommentsCmd(), reviewDiffCmd(ctx), memoryProposalCmd(ctx), memoryApplyCmd(ctx), domainMemoryCmd(ctx))
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

func cleanContextCmd() *cobra.Command {
	var confirm bool
	c := &cobra.Command{
		Use:   "clean-context --confirm",
		Short: "Remove generated agent context packs without touching source, memory, SQLite, or Neo4j",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			result, err := app.CleanGeneratedContext(p, confirm)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed generated context packs: %d\n", len(result.Removed))
			if len(result.Skipped) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Skipped unsafe paths: %d\n", len(result.Skipped))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&confirm, "confirm", false, "confirm removal of generated context pack files")
	return c
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

func doctorCmd(ctx context.Context) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose local agent-brain prerequisites and project wiring",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, _ := loadOrDefaultConfig(p)
			spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			report := app.NewRuntimeService(dockerruntime.Runtime{}, graph).Status(ctx, spec)
			contextStats := app.CountGeneratedContext(p)
			fmt.Fprintf(cmd.OutOrStdout(), "agent-brain doctor\n")
			fmt.Fprintf(cmd.OutOrStdout(), "- repo root: %s\n", p.Root)
			fmt.Fprintf(cmd.OutOrStdout(), "- runtime namespace: %s\n", cfg.RuntimeNamespace)
			fmt.Fprintf(cmd.OutOrStdout(), "- config: %s\n", yesNo(filesystem.LocalFS{}.Exists(p.ConfigPath)))
			fmt.Fprintf(cmd.OutOrStdout(), "- docker: %s\n", yesNo(report.DockerAvailable))
			fmt.Fprintf(cmd.OutOrStdout(), "- neo4j: %s (%s)\n", yesNo(report.Neo4jRunning), cfg.Neo4jURI)
			fmt.Fprintf(cmd.OutOrStdout(), "- sqlite: %s\n", p.SQLitePath)
			fmt.Fprintf(cmd.OutOrStdout(), "- graph: %d nodes / %d relationships\n", report.GraphStats.Nodes, report.GraphStats.Relationships)
			fmt.Fprintf(cmd.OutOrStdout(), "- context packs: %d\n", contextStats.GeneratedPacks)
			if report.DockerAvailable && !report.Neo4jRunning {
				fmt.Fprintln(cmd.OutOrStdout(), "next: run agent-brain up")
			}
			if !report.DockerAvailable {
				fmt.Fprintln(cmd.OutOrStdout(), "next: start Docker Desktop, then run agent-brain up")
			}
			return nil
		},
	}
}

func logsCmd(ctx context.Context) *cobra.Command {
	var tail int
	c := &cobra.Command{
		Use:   "logs",
		Short: "Show local agent-brain runtime logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			cfg, _ := loadOrDefaultConfig(p)
			spec := app.RuntimeSpecFromConfig(cfg, p.ComposePath)
			name := spec.ContainerName
			c := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprintf("%d", tail), name)
			out, err := c.CombinedOutput()
			if err != nil {
				return fmt.Errorf("docker logs failed for %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
			}
			fmt.Fprint(cmd.OutOrStdout(), string(out))
			return nil
		},
	}
	c.Flags().IntVar(&tail, "tail", 120, "number of log lines")
	return c
}

func prepareCmd(ctx context.Context) *cobra.Command {
	var task, topic string
	var budget string
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
			).Prepare(ctx, p, cfg, app.PrepareOptions{TaskPath: task, Topic: topic, Fast: fast, NoIndex: noIndex, Budget: budget})
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
	c.Flags().StringVar(&budget, "budget", app.BudgetCavernicola, "token budget: cavernicola, compact, standard, or deep")
	c.Flags().BoolVar(&fast, "fast", false, "skip reindex if the last index is recent")
	c.Flags().BoolVar(&noIndex, "no-index", false, "do not index before generating context")
	return c
}

func agentStartCmd(ctx context.Context) *cobra.Command {
	var task, topic, budget, memoryArea string
	var fast, noIndex, bootstrapMemory bool
	c := &cobra.Command{
		Use:   "agent-start",
		Short: "Run the complete agent startup workflow for a task or topic",
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
			prepare := app.NewPrepareService(app.NewInitService(filesystem.LocalFS{}), dockerruntime.Runtime{}, golang.Indexer{}, store, graph)
			result, err := app.NewAgentWorkflowService(prepare, store, graph).Start(ctx, p, cfg, app.AgentWorkflowOptions{
				TaskPath:        task,
				Topic:           topic,
				Budget:          budget,
				Fast:            fast,
				NoIndex:         noIndex,
				BootstrapMemory: bootstrapMemory,
				MemoryArea:      memoryArea,
			})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), app.RenderAgentWorkflowResult(result))
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	c.Flags().StringVar(&topic, "topic", "", "topic text")
	c.Flags().StringVar(&budget, "budget", app.BudgetCavernicola, "token budget: cavernicola, compact, standard, or deep")
	c.Flags().BoolVar(&fast, "fast", true, "skip reindex if the last index is recent")
	c.Flags().BoolVar(&noIndex, "no-index", false, "do not index before generating context")
	c.Flags().BoolVar(&bootstrapMemory, "bootstrap-memory", true, "generate a domain memory proposal when memory is empty or bootstrap is requested")
	c.Flags().StringVar(&memoryArea, "memory-area", "all", "memory area: all, graphql, auth, billing, events, persistence, application, domain")
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
	installCodex := &cobra.Command{
		Use:   "install-codex",
		Short: "Install agent-brain MCP server into Codex config",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.InstallCodexMCP()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Codex MCP configured: %s\nCommand: %s\nChanged: %s\n", result.ConfigPath, result.Command, yesNo(result.Changed))
			if result.BackupPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Backup: %s\n", result.BackupPath)
			}
			return nil
		},
	}
	installClaude := &cobra.Command{
		Use:     "install-claude",
		Aliases: []string{"install-cloude"},
		Short:   "Install agent-brain MCP server into Claude Desktop config",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.InstallClaudeMCP()
			if err != nil {
				return err
			}
			printMCPInstallResult(cmd, "Claude", result)
			return nil
		},
	}
	installCursor := &cobra.Command{
		Use:   "install-cursor",
		Short: "Install agent-brain MCP server into Cursor config",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := app.InstallCursorMCP()
			if err != nil {
				return err
			}
			printMCPInstallResult(cmd, "Cursor", result)
			return nil
		},
	}
	installCopilot := &cobra.Command{
		Use:   "install-copilot",
		Short: "Install agent-brain MCP server into workspace .vscode/mcp.json for GitHub Copilot",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			result, err := app.InstallCopilotMCP(p.Root)
			if err != nil {
				return err
			}
			printMCPInstallResult(cmd, "Copilot", result)
			return nil
		},
	}
	root.AddCommand(serve, installCodex, installClaude, installCursor, installCopilot)
	return root
}

func printMCPInstallResult(cmd *cobra.Command, name string, result app.MCPInstallResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s MCP configured: %s\nCommand: %s\nChanged: %s\n", name, result.ConfigPath, result.Command, yesNo(result.Changed))
	if result.BackupPath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Backup: %s\n", result.BackupPath)
	}
}

func jiraCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{Use: "jira", Short: "Import Jira issues into local agent task files"}
	var baseURL, email, token, out string
	var offline bool
	importCmd := &cobra.Command{
		Use:   "import <ISSUE-KEY-or-URL>",
		Short: "Import a Jira issue into .ai/tasks/<KEY>.md",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(p.Root, ".ai", "tasks")
			}
			result, err := app.ImportJiraIssue(ctx, app.JiraImportOptions{
				KeyOrURL:  args[0],
				BaseURL:   baseURL,
				Email:     email,
				Token:     token,
				OutputDir: out,
				Offline:   offline,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Jira task imported: %s\nTask ID: %s\nOffline shell: %s\n", result.Path, result.TaskID, yesNo(result.Offline))
			return nil
		},
	}
	importCmd.Flags().StringVar(&baseURL, "base-url", "", "Jira base URL, or JIRA_BASE_URL")
	importCmd.Flags().StringVar(&email, "email", "", "Jira account email, or JIRA_EMAIL")
	importCmd.Flags().StringVar(&token, "token", "", "Jira API token, or JIRA_API_TOKEN")
	importCmd.Flags().StringVar(&out, "out", "", "task output directory")
	importCmd.Flags().BoolVar(&offline, "offline", false, "create a local task shell without calling Jira")
	var key, sourceURL, summary, description, issueType, status, priority string
	var ac, comments, labels []string
	fromMCP := &cobra.Command{
		Use:   "from-mcp",
		Short: "Create .ai/tasks/<KEY>.md from Jira fields supplied by an agent/Jira MCP",
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				return fmt.Errorf("--key is required")
			}
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(p.Root, ".ai", "tasks")
			}
			result, err := app.ImportJiraIssueContent(app.JiraIssueContentOptions{
				Key:                key,
				SourceURL:          sourceURL,
				Summary:            summary,
				Description:        description,
				AcceptanceCriteria: ac,
				Comments:           comments,
				IssueType:          issueType,
				Status:             status,
				Priority:           priority,
				Labels:             labels,
				OutputDir:          out,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Jira task imported from MCP content: %s\nTask ID: %s\n", result.Path, result.TaskID)
			return nil
		},
	}
	fromMCP.Flags().StringVar(&key, "key", "", "Jira issue key")
	fromMCP.Flags().StringVar(&sourceURL, "source-url", "", "Jira browse URL")
	fromMCP.Flags().StringVar(&summary, "summary", "", "Jira summary")
	fromMCP.Flags().StringVar(&description, "description", "", "Jira description")
	fromMCP.Flags().StringArrayVar(&ac, "ac", nil, "acceptance criterion; repeatable")
	fromMCP.Flags().StringArrayVar(&comments, "comment", nil, "Jira comment; repeatable")
	fromMCP.Flags().StringVar(&issueType, "issue-type", "", "Jira issue type")
	fromMCP.Flags().StringVar(&status, "status", "", "Jira status")
	fromMCP.Flags().StringVar(&priority, "priority", "", "Jira priority")
	fromMCP.Flags().StringArrayVar(&labels, "label", nil, "Jira label; repeatable")
	fromMCP.Flags().StringVar(&out, "out", "", "task output directory")
	root.AddCommand(importCmd, fromMCP)
	return root
}

func githubCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{Use: "github", Short: "Import GitHub PR review context for agents"}
	var pr, repo, out, token string
	var review bool
	comments := &cobra.Command{
		Use:   "review-comments",
		Short: "Import GitHub PR comments into .ai/reviews and optionally build an agent repair plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if pr == "" {
				return fmt.Errorf("--pr is required")
			}
			p, err := paths.Discover(".")
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join(p.Root, ".ai", "reviews")
			}
			result, err := app.ImportGitHubPRComments(ctx, app.GitHubPRCommentsOptions{
				RepoRoot:  p.Root,
				PR:        pr,
				OwnerRepo: repo,
				OutputDir: out,
				Token:     token,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "GitHub PR comments imported: %s\nRepository: %s\nPR: #%s\nComments: %d\nSource: %s\n", result.Path, result.OwnerRepo, result.PR, result.CommentCount, result.Source)
			if review {
				_, summary, err := app.NewReviewService().ReviewComments(result.Path)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprint(cmd.OutOrStdout(), summary)
			}
			return nil
		},
	}
	comments.Flags().StringVar(&pr, "pr", "", "PR number or GitHub pull request URL")
	comments.Flags().StringVar(&repo, "repo", "", "GitHub owner/repo; defaults to git origin")
	comments.Flags().StringVar(&out, "out", "", "review comments output directory")
	comments.Flags().StringVar(&token, "token", "", "GitHub token; defaults to GITHUB_TOKEN or GH_TOKEN")
	comments.Flags().BoolVar(&review, "review", true, "run review-comments after import")
	root.AddCommand(comments)
	return root
}

func indexCmd(ctx context.Context) *cobra.Command {
	var repo string
	var incremental bool
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
			result, err := app.NewIndexService(golang.Indexer{}, store, graph).IndexWithOptions(ctx, repoAbs, app.IndexOptions{Incremental: incremental})
			if err != nil {
				return err
			}
			idx := result.Index
			fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d files, %d nodes, %d relationships\n", len(idx.Files), len(idx.Nodes), len(idx.Relations))
			if incremental {
				fmt.Fprintf(cmd.OutOrStdout(), "Incremental: changed files=%d graph updated=%s\n", len(result.ChangedFiles), yesNo(result.GraphUpdated))
				if result.Unchanged {
					fmt.Fprintln(cmd.OutOrStdout(), "No file hash changes detected; graph write skipped.")
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", ".", "repository path")
	c.Flags().BoolVar(&incremental, "incremental", false, "skip graph rewrite when indexed file hashes did not change")
	return c
}

func contextCmd(ctx context.Context) *cobra.Command {
	var task string
	var budget string
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
			pack, md, js, err := app.NewContextServiceWithBudget(store, graphOrNil(p.ConfigPath), budget).Generate(ctx, p.Root, task, p.RulesDir, p.AIContextDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated context pack %s (%d files)\nJSON: %s\nMarkdown: %s\n", pack.TaskID, len(pack.LikelyRelevantFiles), js, md)
			return nil
		},
	}
	c.Flags().StringVar(&task, "task", "", "task markdown path")
	c.Flags().StringVar(&budget, "budget", app.BudgetCavernicola, "token budget: cavernicola, compact, standard, or deep")
	return c
}

func impactCmd(ctx context.Context) *cobra.Command {
	var topic string
	var budget string
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
			pack, err := app.NewContextServiceWithBudget(store, graph, budget).GenerateForText(ctx, p.Root, "impact", topic, p.RulesDir)
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
	c.Flags().StringVar(&budget, "budget", app.BudgetCavernicola, "token budget: cavernicola, compact, standard, or deep")
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

func reviewCommentsCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "review-comments",
		Short: "Turn code review comments into an agent repair plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			_, summary, err := app.NewReviewService().ReviewComments(file)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), summary)
			return nil
		},
	}
	c.Flags().StringVar(&file, "file", "", "markdown/text file containing review comments")
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

func memoryApplyCmd(ctx context.Context) *cobra.Command {
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
			appliedPath, err := app.NewMemoryService(store, graph).ApplyProposal(ctx, p.Root, args[0], p.LocalMemoryDir, yes)
			if err != nil {
				return fmt.Errorf("confirmation required or invalid proposal; rerun with --yes after reviewing %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Memory proposal applied locally and persisted: %s\n", appliedPath)
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "confirm applying memory proposal")
	return c
}

func domainMemoryCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{Use: "memory-domain", Short: "Propose, apply, and inspect system/domain memory"}
	var task, area string
	propose := &cobra.Command{
		Use:   "propose",
		Short: "Propose domain memory from indexed code and an optional task",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			path, memory, err := app.NewDomainMemoryService(store, graph).Propose(ctx, p.Root, task, p.AIMemoryProposals, area)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Domain memory proposal: %s\nConcepts: %d\nComponents: %d\nRules: %d\nInvariants: %d\n", path, len(memory.DomainConcepts), len(memory.SystemComponents), len(memory.BusinessRules), len(memory.Invariants))
			return nil
		},
	}
	propose.Flags().StringVar(&task, "task", "", "task markdown path")
	propose.Flags().StringVar(&area, "area", "all", "memory area: all, graphql, auth, billing, events, persistence")
	bootstrap := &cobra.Command{
		Use:   "bootstrap",
		Short: "Create an initial business/domain memory proposal from indexed code",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			path, memory, err := app.NewDomainMemoryService(store, graph).Propose(ctx, p.Root, "", p.AIMemoryProposals, area)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Initial domain memory proposal: %s\nConcepts: %d\nComponents: %d\nRules: %d\nInvariants: %d\nHuman approval required before memory-domain apply.\n", path, len(memory.DomainConcepts), len(memory.SystemComponents), len(memory.BusinessRules), len(memory.Invariants))
			return nil
		},
	}
	bootstrap.Flags().StringVar(&area, "area", "all", "memory area: all, graphql, auth, billing, events, persistence, application, domain")
	var yes bool
	apply := &cobra.Command{
		Use:   "apply <proposal.yml>",
		Short: "Apply approved domain memory to local file, SQLite, and Neo4j",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				fmt.Fprint(cmd.OutOrStdout(), "Apply domain memory? Type 'yes' to continue: ")
				var answer string
				fmt.Fscan(os.Stdin, &answer)
				yes = strings.EqualFold(answer, "yes")
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
			memory, err := app.NewDomainMemoryService(store, graph).Apply(ctx, p.Root, args[0], domainMemoryPath(p), yes)
			if err != nil {
				return fmt.Errorf("confirmation required or invalid proposal; rerun with --yes after reviewing %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Domain memory applied: %s\nConcepts: %d\nComponents: %d\nRules: %d\nInvariants: %d\n", domainMemoryPath(p), len(memory.DomainConcepts), len(memory.SystemComponents), len(memory.BusinessRules), len(memory.Invariants))
			return nil
		},
	}
	apply.Flags().BoolVar(&yes, "yes", false, "confirm applying domain memory")
	var topic, listArea string
	list := &cobra.Command{
		Use:   "list",
		Short: "Print compact relevant system/domain memory",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _ := paths.Discover(".")
			store, err := sqlstore.New(p.SQLitePath)
			if err != nil {
				return err
			}
			defer store.Close()
			_ = store.Init(ctx)
			memory, err := app.NewDomainMemoryService(store, nil).Load(ctx, p.Root, domainMemoryPath(p))
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), app.RenderDomainMemory(memory, topic, listArea, 12))
			return nil
		},
	}
	list.Flags().StringVar(&topic, "topic", "", "filter memory by topic")
	list.Flags().StringVar(&listArea, "area", "all", "filter memory by area")
	root.AddCommand(propose, bootstrap, apply, list)
	return root
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
