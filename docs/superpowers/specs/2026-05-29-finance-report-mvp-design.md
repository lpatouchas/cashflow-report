# Finance Report MVP — Design Spec

**Date:** 2026-05-29  
**Status:** Approved

## Overview

A Go CLI tool that scans a folder of bank CSV exports, filters out inter-account transfers, and produces an HTML report summarising total and monthly income, expenses, and savings.

---

## Architecture

Follows Clean Architecture as defined in `AGENTS.md`: `domain ← app ← infra`.  
Module: `github.com/lpatouchas/personal-finance`, Go 1.23. Note: the existing `go.mod` uses module name `personal-finance` — the implementation plan must update it to match AGENTS.md.

```
personal-finance/
├── main.go
├── data/                                 # drop CSV files here (input)
├── report.html                           # generated report (output)
├── docs/
│   ├── plans/                            # implementation plans
│   └── superpowers/specs/               # design specs (this file)
└── internal/
    ├── domain/
    │   └── transaction/
    │       ├── transaction.go            # Transaction entity + Summary/MonthlyBreakdown
    │       └── repository.go             # Repository interface
    ├── app/
    │   └── report/
    │       ├── service.go                # GenerateReport use case
    │       └── renderer.go              # Renderer output port (interface)
    └── infra/
        ├── csv/
        │   └── repository.go             # reads ./data/, parses CSVs
        └── html/
            └── renderer.go              # renders Summary → ./report.html
```

---

## Domain Model

### Transaction (entity)

```go
type Transaction struct {
    ID          string    // Αρ. συναλλαγής
    Date        time.Time // Ημερομηνία, format DD/MM/YYYY
    Description string    // Αιτιολογία
    Amount      float64   // always positive; Ποσό with comma-decimal (53,79 → 53.79)
    IsDebit     bool      // Χ = true (expense), Π = false (income)
    SourceFile  string    // originating CSV filename
}
```

### Transfer Detection Rule

If the same `ID` appears in transactions from 2 or more distinct `SourceFile` values, every transaction with that ID is an inter-account transfer and is excluded from all calculations.

Single-file duplicates (same ID, same file) are treated as data anomalies — all occurrences of that ID within that file are logged and skipped.

### Summary (value object)

```go
type Summary struct {
    TotalIncome   float64
    TotalExpenses float64
    Savings       float64 // TotalIncome - TotalExpenses
    ByMonth       []MonthlyBreakdown
}

type MonthlyBreakdown struct {
    Year    int
    Month   time.Month
    Income   float64
    Expenses float64
    Savings  float64
}
```

`ByMonth` is sorted chronologically (oldest first).

### Repository Interface

```go
// internal/domain/transaction/repository.go
type Repository interface {
    GetAll(ctx context.Context) ([]Transaction, error)
}
```

---

## App Layer

### `internal/app/report/renderer.go`

```go
type Renderer interface {
    Render(ctx context.Context, summary transaction.Summary) error
}
```

### `internal/app/report/service.go`

`GenerateReport(ctx context.Context) error`:
1. Call `repo.GetAll(ctx)` to load all transactions.
2. Group by `ID`; collect distinct `SourceFile` values per group.
3. Exclude any transaction whose `ID` appears across 2+ source files.
4. Aggregate remaining transactions into `Summary` (total + per month).
5. Call `renderer.Render(ctx, summary)`.

---

## Infra Layer

### CSV Repository (`internal/infra/csv/`)

- Scans `./data/` for `*.csv` files; returns `"no CSV files found in ./data/"` if empty.
- CSV format: semicolon-separated, UTF-8, Greek headers.
- Strips `="..."` wrappers from string fields.
- Converts comma-decimal amounts (`53,79` → `53.79`).
- Parses dates as `DD/MM/YYYY`.
- Skips header row and blank/malformed rows (logs each skip with row number and filename).

### HTML Renderer (`internal/infra/html/`)

- Uses Go stdlib `html/template`.
- Writes to `./report.html`.
- Amounts formatted in Greek locale: euro symbol, dot thousands separator, comma decimal (e.g. `€1.234,56`).
- Report layout:
  - Header: generated date + date range of loaded transactions
  - Summary card: Total Income / Total Expenses / Savings (highlighted)
  - Monthly breakdown table: Month | Income | Expenses | Savings, oldest → newest

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| `./data/` is empty | Return error: `"no CSV files found in ./data/"` |
| CSV file unreadable | Return wrapped error with filename |
| Malformed row | Log row number + filename, skip row, continue |
| `./report.html` unwritable | Return wrapped error |

No domain-layer errors needed — domain logic is pure computation.

---

## Testing Strategy

Per `AGENTS.md`: 100% coverage, TDD, table-driven tests, hand-written testify mocks.

| Layer | Strategy |
|---|---|
| Domain | Unit tests — transfer detection, Summary aggregation. Pure functions, no mocks. |
| App | Unit tests — mock `Repository` + mock `Renderer`. Verifies filtering and aggregation. |
| Infra CSV | Table-driven with fixture CSV files: normal rows, `="..."` wrapping, comma decimals, blank rows, empty folder. |
| Infra HTML | Test that `Render` writes a file containing expected month labels and formatted amounts. |

---

## Future Phases (out of scope for MVP)

- Save each report run to SQLite/PostgreSQL (`internal/infra/storage/`)
- Load additional monthly CSV drops without reprocessing existing data
- Per-account income/expense breakdown in the report
- Transaction list drilldown in HTML report
