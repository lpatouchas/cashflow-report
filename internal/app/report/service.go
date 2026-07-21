package report

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// Service orchestrates report generation:
// load → partition bank/VISA → filter transfers (bank only) →
// reconcile VISA → apply exclusion rules → summarize → render.
type Service struct {
	repo      transaction.Repository
	renderer  Renderer
	rules     []transaction.ExclusionRule
	reconcile *transaction.ReconcileConfig // nil = VISA reconciliation disabled
}

func NewService(repo transaction.Repository, renderer Renderer, rules []transaction.ExclusionRule, reconcile *transaction.ReconcileConfig) *Service {
	return &Service{repo: repo, renderer: renderer, rules: rules, reconcile: reconcile}
}

func (s *Service) GenerateReport(ctx context.Context) error {
	all, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("loading transactions: %w", err)
	}

	// VISA rows bypass FilterTransfers: that filter targets bank inter-account
	// transfers and duplicate export rows, which do not apply to card purchases.
	var bank, visa []transaction.Transaction
	for _, t := range all {
		if t.IsVISA {
			visa = append(visa, t)
		} else {
			bank = append(bank, t)
		}
	}

	bankKept := transaction.FilterTransfers(bank)
	if excluded := len(bank) - len(bankKept); excluded > 0 {
		slog.Info("excluded inter-account transfers and duplicates", "count", excluded)
	}

	combined := append(bankKept, visa...)
	if s.reconcile != nil {
		combined = transaction.ReconcileVISA(combined, *s.reconcile)
	}

	before := len(combined)
	kept := transaction.ApplyExclusions(combined, s.rules)
	if dropped := before - len(kept); dropped > 0 {
		slog.Info("excluded transactions by exclusion rule", "count", dropped)
	}

	summary := transaction.Summarize(kept)

	if err := s.renderer.Render(ctx, summary); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	return nil
}
