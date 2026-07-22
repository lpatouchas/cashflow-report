# VISA reconciliation config in the web UI

**Date:** 2026-07-22
**Status:** Design — approved for planning

## Problem

VISA lump reconciliation is configured by the `visaReconcile` object in
`exclusion-rules.json` (`description`, `matchMode`, `branch`). The setting is
fully wired end to end — `config.Load` reads and validates it, and
`report.NewService(..., cfg.VisaReconcile)` honors it at report generation —
but the web config UI never exposes it.

Tracing the web layer:

- `handleIndex` renders only `toRuleViews(cfg.Exclusions)`; the template
  payload is `struct{ Rules []ruleView }` — no reconcile field.
- `parseRules` reads only the `rule.*` form columns; there is no parse path
  for reconciliation.
- `handleGenerate` sets `cfg.Exclusions = specs` and saves. Because it loads
  the full `cfg` first and overwrites only `Exclusions`, an existing
  `VisaReconcile` is preserved across saves — but it can never be created or
  edited from the UI.

Net: to change VISA lump matching today a user must hand-edit JSON. This is
unbuilt scope in the web layer, not a deliberate exclusion.

Additionally, exclusions ship with a seeded default (`DefaultRuleSpecs()`)
that appears pre-filled in the UI on a fresh install; VISA reconciliation has
no equivalent default, so even once exposed a fresh install would show an
empty section.

## Goal

Expose VISA reconciliation in the config UI as an editable section, and give
it a seeded default so a fresh install shows a working example — symmetric
with how exclusion rules already behave.

## Non-goals

- **No domain changes to matching.** `ReconcileConfig`, `Validate`,
  `descriptionMatches`, and homoglyph folding are unchanged. This is a
  web-layer + config-seed addition.
- **No change to `config.Save`.** It already round-trips `VisaReconcile`.
- **Not a list.** `VisaReconcile` is a single optional object, so the UI is a
  single fixed section — no add/remove rows, unlike the exclusion editor.

## Design

### Single record, not a list

`config.File.VisaReconcile` is `*transaction.ReconcileConfig` (one object or
nil), so the UI section is one fixed set of three inputs, not a repeatable
row editor.

### Empty state: blank Description = off

The section is always visible. On submit, a blank Description means "no VISA
reconciliation" and is saved as `nil` (matching how `parseRules` skips fully
blank exclusion rows). A non-blank Description is validated via
`ReconcileConfig.Validate()`.

### Seeded default vs. persisted off-state

The default lives in the **persistence seed**, and the view renders **stored
state faithfully**:

- `config.Load` seeds `VisaReconcile: DefaultReconcileConfig()` only when the
  config file is **missing** (the same trigger that seeds `DefaultRuleSpecs()`).
- `toReconcileView(nil)` renders **blank** fields (MatchMode defaulting to
  `exact`).

Consequences (both intended):

- Fresh install (no file) → form pre-filled with the documented example.
- User deliberately turns it off (blank Description, saved → nil) → the
  section shows blank on reload and stays off; the default is not resurrected.
- A pre-existing config file that predates this feature (has no
  `visaReconcile` block) shows blank — the user fills it in once.

`DefaultReconcileConfig()` returns the documented canonical example, matching
the checked-in `exclusion-rules.json` and README:

```go
func DefaultReconcileConfig() *ReconcileConfig {
    return &ReconcileConfig{
        Description: "ΠΛΗΡΩΜΗ VΙSΑ", // Greek Ι U+0399, Α U+0391 — as in README/sample
        MatchMode:   MatchExact,
        Branch:      "96",
    }
}
```

### Save gating mirrors exclusions

Reconcile edits always apply to the current report run; they persist to
`exclusion-rules.json` only when the "save" checkbox is set — the same gate
`handleGenerate` already applies to exclusions (`r.FormValue("save")`).

### Web layer

New file `internal/infra/web/reconcile.go` (keeps `rules.go` single-purpose):

```go
// reconcileView is the VISA-reconciliation section of the config editor,
// with MatchMode defaulted to "exact" for the template.
type reconcileView struct {
    MatchMode   string
    Description string
    Branch      string
}

// toReconcileView converts the stored config into an editor view. A nil
// config (reconciliation off) renders as a blank section with MatchMode
// defaulted to "exact".
func toReconcileView(c *transaction.ReconcileConfig) reconcileView

// parseReconcile reads the visa.* form fields into a config. A blank
// Description means reconciliation is off and returns (nil, nil). Otherwise
// the config is validated, returning a "VISA reconciliation: ..." error on
// the first problem.
func parseReconcile(form *multipart.Form) (*transaction.ReconcileConfig, error)
```

`toReconcileView` rules:
- `c == nil` → `reconcileView{MatchMode: "exact"}` (Description and Branch empty).
- `c != nil` → copy fields; if `c.MatchMode == ""`, render `"exact"`.

`parseReconcile` rules:
- Read `visa.description`, `visa.matchMode`, `visa.branch`; `TrimSpace`
  description and branch.
- Blank description → `return nil, nil`.
- Else build `&transaction.ReconcileConfig{Description, MatchMode(visa.matchMode), Branch}`,
  call `Validate()`, wrap any error as `fmt.Errorf("VISA reconciliation: %s", err)`.

`server.go`:
- `handleIndex` payload becomes
  `struct{ Reconcile reconcileView; Rules []ruleView }{toReconcileView(cfg.VisaReconcile), toRuleViews(cfg.Exclusions)}`.
- `handleGenerate`: after `parseRules`, call `parseReconcile`; on error return
  the same 400-with-message path. Set `cfg.VisaReconcile = rc` before the save
  gate and before `report.NewService(..., cfg.VisaReconcile)`.

### Template

In `index.html`, add a VISA section **before** `<h2>Exclusion rules</h2>`,
reusing the existing `.rule` widget classes:

- `<h2>VISA reconciliation</h2>` plus a short `.hint` explaining the lump
  matcher (description + match mode + branch; blank description disables it).
- One `.rule` row bound to `.Reconcile`:
  - `<select name="visa.matchMode">` with `exact`/`contains`, `selected` per
    `.Reconcile.MatchMode`.
  - `<input name="visa.description" value="{{.Reconcile.Description}}">`
  - `<input name="visa.branch" value="{{.Reconcile.Branch}}">`

No add/remove buttons and no `rowTemplate` — it is a single fixed record.

### Import direction

No new package dependencies: `web` already imports
`internal/domain/transaction` and `internal/infra/config`.

## Testing

**Domain** (`transaction_test.go`):
- `DefaultReconcileConfig()` returns the documented values and passes
  `Validate()`.

**Config** (`config_test.go`):
- Loading a **missing** file seeds `VisaReconcile` equal to
  `DefaultReconcileConfig()` (in addition to the existing exclusions seed).
- An existing file without a `visaReconcile` block still loads with
  `VisaReconcile == nil` (no retroactive seeding).

**Web** (`reconcile_test.go`):
- `parseReconcile`: blank description → `(nil, nil)`; valid fields →
  populated config; invalid match mode → error prefixed `"VISA reconciliation:"`.
- `toReconcileView`: nil → `{MatchMode: "exact"}` with empty Description/Branch;
  populated config → mirrored fields; empty MatchMode → `"exact"`.
- Handler: a `POST /generate` with `save` set and valid `visa.*` fields
  persists the config; with a blank `visa.description` and `save` set,
  the persisted `VisaReconcile` is nil.

## Files touched

- `internal/domain/transaction/transaction.go` — add `DefaultReconcileConfig`
- `internal/domain/transaction/transaction_test.go` — default test
- `internal/infra/config/config.go` — seed default on missing file; doc comment
- `internal/infra/config/config_test.go` — seeding tests
- **new** `internal/infra/web/reconcile.go` — view + parse
- **new** `internal/infra/web/reconcile_test.go` — view + parse tests
- `internal/infra/web/server.go` — payload + handler wiring
- `internal/infra/web/index.html` — VISA section (first)
- `README.md` — note the section is now editable in the UI (optional, minor)
