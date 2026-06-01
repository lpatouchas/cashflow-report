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

func TestApplyExclusions(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	t.Run("nil rules keep everything", func(t *testing.T) {
		in := []Transaction{tx("A", "f.csv", 10, true, d)}
		require.Equal(t, in, ApplyExclusions(in, nil))
	})

	t.Run("matching transactions are dropped", func(t *testing.T) {
		rule := func(tr Transaction) bool { return tr.SourceFile == "drop.csv" }
		in := []Transaction{
			tx("A", "keep.csv", 10, true, d),
			tx("B", "drop.csv", 20, true, d),
		}
		got := ApplyExclusions(in, []ExclusionRule{rule})
		require.Len(t, got, 1)
		require.Equal(t, "A", got[0].ID)
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		rule := func(tr Transaction) bool { return true }
		require.Empty(t, ApplyExclusions(nil, []ExclusionRule{rule}))
	})

	t.Run("transaction matching any rule is dropped", func(t *testing.T) {
		first := func(tr Transaction) bool { return tr.SourceFile == "x.csv" }
		second := func(tr Transaction) bool { return tr.ID == "B" }
		in := []Transaction{
			tx("A", "keep.csv", 10, true, d),
			tx("B", "keep.csv", 20, true, d),
		}
		got := ApplyExclusions(in, []ExclusionRule{first, second})
		require.Len(t, got, 1)
		require.Equal(t, "A", got[0].ID)
	})
}

func TestDefaultExclusionRules(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	rules := DefaultExclusionRules()

	// NOTE: copy the Description literal verbatim from transaction.go (the external
	// account move rule) — it mixes Greek and Latin look-alike letters and must
	// match byte-for-byte.
	move := Transaction{ID: "M", SourceFile: "invest.csv", Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", Amount: 100, IsDebit: true, Date: d}
	normal := Transaction{ID: "N", SourceFile: "invest.csv", Description: "DIVIDEND", Amount: 50, IsDebit: false, Date: d}

	got := ApplyExclusions([]Transaction{move, normal}, rules)
	require.Len(t, got, 1)
	require.Equal(t, "N", got[0].ID)
}

func boolPtr(b bool) *bool { return &b }

func TestRuleSpecValidate(t *testing.T) {
	require.NoError(t, RuleSpec{MatchMode: MatchExact, Description: "x"}.Validate())
	require.NoError(t, RuleSpec{MatchMode: MatchContains, Description: "x"}.Validate())

	err := RuleSpec{MatchMode: MatchExact, Description: "  "}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "description")

	err = RuleSpec{MatchMode: "regex", Description: "x"}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "match mode")
}

func TestCompileRule(t *testing.T) {
	debit := Transaction{Description: "PAY", IsDebit: true, SourceFile: "a.csv"}
	credit := Transaction{Description: "PAY", IsDebit: false, SourceFile: "b.csv"}

	// exact description only (isDebit any, no source)
	r := CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY"})
	require.True(t, r(debit))
	require.True(t, r(credit))
	require.False(t, r(Transaction{Description: "PAYMENT"}))

	// contains
	r = CompileRule(RuleSpec{MatchMode: MatchContains, Description: "AY"})
	require.True(t, r(Transaction{Description: "PAYMENT"}))
	require.False(t, r(Transaction{Description: "NOPE"}))

	// debit-only
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", IsDebit: boolPtr(true)})
	require.True(t, r(debit))
	require.False(t, r(credit))

	// credit-only
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", IsDebit: boolPtr(false)})
	require.False(t, r(debit))
	require.True(t, r(credit))

	// source-file scoped
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", SourceFile: "a.csv"})
	require.True(t, r(debit))
	require.False(t, r(credit)) // credit is on b.csv
}

func TestDefaultRuleSpecs(t *testing.T) {
	specs := DefaultRuleSpecs()
	require.Len(t, specs, 1)
	require.NoError(t, specs[0].Validate())

	rules := CompileRules(specs)
	hit := Transaction{Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", IsDebit: true, SourceFile: "invest.csv"}
	miss := Transaction{Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", IsDebit: true, SourceFile: "other.csv"}
	require.True(t, rules[0](hit))
	require.False(t, rules[0](miss))
	creditMiss := Transaction{Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", IsDebit: false, SourceFile: "invest.csv"}
	require.False(t, rules[0](creditMiss))
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

	t.Run("attaches each transaction to its month", func(t *testing.T) {
		a := tx("a", "f", 50, false, may)
		b := tx("b", "f", 20, true, may2)
		c := tx("c", "f", 200, false, apr)

		got := Summarize([]Transaction{a, b, c})
		require.Len(t, got.ByMonth, 2)

		// May is newest, so ByMonth[0]; it holds the two May movements.
		require.Equal(t, time.May, got.ByMonth[0].Month)
		require.ElementsMatch(t, []Transaction{a, b}, got.ByMonth[0].Transactions)

		// April holds only c.
		require.Equal(t, time.April, got.ByMonth[1].Month)
		require.Equal(t, []Transaction{c}, got.ByMonth[1].Transactions)
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
		require.InDelta(t, 10, got.Averages.Income, 0.001)  // 60 / 6
		require.InDelta(t, 0, got.Averages.Expenses, 0.001) // 0 / 6
		require.InDelta(t, 10, got.Averages.Savings, 0.001) // (60-0) / 6
	})

	t.Run("breaks each month down by account, sorted by source", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "zeta.csv", 100, false, may),  // income, account zeta
			tx("b", "zeta.csv", 40, true, may2),   // expense, account zeta
			tx("c", "alpha.csv", 200, false, may), // income, account alpha
			tx("d", "alpha.csv", 10, true, apr),   // expense in a different month
		})

		require.Len(t, got.ByMonth, 2)

		// May is newest (ByMonth[0]); alpha sorts before zeta.
		mayMB := got.ByMonth[0]
		require.Equal(t, time.May, mayMB.Month)
		require.Len(t, mayMB.ByAccount, 2)

		require.Equal(t, "alpha.csv", mayMB.ByAccount[0].Source)
		require.InDelta(t, 200, mayMB.ByAccount[0].Income, 0.001)
		require.InDelta(t, 0, mayMB.ByAccount[0].Expenses, 0.001)

		require.Equal(t, "zeta.csv", mayMB.ByAccount[1].Source)
		require.InDelta(t, 100, mayMB.ByAccount[1].Income, 0.001)
		require.InDelta(t, 40, mayMB.ByAccount[1].Expenses, 0.001)

		// April holds only alpha's expense.
		aprMB := got.ByMonth[1]
		require.Equal(t, time.April, aprMB.Month)
		require.Len(t, aprMB.ByAccount, 1)
		require.Equal(t, "alpha.csv", aprMB.ByAccount[0].Source)
		require.InDelta(t, 0, aprMB.ByAccount[0].Income, 0.001)
		require.InDelta(t, 10, aprMB.ByAccount[0].Expenses, 0.001)
	})
}
