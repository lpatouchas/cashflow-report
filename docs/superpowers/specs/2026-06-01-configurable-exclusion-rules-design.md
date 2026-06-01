# Configurable Exclusion Rules — Design

**Date:** 2026-06-01
**Status:** Approved (design)

## Problem

The exclusion rule that drops instant-transfer moves out of the investment
account is hardcoded in `internal/domain/transaction/transaction.go`
(`DefaultExclusionRules`, ~line 80). Changing or adding rules requires editing
Go and rebuilding. The prior CLI/web work left an injectable `ExclusionRule`
seam for exactly this; now we fill it: let users define rules from the web page
or via a config file, with the existing rule pre-filled as the default.

## Goals

- Users define exclusion rules without touching Go.
- Rules persist in a JSON config file, seeded on first run with today's default
  rule.
- The web upload page edits rules inline and can either apply them once or save
  them back to the file.
- The CLI points at a config file via `--config`.
- Reuse the existing `report.NewService(..., rules)` seam unchanged.

## Non-Goals

- CLI rule-management subcommands (`rules add/remove`) or ad-hoc `--exclude`
  flags. CLI users edit JSON or use the web UI.
- Regex or field matching beyond description (exact/contains), debit/credit, and
  source file.
- Multi-user / server-side rule storage. This stays a single local file.

## Rule model

A rule matches a transaction when **all** of its specified conditions hold (AND).
Unspecified fields are wildcards. Description is always required so a rule can
never silently match everything.

| Field | Type | Meaning |
|---|---|---|
| `matchMode` | `"exact"` \| `"contains"` | how `description` is compared |
| `isDebit` | `*bool` (omitempty) | `nil` = any; `true` = debit only; `false` = credit only |
| `description` | `string` (required) | text to match against the transaction description |
| `sourceFile` | `string` (omitempty) | empty = all files; else exact filename, e.g. `invest.csv` |

The current default expressed as a spec:
`{matchMode: "exact", isDebit: true, description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", sourceFile: "invest.csv"}`.

## Approach (chosen — "A")

Domain owns the rule data model and the compile-to-predicate logic (pure,
disk-free, unit-testable). A new `internal/infra/config` adapter owns JSON
persistence and first-run seeding. The composition root loads specs, compiles
them to `[]transaction.ExclusionRule`, and feeds the existing service seam.

### 1. Domain (`internal/domain/transaction`)

```go
// MatchMode controls how a rule's Description is compared.
type MatchMode string

const (
    MatchExact    MatchMode = "exact"
    MatchContains MatchMode = "contains"
)

// RuleSpec is a serializable exclusion rule.
// Description is required. IsDebit nil = any; true = debit; false = credit.
// SourceFile empty = all files.
type RuleSpec struct {
    MatchMode   MatchMode `json:"matchMode"`
    IsDebit     *bool     `json:"isDebit,omitempty"`
    Description string    `json:"description"`
    SourceFile  string    `json:"sourceFile,omitempty"`
}

// Validate reports whether the spec is well-formed:
// Description must be non-empty; MatchMode must be exact or contains.
func (s RuleSpec) Validate() error

// CompileRule turns a spec into a predicate that ANDs its specified fields.
func CompileRule(s RuleSpec) ExclusionRule

// CompileRules compiles a slice of specs (skips Validate; callers validate first).
func CompileRules(specs []RuleSpec) []ExclusionRule

// DefaultRuleSpecs is the built-in rule set expressed as data.
func DefaultRuleSpecs() []RuleSpec
```

- `CompileRule` builds a closure that checks, in order, only the fields that are
  set: debit/credit (skipped when `IsDebit == nil`), description (`==` for
  exact, `strings.Contains` for contains), source file (skipped when empty).
- `DefaultExclusionRules()` is **kept**, reimplemented as
  `CompileRules(DefaultRuleSpecs())`, so existing callers and tests are
  unaffected and behavior is identical.

### 2. Persistence (`internal/infra/config`)

```go
// Load reads and validates rule specs from path.
// If the file is missing, it seeds the file with DefaultRuleSpecs(), saves it,
// and returns those defaults.
func Load(path string) ([]transaction.RuleSpec, error)

// Save writes specs to path as indented JSON.
func Save(path string, specs []transaction.RuleSpec) error

// DefaultPath is exclusion-rules.json next to the executable,
// falling back to ./exclusion-rules.json.
func DefaultPath() string
```

- First run (file missing) writes the defaults to disk and returns them — this
  is the "pre-filled default."
- `Load` validates every spec via `RuleSpec.Validate`. A malformed file or
  invalid spec returns a descriptive error with the path; it never silently
  falls back to "no rules" or "exclude everything."
- `DefaultPath` resolves via `os.Executable()`; if that fails it returns
  `./exclusion-rules.json`.

### 3. Web (`internal/infra/web`)

- The embedded upload page gains an **Exclusion rules** editor: rows of
  `[match mode ▾] [debit/credit/any ▾] [description] [source] [remove]`, an
  **+ add rule** button, a **"Save these rules for next time"** checkbox, and the
  existing **Generate report** button. Rows are pre-filled from `config.Load`.
- `POST /generate` additionally parses the rule rows into `[]RuleSpec`,
  validates them, and compiles them for the run. If the save checkbox is set, it
  calls `config.Save` before generating. A validation failure returns `400`
  with a readable message naming the offending rule
  (e.g. `Rule 2: description is required.`).
- `Server` is constructed with the config path; `New(rules, configPath)` or an
  equivalent. The index handler reads current specs through `config.Load` to
  pre-fill the editor.

### 4. CLI (`main.go`)

- `generate` and `serve` flag sets gain `--config <path>`, defaulting to
  `config.DefaultPath()`.
- `runGenerate` and `runServe` call `config.Load(path)`, then
  `transaction.CompileRules(specs)`, and pass the result into the service /
  web server. No rule-editing subcommands.

### 5. Error handling

- Domain `Validate` returns descriptive errors.
- Web maps validation errors to `400` with a user-readable message; the server
  stays up.
- CLI prints config/validation errors to stderr and exits non-zero.
- A malformed config file fails loudly with path and reason — never a silent
  fallback.

## Components & dependencies

| Package | Role | Depends on |
|---|---|---|
| `internal/domain/transaction` | `RuleSpec`, `MatchMode`, `Validate`, `CompileRule(s)`, `DefaultRuleSpecs`, `DefaultExclusionRules` | — |
| `internal/infra/config` | `Load`, `Save`, `DefaultPath` (JSON, seeding) | domain |
| `internal/infra/web` | rules editor UI, parse/validate/compile in `POST /generate`, optional save | report, csv, html, config, domain |
| `internal/app/report` | `Service` (unchanged — already rules-aware) | domain |
| `main` | `--config` flag plumbing into `runGenerate` / `runServe` | report, csv, html, web, config, domain |

## Testing

- `transaction`: `CompileRule` truth table (each field present/absent; exact vs
  contains; three-state debit), `Validate` (blank description, unknown mode),
  `DefaultRuleSpecs` excludes the original line-68 case, `DefaultExclusionRules`
  behavior unchanged.
- `config`: seed-on-missing (file created with defaults), save/load round-trip,
  malformed JSON → error, invalid spec → error, `DefaultPath`.
- `web` (`httptest`): `POST /generate` with custom rule rows excludes the
  expected transactions; save checkbox writes the file; invalid rule row → 400
  with message; index pre-fills rows from config.
- `main`: `--config` plumbs through `dispatch` into generate/serve.
- Not covered (acceptable): `os.Executable` error fallback, `openBrowser`,
  `ListenAndServe`. Aim high; 100% is not required.

## Future work (out of scope)

- CLI rule-management subcommands or repeatable `--exclude` flags.
- Richer matching (regex, amount thresholds, date ranges).
- A dedicated rules-management page separate from the upload flow.
