package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
)

const usageText = `Telemetry Migration Readiness

Usage:
  tmr validate --migration <path>

Commands:
  validate  Validate a migration manifest
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 1
	}

	switch args[0] {
	case "validate":
		return runValidate(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usageText)
		return 1
	}
}

func runValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tmr validate --migration <path>")
		flags.PrintDefaults()
	}

	migrationPath := flags.String("migration", "", "path to a migration YAML manifest")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "validate does not accept positional arguments: %v\n", flags.Args())
		return 1
	}
	if *migrationPath == "" {
		fmt.Fprintln(stderr, "--migration is required")
		return 1
	}

	migration, err := config.LoadMigration(ctx, *migrationPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Migration manifest is valid.")
	fmt.Fprintf(stdout, "Changes: %d\n", len(migration.Changes))
	return 0
}
