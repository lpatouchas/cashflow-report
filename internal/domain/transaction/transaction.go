package transaction

import "time"

// Transaction is a single bank movement loaded from a CSV export.
// Amount is always positive; direction is carried by IsDebit.
type Transaction struct {
	ID          string    // Αρ. συναλλαγής
	Date        time.Time // Ημερομηνία
	Description string    // Αιτιολογία
	Amount      float64   // Ποσό (positive)
	IsDebit     bool      // Πρόσημο ποσού: Χ = true (expense), Π = false (income)
	SourceFile  string    // originating CSV filename
}

// Summary is the aggregated report over a set of transactions.
type Summary struct {
	TotalIncome   float64
	TotalExpenses float64
	Savings       float64 // TotalIncome - TotalExpenses
	ByMonth       []MonthlyBreakdown
}

// MonthlyBreakdown holds income/expenses/savings for a single calendar month.
type MonthlyBreakdown struct {
	Year     int
	Month    time.Month
	Income   float64
	Expenses float64
	Savings  float64
}

// FilterTransfers removes inter-account transfers and duplicate anomalies.
// Any ID appearing more than once across the input is dropped entirely;
// only transactions whose ID occurs exactly once are returned.
func FilterTransfers(txns []Transaction) []Transaction {
	counts := make(map[string]int, len(txns))
	for _, t := range txns {
		counts[t.ID]++
	}
	var kept []Transaction
	for _, t := range txns {
		if counts[t.ID] == 1 {
			kept = append(kept, t)
		}
	}
	return kept
}
