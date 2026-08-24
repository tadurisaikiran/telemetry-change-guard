package main

import (
	"context"
	"os"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
