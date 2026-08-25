package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/tadurisaikiran/telemetry-change-guard/internal/version"
)

func runVersion(executable string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s version [--format text|json]\n", executable)
		flags.PrintDefaults()
	}
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "version does not accept positional arguments")
		return 1
	}

	info := version.Current()
	switch *format {
	case "text":
		renderVersionText(stdout, info)
		return 0
	case "json":
		if err := renderVersionJSON(stdout, info); err != nil {
			fmt.Fprintf(stderr, "Error: encode version: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unsupported version format %q; expected text or json\n", *format)
		return 1
	}
}

func renderVersionJSON(output io.Writer, info version.Info) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(info)
}

func renderVersionText(output io.Writer, info version.Info) {
	fmt.Fprintln(output, "Telemetry Change Guard")
	fmt.Fprintf(output, "Version: %s\n", info.Version)
	fmt.Fprintf(output, "Commit: %s\n", info.Commit)
	fmt.Fprintf(output, "Build date: %s\n", info.BuildDate)
	fmt.Fprintf(output, "Dirty: %s\n", info.DirtyText())
	fmt.Fprintf(output, "Go version: %s\n", info.GoVersion)
	fmt.Fprintf(output, "Platform: %s/%s\n", info.OS, info.Arch)
}
