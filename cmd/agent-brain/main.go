package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NanoCycles/agent-brain/internal/adapters/cli"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := cli.NewRootCommand(ctx).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "agent-brain: %v\n", err)
		os.Exit(1)
	}
	_ = filepath.Separator
}
