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
		require.Contains(t, content, "€600,00") // avg expenses, euro-formatted
	})

	t.Run("uses singular month wording for a single month", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome: 1000, Savings: 1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1000, Savings: 1000},
			},
			Averages: transaction.MonthlyAverages{Months: 1, Income: 1000, Savings: 1000},
		}

		err := New(out).Render(ctx, summary)
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "over 1 month<")
	})

	t.Run("renders trend, best/lean tags and scaled rate bars", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		// Rising expenses across months -> upward trend. February is the best
		// savings month; March is the leanest (negative savings) and also has
		// no income, exercising the rate guard.
		summary := transaction.Summary{
			TotalIncome:   4000,
			TotalExpenses: 2400,
			Savings:       1600,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.April, Income: 2000, Expenses: 1800, Savings: 200},
				{Year: 2026, Month: time.March, Income: 0, Expenses: 100, Savings: -100},
				{Year: 2026, Month: time.February, Income: 2000, Expenses: 500, Savings: 1500},
			},
			Averages: transaction.MonthlyAverages{
				Months: 3, Income: 1333.33, Expenses: 800, Savings: 533.33,
			},
		}

		require.NoError(t, New(out).Render(ctx, summary))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, "trending up") // rising expenses
		require.Contains(t, content, `<span class="tag best">best</span>`)
		require.Contains(t, content, `<span class="tag worst">lean</span>`)
		require.Contains(t, content, "Best month Feb · leanest Mar")
		require.Contains(t, content, "width: 100.0%") // best month fills the bar
		require.Contains(t, content, "0,0 %")         // zero-income month rate
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
