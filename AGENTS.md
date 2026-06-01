# cashflow-report — Agent Instructions

## What this is

A single Go binary that turns Greek **Alpha Bank** `.csv` exports into one
self-contained, interactive HTML report — totals, per-month breakdown, monthly
averages, a trend chart, and a click-through transaction modal per month.

Two modes (see `main.go` → `dispatch`):

- **`serve`** (default) — starts a local `net/http` web UI to upload CSVs, edit
  exclusion rules, and view the generated report. Opens the browser unless
  `--no-open`.
- **`generate`** — headless: reads a `--data` folder of CSVs and writes
  `--out` HTML.

## Architecture

Clean architecture; the dependency rule points inward: **domain ← app ← infra**.

- **`internal/domain/transaction/`** — pure business logic, zero external deps.
  Entities (`Transaction`, `Summary`, `MonthlyBreakdown`, `AccountBreakdown`,
  `MonthlyAverages`), the `Summarize` aggregation, `FilterTransfers`, the
  exclusion-rule model (`RuleSpec` / `CompileRule(s)` / `ApplyExclusions`), and
  the `Repository` input port. No struct tags of any kind.
- **`internal/app/report/`** — `Service` orchestrates the pipeline:
  load → `FilterTransfers` → `ApplyExclusions` → `Summarize` → render. Defines
  the `Renderer` output port.
- **`internal/infra/`** — adapters that depend on app + domain:
  - `csv/` — `Repository` implementation; parses semicolon-separated Greek CSV.
  - `html/` — `Renderer` implementation (`NewFile` / `NewWriter`); one
    `html/template` produces the whole page (inline CSS/JS, three themes).
  - `web/` — stdlib `net/http` server: upload, rules editor, generate-and-view.
  - `config/` — load/save exclusion rules as JSON.

### Key rules

1. **Ports & adapters.** Interfaces (`transaction.Repository`,
   `report.Renderer`) are defined in the inner layer; `infra` implements them.
   `domain` and `app` never import `infra`.
2. **Pure domain, no tags.** No `json`/`db`/framework tags on domain types;
   any transport/serialization concern lives in the adapter (e.g. the
   `json`-tagged view structs are private to `infra/html`; `RuleSpec` carries
   `json` tags because it is the persisted config shape).
3. **Mocks live next to the interface they mock**, hand-written with testify
   (`domain/transaction/mock_repository.go`, `app/report/mock_renderer.go`).
   No code generation.
4. **Per-layer wiring in `main.go`** (`runGenerate` / `runServe`). No DI
   framework; the graph is small enough to assemble by hand.
5. **Keep HTTP handlers thin.** Parse the request, build the `report.Service`,
   delegate; the pipeline logic stays in `app`/`domain`.

## Domain notes

- **Inter-account transfers / duplicates:** any transaction ID
  (`Αρ. συναλλαγής`) appearing more than once across the loaded files is
  dropped entirely (`FilterTransfers`), before exclusion rules run.
- **Exclusion rules:** a `RuleSpec` matches when every specified field matches
  (AND): `matchMode` (`exact`/`contains`), optional `isDebit`, required
  `description`, optional `sourceFile`. Stored in `exclusion-rules.json` beside
  the binary (seeded on first run); editable from the web UI.
- **Greek formatting:** amounts are `1.550,00` (`.` thousands, `,` decimals);
  sign `Χ` = debit (expense), `Π` = credit (income); dates `DD/MM/YYYY`. Output
  currency is rendered EU-style (`€1.234,56`).

## Tech stack

- **Module:** `github.com/lpatouchas/cashflow-report`, **Go 1.23**
- **Standard library only:** `net/http` (`ServeMux` with Go 1.22 method
  routing, e.g. `"POST /generate"`), `html/template`, `encoding/csv`,
  `encoding/json`, `log/slog`, `flag`.
- **Sole dependency:** `github.com/stretchr/testify` (tests only).

There is intentionally no database, web framework, auth, or metrics stack —
keep it that way unless a plan says otherwise.

## Logging

- `log/slog` structured logging. Malformed/skipped CSV rows are logged as
  warnings with `file` + `line`; do not fail the whole run on one bad row.

## Testing

- **TDD (Red-Green-Refactor)** for new features.
- **Table-driven tests** with testify; one `_test.go` per package.
- Aim for high coverage; there is no CI gate yet, so coverage is on you.
- Hand-written mocks next to the interface they mock (see Key rule 3).

## Code style

- Idiomatic Go (Effective Go, Go proverbs).
- Comments only on non-obvious business logic — don't narrate what code does.
- `context.Context` is the first parameter on interface methods.

## Plans

Plans live in `docs/superpowers/plans/` (and the original MVP in `docs/plans/`),
named `yyyy-mm-dd-<slug>.md`; matching design specs live in
`docs/superpowers/specs/`. When adding a plan, add a reference below.

- [Finance Report MVP](docs/plans/2026-05-29_finance-report-mvp.md) — CSV → HTML report, excluding inter-account transfers.
- [Monthly Average](docs/superpowers/plans/2026-05-30-monthly-average.md) — per-month averages over the data's calendar span.
- [Month Transaction Detail](docs/superpowers/plans/2026-05-30-month-transaction-detail.md) — click a month to open a sortable modal of its transactions.
- [Popup Account Breakdown](docs/superpowers/plans/2026-05-31-popup-account-breakdown.md) — per-account income/expense rows inside the month modal.
- [CLI and Web App](docs/superpowers/plans/2026-06-01-cli-and-web-app.md) — `serve`/`generate` subcommands and the upload UI.
- [Configurable Exclusion Rules](docs/superpowers/plans/2026-06-01-configurable-exclusion-rules.md) — editable, persisted exclusion rules.

## Branching

Gitflow so `main` stays releasable:

- Branch from up-to-date `main`: `git checkout -b feature/<slug>` (slug mirrors
  the plan filename). One branch per plan.
- `fix/<slug>` for bug fixes without a plan; `chore/<slug>` for tooling-only.
- Open the PR against `main` only after tests pass and docs are updated.
- Never commit feature work directly to `main`.

## Commit messages

Conventional-Commits style, matching the existing history:
`type(scope): summary` (e.g. `docs:`, `refactor:`, `feat(web):`). Keep the
subject imperative and concise; add a body when the why isn't obvious.

## Updating this file

Update `AGENTS.md` when any of the following change: an architectural decision
or convention, the tech stack/dependencies, the project structure, the testing
strategy, or the set of plans. Keep `README.md` accurate for local development
in step with code. Keep this file concise and actionable — a quick reference,
not a design document.
