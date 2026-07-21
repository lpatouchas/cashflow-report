package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lpatouchas/cashflow-report/internal/app/report"
	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/lpatouchas/cashflow-report/internal/infra/config"
	"github.com/lpatouchas/cashflow-report/internal/infra/csv"
	"github.com/lpatouchas/cashflow-report/internal/infra/html"
	"github.com/lpatouchas/cashflow-report/internal/infra/web"
)

const (
	defaultDataDir = "./data"
	defaultOutput  = "./report.html"
	defaultAddr    = ":8080"
)

// version is the build version, overridden at release time via
// -ldflags="-X main.version=<tag>". Defaults to "dev" for local builds.
var version = "dev"

const usage = `cashflow-report — summarise bank CSV exports into an HTML report

Usage:
  cashflow-report                   Start the web app (opens browser, upload CSVs)
  cashflow-report serve [flags]     Start the web app
  cashflow-report generate [flags]  Generate report.html from a data folder

serve flags:
  --addr     address to listen on (default ":8080")
  --no-open  do not open the browser
  --config   exclusion-rules JSON file (default: beside the binary)

generate flags:
  --data    folder of CSV exports (default "./data")
  --out     output HTML path (default "./report.html")
  --config  exclusion-rules JSON file (default: beside the binary)
`

// runGenerate produces the HTML report from a folder of CSV exports.
func runGenerate(dataDir, outputPath, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	repo := csv.New(dataDir)
	renderer := html.NewFile(outputPath)
	svc := report.NewService(repo, renderer, transaction.CompileRules(cfg.Exclusions), cfg.VisaReconcile)
	if err := svc.GenerateReport(context.Background()); err != nil {
		return err
	}
	slog.Info("report generated", "path", outputPath)
	return nil
}

// runServe starts the local web app and blocks.
func runServe(addr, configPath string, open bool) error {
	return web.New(configPath).Run(addr, open)
}

// dispatch routes CLI args to a subcommand. With no command it serves the web
// app. Returns an error for unknown commands, flag errors, or generation
// failures.
func dispatch(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		first := args[0]
		switch first {
		case "-h", "--help", "-help":
			first = "help"
		case "--version", "-version":
			first = "version"
		}
		if !strings.HasPrefix(first, "-") {
			cmd, args = first, args[1:]
		}
	}

	switch cmd {
	case "help":
		fmt.Print(usage)
		return nil
	case "version":
		fmt.Println("cashflow-report " + version)
		return nil
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		data := fs.String("data", defaultDataDir, "folder of CSV exports")
		out := fs.String("out", defaultOutput, "output HTML path")
		cfg := fs.String("config", config.DefaultPath(), "exclusion-rules JSON file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runGenerate(*data, *out, *cfg)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		addr := fs.String("addr", defaultAddr, "address to listen on")
		noOpen := fs.Bool("no-open", false, "do not open the browser")
		cfg := fs.String("config", config.DefaultPath(), "exclusion-rules JSON file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runServe(*addr, *cfg, !*noOpen)
	default:
		return fmt.Errorf("unknown command %q (try 'cashflow-report help')", cmd)
	}
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
