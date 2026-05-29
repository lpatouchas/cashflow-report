# personal-finance

A CLI that summarises bank transactions into an HTML report.

## Usage

1. Drop one or more bank CSV exports into `./data/`.
2. Run:

   ```bash
   go run .
   ```

3. Open the generated `./report.html`.

## What it does

- Loads every `*.csv` in `./data/` (semicolon-separated Greek bank export format).
- Excludes inter-account transfers: any transaction ID (`Αρ. συναλλαγής`)
  appearing more than once across the loaded files is treated as a transfer
  or duplicate and left out of the totals.
- Reports total income, expenses, and savings, plus a per-month breakdown.

## Development

```bash
go test ./... -cover   # all packages must stay at 100%
```
