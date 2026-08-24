package main

import (
	"context"
	"io"
	"os"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/cli"
	"github.com/tadurisaikiran/telemetry-change-guard/internal/readiness"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return cli.RunCompatibility(ctx, args, stdout, stderr)
}

func readinessExitCode(status readiness.Status) int {
	return cli.ReadinessExitCode(status)
}
