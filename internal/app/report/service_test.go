package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGenerateReport(t *testing.T) {
	ctx := context.Background()
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	t.Run("filters transfers then renders summary", func(t *testing.T) {
		txns := []transaction.Transaction{
			{ID: "T", SourceFile: "a.csv", Amount: 100, IsDebit: true, Date: d},
			{ID: "T", SourceFile: "b.csv", Amount: 100, IsDebit: false, Date: d},
			{ID: "INC", SourceFile: "a.csv", Amount: 500, IsDebit: false, Date: d},
			{ID: "EXP", SourceFile: "a.csv", Amount: 200, IsDebit: true, Date: d},
		}

		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(txns, nil)

		var captured transaction.Summary
		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).
			Run(func(args mock.Arguments) {
				captured = args.Get(1).(transaction.Summary)
			}).
			Return(nil)

		svc := NewService(repo, renderer, nil, nil)
		err := svc.GenerateReport(ctx)

		require.NoError(t, err)
		require.InDelta(t, 500, captured.TotalIncome, 0.001)
		require.InDelta(t, 200, captured.TotalExpenses, 0.001)
		require.InDelta(t, 300, captured.Savings, 0.001)
		repo.AssertExpectations(t)
		renderer.AssertExpectations(t)
	})

	t.Run("returns repo error without rendering", func(t *testing.T) {
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(nil, errors.New("boom"))

		renderer := &MockRenderer{}

		svc := NewService(repo, renderer, nil, nil)
		err := svc.GenerateReport(ctx)

		require.Error(t, err)
		renderer.AssertNotCalled(t, "Render", mock.Anything, mock.Anything)
	})

	t.Run("propagates renderer error", func(t *testing.T) {
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return([]transaction.Transaction{}, nil)

		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).Return(errors.New("write failed"))

		svc := NewService(repo, renderer, nil, nil)
		err := svc.GenerateReport(ctx)

		require.Error(t, err)
	})

	t.Run("applies exclusion rules before summarizing", func(t *testing.T) {
		txns := []transaction.Transaction{
			{ID: "INC", SourceFile: "a.csv", Amount: 500, IsDebit: false, Date: d},
			{ID: "DROP", SourceFile: "a.csv", Amount: 200, IsDebit: true, Date: d},
		}
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(txns, nil)

		var captured transaction.Summary
		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).
			Run(func(args mock.Arguments) {
				captured = args.Get(1).(transaction.Summary)
			}).
			Return(nil)

		rules := []transaction.ExclusionRule{
			func(t transaction.Transaction) bool { return t.ID == "DROP" },
		}
		svc := NewService(repo, renderer, rules, nil)
		require.NoError(t, svc.GenerateReport(ctx))
		require.InDelta(t, 500, captured.TotalIncome, 0.001)
		require.InDelta(t, 0, captured.TotalExpenses, 0.001)
		repo.AssertExpectations(t)
		renderer.AssertExpectations(t)
	})

	t.Run("VISA rows bypass FilterTransfers and reconcile", func(t *testing.T) {
		d := time.Date(2025, time.July, 1, 0, 0, 0, 0, time.UTC)
		lumpDesc := "ΠΛΗΡΩΜΗ VΙSΑ"
		txns := []transaction.Transaction{
			// A bank inter-account transfer pair (same ID+amount) must be filtered out.
			{ID: "T", SourceFile: "checking.csv", Amount: 100, IsDebit: true, Date: d, Branch: "12"},
			{ID: "T", SourceFile: "savings.csv", Amount: 100, IsDebit: false, Date: d, Branch: "12"},
			// The VISA lump (branch 96) is replaced by its itemized purchases.
			{ID: "L", SourceFile: "checking.csv", Amount: 200, IsDebit: true, Date: time.Date(2025, time.July, 15, 0, 0, 0, 0, time.UTC), Branch: "96", Description: lumpDesc},
			// Two VISA purchases; note they share ID+amount but must NOT be filtered as a transfer.
			{ID: "VISA-a", SourceFile: "visa.csv", Amount: 80, IsDebit: true, Date: time.Date(2025, time.July, 3, 0, 0, 0, 0, time.UTC), IsVISA: true, Description: "SHOP"},
			{ID: "VISA-b", SourceFile: "visa.csv", Amount: 80, IsDebit: true, Date: time.Date(2025, time.July, 4, 0, 0, 0, 0, time.UTC), IsVISA: true, Description: "SHOP"},
		}
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(txns, nil)

		var captured transaction.Summary
		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).
			Run(func(args mock.Arguments) { captured = args.Get(1).(transaction.Summary) }).
			Return(nil)

		cfg := &transaction.ReconcileConfig{Description: lumpDesc, MatchMode: transaction.MatchExact, Branch: "96"}
		svc := NewService(repo, renderer, nil, cfg)
		require.NoError(t, svc.GenerateReport(ctx))

		// Transfer pair filtered (income 0). Expenses = 80+80 purchases + 40 leftover = 200.
		require.InDelta(t, 0, captured.TotalIncome, 0.001)
		require.InDelta(t, 200, captured.TotalExpenses, 0.001)
	})
}
