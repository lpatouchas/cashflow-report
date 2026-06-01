package report

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// Service orchestrates report generation:
// load → filter transfers → apply exclusion rules → summarize → render.
type Service struct {
	repo     transaction.Repository
	renderer Renderer
	rules    []transaction.ExclusionRule
}

func NewService(repo transaction.Repository, renderer Renderer, rules []transaction.ExclusionRule) *Service {
	return &Service{repo: repo, renderer: renderer, rules: rules}
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

	before := len(kept)
	kept = transaction.ApplyExclusions(kept, s.rules)
	if dropped := before - len(kept); dropped > 0 {
		slog.Info("excluded transactions by exclusion rule", "count", dropped)
	}

	summary := transaction.Summarize(kept)

	if err := s.renderer.Render(ctx, summary); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	return nil
}
