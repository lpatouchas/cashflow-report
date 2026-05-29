package report

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Service orchestrates report generation: load → filter → summarize → render.
type Service struct {
	repo     transaction.Repository
	renderer Renderer
}

func NewService(repo transaction.Repository, renderer Renderer) *Service {
	return &Service{repo: repo, renderer: renderer}
}

func (s *Service) GenerateReport(ctx context.Context) error {
	all, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("loading transactions: %w", err)
	}

	kept := transaction.FilterTransfers(all)
	if excluded := len(all) - len(kept); excluded > 0 {
		slog.Info("excluded inter-account transfers and duplicates", "count", excluded)
	}

	summary := transaction.Summarize(kept)

	if err := s.renderer.Render(ctx, summary); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	return nil
}
