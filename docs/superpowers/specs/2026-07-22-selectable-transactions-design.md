# Selectable Transactions with Running Total

**Date:** 2026-07-22
**Status:** Approved

## Problem

The transaction modal (the "popup" opened by clicking a ledger row) lists a period's
transactions read-only. Users want to pick a subset of those transactions and see the
summed expense of just that subset, so they can answer questions like "how much did
these five charges cost me together?"

## Scope

Front-end only. All changes live in `internal/infra/html/report.html` (the Go template
rendered by `internal/infra/html/renderer.go`). No Go, backend, or data-shape changes.

## Design

### Selection column
- Add a leading checkbox column to `.tx-table`: a header `<th>` holding a "select all"
  checkbox, and a checkbox `<td>` per row.
- Selection is tracked in a JavaScript `Set` of transaction-row **object references**.
  Because `renderRows` re-sorts the same `rows` objects rather than recreating them,
  the Set survives sorting and checkbox state is restored on each re-render.
- `open()` empties the Set so every time the modal opens it starts with nothing selected.

### Sticky footer bar (`.tx-selbar`)
- Rendered inside `.tx-dialog`, pinned below `.tx-scroll`.
- Carries the `[hidden]` attribute while 0 rows are selected; shown once ≥1 is selected.
- Displays three figures scoped to the current selection, styled to match the existing
  top totals bar:
  - **Income** — sum of selected transactions with `amt > 0`
  - **Expenses** — sum of selected transactions with `amt < 0` (the primary figure)
  - **Net** — income − expenses
- Also shows an "N selected" count and a **Clear** button.
- Figures recompute on every checkbox change and on select-all.

### Wiring
- `renderRows` emits the checkbox cell and sets `checked` from the Set.
- A checkbox `change` adds/removes that row's object in the Set, then recomputes and
  re-renders the footer.
- The header select-all checkbox adds or removes all currently-rendered rows, then runs
  the same recompute.
- Footer **Clear** and the existing `close()` both empty the Set and hide the footer.

### Styling & print
- `.tx-selbar` and the checkbox column use existing CSS variables (`--sheet`, `--rule`,
  `--c-sav`, `--neg`, `--muted`) so they theme correctly across the Nocturne, Ledger,
  and Almanac editions.
- Add `.tx-selbar` (and the checkbox column, via the modal being hidden already) to the
  `@media print` suppression rule, consistent with `.tx-modal` being hidden in print.

## Testing

`report.html` is a Go template with no JS unit harness. Verification is manual:

1. `go build ./...` succeeds.
2. Render a report and open the modal.
3. Confirm: individual select, select-all, clear, footer math (Income/Expenses/Net),
   and that selections persist across a column sort.

The selection JavaScript is **not** covered by automated tests — noted explicitly.

## Out of Scope

- Persisting selections across modal opens or page reloads.
- Exporting or acting on the selected set (copy, CSV, etc.).
- Selection in any view other than the transaction modal.
