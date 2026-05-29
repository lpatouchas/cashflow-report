package transaction

import (
	"sort"
	"time"
)

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
	Averages      MonthlyAverages
}

// MonthlyAverages holds per-month averages over the report's calendar span
// (earliest to latest transaction month, inclusive; empty months count as zero).
type MonthlyAverages struct {
	Months   int // calendar-span divisor; 0 when there are no transactions
	Income   float64
	Expenses float64
	Savings  float64
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

// Summarize aggregates transactions into totals and a per-month breakdown
// ordered newest month first. Debits are expenses; credits are income.
func Summarize(txns []Transaction) Summary {
	type key struct {
		year  int
		month time.Month
	}
	months := make(map[key]*MonthlyBreakdown)

	var s Summary
	var earliest, latest time.Time
	for i, t := range txns {
		if t.IsDebit {
			s.TotalExpenses += t.Amount
		} else {
			s.TotalIncome += t.Amount
		}

		if i == 0 || t.Date.Before(earliest) {
			earliest = t.Date
		}
		if i == 0 || t.Date.After(latest) {
			latest = t.Date
		}

		k := key{t.Date.Year(), t.Date.Month()}
		mb := months[k]
		if mb == nil {
			mb = &MonthlyBreakdown{Year: k.year, Month: k.month}
			months[k] = mb
		}
		if t.IsDebit {
			mb.Expenses += t.Amount
		} else {
			mb.Income += t.Amount
		}
	}
	s.Savings = s.TotalIncome - s.TotalExpenses

	for _, mb := range months {
		mb.Savings = mb.Income - mb.Expenses
		s.ByMonth = append(s.ByMonth, *mb)
	}
	sort.Slice(s.ByMonth, func(i, j int) bool {
		if s.ByMonth[i].Year != s.ByMonth[j].Year {
			return s.ByMonth[i].Year > s.ByMonth[j].Year
		}
		return s.ByMonth[i].Month > s.ByMonth[j].Month
	})

	if len(txns) > 0 {
		span := (latest.Year()*12 + int(latest.Month())) -
			(earliest.Year()*12 + int(earliest.Month())) + 1
		s.Averages = MonthlyAverages{
			Months:   span,
			Income:   s.TotalIncome / float64(span),
			Expenses: s.TotalExpenses / float64(span),
			Savings:  s.Savings / float64(span),
		}
	}

	return s
}
