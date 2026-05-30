# Month Transaction Detail — Design

- **Date:** 2026-05-30
- **Status:** Ready for implementation
- **Branch:** `feature/month-transaction-detail` (branch from up-to-date `main`)

## Overview

The HTML report's Monthly Breakdown table shows per-month totals. This feature
lets the reader **click a month row to open a modal listing that month's
individual transactions, one line per transaction, sortable by any column**.

The transactions shown are exactly the filtered set that feeds the month's
totals (inter-account transfers and excluded anomalies are already removed
upstream by `FilterTransfers`), so the modal is always consistent with the row
it expands.

## Goals

- Click a month row → modal with that month's transactions, one per line.
- Columns: **Date · Description · Amount (signed) · Source file**.
- Every column is click-to-sort (asc/desc toggle), matching the existing
  monthly-table sort behavior.
- Default sort on open: **Date, newest first**.
- Modal matches the active edition theme and is hidden in print.

## Non-Goals

- No filtering/search within the modal.
- No editing, categorizing, or annotating transactions.
- No pagination — months have a bounded number of transactions.
- No JS unit-test harness (the project has none today; consistent with the
  existing chart/sort JS, which is not unit-tested).

## Approach

**Approach A (chosen): retain transactions in the domain `Summary`, render the
modal client-side.** `Summarize()` already buckets transactions by month; it
will also keep each month's transactions. The renderer serializes them to JSON
alongside the existing chart payload (`window.FIN`), and a small JS handler
opens a single reusable modal and sorts rows client-side — reusing the patterns
already in `renderer.go`.

Rejected alternatives:
- **B — pass filtered transactions to the renderer as a second argument:**
  leaks month-grouping into infra when the domain already does it, and creates
  two inputs that must stay consistent.
- **C — server-render every modal table into the HTML:** heavy duplication and
  still needs JS for open/close and sorting, so it saves nothing.

## Design

### 1. Domain (`internal/domain/transaction/transaction.go`)

Add one field to `MonthlyBreakdown`:

```go
type MonthlyBreakdown struct {
    Year         int
    Month        time.Month
    Income       float64
    Expenses     float64
    Savings      float64
    Transactions []Transaction // the movements that make up this month
}
```

`Summarize()` appends each transaction to `mb.Transactions` in the same loop
that accumulates `Income`/`Expenses` — no extra pass. Ordering within the slice
follows input order; the modal sorts client-side so domain order is not
significant (default sort is applied in JS).

### 2. Rendering (`internal/infra/html/renderer.go`)

Add a per-transaction view model:

```go
type txVM struct {
    Date   string  `json:"date"` // "12 May 2026", for display
    Sort   string  `json:"k"`    // "2026-05-12", for date sorting
    Desc   string  `json:"desc"`
    Amount float64 `json:"amt"`  // signed: income +, expense −
    Source string  `json:"src"`
}
```

`buildView` builds a `map[string]([]txVM)` keyed by the same `Key` the table
rows already use (`"2026-05"`), populated from `mb.Transactions`. Amount is
signed at build time (`-Amount` when `IsDebit`).

The JSON payload embedded as `window.FIN` gains a `tx` field:
`{"months": [...], "tx": {"2026-05": [ {…}, … ], …}}`.

Each monthly `<tr>` already carries `data-key="{{ .Key }}"`; that is the lookup
key. Rows are made interactive (`role="button"`, `tabindex="0"`, pointer
cursor) so they open the modal on click or Enter. Sort `<th>` elements live in
`<thead>` and keep their own handler; only `<tbody>` rows get the open handler,
so there is no conflict.

### 3. Modal markup & styling

- **One reusable modal element**, added once to the HTML, hidden by default —
  not one per month. JS fills its title (`"May 2026"`), a context line with the
  month's totals (income / expenses / savings), and the transaction table body
  on open.
- **Columns:** Date · Description · Amount (signed; green/red via existing
  `--pos` / `--neg`) · Source. All four headers click-to-sort.
- **Dismiss:** backdrop click, an × button, or Escape. On close, focus returns
  to the row that opened the modal.
- **Theming:** uses the existing CSS custom properties (`--sheet`, `--ink`,
  `--rule`, `--muted`, `--hair`, `--pos`, `--neg`) so it matches the active
  edition (Ledger / Almanac / Nocturne).
- **Print:** hidden via the existing `@media print` block, like the other
  interactive controls.

### 4. Sorting inside the modal

Mirrors the existing table-sort logic in `renderer.go`:

- Click a header → toggle asc/desc on that key, re-render rows, show the ▼/▲
  arrow on the active column.
- Date sorts on the `k` field (`2026-05-12`). Amount sorts numerically.
  Description and Source sort as strings via locale-aware `localeCompare`.
- Default on open: **Date, descending (newest first)**.

## Edge Cases

- A `MonthlyBreakdown` always has ≥1 transaction (it would not exist
  otherwise), so the modal needs no empty state.
- Excluded transfers/anomalies are filtered before `Summarize()`, so they never
  reach `Transactions` and never appear in the modal.
- "No transactions to report" report state (`HasData == false`) renders no
  table and therefore no modal triggers — unchanged.
- Months with a single transaction still render a one-row sortable table.

## Testing

Per project rules (TDD, table-driven, testify):

- **Domain** (`transaction_test.go`): table-driven cases asserting
  `Summarize()` attaches the correct transactions to each `MonthlyBreakdown`
  (count and contents per month), and that filtered-out transfers/anomalies do
  not appear in any month's `Transactions`.
- **Renderer** (`renderer_test.go`): assert the rendered HTML contains the modal
  scaffold, that `<tbody>` rows carry the interactive trigger attributes, and
  that the embedded JSON includes the `tx` map with the right per-month keys and
  signed amounts.
- **JS behavior** (open/close, sort): not unit-tested — no JS harness exists in
  the project, consistent with the existing chart and table-sort JS.

## Documentation Impact

- `README.md`: note that month rows are clickable to view transactions.
- `AGENTS.md`: add this spec under the Plans/specs references when the
  implementation plan is created.
