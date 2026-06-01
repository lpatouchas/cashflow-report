package transaction

import (
	"errors"
	"fmt"
	"sort"
	"strings"
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

// AccountBreakdown is per-source income/expense totals within one month.
type AccountBreakdown struct {
	Source   string // raw SourceFile, e.g. "kathimerinos.csv"
	Income   float64
	Expenses float64
}

// MonthlyBreakdown holds income/expenses/savings for a single calendar month,
// along with the transactions that make it up and a per-account breakdown.
type MonthlyBreakdown struct {
	Year         int
	Month        time.Month
	Income       float64
	Expenses     float64
	Savings      float64
	Transactions []Transaction
	ByAccount    []AccountBreakdown
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

// ExclusionRule reports whether a transaction should be excluded from the
// report totals. Rules are applied after transfer/duplicate filtering.
type ExclusionRule func(Transaction) bool

// MatchMode controls how a RuleSpec's Description is compared.
type MatchMode string

const (
	MatchExact    MatchMode = "exact"
	MatchContains MatchMode = "contains"
)

// RuleSpec is a serializable exclusion rule. A transaction matches when every
// specified field matches (AND); unspecified fields are wildcards.
// Description is required. IsDebit nil = any; true = debit only; false = credit
// only. SourceFile empty = all files.
// MatchMode is required; the zero value is rejected by Validate.
type RuleSpec struct {
	MatchMode   MatchMode `json:"matchMode"`
	IsDebit     *bool     `json:"isDebit,omitempty"`
	Description string    `json:"description"`
	SourceFile  string    `json:"sourceFile,omitempty"`
}

// Validate reports whether the spec is well-formed.
func (s RuleSpec) Validate() error {
	if strings.TrimSpace(s.Description) == "" {
		return errors.New("description is required")
	}
	switch s.MatchMode {
	case MatchExact, MatchContains:
		return nil
	default:
		return fmt.Errorf("unknown match mode %q (use %q or %q)", s.MatchMode, MatchExact, MatchContains)
	}
}

// CompileRule turns a spec into a predicate that ANDs its specified fields.
// Call Validate first: CompileRule assumes a valid spec and treats any mode
// other than MatchContains as an exact-description match.
func CompileRule(s RuleSpec) ExclusionRule {
	return func(t Transaction) bool {
		if s.IsDebit != nil && t.IsDebit != *s.IsDebit {
			return false
		}
		if s.MatchMode == MatchContains {
			if !strings.Contains(t.Description, s.Description) {
				return false
			}
		} else if t.Description != s.Description {
			return false
		}
		if s.SourceFile != "" && t.SourceFile != s.SourceFile {
			return false
		}
		return true
	}
}

// CompileRules compiles specs into predicates. It does not validate; validate
// specs before compiling.
func CompileRules(specs []RuleSpec) []ExclusionRule {
	rules := make([]ExclusionRule, 0, len(specs))
	for _, s := range specs {
		rules = append(rules, CompileRule(s))
	}
	return rules
}

// DefaultRuleSpecs is the built-in rule set expressed as data: the single
// "external account move" rule (an instant-transfer debit on invest.csv).
func DefaultRuleSpecs() []RuleSpec {
	debit := true
	return []RuleSpec{{
		MatchMode:   MatchExact,
		IsDebit:     &debit,
		Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS",
		SourceFile:  "invest.csv",
	}}
}

// DefaultExclusionRules are the built-in rules applied until external
// configuration replaces them. Equivalent to CompileRules(DefaultRuleSpecs()).
func DefaultExclusionRules() []ExclusionRule {
	return CompileRules(DefaultRuleSpecs())
}

// ApplyExclusions drops every transaction matching any of the rules.
// With no rules the input is returned unchanged.
func ApplyExclusions(txns []Transaction, rules []ExclusionRule) []Transaction {
	if len(rules) == 0 {
		return txns
	}
	var kept []Transaction
	for _, t := range txns {
		excluded := false
		for _, rule := range rules {
			if rule(t) {
				excluded = true
				break
			}
		}
		if !excluded {
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
	// accounts[monthKey][sourceFile] accumulates per-account totals for that month.
	accounts := make(map[key]map[string]*AccountBreakdown)

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
			accounts[k] = make(map[string]*AccountBreakdown)
		}
		if t.IsDebit {
			mb.Expenses += t.Amount
		} else {
			mb.Income += t.Amount
		}
		mb.Transactions = append(mb.Transactions, t)

		ab := accounts[k][t.SourceFile]
		if ab == nil {
			ab = &AccountBreakdown{Source: t.SourceFile}
			accounts[k][t.SourceFile] = ab
		}
		if t.IsDebit {
			ab.Expenses += t.Amount
		} else {
			ab.Income += t.Amount
		}
	}
	s.Savings = s.TotalIncome - s.TotalExpenses

	for k, mb := range months {
		mb.Savings = mb.Income - mb.Expenses
		acc := accounts[k]
		mb.ByAccount = make([]AccountBreakdown, 0, len(acc))
		for _, ab := range acc {
			mb.ByAccount = append(mb.ByAccount, *ab)
		}
		sort.Slice(mb.ByAccount, func(i, j int) bool {
			return mb.ByAccount[i].Source < mb.ByAccount[j].Source
		})
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
