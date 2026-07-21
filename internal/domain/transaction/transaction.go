package transaction

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	Branch      string    // Κατάστημα (bank col 3); "" for VISA rows
	IsVISA      bool      // true for rows parsed from a VISA file
	Category    string    // Κατηγορία δαπάνης (VISA col 2, e.g. "Supermarket / Διατροφή"); "" for bank rows
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

// matchKey identifies transactions that represent the same underlying movement.
// Two transactions collide when they share an ID and the same amount (to the
// cent). Transaction IDs are unique per movement, so the date is deliberately
// not part of the key: a bank may record the two legs of one transfer on
// different days, and including the date would split that single movement into
// two groups and miss it.
type matchKey struct {
	id    string
	cents int64
}

// amountCents rounds an amount to whole cents for robust comparison, collapsing
// float-representation noise (e.g. 100.00 vs 100.001) onto a single value.
func amountCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// keyOf builds the composite match key for a transaction.
func keyOf(t Transaction) matchKey {
	return matchKey{id: t.ID, cents: amountCents(t.Amount)}
}

// FilterTransfers removes inter-account transfers and duplicate anomalies.
//
// Transactions are grouped by (ID, amount-in-cents). Any group with more than
// one member is dropped entirely; only transactions whose key is unique are
// returned. A collision is one of two kinds, distinguished solely by direction —
// both are excluded:
//
//   - Inter-account transfer: two legs with opposite direction (one debit, one
//     credit). The money leaves one account and enters another, possibly posting
//     on different days; both legs carry the same unique ID.
//   - Duplicate anomaly: two or more records with the same direction. A repeated
//     export row.
//
// Transactions that share only an ID but differ in amount are unrelated and kept.
func FilterTransfers(txns []Transaction) []Transaction {
	counts := make(map[matchKey]int, len(txns))
	for _, t := range txns {
		counts[keyOf(t)]++
	}
	var kept []Transaction
	for _, t := range txns {
		if counts[keyOf(t)] == 1 {
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

// ReconcileConfig configures VISA lump reconciliation. A bank row is a VISA
// lump when its Description matches per MatchMode AND its Branch equals Branch.
// An empty MatchMode means exact.
type ReconcileConfig struct {
	Description string    `json:"description"`
	MatchMode   MatchMode `json:"matchMode"`
	Branch      string    `json:"branch"`
}

// Validate reports whether the reconcile config is well-formed.
func (c ReconcileConfig) Validate() error {
	if strings.TrimSpace(c.Description) == "" {
		return errors.New("description is required")
	}
	switch c.MatchMode {
	case "", MatchExact, MatchContains:
		return nil
	default:
		return fmt.Errorf("unknown match mode %q (use %q or %q)", c.MatchMode, MatchExact, MatchContains)
	}
}

// descriptionMatches reports whether desc satisfies the config's description
// rule. An empty MatchMode is treated as exact.
func (c ReconcileConfig) descriptionMatches(desc string) bool {
	if c.MatchMode == MatchContains {
		return strings.Contains(desc, c.Description)
	}
	return desc == c.Description
}

// DefaultRuleSpecs is the built-in rule set expressed as data: the single
// "external account move" rule (an instant-transfer debit on invest.csv).
func DefaultRuleSpecs() []RuleSpec {
	debit := true
	return []RuleSpec{{
		MatchMode:   MatchExact,
		IsDebit:     &debit,
		Description: "SAMPLE DESCRIPTION",
		SourceFile:  "account.csv",
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

// ReconcileVISA replaces each month's matched VISA lump payment(s) with the
// itemized VISA purchases behind them, plus a single VISA LEFTOVERS row that
// preserves the month's net outflow (purchaseSum + leftover == paymentSum).
//
// It runs after FilterTransfers, on the combined bank+VISA slice. When no VISA
// purchases are present the input is returned unchanged (lumps stay intact).
func ReconcileVISA(txns []Transaction, cfg ReconcileConfig) []Transaction {
	// Itemized purchases (the parser keeps only negative VISA rows).
	var purchases []Transaction
	for _, t := range txns {
		if t.IsVISA {
			purchases = append(purchases, t)
		}
	}
	if len(purchases) == 0 {
		return txns // §5: lumps present, no VISA file — leave untouched.
	}

	// Step 1 — identify lumps among bank rows; warn on partial matches.
	isLump := make([]bool, len(txns))
	lumpTotals := make(map[string]int64) // SourceFile -> total cents
	for i, t := range txns {
		if t.IsVISA {
			continue
		}
		descMatch := cfg.descriptionMatches(t.Description)
		branchMatch := t.Branch == cfg.Branch
		switch {
		case descMatch && branchMatch:
			isLump[i] = true
			lumpTotals[t.SourceFile] += amountCents(t.Amount)
		case branchMatch && !descMatch:
			slog.Warn("branch matches VISA config but description does not; not treating as a VISA lump",
				"date", t.Date.Format("2006-01-02"), "description", t.Description, "branch", t.Branch, "amount", t.Amount)
		case descMatch && !branchMatch:
			slog.Warn("description matches VISA config but branch does not; not treating as a VISA lump",
				"date", t.Date.Format("2006-01-02"), "description", t.Description, "branch", t.Branch, "amount", t.Amount)
		}
	}

	payingAccount := payingAccountFrom(lumpTotals)

	type ym struct {
		year  int
		month time.Month
	}

	// Bank non-lump rows pass through untouched.
	out := make([]Transaction, 0, len(txns))
	for i, t := range txns {
		if t.IsVISA || isLump[i] {
			continue
		}
		out = append(out, t)
	}

	// Re-tagged purchases; accumulate per-month purchase sums.
	purchaseSum := make(map[ym]int64)
	for _, p := range purchases {
		p.SourceFile = payingAccount
		out = append(out, p)
		purchaseSum[ym{p.Date.Year(), p.Date.Month()}] += amountCents(p.Amount)
	}

	// Per-month payment sums and the last lump date in each month.
	paymentSum := make(map[ym]int64)
	lastLump := make(map[ym]time.Time)
	for i, t := range txns {
		if !isLump[i] {
			continue
		}
		k := ym{t.Date.Year(), t.Date.Month()}
		paymentSum[k] += amountCents(t.Amount)
		if d, ok := lastLump[k]; !ok || t.Date.After(d) {
			lastLump[k] = t.Date
		}
	}

	// Union of months, sorted for deterministic output.
	monthSet := make(map[ym]bool)
	for k := range purchaseSum {
		monthSet[k] = true
	}
	for k := range paymentSum {
		monthSet[k] = true
	}
	months := make([]ym, 0, len(monthSet))
	for k := range monthSet {
		months = append(months, k)
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].year != months[j].year {
			return months[i].year < months[j].year
		}
		return months[i].month < months[j].month
	})

	for _, k := range months {
		leftover := paymentSum[k] - purchaseSum[k]
		if leftover == 0 {
			continue // §7
		}
		row := Transaction{
			Description: "VISA LEFTOVERS",
			SourceFile:  payingAccount,
			ID:          fmt.Sprintf("VISA-LEFTOVERS-%04d-%02d", k.year, int(k.month)),
		}
		if leftover > 0 {
			row.IsDebit = true
			row.Amount = float64(leftover) / 100
		} else {
			row.IsDebit = false
			row.Amount = float64(-leftover) / 100
		}
		if d, ok := lastLump[k]; ok {
			row.Date = d
		} else {
			// No payment this month: date on the month's last day.
			row.Date = time.Date(k.year, k.month+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		}
		out = append(out, row)
	}
	return out
}

// payingAccountFrom picks the SourceFile carrying the largest lump total,
// warning about the rest. With no lumps it falls back to the label "VISA".
func payingAccountFrom(lumpTotals map[string]int64) string {
	if len(lumpTotals) == 0 {
		slog.Warn("VISA purchases found but no matching bank lump; attributing to \"VISA\"")
		return "VISA"
	}
	files := make([]string, 0, len(lumpTotals))
	for f := range lumpTotals {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if lumpTotals[files[i]] != lumpTotals[files[j]] {
			return lumpTotals[files[i]] > lumpTotals[files[j]] // largest first
		}
		return files[i] < files[j] // stable tie-break
	})
	for _, f := range files[1:] {
		slog.Warn("multiple bank files carry VISA lumps; consolidating all VISA activity onto the largest-total account",
			"consolidatedInto", files[0], "absorbedFile", f, "absorbedTotal", float64(lumpTotals[f])/100)
	}
	return files[0]
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
