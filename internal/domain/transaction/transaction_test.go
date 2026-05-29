package transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func tx(id, file string, amount float64, debit bool, date time.Time) Transaction {
	return Transaction{ID: id, SourceFile: file, Amount: amount, IsDebit: debit, Date: date}
}

func TestFilterTransfers(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   []Transaction
		wantIDs []string
	}{
		{
			name:    "empty input",
			input:   nil,
			wantIDs: nil,
		},
		{
			name: "all unique are kept",
			input: []Transaction{
				tx("A", "f1.csv", 10, true, d),
				tx("B", "f2.csv", 20, false, d),
			},
			wantIDs: []string{"A", "B"},
		},
		{
			name: "cross-file transfer is excluded",
			input: []Transaction{
				tx("T", "f1.csv", 100, true, d),
				tx("T", "f2.csv", 100, false, d),
				tx("K", "f1.csv", 5, true, d),
			},
			wantIDs: []string{"K"},
		},
		{
			name: "single-file duplicate is excluded",
			input: []Transaction{
				tx("D", "f1.csv", 7, true, d),
				tx("D", "f1.csv", 7, true, d),
				tx("U", "f1.csv", 9, false, d),
			},
			wantIDs: []string{"U"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterTransfers(tc.input)
			var gotIDs []string
			for _, x := range got {
				gotIDs = append(gotIDs, x.ID)
			}
			require.Equal(t, tc.wantIDs, gotIDs)
		})
	}
}

func TestSummarize(t *testing.T) {
	may := time.Date(2026, time.May, 10, 0, 0, 0, 0, time.UTC)
	may2 := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	apr := time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC)

	t.Run("empty input yields zero summary", func(t *testing.T) {
		got := Summarize(nil)
		require.Equal(t, Summary{}, got)
	})

	t.Run("aggregates totals and savings", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "f", 100, false, may), // income
			tx("b", "f", 30, true, may),   // expense
		})
		require.InDelta(t, 100, got.TotalIncome, 0.001)
		require.InDelta(t, 30, got.TotalExpenses, 0.001)
		require.InDelta(t, 70, got.Savings, 0.001)
	})

	t.Run("groups by month sorted newest first", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "f", 50, false, may),
			tx("b", "f", 20, true, may2),
			tx("c", "f", 200, false, apr),
		})
		require.Len(t, got.ByMonth, 2)

		require.Equal(t, 2026, got.ByMonth[0].Year)
		require.Equal(t, time.May, got.ByMonth[0].Month)
		require.InDelta(t, 50, got.ByMonth[0].Income, 0.001)
		require.InDelta(t, 20, got.ByMonth[0].Expenses, 0.001)
		require.InDelta(t, 30, got.ByMonth[0].Savings, 0.001)

		require.Equal(t, time.April, got.ByMonth[1].Month)
		require.InDelta(t, 200, got.ByMonth[1].Income, 0.001)
		require.InDelta(t, 0, got.ByMonth[1].Expenses, 0.001)
		require.InDelta(t, 200, got.ByMonth[1].Savings, 0.001)
	})

	t.Run("sorts across years newest first", func(t *testing.T) {
		dec2025 := time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC)
		got := Summarize([]Transaction{
			tx("a", "f", 10, false, may),
			tx("b", "f", 5, false, dec2025),
		})
		require.Len(t, got.ByMonth, 2)
		require.Equal(t, 2026, got.ByMonth[0].Year)
		require.Equal(t, time.May, got.ByMonth[0].Month)
		require.Equal(t, 2025, got.ByMonth[1].Year)
		require.Equal(t, time.December, got.ByMonth[1].Month)
	})

	t.Run("single month averages equal totals", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "f", 100, false, may),
			tx("b", "f", 30, true, may2),
		})
		require.Equal(t, 1, got.Averages.Months)
		require.InDelta(t, 100, got.Averages.Income, 0.001)
		require.InDelta(t, 30, got.Averages.Expenses, 0.001)
		require.InDelta(t, 70, got.Averages.Savings, 0.001)
	})

	t.Run("gap month counts in the calendar span", func(t *testing.T) {
		mar := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)
		// March + May, no April: span is 3 months.
		got := Summarize([]Transaction{
			tx("a", "f", 300, false, mar),
			tx("b", "f", 600, true, may),
		})
		require.Equal(t, 3, got.Averages.Months)
		require.InDelta(t, 100, got.Averages.Income, 0.001)   // 300 / 3
		require.InDelta(t, 200, got.Averages.Expenses, 0.001) // 600 / 3
		require.InDelta(t, -100, got.Averages.Savings, 0.001) // (300-600)/3
	})

	t.Run("calendar span crosses a year boundary", func(t *testing.T) {
		dec2025 := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)
		// Dec 2025 .. May 2026 inclusive = 6 months.
		got := Summarize([]Transaction{
			tx("a", "f", 60, false, dec2025),
			tx("b", "f", 0, true, may),
		})
		require.Equal(t, 6, got.Averages.Months)
		require.InDelta(t, 10, got.Averages.Income, 0.001) // 60 / 6
		require.InDelta(t, 0, got.Averages.Expenses, 0.001)  // 0 / 6
		require.InDelta(t, 10, got.Averages.Savings, 0.001)  // (60-0) / 6
	})
}
