package html

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestFormatEuro(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "€0,00"},
		{53.79, "€53,79"},
		{1234.56, "€1.234,56"},
		{1000000, "€1.000.000,00"},
		{-1234.5, "-€1.234,50"},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, formatEuro(tc.in))
	}
}

func TestRender(t *testing.T) {
	ctx := context.Background()

	t.Run("writes report with totals and month rows", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome:   1500,
			TotalExpenses: 500,
			Savings:       1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000},
			},
		}

		err := New(out).Render(ctx, summary)
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, "Total Income")
		require.Contains(t, content, "€1.500,00")
		require.Contains(t, content, "€1.000,00")
		require.Contains(t, content, "May 2026")
	})

	t.Run("renders empty summary without month rows", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		err := New(out).Render(ctx, transaction.Summary{})
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "No transactions")
	})

	t.Run("returns error when path is unwritable", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "nonexistent-dir", "report.html")
		err := New(out).Render(ctx, transaction.Summary{})
		require.Error(t, err)
	})

	t.Run("renders monthly average cards", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome:   3000,
			TotalExpenses: 1200,
			Savings:       1800,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.April, Income: 1500, Expenses: 600, Savings: 900},
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 600, Savings: 900},
			},
			Averages: transaction.MonthlyAverages{
				Months: 2, Income: 1500, Expenses: 600, Savings: 900,
			},
		}

		err := New(out).Render(ctx, summary)
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, "Monthly Average")
		require.Contains(t, content, "over 2 months")
		require.Contains(t, content, "Avg Income / mo")
		require.Contains(t, content, "Avg Expenses / mo")
		require.Contains(t, content, "Avg Savings / mo")
	})

	t.Run("omits average cards when there are no transactions", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		err := New(out).Render(ctx, transaction.Summary{})
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.NotContains(t, string(data), "Monthly Average")
	})
}
