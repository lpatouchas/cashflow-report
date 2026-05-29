# Monthly Average Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-month average (income, expenses, savings) over the data's full calendar span and show it as a second row of cards in the HTML finance report.

**Architecture:** The average is computed in `domain.Summarize` as part of the existing aggregation and carried on a new `Summary.Averages` field; the HTML renderer displays it as cards. No changes to the CSV repo, the app service, the `Renderer` port, or `main.go` — the new field flows through the existing `Summary` value.

**Tech Stack:** Go 1.23, `html/template`, testify (`require`), table-driven tests. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-30-monthly-average-design.md`

---

## File Structure

- `internal/domain/transaction/transaction.go` — add `MonthlyAverages` type, add `Averages` field to `Summary`, extend `Summarize`.
- `internal/domain/transaction/transaction_test.go` — extend `TestSummarize` with average cases.
- `internal/infra/html/renderer.go` — add the average-cards block to the template.
- `internal/infra/html/renderer_test.go` — assert average cards render and are omitted when empty.

---

## Task 1: Domain — compute monthly averages in `Summarize`

**Files:**
- Modify: `internal/domain/transaction/transaction.go`
- Test: `internal/domain/transaction/transaction_test.go`

- [ ] **Step 1: Write the failing tests**

Add these subtests inside the existing `TestSummarize` function in `internal/domain/transaction/transaction_test.go` (after the "sorts across years oldest first" subtest, before the closing `}` of `TestSummarize`). They reuse the `tx` helper and the `may`, `may2`, `apr` dates already defined at the top of `TestSummarize`.

```go
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
	})
```

The existing "empty input yields zero summary" subtest (`require.Equal(t, Summary{}, got)`) already covers the empty case: with no transactions, `Averages` must stay the zero `MonthlyAverages{}`. Do not modify it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/transaction/ -run TestSummarize -v`
Expected: FAIL — compile error `got.Averages undefined (type Summary has no field or method Averages)`.

- [ ] **Step 3: Add the `MonthlyAverages` type and `Summary` field**

In `internal/domain/transaction/transaction.go`, add the new type after the `Summary` struct and add the `Averages` field to `Summary`. The updated `Summary` and new type:

```go
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
```

- [ ] **Step 4: Extend `Summarize` to compute the averages**

In `internal/domain/transaction/transaction.go`, replace the body of `Summarize` with the version below. The changes: track `earliest`/`latest` dates during the loop, and after sorting compute the calendar span and averages when there is at least one transaction.

```go
// Summarize aggregates transactions into totals and a chronological
// per-month breakdown. Debits are expenses; credits are income.
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
			return s.ByMonth[i].Year < s.ByMonth[j].Year
		}
		return s.ByMonth[i].Month < s.ByMonth[j].Month
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/domain/transaction/ -v`
Expected: PASS — all `TestSummarize` subtests (including the three new ones and the unchanged "empty input yields zero summary") and `TestFilterTransfers` pass.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: compute monthly averages over calendar span"
```

---

## Task 2: HTML renderer — show average cards

**Files:**
- Modify: `internal/infra/html/renderer.go`
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing tests**

Add two subtests inside the existing `TestRender` function in `internal/infra/html/renderer_test.go`, after the "writes report with totals and month rows" subtest.

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: FAIL — "renders monthly average cards" fails because the output does not yet contain "Monthly Average".

- [ ] **Step 3: Add the average-cards block to the template**

In `internal/infra/html/renderer.go`, in the `reportHTML` constant, insert the block below immediately after the existing totals `</div>` (the line closing `<div class="cards">` that contains Total Income/Expenses/Savings) and before the `{{ if .Summary.ByMonth }}` line. `gt` is a built-in `html/template` function, so no new `FuncMap` entry is needed.

```html
{{ if gt .Summary.Averages.Months 0 }}
<h2>Monthly Average <small>(over {{ .Summary.Averages.Months }} months)</small></h2>
<div class="cards">
  <div class="card"><div>Avg Income / mo</div><div class="value">{{ euro .Summary.Averages.Income }}</div></div>
  <div class="card"><div>Avg Expenses / mo</div><div class="value">{{ euro .Summary.Averages.Expenses }}</div></div>
  <div class="card savings"><div>Avg Savings / mo</div><div class="value">{{ euro .Summary.Averages.Savings }}</div></div>
</div>
{{ end }}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/html/ -v`
Expected: PASS — both new subtests plus the existing `TestFormatEuro` and the other `TestRender` subtests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: render monthly average cards in HTML report"
```

---

## Task 3: Full build, test, and coverage check

**Files:** none (verification only)

- [ ] **Step 1: Build the whole module**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2: Run the full test suite with coverage**

Run: `go test ./... -cover`
Expected: PASS for all packages; `internal/domain/transaction` and `internal/infra/html` coverage at or near 100% (the project requires close-to-100% coverage).

- [ ] **Step 3: Generate the report end-to-end (smoke check)**

Run: `go run . && echo "wrote report.html"`
Expected: logs "report generated" and writes `report.html`. With sample CSVs in `./data`, the report contains a "Monthly Average" section; with an empty `./data`, it omits that section and shows "No transactions to report." (`report.html` is git-ignored output — do not commit it.)

---

## Self-Review

- **Spec coverage:** Averages of income/expenses/savings (Task 1 type + computation); full-calendar-span divisor with empty months as zero (Task 1 span arithmetic + gap-month test); divide-by-zero guard for empty input (Task 1 `len(txns) > 0` guard + unchanged empty-summary test); average cards as a second card row with "over N months" subtitle (Task 2 template + tests); omitted when no transactions (Task 2 omit test). All spec sections map to a task.
- **Type consistency:** `MonthlyAverages{Months, Income, Expenses, Savings}` and `Summary.Averages` are defined in Task 1 Step 3 and referenced identically in Task 1 tests and Task 2 (`transaction.MonthlyAverages`, `.Summary.Averages.Months/Income/Expenses/Savings`). Template uses `gt .Summary.Averages.Months 0`, matching the `int` `Months` field.
- **Placeholder scan:** No TBD/TODO; every code and command step shows concrete content and expected output.
