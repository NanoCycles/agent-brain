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
	root.AddCommand(initCmd(ctx), upCmd(ctx), downCmd(ctx), statusCmd(ctx), indexCmd(ctx), contextCmd(ctx), impactCmd(ctx), reviewPlanCmd(), reviewDiffCmd(ctx), memoryProposalCmd(ctx), memoryApplyCmd())
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
			if err := app.NewRuntimeService(dockerruntime.Runtime{}, nil).Up(ctx, p.ComposePath); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Neo4j is running: http://localhost:7474 (neo4j / agentbrain)")
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
			if err := app.NewRuntimeService(dockerruntime.Runtime{}, nil).Down(ctx, p.ComposePath); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "agent-brain services stopped")
			return nil
		},
	}
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
			graph := graphOrNil(p.ConfigPath)
			if graph != nil {
				defer graph.Close(ctx)
			}
			report := app.NewRuntimeService(dockerruntime.Runtime{}, graph).Status(ctx)
			repoInitialized := filesystem.LocalFS{}.Exists(p.ConfigPath)
			store, _ := sqlstore.New(p.SQLitePath)
			var last string
			if store != nil {
				_ = store.Init(ctx)
				run, _ := store.LastIndexRun(ctx, p.Root)
				if run != nil {
					last = run.CompletedAt.Format("2006-01-02 15:04:05")
				}
				_ = store.Close()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Docker available: %s\nNeo4j running: %s\nSQLite metadata path: %s\nRepo initialized: %s\nLast index: %s\nGraph nodes: %d\nGraph relationships: %d\n",
				yesNo(report.DockerAvailable), yesNo(report.Neo4jRunning), p.SQLitePath, yesNo(repoInitialized), valueOr(last, "none"), report.GraphStats.Nodes, report.GraphStats.Relationships)
			return nil
		},
	}
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
			pack, md, js, err := app.NewContextService(store, graphOrNil(p.ConfigPath)).Generate(ctx, task, p.RulesDir, p.AIContextDir)
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
			graph := graphOrNil(p.ConfigPath)
			if graph == nil {
				return fmt.Errorf("neo4j graph is not configured or reachable")
			}
			defer graph.Close(ctx)
			nodes, err := graph.SearchImpact(ctx, topic, 20)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Impact for %q\n", topic)
			for _, n := range nodes {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s %s %s [%s]\n", n.Label, n.Name, n.Path, n.Layer)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Rules/risks: run context or review-plan for matched structured rules.")
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
