# CLI + Local Web App — Design

**Date:** 2026-06-01
**Status:** Approved (design)

## Problem

Generating `report.html` today requires the Go toolchain and a repo checkout:
`main.go` hardcodes `dataDir = "./data"` and `outputPath = "./report.html"`, takes
no flags, and the README instructs users to run `go run .`. This blocks two
audiences at once:

- **Technical users** want a real binary with flags, not `go run .`.
- **Non-technical users** can't use a terminal at all.

## Goals

- A prebuilt single binary, no Go toolchain needed to run.
- A proper **CLI** (`generate` with `--data` / `--out`) for technical/scriptable use.
- A **local web app** (double-click → browser) for non-technical users: upload
  CSVs, click Generate, view the report.
- Reuse the existing pipeline (`GetAll → FilterTransfers → Summarize → Render`)
  with the smallest sensible refactor.
- Lay a clean seam for **future external exclusion-rules config** (see line 68 of
  `internal/domain/transaction/transaction.go`) without building the config
  system yet.

## Non-Goals

- The external config file format and its loading (future sub-project; this design
  only prepares the seam).
- Cross-compilation / release artifacts (YAGNI for now; documented build steps only).
- A native desktop GUI.
- 100% test coverage. Aim high; the genuinely untestable bits (`openBrowser`,
  `ListenAndServe`) are kept thin and left uncovered.

## Approach (chosen)

Single binary, two modes. No new heavy dependencies — stdlib `net/http`, `flag`,
`embed`.

### 1. Command structure

`main.go` becomes a thin dispatcher; per-subcommand logic lives in testable
functions (`runGenerate`, `runServe`). `main` only parses `os.Args` and dispatches.

| Invocation | Behavior |
|---|---|
| `personal-finance` (no args / double-click) | Aliases `serve` |
| `personal-finance serve [--addr :8080] [--no-open]` | Start local web server, open browser |
| `personal-finance generate [--data ./data] [--out ./report.html]` | Headless one-shot (today's behavior, now flagged) |
| `personal-finance --help` / `--version` | Usage / version |

Note: today's bare-binary behavior (generate from `./data`) moves under
`generate`. No-args must be the friendly web path because a double-click can't
pass arguments.

### 2. Renderer refactor

`internal/infra/html` currently writes to a fixed path via `Render(ctx, summary)`.
Extract the template-building into a shared internal `render(w io.Writer, summary)`
and expose two constructors, **both satisfying the existing `report.Renderer`
port** so `report.Service` is untouched:

- `html.NewFile(path)` — writes to disk (CLI).
- `html.NewWriter(w io.Writer)` — writes to any writer; web uses a `bytes.Buffer`.

### 3. Exclusion-rules seam (domain)

Move the hardcoded business rule out of `FilterTransfers` into a named, injectable,
swappable concept. No behavior change.

```go
// ExclusionRule reports whether a transaction should be excluded from totals.
type ExclusionRule func(Transaction) bool

// DefaultExclusionRules are the built-in rules applied until external config exists.
func DefaultExclusionRules() []ExclusionRule

// ApplyExclusions drops any transaction matching any rule.
func ApplyExclusions(txns []Transaction, rules []ExclusionRule) []Transaction
```

- `FilterTransfers` is narrowed to its single responsibility: dropping
  transfer/duplicate IDs (count > 1). The external-account-move rule (line 68)
  moves into `DefaultExclusionRules`.
- `report.Service` is constructed with `rules []transaction.ExclusionRule`. Pipeline
  becomes `GetAll → FilterTransfers → ApplyExclusions(rules) → Summarize → Render`.
- Both CLI and web pass `transaction.DefaultExclusionRules()` for now. Later, a
  `config` adapter loads rules and feeds the same seam — a one-line change at the
  composition root instead of domain surgery.

### 4. Web mode (`internal/infra/web`)

New delivery adapter alongside `csv` and `html`. A `Server` holds the mux +
handlers; a thin `Run()` calls `ListenAndServe`.

```
GET  /          → embedded upload page (drag/drop or file-picker, multiple CSVs)
POST /generate  → save uploads to a temp dir (os.MkdirTemp)
                → csv.New(tempDir) + html.NewWriter(buf) + report.Service(rules)
                → run pipeline → respond with the rendered report HTML
                → defer os.RemoveAll(tempDir)
```

- Saving uploads to a temp dir reuses the CSV repo and the **cross-file
  transfer-exclusion logic unchanged** (it must see all files together).
- `POST /generate` responds with the full report HTML; the browser navigates to
  it. The report includes a "Generate another" link back to `/`.
- The upload page is `//go:embed`-ed so the binary is a single self-contained file.
- `openBrowser(url)` is an isolated per-OS helper (`open` / `xdg-open` / `start`),
  skippable via `--no-open`.

### 5. Error handling

- **generate**: unchanged — `slog.Error` + `os.Exit(1)`.
- **web**: handlers return readable messages with appropriate status and the
  server stays up:
  - no files uploaded → `400` "Please upload at least one CSV file."
  - unparseable CSV → `422` "Couldn't read <file>: <reason>."
  Non-technical users never see a stack trace.

## Components & dependencies

| Package | Role | Depends on |
|---|---|---|
| `internal/domain/transaction` | Entities, `FilterTransfers`, `ApplyExclusions`, `ExclusionRule`, `DefaultExclusionRules`, `Summarize` | — |
| `internal/infra/csv` | `Repository.GetAll` from a dir | domain |
| `internal/infra/html` | `NewFile`, `NewWriter`, shared `render` | domain |
| `internal/infra/web` | `Server`, handlers, embedded page, `openBrowser` | report, csv, html, domain |
| `internal/app/report` | `Service` (now rules-aware) | domain |
| `main` | flag dispatch: `runGenerate`, `runServe` | report, csv, html, web, domain |

## Testing

- `transaction`: `FilterTransfers` (dedup only), `ApplyExclusions` with rules,
  `DefaultExclusionRules` covers the line-68 case.
- `html`: writer-based render produces expected totals/markup; file constructor
  writes to disk.
- `web` (`httptest`): `GET /` serves the page; `POST /generate` with sample
  multipart CSV returns a report containing expected numbers; no-files → 400;
  bad CSV → 422 with message.
- `main`: table-test subcommand dispatch where practical.
- Not covered (acceptable): `openBrowser`, `ListenAndServe`.

## README

Three documented paths: build/download the binary; **double-click for the web
app**; or `personal-finance generate` for the CLI.

## Future work (out of scope)

- External config file (format + loader) feeding `ExclusionRule`s; `--config` flag;
  possibly a web UI for managing rules. The seam in §3 is the attach point.
- Optional cross-compiled release artifacts.
