# Popup Per-Account Income/Expense Breakdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "By Account" block to the month transaction modal showing income and expenses per `.csv`, with the `.csv` extension stripped from account labels in both the new block and the transaction list's Source column.

**Architecture:** Aggregation lives in the domain (`Summarize` populates a new `MonthlyBreakdown.ByAccount` slice, sorted alphabetically). The renderer serializes a parallel per-month `FIN.acct` payload (with `.csv` stripped via an `accountLabel` helper, also applied to the transaction list's source), and the modal JS renders the block when a month opens.

**Tech Stack:** Go 1.23, `html/template`, `encoding/json`, testify. Spec: `docs/superpowers/specs/2026-05-31-popup-account-breakdown-design.md`.

---

## File Structure

- `internal/domain/transaction/transaction.go` — add `AccountBreakdown` type, `MonthlyBreakdown.ByAccount` field, per-account tally in `Summarize`.
- `internal/domain/transaction/transaction_test.go` — domain tests for `ByAccount`.
- `internal/infra/html/renderer.go` — `accountLabel` helper, `acctVM` type, `acctByMonth` payload as `FIN.acct`, strip source in `txVM`, modal markup + JS.
- `internal/infra/html/renderer_test.go` — renderer tests for `FIN.acct` and stripped source; update one existing assertion.

---

## Task 1: Domain — `AccountBreakdown` type and `ByAccount` field

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (add type near `MonthlyBreakdown`, ~line 39-46)

- [ ] **Step 1: Add the type and field**

Add the `AccountBreakdown` type and a `ByAccount` field on `MonthlyBreakdown`. Replace the existing `MonthlyBreakdown` struct (lines 37-46) with:

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: builds clean (the field is unused but valid).

- [ ] **Step 3: Commit**

```bash
git add internal/domain/transaction/transaction.go
git commit -m "feat: add AccountBreakdown type and MonthlyBreakdown.ByAccount field"
```

---

## Task 2: Domain — populate `ByAccount` in `Summarize`

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (`Summarize`, lines 71-132)
- Test: `internal/domain/transaction/transaction_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest inside `TestSummarize` in `internal/domain/transaction/transaction_test.go` (after the "attaches each transaction to its month" subtest):

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/domain/transaction/ -run 'TestSummarize/breaks_each_month' -v`
Expected: FAIL — `may.ByAccount` is empty (`Len` assertion fails).

- [ ] **Step 3: Implement per-account tally in `Summarize`**

In `Summarize`, the per-transaction loop builds each `*MonthlyBreakdown`. Track per-account totals in a map keyed by month + source, then emit sorted `ByAccount` slices.

First, inside the loop body (lines 80-106), after the block that appends to `mb.Transactions`, the income/expense tally already updates `mb.Income`/`mb.Expenses`. Add a parallel per-account map. Change the `months` map setup and loop as follows.

Replace lines 76-106 (from `months := make(...)` through the close of the `for i, t := range txns` loop) with:

```go
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
```

Then, in the block that finalizes months (lines 109-112), attach the sorted `ByAccount`. Replace:

```go
	for _, mb := range months {
		mb.Savings = mb.Income - mb.Expenses
		s.ByMonth = append(s.ByMonth, *mb)
	}
```

with:

```go
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
```

(`sort` is already imported.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/domain/transaction/ -run 'TestSummarize' -v`
Expected: PASS (all `TestSummarize` subtests, including the new one).

- [ ] **Step 5: Run the full domain package to confirm no regressions**

Run: `go test ./internal/domain/transaction/`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: populate per-account breakdown in Summarize"
```

---

## Task 3: Renderer — `accountLabel` helper and stripped transaction source

**Files:**
- Modify: `internal/infra/html/renderer.go` (add helper; apply in `buildView` `txVM` construction, ~lines 117-123)
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing helper test**

Add this test function to `internal/infra/html/renderer_test.go` (after `TestFormatEuro`):

```go
func TestAccountLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"kathimerinos.csv", "kathimerinos"},
		{"acc.CSV", "acc"},
		{"no-extension", "no-extension"},
		{"", ""},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, accountLabel(tc.in))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infra/html/ -run 'TestAccountLabel' -v`
Expected: FAIL — `undefined: accountLabel` (compile error).

- [ ] **Step 3: Implement `accountLabel`**

Add this helper to `internal/infra/html/renderer.go` (next to `monthShort`, after line 209). It strips a trailing `.csv` case-insensitively:

```go
// accountLabel renders a source filename for display, dropping a trailing
// ".csv" extension (case-insensitive). Other names pass through unchanged.
func accountLabel(src string) string {
	if len(src) >= 4 && strings.EqualFold(src[len(src)-4:], ".csv") {
		return src[:len(src)-4]
	}
	return src
}
```

(`strings` is already imported.)

- [ ] **Step 4: Apply it to the transaction list source**

In `buildView`, the `txVM` literal sets `Source: t.SourceFile` (line 122). Change it to strip the extension:

```go
				Source: accountLabel(t.SourceFile),
```

- [ ] **Step 5: Update the existing renderer test assertion**

The `"embeds per-month transactions as JSON"` subtest asserts `"src":"acc.csv"` (line 223). With stripping, the source is now `acc`. Change that line to:

```go
			require.Contains(t, content, `"src":"acc"`)
```

- [ ] **Step 6: Run the helper and renderer tests**

Run: `go test ./internal/infra/html/ -run 'TestAccountLabel|TestRender' -v`
Expected: PASS (helper passes; the updated JSON assertion passes).

- [ ] **Step 7: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: strip .csv from transaction source labels"
```

---

## Task 4: Renderer — serialize `FIN.acct` per-month payload

**Files:**
- Modify: `internal/infra/html/renderer.go` (add `acctVM`; build `acctByMonth`; add to payload, ~lines 49-56 and 108-128)
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest to `TestRender` in `internal/infra/html/renderer_test.go` (after `"embeds per-month transactions as JSON"`):

```go
	t.Run("embeds per-account breakdown as JSON", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome: 1700, TotalExpenses: 540, Savings: 1160,
			ByMonth: []transaction.MonthlyBreakdown{
				{
					Year: 2026, Month: time.May, Income: 1700, Expenses: 540, Savings: 1160,
					ByAccount: []transaction.AccountBreakdown{
						{Source: "kathimerinos.csv", Income: 200, Expenses: 540},
						{Source: "misthodosia.csv", Income: 1500, Expenses: 0},
					},
				},
			},
		}

		require.NoError(t, New(out).Render(ctx, summary))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, `"acct":{`)               // account map present
		require.Contains(t, content, `"src":"kathimerinos"`)   // .csv stripped
		require.Contains(t, content, `"src":"misthodosia"`)
		require.Contains(t, content, `"inc":1500`)             // misthodosia income
		require.Contains(t, content, `"exp":540`)              // kathimerinos expenses
	})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infra/html/ -run 'TestRender/embeds_per-account' -v`
Expected: FAIL — content does not contain `"acct":{`.

- [ ] **Step 3: Add the `acctVM` view model**

Add to `internal/infra/html/renderer.go` after the `txVM` type (after line 56):

```go
// acctVM is one account's income/expense totals inside a month's modal, serialized to JS.
type acctVM struct {
	Source   string  `json:"src"` // display label, .csv stripped
	Income   float64 `json:"inc"`
	Expenses float64 `json:"exp"`
}
```

- [ ] **Step 4: Build `acctByMonth` and add it to the payload**

In `buildView`, after the `txByMonth` build loop ends (after line 126, before the `json.Marshal` on line 127), insert:

```go
	acctByMonth := make(map[string][]acctVM, n)
	for _, mb := range summary.ByMonth {
		key := fmt.Sprintf("%04d-%02d", mb.Year, int(mb.Month))
		accs := make([]acctVM, len(mb.ByAccount))
		for j, a := range mb.ByAccount {
			accs[j] = acctVM{
				Source:   accountLabel(a.Source),
				Income:   a.Income,
				Expenses: a.Expenses,
			}
		}
		acctByMonth[key] = accs
	}
```

Then change the `json.Marshal` call (line 127) from:

```go
	payload, _ := json.Marshal(map[string]any{"months": chart, "tx": txByMonth})
```

to:

```go
	payload, _ := json.Marshal(map[string]any{"months": chart, "tx": txByMonth, "acct": acctByMonth})
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/infra/html/ -run 'TestRender/embeds_per-account' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: serialize per-account breakdown into report payload"
```

---

## Task 5: Modal markup + JS for the By Account block

**Files:**
- Modify: `internal/infra/html/renderer.go` (CSS in `reportHTML`; modal markup ~line 750; modal JS ~lines 951-1041)
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest to `TestRender` in `internal/infra/html/renderer_test.go` (after the previous one):

```go
	t.Run("renders the By Account block scaffold in the modal", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome: 1500, TotalExpenses: 500, Savings: 1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000},
			},
		}

		require.NoError(t, New(out).Render(ctx, summary))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, `id="tx-accounts"`)        // block container
		require.Contains(t, content, "By Account")              // block heading
		require.Contains(t, content, "window.FIN.acct")         // JS reads the payload
	})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infra/html/ -run 'TestRender/renders_the_By_Account' -v`
Expected: FAIL — content does not contain `id="tx-accounts"`.

- [ ] **Step 3: Add the modal markup**

In `reportHTML`, the modal has `<div class="tx-totals" id="tx-totals"></div>` followed by `<div class="tx-scroll">` (lines 750-751). Insert the By Account container between them. Replace:

```html
      <div class="tx-totals" id="tx-totals"></div>
      <div class="tx-scroll">
```

with:

```html
      <div class="tx-totals" id="tx-totals"></div>
      <div class="tx-accounts" id="tx-accounts" hidden>
        <div class="tx-acc-head">By Account</div>
        <div class="tx-acc-rows" id="tx-acc-rows"></div>
      </div>
      <div class="tx-scroll">
```

- [ ] **Step 4: Add CSS for the block**

In the `/* ---------- TRANSACTION MODAL ---------- */` CSS section, after the `.tx-totals b` rule (line 581), insert:

```css
.tx-accounts { padding: 12px 24px 14px; border-bottom: 1px solid var(--hair); }
.tx-accounts[hidden] { display: none; }
.tx-acc-head {
  font-family: var(--sans); font-size: 11px; font-weight: 600; letter-spacing: .1em;
  text-transform: uppercase; color: var(--muted); margin-bottom: 8px;
}
.tx-acc-row {
  display: flex; justify-content: space-between; gap: 16px; padding: 4px 0;
  font-family: var(--sans); font-size: 13px; font-variant-numeric: tabular-nums;
}
.tx-acc-name { color: var(--ink); }
.tx-acc-figs { display: inline-flex; gap: 16px; }
```

- [ ] **Step 5: Wire the JS to render the block on open**

In the modal IIFE (lines 951-1041), do two edits.

First, after the line `var TX = (window.FIN && window.FIN.tx) || {};` (line 953), add:

```javascript
  var ACCT = (window.FIN && window.FIN.acct) || {};
```

Second, after the line that grabs `var totalsEl = document.getElementById('tx-totals');` (line 959), add references to the new elements:

```javascript
  var acctWrap = document.getElementById('tx-accounts');
  var acctRowsEl = document.getElementById('tx-acc-rows');
```

Third, in the `open` function, after the `totalsEl.innerHTML = ...` assignment (ends line 1005, before `sort = { key: 'k', dir: 'desc' };` on line 1006), insert the block render:

```javascript
    var accts = ACCT[key] || [];
    if (accts.length) {
      var ah = '';
      accts.forEach(function (a) {
        ah += '<div class="tx-acc-row"><span class="tx-acc-name">' + esc(a.src) + '</span>' +
              '<span class="tx-acc-figs">' +
              '<span class="tx-amt pos">' + eu(a.inc) + '</span>' +
              '<span class="tx-amt neg">' + eu(-a.exp) + '</span>' +
              '</span></div>';
      });
      acctRowsEl.innerHTML = ah;
      acctWrap.hidden = false;
    } else {
      acctRowsEl.innerHTML = '';
      acctWrap.hidden = true;
    }
```

Note: `eu(-a.exp)` renders expenses as a negative euro figure (e.g. `−€540,00`), matching the `neg` color and the spec's `−€expenses` format. `eu` and `esc` are already defined in this IIFE.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/infra/html/ -run 'TestRender/renders_the_By_Account' -v`
Expected: PASS.

- [ ] **Step 7: Run the full infra/html package**

Run: `go test ./internal/infra/html/`
Expected: ok.

- [ ] **Step 8: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: render By Account block in transaction modal"
```

---

## Task 6: Full verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: all packages `ok`.

- [ ] **Step 2: Regenerate the report and eyeball the modal**

Run: `go run . && open report.html`
Expected: clicking a month row opens the modal; a "By Account" block appears between the totals bar and the transaction list, one row per `.csv` (extension stripped), income in green and expenses in red; the transaction list's Source column also shows stripped names.

- [ ] **Step 3: Final commit (only if uncommitted changes remain)**

```bash
git status --porcelain
```

If clean, the feature is complete. The pre-existing uncommitted edit to `FilterTransfers` in `transaction.go` is unrelated to this feature and should be left as-is.

---

## Notes

- The pre-existing working-tree edit to `FilterTransfers` is unrelated; do not stage or revert it.
- No README/AGENTS.md changes are required — this is a display-only addition with no new commands, dependencies, or conventions. Add a Plans entry in AGENTS.md only if the project's process requires it (optional).
