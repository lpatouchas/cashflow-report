# Month Transaction Detail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a reader click a month row in the HTML report's breakdown table to open a modal listing that month's individual transactions, one per line, sortable by any column.

**Architecture:** `Summarize()` retains each month's transactions on `MonthlyBreakdown`. The HTML renderer serializes them to JSON beside the existing `window.FIN` chart payload; a small vanilla-JS handler opens one reusable modal and sorts rows client-side, mirroring the patterns already in `renderer.go`.

**Tech Stack:** Go 1.23, `html/template`, vanilla JS (no framework), testify.

**Spec:** `docs/superpowers/specs/2026-05-30-month-transaction-detail-design.md`

---

## File Structure

- `internal/domain/transaction/transaction.go` — add `Transactions []Transaction` to `MonthlyBreakdown`; populate in `Summarize()`.
- `internal/domain/transaction/transaction_test.go` — assert transactions are attached to the right month.
- `internal/infra/html/renderer.go` — add `txVM` type, embed per-month transactions in the JSON payload, make month rows interactive, add modal markup + CSS + JS.
- `internal/infra/html/renderer_test.go` — assert the JSON payload carries per-month transactions and the modal scaffold renders.
- `README.md` — note that month rows are clickable.

All work happens on branch `feature/month-transaction-detail` (branch from up-to-date `main`).

---

## Task 1: Retain transactions per month in the domain

**Files:**
- Modify: `internal/domain/transaction/transaction.go`
- Test: `internal/domain/transaction/transaction_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest inside `TestSummarize` in `internal/domain/transaction/transaction_test.go` (after the `"groups by month sorted newest first"` subtest):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/transaction/ -run TestSummarize -v`
Expected: FAIL — compile error `unknown field 'Transactions' in struct literal` / `mb.Transactions undefined`.

- [ ] **Step 3: Add the field to `MonthlyBreakdown`**

In `internal/infra/.../transaction.go`, replace the struct:

```go
// MonthlyBreakdown holds income/expenses/savings for a single calendar month.
type MonthlyBreakdown struct {
	Year     int
	Month    time.Month
	Income   float64
	Expenses float64
	Savings  float64
}
```

with:

```go
// MonthlyBreakdown holds income/expenses/savings for a single calendar month,
// along with the transactions that make it up.
type MonthlyBreakdown struct {
	Year         int
	Month        time.Month
	Income       float64
	Expenses     float64
	Savings      float64
	Transactions []Transaction
}
```

- [ ] **Step 4: Populate it in `Summarize()`**

In the per-month accumulation block inside `Summarize()`, replace:

```go
		if t.IsDebit {
			mb.Expenses += t.Amount
		} else {
			mb.Income += t.Amount
		}
	}
	s.Savings = s.TotalIncome - s.TotalExpenses
```

with:

```go
		if t.IsDebit {
			mb.Expenses += t.Amount
		} else {
			mb.Income += t.Amount
		}
		mb.Transactions = append(mb.Transactions, t)
	}
	s.Savings = s.TotalIncome - s.TotalExpenses
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/domain/transaction/ -run TestSummarize -v`
Expected: PASS (all subtests, including the new one).

- [ ] **Step 6: Run the full package to confirm nothing regressed**

Run: `go test ./internal/domain/transaction/ -cover`
Expected: `ok` with coverage unchanged or higher.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: retain per-month transactions in summary"
```

---

## Task 2: Embed per-month transactions in the rendered JSON

**Files:**
- Modify: `internal/infra/html/renderer.go`
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest inside `TestRender` in `internal/infra/html/renderer_test.go`:

```go
	t.Run("embeds per-month transactions as JSON", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome: 1500, TotalExpenses: 500, Savings: 1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{
					Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000,
					Transactions: []transaction.Transaction{
						{ID: "1", Date: time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC), Description: "Salary", Amount: 1500, IsDebit: false, SourceFile: "acc.csv"},
						{ID: "2", Date: time.Date(2026, time.May, 3, 0, 0, 0, 0, time.UTC), Description: "Rent", Amount: 500, IsDebit: true, SourceFile: "acc.csv"},
					},
				},
			},
		}

		require.NoError(t, New(out).Render(ctx, summary))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, `"2026-05"`)       // month key in the tx map
		require.Contains(t, content, `"desc":"Salary"`)  // income description
		require.Contains(t, content, `"amt":1500`)       // income signed positive
		require.Contains(t, content, `"amt":-500`)        // expense signed negative
		require.Contains(t, content, `"src":"acc.csv"`)
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: FAIL — the embedded JSON has no `tx` map, so `"2026-05"` / `"desc":"Salary"` are not found.

- [ ] **Step 3: Add the `txVM` type**

In `internal/infra/html/renderer.go`, immediately after the `chartMonth` struct definition, add:

```go
// txVM is one transaction line inside a month's detail modal, serialized to JS.
type txVM struct {
	Date   string  `json:"date"` // "12 May 2026", for display
	Sort   string  `json:"k"`    // "2026-05-12", for date sorting
	Desc   string  `json:"desc"`
	Amount float64 `json:"amt"` // signed: income +, expense −
	Source string  `json:"src"`
}
```

- [ ] **Step 4: Build the tx map and add it to the payload**

In `buildView`, find the chart-building block and the marshal line:

```go
	payload, _ := json.Marshal(map[string]any{"months": chart})
```

Replace that single line with:

```go
	txByMonth := make(map[string][]txVM, n)
	for _, mb := range summary.ByMonth {
		key := fmt.Sprintf("%04d-%02d", mb.Year, int(mb.Month))
		lines := make([]txVM, len(mb.Transactions))
		for j, t := range mb.Transactions {
			amt := t.Amount
			if t.IsDebit {
				amt = -amt
			}
			lines[j] = txVM{
				Date:   t.Date.Format("02 January 2006"),
				Sort:   t.Date.Format("2006-01-02"),
				Desc:   t.Description,
				Amount: amt,
				Source: t.SourceFile,
			}
		}
		txByMonth[key] = lines
	}
	payload, _ := json.Marshal(map[string]any{"months": chart, "tx": txByMonth})
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: embed per-month transactions in report JSON"
```

---

## Task 3: Make month rows interactive and add the modal scaffold

**Files:**
- Modify: `internal/infra/html/renderer.go` (template HTML + CSS)
- Test: `internal/infra/html/renderer_test.go`

- [ ] **Step 1: Write the failing test**

Add this subtest inside `TestRender` in `internal/infra/html/renderer_test.go`:

```go
	t.Run("renders clickable month rows and the modal scaffold", func(t *testing.T) {
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

		require.Contains(t, content, `id="tx-modal"`)        // modal element
		require.Contains(t, content, `id="tx-body"`)         // modal table body
		require.Contains(t, content, `class="row clickable`) // rows are clickable
		require.Contains(t, content, `role="button"`)        // keyboard/AT affordance
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: FAIL — `id="tx-modal"` and `class="row clickable` are not present.

- [ ] **Step 3: Make the month rows interactive**

In the `reportHTML` template, replace the breakdown row opening tag:

```html
            <tr class="row{{ if .Best }} best{{ end }}{{ if .Worst }} worst{{ end }}" data-key="{{ .Key }}" data-income="{{ .Income }}" data-expenses="{{ .Expenses }}" data-savings="{{ .Savings }}" data-rate="{{ .Rate }}">
```

with:

```html
            <tr class="row clickable{{ if .Best }} best{{ end }}{{ if .Worst }} worst{{ end }}" data-key="{{ .Key }}" data-income="{{ .Income }}" data-expenses="{{ .Expenses }}" data-savings="{{ .Savings }}" data-rate="{{ .Rate }}" tabindex="0" role="button" aria-label="View {{ .Label }} transactions">
```

- [ ] **Step 4: Add the modal markup**

In the `reportHTML` template, find the line that opens the chart data script:

```html
<script>window.FIN = {{ .ChartJSON }};</script>
```

Insert this modal markup immediately **before** that line:

```html
<div class="tx-modal" id="tx-modal" hidden>
  <div class="tx-backdrop" data-close></div>
  <div class="tx-dialog" role="dialog" aria-modal="true" aria-labelledby="tx-title">
    <div class="tx-head">
      <h2 class="tx-title" id="tx-title"></h2>
      <button class="tx-close" data-close aria-label="Close">&times;</button>
    </div>
    <div class="tx-totals" id="tx-totals"></div>
    <div class="tx-scroll">
      <table class="tx-table" id="tx-table">
        <thead>
          <tr>
            <th class="l active" data-key="k"><span class="th-in">Date<span class="sort-arrow show">&#9660;</span></span></th>
            <th class="l" data-key="desc"><span class="th-in">Description<span class="sort-arrow">&#9660;</span></span></th>
            <th class="r" data-key="amt"><span class="th-in">Amount<span class="sort-arrow">&#9660;</span></span></th>
            <th class="l" data-key="src"><span class="th-in">Source<span class="sort-arrow">&#9660;</span></span></th>
          </tr>
        </thead>
        <tbody id="tx-body"></tbody>
      </table>
    </div>
  </div>
</div>
```

- [ ] **Step 5: Add the modal CSS**

In the `<style>` block, find the print media-query line:

```css
  .editions, .chart-controls, .block-hint { display: none !important; }
```

Replace it with (adds `.tx-modal` to the print-hidden list):

```css
  .editions, .chart-controls, .block-hint, .tx-modal { display: none !important; }
```

Then, immediately **before** the `/* ---------- PRINT` comment, insert this stylesheet section:

```css
/* ---------- TRANSACTION MODAL ---------- */
.row.clickable { cursor: pointer; }
.tx-modal {
  position: fixed; inset: 0; z-index: 50;
  display: flex; align-items: center; justify-content: center; padding: 20px;
}
.tx-modal[hidden] { display: none; }
.tx-backdrop { position: absolute; inset: 0; background: rgba(20,18,12,.55); }
.tx-dialog {
  position: relative; z-index: 1; width: min(680px, 100%); max-height: 84vh;
  display: flex; flex-direction: column; overflow: hidden;
  background: var(--sheet); color: var(--ink); border: 1px solid var(--rule);
  box-shadow: 0 30px 80px -30px rgba(0,0,0,.6);
}
.tx-head {
  display: flex; align-items: baseline; gap: 12px;
  padding: 22px 24px 14px; border-bottom: 1px solid var(--rule);
}
.tx-title {
  font-family: var(--serif); font-weight: 400; font-size: 24px;
  margin: 0; letter-spacing: -0.01em;
}
.tx-close {
  margin-left: auto; font-size: 22px; line-height: 1; color: var(--muted);
  background: transparent; border: none; cursor: pointer; padding: 0 4px;
}
.tx-close:hover { color: var(--ink); }
.tx-totals {
  display: flex; gap: 22px; flex-wrap: wrap; padding: 12px 24px;
  font-family: var(--sans); font-size: 12px; color: var(--muted);
  border-bottom: 1px solid var(--hair); font-variant-numeric: tabular-nums;
}
.tx-totals b { color: var(--ink); font-weight: 600; }
.tx-scroll { overflow-y: auto; }
.tx-table { width: 100%; border-collapse: collapse; }
.tx-table th {
  font-family: var(--sans); font-size: 11px; font-weight: 600; letter-spacing: .1em;
  text-transform: uppercase; color: var(--muted); padding: 12px 24px; cursor: pointer;
  user-select: none; border-bottom: 1px solid var(--rule);
  position: sticky; top: 0; background: var(--sheet); z-index: 1;
}
.tx-table th.r { text-align: right; }
.tx-table th.l { text-align: left; }
.tx-table th:hover, .tx-table th.active { color: var(--ink); }
.tx-table td {
  padding: 11px 24px; font-family: var(--sans); font-size: 13.5px;
  border-bottom: 1px solid var(--hair); font-variant-numeric: tabular-nums;
}
.tx-table td.r { text-align: right; }
.tx-table td.l { text-align: left; }
.tx-amt.pos { color: var(--pos); }
.tx-amt.neg { color: var(--neg); }
.tx-src { color: var(--muted); font-size: 12px; }
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: PASS (all subtests).

- [ ] **Step 7: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: add clickable month rows and transaction modal scaffold"
```

---

## Task 4: Wire up modal open/close and column sorting (JS)

**Files:**
- Modify: `internal/infra/html/renderer.go` (template JS)
- Modify: `README.md`

> No automated test: the project has no JS test harness, consistent with how the existing chart and table-sort JS are handled. This task ends with a manual verification step.

- [ ] **Step 1: Add the modal JS handler**

In the `reportHTML` template, find the end of the existing script — the chart IIFE's closing line followed by the closing script tag:

```html
  render();
})();
</script>
</body>
```

Replace it with (adds a second IIFE for the modal before `</body>`):

```html
  render();
})();
</script>
<script>
(function () {
  var TX = (window.FIN && window.FIN.tx) || {};
  var modal = document.getElementById('tx-modal');
  var ledger = document.getElementById('ledger');
  if (!modal || !ledger) return;

  var titleEl = document.getElementById('tx-title');
  var totalsEl = document.getElementById('tx-totals');
  var bodyEl = document.getElementById('tx-body');
  var tableEl = document.getElementById('tx-table');
  var lastTrigger = null;
  var rows = [];
  var sort = { key: 'k', dir: 'desc' };

  var eu = function (v) {
    return (v < 0 ? '−€' : '€') + Math.abs(v).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  };
  var esc = function (s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); };

  var renderRows = function () {
    var sorted = rows.slice().sort(function (a, b) {
      var av = a[sort.key], bv = b[sort.key], c;
      if (sort.key === 'amt') { c = av > bv ? 1 : av < bv ? -1 : 0; }
      else { c = String(av).localeCompare(String(bv)); }
      return sort.dir === 'asc' ? c : -c;
    });
    var html = '';
    sorted.forEach(function (t) {
      var cls = t.amt < 0 ? 'neg' : 'pos';
      html += '<tr><td class="l">' + esc(t.date) + '</td>' +
              '<td class="l">' + esc(t.desc) + '</td>' +
              '<td class="r tx-amt ' + cls + '">' + eu(t.amt) + '</td>' +
              '<td class="l tx-src">' + esc(t.src) + '</td></tr>';
    });
    bodyEl.innerHTML = html;
    tableEl.querySelectorAll('th').forEach(function (th) {
      var on = th.getAttribute('data-key') === sort.key;
      th.classList.toggle('active', on);
      var ar = th.querySelector('.sort-arrow');
      if (ar) { ar.classList.toggle('show', on); ar.textContent = sort.dir === 'asc' ? '▲' : '▼'; }
    });
  };

  var open = function (tr) {
    var key = tr.getAttribute('data-key');
    rows = TX[key] || [];
    lastTrigger = tr;
    var mcell = tr.querySelector('.mcell');
    titleEl.textContent = mcell ? mcell.textContent.replace(/best|lean/gi, '').trim() : key;
    totalsEl.innerHTML =
      '<span>Income <b>' + eu(parseFloat(tr.getAttribute('data-income'))) + '</b></span>' +
      '<span>Expenses <b>' + eu(parseFloat(tr.getAttribute('data-expenses'))) + '</b></span>' +
      '<span>Savings <b>' + eu(parseFloat(tr.getAttribute('data-savings'))) + '</b></span>';
    sort = { key: 'k', dir: 'desc' };
    renderRows();
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
  };

  var close = function () {
    modal.hidden = true;
    document.body.style.overflow = '';
    if (lastTrigger) lastTrigger.focus();
  };

  ledger.querySelectorAll('tbody tr').forEach(function (tr) {
    tr.addEventListener('click', function () { open(tr); });
    tr.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(tr); }
    });
  });
  modal.querySelectorAll('[data-close]').forEach(function (el) {
    el.addEventListener('click', close);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !modal.hidden) close();
  });
  tableEl.querySelectorAll('th').forEach(function (th) {
    th.addEventListener('click', function () {
      var k = th.getAttribute('data-key');
      if (sort.key === k) { sort.dir = sort.dir === 'asc' ? 'desc' : 'asc'; }
      else { sort = { key: k, dir: k === 'amt' ? 'desc' : 'asc' }; }
      renderRows();
    });
  });
})();
</script>
</body>
```

- [ ] **Step 2: Confirm the template still compiles and all tests pass**

Run: `go test ./... -cover`
Expected: every package `ok`. (`html/template` parse errors surface here because `tmpl` uses `template.Must`.)

- [ ] **Step 3: Manually verify the modal in a browser**

Run: `go run .`
Then open `./report.html` and check:
- Clicking a month row opens the modal titled with that month (e.g. "May 2026").
- One line per transaction; Amount column shows income green / expenses red.
- Default order is Date, newest first.
- Clicking each header (Date, Description, Amount, Source) toggles asc/desc and moves the ▼/▲ arrow.
- The totals line matches the row (income / expenses / savings).
- Backdrop click, the × button, and Escape all close it; switching editions restyles it.

Expected: all behaviors as described.

- [ ] **Step 4: Update the README**

In `README.md`, under "What it does", add a bullet:

```markdown
- The HTML report's monthly table is interactive: click a month to open a modal
  listing that month's individual transactions, sortable by any column.
```

- [ ] **Step 5: Commit**

```bash
git add internal/infra/html/renderer.go README.md
git commit -m "feat: open sortable transaction modal on month click"
```

---

## Self-Review

**Spec coverage:**
- Click month → modal with transactions → Tasks 3 (trigger + scaffold) & 4 (open).
- Columns Date · Description · Amount (signed) · Source → Task 2 (data) & 3 (headers) & 4 (rows).
- Every column click-to-sort → Task 4.
- Default Date newest-first → Task 4 (`sort = { key: 'k', dir: 'desc' }`).
- Theme-matching + print-hidden → Task 3 CSS.
- Excluded transfers absent → guaranteed upstream (`FilterTransfers` runs before `Summarize`); verified implicitly since `Summarize` only attaches what it receives (Task 1 test).
- Dismiss via backdrop/×/Escape + focus restore → Task 4.

**Placeholder scan:** No TBD/TODO; every code step shows complete code.

**Type consistency:** `Transactions` field (Task 1) → read in `buildView` (Task 2) → keyed map `txByMonth["2026-05"]` matches the row `data-key` (`fmt.Sprintf("%04d-%02d", ...)`, identical to existing `rowVM.Key`) → JS `TX[key]` (Task 4). JSON field tags (`date`,`k`,`desc`,`amt`,`src`) on `txVM` match the JS property names used in `renderRows`/`open`. Sort keys (`k`,`desc`,`amt`,`src`) match the `data-key` attributes on the modal `<th>` elements (Task 3).
