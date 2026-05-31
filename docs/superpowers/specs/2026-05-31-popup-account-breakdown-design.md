# Popup Per-Account Income/Expense Breakdown — Design

- **Date:** 2026-05-31
- **Status:** Draft (awaiting review)
- **Topic:** Add a per-account (per `.csv`) income/expense summary to the month transaction modal, and clean up source labels.

## Overview

The month transaction modal (`tx-modal`) opens when a user clicks a month row in
the Monthly Breakdown table. Today it shows a month-wide totals bar
(Income / Expenses / Savings) and a flat transaction list with a `Source` column
holding the originating CSV filename (e.g. `kathimerinos.csv`).

This change adds a **By Account** breakdown block between the month totals bar and
the transaction list, showing **income and expenses per `.csv` account** (no net
column). Account labels are shown **without the `.csv` extension**, both in the new
block and in the existing transaction list's `Source` column.

## Decisions

| Decision | Choice |
| --- | --- |
| Placement | Breakdown block **above** the transaction list, below the month totals bar |
| Figures per account | **Income + Expenses only** (no net) |
| Aggregation layer | **Domain** (`Summarize`) — most testable, architecturally consistent |
| Label format | Strip `.csv` extension at the view boundary (renderer), in both the block and the list |

## Architecture

Aggregation is business logic, so it lives in the domain. The renderer serializes
it into the existing `window.FIN` payload, and the modal JS renders it. The raw
`SourceFile` identity stays in the domain; the `.csv` extension is stripped only
for display in the renderer.

### Domain — `internal/domain/transaction/transaction.go`

- New value type:
  ```go
  // AccountBreakdown is per-source income/expense totals within one month.
  type AccountBreakdown struct {
      Source   string  // raw SourceFile, e.g. "kathimerinos.csv"
      Income   float64
      Expenses float64
  }
  ```
- `MonthlyBreakdown` gains `ByAccount []AccountBreakdown`.
- In `Summarize`'s existing per-transaction loop, tally income/expenses into a
  per-`SourceFile` map scoped to each month's `MonthlyBreakdown`. After the loop,
  emit `ByAccount` **sorted alphabetically by `Source`** so output is deterministic.

### Renderer — `internal/infra/html/renderer.go`

- New view model:
  ```go
  type acctVM struct {
      Source   string  `json:"src"` // display label, .csv stripped
      Income   float64 `json:"inc"`
      Expenses float64 `json:"exp"`
  }
  ```
- New helper `accountLabel(src string) string` strips a trailing `.csv`
  (case-insensitive on the extension) for display.
- Build `acctByMonth map[string][]acctVM`, keyed by the same `"YYYY-MM"` month key
  as `txByMonth`, and add it to the serialized payload as `FIN.acct`.
- Apply `accountLabel` to `txVM.Source` as well, so the transaction list's `Source`
  column shows stripped names.
- Add markup: a `tx-accounts` container/section between `#tx-totals` and `.tx-scroll`.

### Modal JS

- Read `var ACCT = (window.FIN && window.FIN.acct) || {};`
- In `open(tr)`, look up `ACCT[key]` and render the **By Account** block before the
  transaction table renders. One row per account: stripped source name,
  `+€income`, `−€expenses`, reusing the existing `eu()` formatter and the
  `tx-amt pos` / `tx-amt neg` color classes. Hide the block when there are no
  accounts.

## Data Flow

```
CSV repo (SourceFile set)
  → Summarize: per-month ByAccount[] (raw Source, sorted)
    → buildView: acctByMonth{"YYYY-MM": [acctVM]} with accountLabel() applied
       + txVM.Source stripped via accountLabel()
      → window.FIN.acct (JSON)
        → modal open(): render By Account block + transaction list
```

## Error / Edge Handling

- Month with a single account → one row.
- Account with only income or only expenses → the empty side shows `€0,00`.
- Empty `ByAccount` (defensive) → block hidden.
- Source without a `.csv` suffix → shown unchanged.
- Sort is alphabetical by raw `Source`, stable across renders.

## Testing (TDD, table-driven)

**Domain (`transaction_test.go`):**
- Single account in a month.
- Multiple accounts in one month, asserting alphabetical `ByAccount` order.
- Account with only income; account with only expenses.
- Totals per account sum back to the month's `Income`/`Expenses`.

**Renderer (`renderer_test.go`):**
- Multi-account month: `FIN.acct` JSON present, correctly shaped and keyed.
- `accountLabel` strips `.csv` (and leaves non-`.csv` names unchanged).
- Transaction list `Source` values are stripped of `.csv`.

## Out of Scope

- Net/savings column per account.
- Sorting/interactivity within the By Account block.
- Changes to the main Monthly Breakdown table or hero stats.
