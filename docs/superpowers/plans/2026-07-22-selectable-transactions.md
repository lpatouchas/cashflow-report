# Selectable Transactions with Running Total — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users tick individual transactions in the report modal and see a live Income / Expenses / Net total for just the selected subset.

**Architecture:** Front-end only, inside the single Go template `internal/infra/html/report.html`. A leading checkbox column is added to the modal table; selection is held in a JavaScript `Set` of transaction **object references** (stable across re-sort because `renderRows` re-sorts the same objects). A sticky footer bar (`.tx-selbar`) is hidden at zero selection and shows the three figures once ≥1 row is ticked.

**Tech Stack:** Go `html/template` (embedded `report.html`), vanilla ES5-style browser JS (matches the existing IIFE), CSS custom properties for theming. Tests: Go `testing` + `testify/require` for rendered-markup assertions; JS behaviour is verified manually.

## Global Constraints

- All changes confined to `internal/infra/html/report.html`. No Go logic, data-shape, or backend changes.
- Currency figures use the existing `eu()` formatter (EUR, `de-DE` locale). Do not introduce a second formatter.
- Style only with the existing CSS variables (`--sheet`, `--rule`, `--hair`, `--ink`, `--muted`, `--accent`, `--c-sav`, `--neg`, `--sans`) so all three editions (Nocturne, Ledger, Almanac) theme correctly.
- Match the existing code idiom: `var`, function expressions, no arrow functions, no new dependencies.
- Selection is modal-only, resets on every open, and is never persisted or exported (out of scope).
- Commit trailer required on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Do NOT open a PR — commit and push only.

---

### Task 1: Selection scaffold — checkbox column, footer bar, CSS, print

Adds the static markup and styling. Deliverable is verifiable by a Go test asserting the new elements render in the template output, plus a build.

**Files:**
- Modify: `internal/infra/html/report.html` (thead `~528-533`, after `.tx-scroll` close `~537`, CSS `~367-371`, print rule `384`)
- Test: `internal/infra/html/renderer_test.go` (add one subtest inside `TestRender`)

**Interfaces:**
- Consumes: nothing (first task).
- Produces (relied on by Task 2, exact IDs/classes):
  - `<input id="tx-all" class="tx-all-check">` — the select-all checkbox, in the first `<th>`, no `data-key`.
  - `.tx-check` `<th>`/`<td>` — the checkbox column cells.
  - `.tx-row-check` — class used on each per-row checkbox (emitted by Task 2's `renderRows`).
  - `<div id="tx-selbar" hidden>` containing `<span id="tx-sel-count">`, `<span id="tx-sel-figs">`, `<button id="tx-sel-clear">`.

- [ ] **Step 1: Write the failing test**

Add this subtest at the end of `TestRender` in `internal/infra/html/renderer_test.go`, just before the closing `}` of the outer `func TestRender`:

```go
	t.Run("renders the selection scaffold in the modal", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome: 1500, TotalExpenses: 500, Savings: 1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000},
			},
		}

		require.NoError(t, NewFile(out).Render(ctx, summary))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, `id="tx-all"`)      // select-all checkbox
		require.Contains(t, content, `id="tx-selbar"`)   // sticky footer bar
		require.Contains(t, content, `id="tx-sel-figs"`) // running-total figures slot
		require.Contains(t, content, `id="tx-sel-clear"`) // clear button
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/html/ -run TestRender/renders_the_selection_scaffold -v`
Expected: FAIL — assertion `Contains` reports `id="tx-all"` (and the others) not found in the rendered output.

- [ ] **Step 3: Add the select-all header cell**

In `internal/infra/html/report.html`, in the modal `<thead>` (around line 528), add a new first `<th>` so the header row reads:

```html
            <tr>
              <th class="l tx-check"><input type="checkbox" id="tx-all" class="tx-all-check" aria-label="Select all transactions"></th>
              <th class="l active" data-key="k"><span class="th-in">Date<span class="sort-arrow show">▼</span></span></th>
              <th class="l" data-key="desc"><span class="th-in">Description<span class="sort-arrow">▼</span></span></th>
              <th class="r" data-key="amt"><span class="th-in">Amount<span class="sort-arrow">▼</span></span></th>
              <th class="l" data-key="src"><span class="th-in">Source<span class="sort-arrow">▼</span></span></th>
            </tr>
```

- [ ] **Step 4: Add the sticky footer bar**

In the same file, insert the footer bar between the `</div>` that closes `.tx-scroll` and the `</div>` that closes `.tx-dialog` (around line 537). The region becomes:

```html
      <div class="tx-scroll">
        <table class="tx-table" id="tx-table">
          <thead>
            <!-- header row from Step 3 -->
          </thead>
          <tbody id="tx-body"></tbody>
        </table>
      </div>
      <div class="tx-selbar" id="tx-selbar" hidden>
        <span class="tx-sel-count" id="tx-sel-count"></span>
        <span class="tx-sel-figs" id="tx-sel-figs"></span>
        <button class="tx-sel-clear" id="tx-sel-clear" type="button">Clear</button>
      </div>
    </div>
```

- [ ] **Step 5: Add the CSS**

In the `<style>` block, immediately after the `.tx-cat { ... }` rule (around line 371) and before the `/* ---------- PRINT ... */` comment, add:

```css
.tx-check { width: 1%; white-space: nowrap; }
.tx-row-check, .tx-all-check { cursor: pointer; accent-color: var(--accent); margin: 0; }
.tx-selbar {
  display: flex; align-items: center; gap: 22px; padding: 12px 24px;
  border-top: 1px solid var(--rule); background: var(--sheet);
  font-family: var(--sans); font-size: 12px; color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.tx-selbar[hidden] { display: none; }
.tx-selbar b { color: var(--ink); font-weight: 600; }
.tx-sel-count { font-weight: 600; color: var(--ink); }
.tx-sel-figs { display: inline-flex; gap: 22px; flex-wrap: wrap; }
.tx-sel-clear {
  margin-left: auto; background: transparent; border: 1px solid var(--rule);
  color: var(--muted); font-family: var(--sans); font-size: 12px;
  padding: 4px 12px; cursor: pointer;
}
.tx-sel-clear:hover { color: var(--ink); border-color: var(--ink); }
```

- [ ] **Step 6: Keep the Date column non-wrapping**

The checkbox is now the first column, so the existing `:first-child` no-wrap rule no longer protects the Date cell. Update the rule at line 364 from:

```css
.tx-table th:first-child, .tx-table td:first-child { white-space: nowrap; }
```

to also cover the Date column (now the second cell):

```css
.tx-table th:first-child, .tx-table td:first-child,
.tx-table th:nth-child(2), .tx-table td:nth-child(2) { white-space: nowrap; }
```

- [ ] **Step 7: Add the footer bar to the print-suppression rule**

Update the print rule at line 384 from:

```css
  .editions, .chart-controls, .block-hint, .tx-modal { display: none !important; }
```

to:

```css
  .editions, .chart-controls, .block-hint, .tx-modal, .tx-selbar { display: none !important; }
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: PASS — all `TestRender` subtests pass, including `renders_the_selection_scaffold`.

- [ ] **Step 9: Commit**

```bash
git add internal/infra/html/report.html internal/infra/html/renderer_test.go
git commit -m "feat(report): add transaction-selection scaffold to the modal

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Selection behaviour — checkboxes, running total, clear

Wires the modal JS so ticking rows drives the footer figures. JS behaviour is not unit-testable here; verification is `go build`/`go test` (template stays valid) plus a manual browser checklist.

**Files:**
- Modify: `internal/infra/html/report.html` — the modal JS IIFE (lines ~725-834)

**Interfaces:**
- Consumes (from Task 1): `#tx-all`, `#tx-selbar`, `#tx-sel-count`, `#tx-sel-figs`, `#tx-sel-clear`, and the `.tx-check` / `.tx-row-check` / `.tx-all-check` classes.
- Consumes (existing): `rows` (array of tx objects with `k, date, desc, amt, src, cat`), `sort`, `eu()`, `esc()`, `renderRows()`, `open()`, `close()`, `tableEl`, `bodyEl`.
- Produces: no exports — self-contained IIFE behaviour.

- [ ] **Step 1: Add selection element refs and the Set**

In the IIFE, after the existing element lookups (after `var tableEl = document.getElementById('tx-table');`, around line 738), add:

```js
  var selbar = document.getElementById('tx-selbar');
  var selCountEl = document.getElementById('tx-sel-count');
  var selFigsEl = document.getElementById('tx-sel-figs');
  var selClearBtn = document.getElementById('tx-sel-clear');
  var allEl = document.getElementById('tx-all');
  var selected = new Set();
```

- [ ] **Step 2: Add the selection helper functions**

Immediately after `var esc = ...;` (around line 746) and before `var renderRows = ...`, add three helpers:

```js
  var syncSelAll = function () {
    if (!allEl) return;
    var n = rows.length, s = 0;
    rows.forEach(function (t) { if (selected.has(t)) s++; });
    allEl.checked = n > 0 && s === n;
    allEl.indeterminate = s > 0 && s < n;
  };

  var renderSelbar = function () {
    var inc = 0, exp = 0, n = 0;
    selected.forEach(function (t) {
      n++;
      if (t.amt > 0) { inc += t.amt; } else { exp += t.amt; }
    });
    if (!n) { if (selbar) selbar.hidden = true; return; }
    selCountEl.textContent = n + ' selected';
    selFigsEl.innerHTML =
      '<span>Income <b>' + eu(inc) + '</b></span>' +
      '<span>Expenses <b>' + eu(exp) + '</b></span>' +
      '<span>Net <b>' + eu(inc + exp) + '</b></span>';
    selbar.hidden = false;
  };

  var clearSel = function () { selected.clear(); renderRows(); renderSelbar(); };
```

Note: `exp` accumulates negative amounts, so `eu(exp)` prints the expense with a leading `−` and `inc + exp` is income minus expenses = Net.

- [ ] **Step 3: Emit the checkbox cell and restore checked state in `renderRows`**

Replace the entire existing `var renderRows = function () { ... };` (lines ~748-772) with:

```js
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
      var cat = t.cat ? ' <span class="tx-cat">' + esc(t.cat) + '</span>' : '';
      html += '<tr><td class="l tx-check"><input type="checkbox" class="tx-row-check" aria-label="Select transaction"></td>' +
              '<td class="l">' + esc(t.date) + '</td>' +
              '<td class="l">' + esc(t.desc) + cat + '</td>' +
              '<td class="r tx-amt ' + cls + '">' + eu(t.amt) + '</td>' +
              '<td class="l tx-src">' + esc(t.src) + '</td></tr>';
    });
    bodyEl.innerHTML = html;
    var trs = bodyEl.querySelectorAll('tr');
    sorted.forEach(function (t, i) {
      var cb = trs[i].querySelector('.tx-row-check');
      if (cb) { cb._tx = t; cb.checked = selected.has(t); }
    });
    syncSelAll();
    tableEl.querySelectorAll('th').forEach(function (th) {
      var on = th.getAttribute('data-key') === sort.key;
      th.classList.toggle('active', on);
      th.setAttribute('aria-sort', on ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none');
      var ar = th.querySelector('.sort-arrow');
      if (ar) { ar.classList.toggle('show', on); ar.textContent = sort.dir === 'asc' ? '▲' : '▼'; }
    });
  };
```

- [ ] **Step 4: Reset selection when the modal opens**

In `open()`, add `selected.clear();` on the line just before `sort = { key: 'k', dir: 'desc' };` (around line 800), and add `renderSelbar();` on the line just after `renderRows();` (around line 801). The tail of `open()` becomes:

```js
    selected.clear();
    sort = { key: 'k', dir: 'desc' };
    renderRows();
    renderSelbar();
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
    var closeBtn = modal.querySelector('[data-close]');
    if (closeBtn && closeBtn.focus) closeBtn.focus();
```

- [ ] **Step 5: Clear selection when the modal closes**

Replace the existing `var close = function () { ... };` (lines ~808-812) with:

```js
  var close = function () {
    selected.clear();
    if (selbar) selbar.hidden = true;
    modal.hidden = true;
    document.body.style.overflow = '';
    if (lastTrigger) lastTrigger.focus();
  };
```

- [ ] **Step 6: Wire the checkbox, select-all, and clear handlers**

After the existing `modal.querySelectorAll('[data-close]')...` block (around line 822), add:

```js
  bodyEl.addEventListener('change', function (e) {
    var cb = e.target;
    if (!cb || !cb.classList || !cb.classList.contains('tx-row-check')) return;
    if (cb.checked) { selected.add(cb._tx); } else { selected.delete(cb._tx); }
    syncSelAll();
    renderSelbar();
  });
  if (allEl) {
    allEl.addEventListener('change', function () {
      rows.forEach(function (t) { if (allEl.checked) { selected.add(t); } else { selected.delete(t); } });
      renderRows();
      renderSelbar();
    });
  }
  if (selClearBtn) { selClearBtn.addEventListener('click', clearSel); }
```

- [ ] **Step 7: Stop the select-all header from triggering a sort**

The header sort handler binds to every `<th>`; the select-all `<th>` has no `data-key`. In the `tableEl.querySelectorAll('th')` click handler (around line 826), add a guard so a keyless header (and clicks on its checkbox) does not change the sort. The handler becomes:

```js
  tableEl.querySelectorAll('th').forEach(function (th) {
    th.addEventListener('click', function () {
      var k = th.getAttribute('data-key');
      if (!k) return;
      if (sort.key === k) { sort.dir = sort.dir === 'asc' ? 'desc' : 'asc'; }
      else { sort = { key: k, dir: k === 'amt' ? 'desc' : 'asc' }; }
      renderRows();
    });
  });
```

- [ ] **Step 8: Verify the build and template still pass**

Run: `go build ./... && go test ./internal/infra/html/ -run TestRender -v`
Expected: build succeeds; all `TestRender` subtests PASS (the template is still valid Go `html/template` and the Task 1 scaffold assertions still hold).

- [ ] **Step 9: Manual browser verification**

Generate a report and open it:

```bash
make generate DATA=./data OUT=./report.html
```

Open `report.html` in a browser, click a month row to open the modal, and confirm:
- Each transaction row has a checkbox; ticking one reveals the footer bar with **Income / Expenses / Net** and an "N selected" count.
- Expenses shows a negative (`−€…`) total; Net equals Income minus the absolute Expenses.
- The header **select-all** checkbox ticks/unticks every row; it shows an indeterminate state on a partial selection.
- Sorting a column (click Date/Amount/etc.) keeps the current selection ticked; clicking the select-all header does **not** re-sort.
- **Clear** empties the selection and hides the footer bar.
- Re-opening the modal (another month, or reopening the same one) starts with nothing selected and the bar hidden.
- Switch editions (Nocturne / Ledger / Almanac) — the bar and checkboxes theme correctly.

- [ ] **Step 10: Commit**

```bash
git add internal/infra/html/report.html
git commit -m "feat(report): sum selected transactions in a sticky modal footer

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Selection column with select-all + per-row checkboxes → Task 1 Step 3, Task 2 Step 3. ✔
- Selection as a `Set` of object references surviving re-sort → Task 2 Steps 1, 3 (objects re-sorted, `checked` restored from Set). ✔
- `open()` empties the Set → Task 2 Step 4. ✔
- Sticky footer `.tx-selbar`, `[hidden]` at 0, shown ≥1 → Task 1 Steps 4-5, Task 2 Step 2 (`renderSelbar`). ✔
- Income (`amt>0`) / Expenses (`amt<0`, primary) / Net (income − expenses) → Task 2 Step 2. ✔
- "N selected" count + Clear button → Task 1 Step 4, Task 2 Steps 2, 6. ✔
- Figures recompute on change and select-all → Task 2 Step 6. ✔
- Clear and `close()` both empty the Set and hide the footer → Task 2 Steps 5, 6. ✔
- Existing CSS variables for theming → Task 1 Step 5. ✔
- `.tx-selbar` in `@media print` suppression → Task 1 Step 7. ✔
- Manual-only testing, JS not automated → Task 2 Step 9 (Go test only guards template validity). ✔
- Out of scope (persistence, export, other views) → nothing in the plan adds these. ✔

**Placeholder scan:** No TBD/TODO/"handle edge cases"; every code step shows complete code. ✔

**Type/name consistency:** IDs and classes produced in Task 1 (`tx-all`, `tx-selbar`, `tx-sel-count`, `tx-sel-figs`, `tx-sel-clear`, `tx-check`, `tx-row-check`, `tx-all-check`) match exactly the IDs/classes consumed in Task 2. Helper names `syncSelAll`, `renderSelbar`, `clearSel`, and `selected` are consistent across all Task 2 steps. ✔