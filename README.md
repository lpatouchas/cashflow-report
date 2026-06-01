# personal-finance

Summarises bank transactions into an interactive HTML report. Use it from a
browser (no terminal needed) or from the command line.

## Quick start (web app)

1. Build the binary once (or download a prebuilt one):

   ```bash
   go build -o personal-finance .
   ```

2. Double-click `personal-finance` (or run `./personal-finance`). Your browser
   opens to a local page.
3. Drop your bank CSV exports onto the page and click **Generate report**.

The server listens on `http://localhost:8080`. Use `--no-open` to skip opening
the browser, or `--addr :1234` to change the port:

```bash
./personal-finance serve --addr :1234 --no-open
```

## Command line

Generate a report headlessly from a folder of CSV exports:

```bash
./personal-finance generate --data ./data --out ./report.html
```

`--data` defaults to `./data` and `--out` to `./report.html`. Then open the
generated `report.html`.

Use `--config path/to/rules.json` with `generate` or `serve` to choose a
different exclusion-rules file.

## What it does

- Loads every `*.csv` in the data folder (semicolon-separated Greek bank export
  format).
- Excludes inter-account transfers: any transaction ID (`Αρ. συναλλαγής`)
  appearing more than once across the loaded files is treated as a transfer or
  duplicate and left out of the totals.
- Applies user-defined exclusion rules. Rules live in `exclusion-rules.json`
  (created next to the binary on first run, pre-filled with the built-in
  instant-transfer rule). Each rule matches a transaction by description
  (exact or contains), optionally constrained to debit/credit and a single
  source file. Edit rules right on the web page (tick "Save these rules for
  next time" to persist them), or point at a different file with `--config`.
- Reports total income, expenses, and savings, plus a per-month breakdown.
- The report's monthly table is interactive: click a month to open a modal
  listing that month's individual transactions, sortable by any column.

## Development

```bash
go test ./...   # run the test suite
```
