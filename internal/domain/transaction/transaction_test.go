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
		{
			name: "same id different amount are kept",
			input: []Transaction{
				tx("X", "f1.csv", 10, true, d),
				tx("X", "f2.csv", 20, false, d),
			},
			wantIDs: []string{"X", "X"},
		},
		{
			// The date is not part of the match key, so a transfer whose two
			// legs post on different days still collides on (ID, amount) and is
			// excluded.
			name: "same id same amount different date are excluded",
			input: []Transaction{
				tx("Y", "f1.csv", 10, true, d),
				tx("Y", "f2.csv", 10, false, d.AddDate(0, 0, 1)),
			},
			wantIDs: nil,
		},
		{
			// 100.00 and 100.001 both round to 10000 cents, so they share a
			// match key and are excluded as a transfer/duplicate pair.
			name: "float-noise amounts still excluded",
			input: []Transaction{
				tx("Z", "f1.csv", 100.00, true, d),
				tx("Z", "f2.csv", 100.001, false, d),
			},
			wantIDs: nil,
		},
		{
			// Guards the "count == 1" logic against groups larger than a pair:
			// every member of a 3-way collision must be dropped.
			name: "triple duplicate is fully excluded",
			input: []Transaction{
				tx("D3", "f1.csv", 7, true, d),
				tx("D3", "f1.csv", 7, true, d),
				tx("D3", "f1.csv", 7, true, d),
			},
			wantIDs: nil,
		},
		{
			// Rounding must not over-collapse: a genuine one-cent difference
			// stays distinct, so neither row is treated as a pair.
			name: "amounts one cent apart are kept",
			input: []Transaction{
				tx("C", "f1.csv", 100.00, true, d),
				tx("C", "f2.csv", 100.01, false, d),
			},
			wantIDs: []string{"C", "C"},
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

	// One bank reference can carry a matched transfer pair (same amount,
	// opposite legs) AND the order fee (a different amount). The two legs
	// collide on (ID, amount) and drop; the fee has a unique key and survives
	// as a real expense. This is the real-world shape the old ID-only rule got
	// wrong — it dropped the fee too. The wantIDs-only table can't assert which
	// row survives (all three share an ID), so check the amount directly.
	t.Run("transfer pair and its fee share one id: legs drop, fee survives", func(t *testing.T) {
		d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
		got := FilterTransfers([]Transaction{
			tx("M", "checking.csv", 800, true, d),  // transfer leg
			tx("M", "savings.csv", 800, false, d),  // matching leg (opposite sign)
			tx("M", "checking.csv", 0.50, true, d), // the order fee
		})
		require.Len(t, got, 1)
		require.Equal(t, 0.50, got[0].Amount)
	})
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

	// move matches the built-in default rule (see DefaultRuleSpecs in transaction.go);
	// normal does not and must survive.
	move := Transaction{ID: "M", SourceFile: "account.csv", Description: "SAMPLE DESCRIPTION", Amount: 100, IsDebit: true, Date: d}
	normal := Transaction{ID: "N", SourceFile: "account.csv", Description: "DIVIDEND", Amount: 50, IsDebit: false, Date: d}

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

func TestCompileRuleContainsToleratesLookalike(t *testing.T) {
	// Rule typed with Latin "VISA"; transaction description carries Greek
	// lookalikes "VΙSΑ" (Greek Ι, Α). Contains match must still fire.
	spec := RuleSpec{MatchMode: MatchContains, Description: "VISA"}
	rule := CompileRule(spec)
	txn := Transaction{Description: "MONTHLY VΙSΑ PAYMENT"}
	if !rule(txn) {
		t.Errorf("contains rule should match across Greek↔Latin lookalikes")
	}
}

func TestCompileRuleExactToleratesLookalike(t *testing.T) {
	spec := RuleSpec{MatchMode: MatchExact, Description: "VISA"}
	rule := CompileRule(spec)
	txn := Transaction{Description: "VΙSΑ"} // Greek Ι, Α
	if !rule(txn) {
		t.Errorf("exact rule should match across Greek↔Latin lookalikes")
	}
}

func TestCompileRuleStillRejectsDifferentText(t *testing.T) {
	// Folding must not create false matches: digit 0 vs letter O stays distinct.
	spec := RuleSpec{MatchMode: MatchExact, Description: "COOP"}
	rule := CompileRule(spec)
	if rule(Transaction{Description: "CO0P"}) {
		t.Errorf("exact rule must not match CO0P against COOP")
	}
}

func TestDefaultRuleSpecs(t *testing.T) {
	specs := DefaultRuleSpecs()
	require.Len(t, specs, 1)
	require.NoError(t, specs[0].Validate())

	rules := CompileRules(specs)
	hit := Transaction{Description: "SAMPLE DESCRIPTION", IsDebit: true, SourceFile: "account.csv"}
	miss := Transaction{Description: "SAMPLE DESCRIPTION", IsDebit: true, SourceFile: "other.csv"}
	require.True(t, rules[0](hit))
	require.False(t, rules[0](miss))
	creditMiss := Transaction{Description: "SAMPLE DESCRIPTION", IsDebit: false, SourceFile: "account.csv"}
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

func TestReconcileConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ReconcileConfig
		wantErr bool
	}{
		{"exact ok", ReconcileConfig{Description: "X", MatchMode: MatchExact, Branch: "96"}, false},
		{"contains ok", ReconcileConfig{Description: "X", MatchMode: MatchContains, Branch: "96"}, false},
		{"empty mode defaults to exact", ReconcileConfig{Description: "X", Branch: "96"}, false},
		{"missing description", ReconcileConfig{MatchMode: MatchExact, Branch: "96"}, true},
		{"blank description", ReconcileConfig{Description: "   ", MatchMode: MatchExact}, true},
		{"unknown mode", ReconcileConfig{Description: "X", MatchMode: "fuzzy"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// visaLumpDesc is the exact mixed-script bank description "ΠΛΗΡΩΜΗ VΙSA".
const visaLumpDesc = "ΠΛΗΡΩΜΗ VΙSΑ"

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func reconcileCfg() ReconcileConfig {
	return ReconcileConfig{Description: visaLumpDesc, MatchMode: MatchExact, Branch: "96"}
}

func lumpTx(amount float64, date time.Time, file string) Transaction {
	return Transaction{Description: visaLumpDesc, Branch: "96", Amount: amount, IsDebit: true, Date: date, SourceFile: file}
}

func purchaseTx(desc string, amount float64, date time.Time) Transaction {
	return Transaction{Description: desc, Amount: amount, IsDebit: true, Date: date, SourceFile: "visa.csv", IsVISA: true}
}

// findLeftover returns the single VISA LEFTOVERS row for the given month, or a
// zero Transaction with ok=false when none was emitted.
func findLeftover(txns []Transaction, y int, m time.Month) (Transaction, bool) {
	for _, t := range txns {
		if t.Description == "VISA LEFTOVERS" && t.Date.Year() == y && t.Date.Month() == m {
			return t, true
		}
	}
	return Transaction{}, false
}

func TestReconcileVISA(t *testing.T) {
	t.Run("no VISA purchases leaves lumps untouched (§5)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			{ID: "R", Description: "RENT", Amount: 600, IsDebit: true, Date: day(2025, time.July, 1), SourceFile: "checking.csv", Branch: "99"},
		}
		out := ReconcileVISA(in, reconcileCfg())
		require.Equal(t, in, out)
	})

	t.Run("single month: positive leftover is a debit dated the lump date", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("SHOP", 50, day(2025, time.July, 3)),
			purchaseTx("CAFE", 20, day(2025, time.July, 10)),
			purchaseTx("GADGET", 150, day(2025, time.July, 14)),
		}
		out := ReconcileVISA(in, reconcileCfg())

		// lump removed
		for _, o := range out {
			require.NotEqual(t, visaLumpDesc, o.Description)
		}
		// purchases re-tagged onto the paying account
		var purchases int
		for _, o := range out {
			if o.IsVISA {
				require.Equal(t, "checking.csv", o.SourceFile)
				purchases++
			}
		}
		require.Equal(t, 3, purchases)
		// leftover = 300 - 220 = 80, debit, dated the lump date
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.True(t, lo.IsDebit)
		require.InDelta(t, 80, lo.Amount, 0.001)
		require.Equal(t, day(2025, time.July, 15), lo.Date)
		require.Equal(t, "checking.csv", lo.SourceFile)
	})

	t.Run("purchases but no lump that month: negative leftover is a credit on month-end (§2)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"), // sets paying account
			purchaseTx("SHOP", 300, day(2025, time.July, 3)),
			purchaseTx("AUG-BUY", 100, day(2025, time.August, 4)), // no August lump
		}
		out := ReconcileVISA(in, reconcileCfg())

		// July nets to zero leftover -> no row
		_, july := findLeftover(out, 2025, time.July)
		require.False(t, july)
		// August: leftover = 0 - 100 = -100 -> credit, dated 31 Aug
		aug, ok := findLeftover(out, 2025, time.August)
		require.True(t, ok)
		require.False(t, aug.IsDebit)
		require.InDelta(t, 100, aug.Amount, 0.001)
		require.Equal(t, day(2025, time.August, 31), aug.Date)
	})

	t.Run("lump but no purchases that month: whole amount is a leftover debit (§1)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.June, 20), "checking.csv"),
			purchaseTx("JULY-BUY", 50, day(2025, time.July, 2)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		june, ok := findLeftover(out, 2025, time.June)
		require.True(t, ok)
		require.True(t, june.IsDebit)
		require.InDelta(t, 300, june.Amount, 0.001)
		require.Equal(t, day(2025, time.June, 20), june.Date)
	})

	t.Run("no bank lump anywhere: purchases attributed to \"VISA\" (§4)", func(t *testing.T) {
		in := []Transaction{
			purchaseTx("SHOP", 40, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		for _, o := range out {
			if o.IsVISA {
				require.Equal(t, "VISA", o.SourceFile)
			}
		}
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.False(t, lo.IsDebit) // -40 -> credit
		require.InDelta(t, 40, lo.Amount, 0.001)
		require.Equal(t, "VISA", lo.SourceFile)
	})

	t.Run("zero leftover emits no row (§7)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(220, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("SHOP", 220, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		_, ok := findLeftover(out, 2025, time.July)
		require.False(t, ok)
	})

	t.Run("multiple lumps in one month sum", func(t *testing.T) {
		in := []Transaction{
			lumpTx(100, day(2025, time.July, 9), "checking.csv"),
			lumpTx(200, day(2025, time.July, 9), "checking.csv"),
			purchaseTx("SHOP", 220, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.InDelta(t, 80, lo.Amount, 0.001) // 300 - 220
	})

	t.Run("branch gating: right description, wrong branch is not a lump", func(t *testing.T) {
		wrong := lumpTx(300, day(2025, time.July, 15), "checking.csv")
		wrong.Branch = "12" // not 96
		in := []Transaction{wrong, purchaseTx("SHOP", 50, day(2025, time.July, 3))}
		out := ReconcileVISA(in, reconcileCfg())
		// the branch-12 row is NOT removed; it passes through untouched
		var kept bool
		for _, o := range out {
			if o.Description == visaLumpDesc && o.Branch == "12" {
				kept = true
			}
		}
		require.True(t, kept)
		// leftover = 0 - 50 = -50 (no payment matched this month)
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.False(t, lo.IsDebit)
		require.InDelta(t, 50, lo.Amount, 0.001)
	})

	t.Run("per-month invariant: purchaseSum + leftover == paymentSum", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("A", 50, day(2025, time.July, 3)),
			purchaseTx("B", 20, day(2025, time.July, 10)),
			purchaseTx("C", 150, day(2025, time.July, 14)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		var purchaseCents, leftoverCents int64
		for _, o := range out {
			if o.Date.Month() != time.July {
				continue
			}
			switch {
			case o.IsVISA:
				purchaseCents += amountCents(o.Amount)
			case o.Description == "VISA LEFTOVERS":
				if o.IsDebit {
					leftoverCents += amountCents(o.Amount)
				} else {
					leftoverCents -= amountCents(o.Amount)
				}
			}
		}
		require.Equal(t, amountCents(300), purchaseCents+leftoverCents)
	})

	t.Run("multiple bank files carry lumps: largest total wins", func(t *testing.T) {
		in := []Transaction{
			lumpTx(100, day(2025, time.July, 15), "small.csv"),
			lumpTx(300, day(2025, time.July, 15), "big.csv"),
			purchaseTx("SHOP", 50, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		for _, o := range out {
			if o.IsVISA || o.Description == "VISA LEFTOVERS" {
				require.Equal(t, "big.csv", o.SourceFile)
			}
		}
		// Both lumps are folded into the month's payment total and consolidated
		// onto the largest account: leftover = (100 + 300) - 50 = 350 debit.
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.True(t, lo.IsDebit)
		require.InDelta(t, 350, lo.Amount, 0.001)
		require.Equal(t, "big.csv", lo.SourceFile)
		// Neither raw lump survives as a passed-through row.
		for _, o := range out {
			require.NotEqual(t, visaLumpDesc, o.Description)
		}
	})

	t.Run("december leftover with no lump dates on 31 Dec (month+1 rollover)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(100, day(2025, time.July, 15), "checking.csv"), // establishes the paying account
			purchaseTx("JULY", 100, day(2025, time.July, 2)),      // July nets to zero -> no row
			purchaseTx("XMAS", 60, day(2025, time.December, 20)),  // December: no lump
		}
		out := ReconcileVISA(in, reconcileCfg())
		dec, ok := findLeftover(out, 2025, time.December)
		require.True(t, ok)
		require.False(t, dec.IsDebit) // 0 - 60 = -60 -> credit
		require.InDelta(t, 60, dec.Amount, 0.001)
		require.Equal(t, day(2025, time.December, 31), dec.Date)
	})
}

func TestReconcileDescriptionMatchesLookalike(t *testing.T) {
	// Consolidation config typed with Latin "VISA"; bank lump description
	// carries Greek lookalikes. The lump must be consolidated.
	cfg := ReconcileConfig{Description: "VISA", MatchMode: MatchContains, Branch: "HQ"}
	lump := Transaction{Description: "VΙSΑ CARD", Branch: "HQ", Amount: 100, IsDebit: true, SourceFile: "bank.csv"}
	purchase := Transaction{Description: "SHOP", Amount: 40, IsDebit: true, IsVISA: true, Date: lump.Date}
	out := ReconcileVISA([]Transaction{lump, purchase}, cfg)
	for _, txn := range out {
		if txn.Description == "VΙSΑ CARD" {
			t.Errorf("VISA lump with lookalike description should have been consolidated, not passed through")
		}
	}
}
