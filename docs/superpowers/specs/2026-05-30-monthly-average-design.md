# Monthly Average — Design

- **Date:** 2026-05-30
- **Status:** Approved (ready for implementation plan)
- **Branch:** `feature/monthly-average`

## Overview

The finance report already produces totals (income / expenses / savings) and a
chronological per-month breakdown. This feature adds a **monthly average** for
income, expenses, and savings, displayed as a second row of summary cards in the
HTML report.

The average is computed over the **full calendar span** of the data: every month
from the earliest to the latest transaction is counted in the divisor, including
months with no transactions (counted as zero). This gives a realistic average
spend/save rate over a continuous period rather than inflating it by skipping
empty months.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| What to average | Income, expenses, and savings | Mirrors the existing summary cards; complete picture. |
| Divisor (month count) | Full calendar span (earliest→latest inclusive, empty months = 0) | More accurate over a continuous period; avoids overstating the per-month rate. |
| Display | New row of average cards under the totals | Visually prominent and consistent with existing cards. |
| Where computed | `domain.Summarize` | Pure business logic; belongs next to existing aggregation; trivially unit-testable; renderer only displays. |
| Divisor transparency | Subtitle "over N months" on the average cards | Makes the denominator visible to the reader. |

## Domain Changes — `internal/domain/transaction/transaction.go`

New value type and a field on the existing `Summary`:

```go
// MonthlyAverages holds per-month averages over the report's calendar span.
type MonthlyAverages struct {
    Months   int     // calendar-span divisor (earliest→latest inclusive)
    Income   float64
    Expenses float64
    Savings  float64
}

type Summary struct {
    TotalIncome   float64
    TotalExpenses float64
    Savings       float64
    ByMonth       []MonthlyBreakdown
    Averages      MonthlyAverages // new
}
```

`Summarize` is extended to compute the divisor and averages:

- Track the earliest and latest transaction dates while iterating.
- `span = (latest.Year*12 + int(latest.Month)) - (earliest.Year*12 + int(earliest.Month)) + 1`
- `Averages.Months = span`
- `Averages.Income = TotalIncome / span`, `Averages.Expenses = TotalExpenses / span`,
  `Averages.Savings = Savings / span` (equivalently `Income - Expenses`, consistent).

### Edge cases

- **No transactions:** `Months = 0`, all averages `0.0`. No division performed (no
  divide-by-zero).
- **Single month (or all transactions in one month):** `span = 1`; averages equal totals.
- **Multi-year span:** handled by the `Year*12 + Month` arithmetic.
- **Gap months:** e.g. data in Jan and Mar only → `span = 3`; Feb counts as zero.

## Renderer Changes — `internal/infra/html/renderer.go`

Add a second row of cards beneath the existing totals row, rendered only when
`Summary.Averages.Months > 0`:

```
Monthly Average (over N months)
┌ Avg Income / mo ┐ ┌ Avg Expenses / mo ┐ ┌ Avg Savings / mo ┐
│   €2.100,00     │ │     €1.450,00     │ │     €650,00      │
```

- Reuses the existing `.cards` / `.card` CSS classes and the `euro` template function.
- The subtitle shows `Summary.Averages.Months` so the divisor is transparent.
- When `Months == 0` (no transactions), the average row is omitted entirely, matching
  the existing "No transactions to report." behaviour.

No changes to the CSV repository, the `app/report` service, the `Renderer` port, or
`main.go` — the new `Averages` field flows through the existing `Summary` value.

## Testing

Following the project's TDD + table-driven + testify conventions.

**Domain (`transaction_test.go`)** — extend the `Summarize` table:
- Gap month (Jan + Mar) → `Months == 3`, averages divide by 3.
- Single month → `Months == 1`, averages equal totals.
- Empty input → `Months == 0`, all averages `0`, no panic.
- Multi-year span → correct month count across a year boundary.

**Infra HTML (`renderer_test.go`)**:
- Average cards render with correctly Greek-formatted values and the "over N months" subtitle.
- Average row omitted when the summary has no transactions.

## Out of Scope (YAGNI)

- Median or other statistics.
- Configurable divisor (data-months vs calendar-span) — full calendar span is fixed.
- Averaging in any non-HTML output (no other renderer exists yet).
