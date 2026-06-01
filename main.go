package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/lpatouchas/personal-finance/internal/app/report"
	"github.com/lpatouchas/personal-finance/internal/infra/csv"
	"github.com/lpatouchas/personal-finance/internal/infra/html"
)

const (
	dataDir    = "./data"
	outputPath = "./report.html"
)

func run(dataDir, outputPath string) error {
	repo := csv.New(dataDir)
	renderer := html.NewFile(outputPath)
	svc := report.NewService(repo, renderer)
	return svc.GenerateReport(context.Background())
}

func main() {
	if err := run(dataDir, outputPath); err != nil {
		slog.Error("report generation failed", "error", err)
		os.Exit(1)
	}
	slog.Info("report generated", "path", outputPath)
}
