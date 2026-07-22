# VISA reconciliation config in the web UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose VISA lump reconciliation as an editable section in the web config UI, with a seeded default so a fresh install shows a working example.

**Architecture:** Web-layer + config-seed addition only. A new `internal/infra/web/reconcile.go` mirrors the existing `rules.go` (view + parse helpers) but for a single optional record instead of a repeatable list. `config.Load` seeds a default `VisaReconcile` on a missing file (symmetric with `DefaultRuleSpecs()`); `handleGenerate` treats the submitted form as the source of truth for `VisaReconcile`, saving it under the same "save" checkbox gate as exclusions. No domain matching logic changes.

**Tech Stack:** Go 1.23, `net/http` + `html/template`, `github.com/stretchr/testify/require` for tests. No new dependencies.

**Design spec:** `docs/superpowers/specs/2026-07-22-visa-reconcile-config-ui-design.md`

## Global Constraints

- **No new dependencies.** `web` already imports `internal/domain/transaction` and `internal/infra/config`; `textfold` and matching logic are untouched.
- **No domain matching changes.** `ReconcileConfig`, `Validate`, `descriptionMatches`, and homoglyph folding stay exactly as they are. This plan only adds `DefaultReconcileConfig`.
- **Blank Description = off.** On submit, a blank VISA description means "no reconciliation" and persists as `nil`. A non-blank description is validated via `ReconcileConfig.Validate()`.
- **VISA section renders FIRST** in the config editor, above `<h2>Exclusion rules</h2>`.
- **Seeded default values (verbatim):** `Description = "ΠΛΗΡΩΜΗ VΙSΑ"` (Greek Ι U+0399, Α U+0391 — 12 runes: `0x3a0 0x39b 0x397 0x3a1 0x3a9 0x39c 0x397 0x20 0x56 0x399 0x53 0x391`), `MatchMode = MatchExact` (`"exact"`), `Branch = "96"`. These match `exclusion-rules.json` and the README.
- **Form field names:** `visa.description`, `visa.matchMode`, `visa.branch` (single record, no index).
- **Save gating mirrors exclusions:** persist only when `r.FormValue("save") != ""`; edits always apply to the current run regardless.
- **Commit trailer** on every commit:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- **Do not push or open a PR.** Commit locally only.

## Pre-existing working-tree note

`internal/infra/config/config_test.go` has an uncommitted, unrelated edit that
prepended a stray `]` to a test string (line ~35: `"]ΠΛΗΡΩΜΗ VΙSA"`). Task 2
edits this file. **Before Task 2's first change, revert that one line** back to
`"ΠΛΗΡΩΜΗ VΙSA"` so the stray character is not swept into a feature commit. This
is the only pre-existing change; leave nothing else of it behind.

## File Structure

- `internal/domain/transaction/transaction.go` — add `DefaultReconcileConfig()` (Task 1)
- `internal/domain/transaction/transaction_test.go` — default-config test (Task 1)
- `internal/infra/config/config.go` — seed default on missing file; update doc comment (Task 2)
- `internal/infra/config/config_test.go` — seeding tests (Task 2)
- **new** `internal/infra/web/reconcile.go` — `reconcileView`, `toReconcileView`, `parseReconcile` (Task 3)
- **new** `internal/infra/web/reconcile_test.go` — view + parse unit tests (Task 3)
- `internal/infra/web/server.go` — handler payload + wiring (Task 4)
- `internal/infra/web/index.html` — VISA section, rendered first (Task 4)
- `internal/infra/web/server_test.go` — handler save/off/render tests (Task 4)
- `README.md` — note the section is now editable in the UI (Task 4, minor)

---

### Task 1: Domain — `DefaultReconcileConfig`

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (add after `DefaultRuleSpecs`, ~line 226)
- Test: `internal/domain/transaction/transaction_test.go` (add after `TestDefaultRuleSpecs`, ~line 273)

**Interfaces:**
- Consumes: existing `ReconcileConfig`, `MatchExact`.
- Produces: `func DefaultReconcileConfig() *transaction.ReconcileConfig` — used by `config.Load` (Task 2). Returns a non-nil pointer to the documented canonical example.

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/transaction/transaction_test.go` (imports already include `testing` and `github.com/stretchr/testify/require`):

```go
func TestDefaultReconcileConfig(t *testing.T) {
	cfg := DefaultReconcileConfig()
	require.NotNil(t, cfg)
	require.NoError(t, cfg.Validate())
	require.Equal(t, MatchExact, cfg.MatchMode)
	require.Equal(t, "96", cfg.Branch)
	// Description is the documented homoglyph example (Greek Ι U+0399, Α U+0391).
	// Pin the exact runes so an accidental Latin-lookalike edit is caught.
	require.Equal(t,
		[]rune{0x3a0, 0x39b, 0x397, 0x3a1, 0x3a9, 0x39c, 0x397, 0x20, 0x56, 0x399, 0x53, 0x391},
		[]rune(cfg.Description),
	)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/transaction/ -run TestDefaultReconcileConfig`
Expected: FAIL — `undefined: DefaultReconcileConfig`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/domain/transaction/transaction.go`, immediately after `DefaultRuleSpecs` (after line 226, before `DefaultExclusionRules`):

```go
// DefaultReconcileConfig is the built-in VISA reconciliation example, matching
// the documented sample in exclusion-rules.json and the README. It seeds a
// fresh config so the web UI shows a working example on first run.
func DefaultReconcileConfig() *ReconcileConfig {
	return &ReconcileConfig{
		Description: "ΠΛΗΡΩΜΗ VΙSΑ", // Greek Ι U+0399, Α U+0391 — as in README/sample
		MatchMode:   MatchExact,
		Branch:      "96",
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/transaction/ -run TestDefaultReconcileConfig`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat(visa): add DefaultReconcileConfig seed for VISA reconciliation

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Config — seed default `VisaReconcile` on a missing file

**Files:**
- Modify: `internal/infra/config/config.go:31-42` (doc comment + missing-file seed)
- Test: `internal/infra/config/config_test.go` (update `TestLoadSeedsWhenMissing`; add `TestLoadSeedsVisaReconcileWhenMissing`)

**Interfaces:**
- Consumes: `transaction.DefaultReconcileConfig()` (Task 1), `transaction.DefaultRuleSpecs()`.
- Produces: `config.Load` on a **missing** file now returns `File{Exclusions: DefaultRuleSpecs(), VisaReconcile: DefaultReconcileConfig()}` and persists it. Existing files are unchanged: absence of a `visaReconcile` block still loads as `nil`.

- [ ] **Step 0: Revert the stray pre-existing edit**

In `internal/infra/config/config_test.go`, restore the one line changed in the
working tree so no stray character is committed:

```go
		VisaReconcile: &transaction.ReconcileConfig{
			Description: "ΠΛΗΡΩΜΗ VΙSA", MatchMode: transaction.MatchExact, Branch: "96",
		},
```

(Remove the leading `]` from the description string; leave the rest of the file
as-is until the steps below.) Verify with `git diff internal/infra/config/config_test.go`
— it should show no changes to that line after reverting.

- [ ] **Step 1: Update the failing tests**

The current `TestLoadSeedsWhenMissing` asserts `require.Nil(t, f.VisaReconcile)`;
that becomes wrong once we seed. Replace that assertion and add a focused test.

In `internal/infra/config/config_test.go`, change the body of
`TestLoadSeedsWhenMissing` so the VisaReconcile assertion reads:

```go
	require.Equal(t, transaction.DefaultReconcileConfig(), f.VisaReconcile)
```

(Replace the existing `require.Nil(t, f.VisaReconcile)` line. Leave the rest of
the test — the `DefaultRuleSpecs` check, `FileExists`, and re-load equality —
unchanged.)

Then add a new test after it:

```go
func TestLoadSeedsVisaReconcileWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")

	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, transaction.DefaultReconcileConfig(), f.VisaReconcile)

	// The seed is persisted, so a reload returns the same VISA config.
	again, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, f.VisaReconcile, again.VisaReconcile)
}
```

`TestLoadVisaReconcileAbsentIsNil` already covers "existing file without a
`visaReconcile` block loads as nil" — leave it as the no-retroactive-seeding
guard.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/config/ -run 'TestLoadSeeds'`
Expected: FAIL — seeded `VisaReconcile` is currently `nil`, not `DefaultReconcileConfig()`.

- [ ] **Step 3: Update the doc comment and seed**

In `internal/infra/config/config.go`, replace the `Load` doc comment (lines 31-34) and the missing-file seed line (line 38).

Doc comment becomes:

```go
// Load reads and validates the config object from path. A missing file is
// seeded with DefaultRuleSpecs() and DefaultReconcileConfig(), saved, and
// returned. A malformed file or invalid entry returns a descriptive error
// naming the path; it never silently falls back.
```

Seed line becomes:

```go
		f := File{
			Exclusions:    transaction.DefaultRuleSpecs(),
			VisaReconcile: transaction.DefaultReconcileConfig(),
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/config/`
Expected: PASS (all config tests, including the round-trip and absent-is-nil cases).

- [ ] **Step 5: Commit**

```bash
git add internal/infra/config/config.go internal/infra/config/config_test.go
git commit -m "feat(visa): seed default VISA reconciliation on a fresh config file

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Web — `reconcileView` + `parseReconcile`

**Files:**
- Create: `internal/infra/web/reconcile.go`
- Test: `internal/infra/web/reconcile_test.go`

**Interfaces:**
- Consumes: `transaction.ReconcileConfig`, `transaction.MatchMode`, `transaction.MatchExact`.
- Produces:
  - `type reconcileView struct { MatchMode, Description, Branch string }`
  - `func toReconcileView(c *transaction.ReconcileConfig) reconcileView` — nil → `{MatchMode: "exact"}` with empty Description/Branch; non-nil → mirror fields, empty MatchMode rendered as `"exact"`.
  - `func parseReconcile(form *multipart.Form) (*transaction.ReconcileConfig, error)` — reads `visa.description`/`visa.matchMode`/`visa.branch`; blank (trimmed) description → `(nil, nil)`; otherwise builds and validates the config, wrapping errors as `"VISA reconciliation: ..."`.
  - Both are used by `server.go` (Task 4). The template (Task 4) binds to `.Reconcile.MatchMode`, `.Reconcile.Description`, `.Reconcile.Branch`.

- [ ] **Step 1: Write the failing tests**

Create `internal/infra/web/reconcile_test.go`:

```go
package web

import (
	"mime/multipart"
	"testing"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestToReconcileView(t *testing.T) {
	t.Run("nil renders blank with exact default", func(t *testing.T) {
		v := toReconcileView(nil)
		require.Equal(t, reconcileView{MatchMode: "exact"}, v)
	})

	t.Run("populated config mirrors fields", func(t *testing.T) {
		v := toReconcileView(&transaction.ReconcileConfig{
			Description: "ΠΛΗΡΩΜΗ VΙSΑ", MatchMode: transaction.MatchContains, Branch: "96",
		})
		require.Equal(t, reconcileView{MatchMode: "contains", Description: "ΠΛΗΡΩΜΗ VΙSΑ", Branch: "96"}, v)
	})

	t.Run("empty match mode renders as exact", func(t *testing.T) {
		v := toReconcileView(&transaction.ReconcileConfig{Description: "X", Branch: "96"})
		require.Equal(t, "exact", v.MatchMode)
	})
}

// reconcileForm builds a multipart.Form carrying only visa.* fields.
func reconcileForm(desc, mode, branch string) *multipart.Form {
	return &multipart.Form{Value: map[string][]string{
		"visa.description": {desc},
		"visa.matchMode":   {mode},
		"visa.branch":      {branch},
	}}
}

func TestParseReconcile(t *testing.T) {
	t.Run("blank description is off", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm("   ", "exact", "96"))
		require.NoError(t, err)
		require.Nil(t, rc)
	})

	t.Run("missing fields are off", func(t *testing.T) {
		rc, err := parseReconcile(&multipart.Form{Value: map[string][]string{}})
		require.NoError(t, err)
		require.Nil(t, rc)
	})

	t.Run("valid fields build a config", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm(" ΠΛΗΡΩΜΗ VΙSΑ ", "exact", " 96 "))
		require.NoError(t, err)
		require.NotNil(t, rc)
		require.Equal(t, "ΠΛΗΡΩΜΗ VΙSΑ", rc.Description)
		require.Equal(t, transaction.MatchExact, rc.MatchMode)
		require.Equal(t, "96", rc.Branch)
	})

	t.Run("invalid match mode is a VISA-prefixed error", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm("X", "fuzzy", "96"))
		require.Error(t, err)
		require.Nil(t, rc)
		require.Contains(t, err.Error(), "VISA reconciliation:")
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/web/ -run 'TestToReconcileView|TestParseReconcile'`
Expected: FAIL — `undefined: reconcileView`, `toReconcileView`, `parseReconcile`.

- [ ] **Step 3: Write the implementation**

Create `internal/infra/web/reconcile.go`:

```go
package web

import (
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// reconcileView is the VISA-reconciliation section of the config editor, with
// MatchMode defaulted to "exact" for the template. A nil stored config renders
// as a blank section (reconciliation off).
type reconcileView struct {
	MatchMode   string
	Description string
	Branch      string
}

// toReconcileView converts the stored config into an editor view. A nil config
// (reconciliation off) renders as a blank section with MatchMode "exact".
func toReconcileView(c *transaction.ReconcileConfig) reconcileView {
	if c == nil {
		return reconcileView{MatchMode: string(transaction.MatchExact)}
	}
	mode := string(c.MatchMode)
	if mode == "" {
		mode = string(transaction.MatchExact)
	}
	return reconcileView{
		MatchMode:   mode,
		Description: c.Description,
		Branch:      c.Branch,
	}
}

// parseReconcile reads the visa.* form fields into a config. A blank Description
// means reconciliation is off and returns (nil, nil). Otherwise the config is
// validated, returning a "VISA reconciliation: ..." error on the first problem.
func parseReconcile(form *multipart.Form) (*transaction.ReconcileConfig, error) {
	desc := strings.TrimSpace(valueAt(form.Value["visa.description"], 0))
	if desc == "" {
		return nil, nil
	}
	cfg := &transaction.ReconcileConfig{
		Description: desc,
		MatchMode:   transaction.MatchMode(valueAt(form.Value["visa.matchMode"], 0)),
		Branch:      strings.TrimSpace(valueAt(form.Value["visa.branch"], 0)),
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("VISA reconciliation: %s", err)
	}
	return cfg, nil
}
```

(`valueAt` already exists in `rules.go` in this package and returns `""` for an out-of-range index, so a missing form field folds to the blank-description "off" path.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/web/ -run 'TestToReconcileView|TestParseReconcile'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/infra/web/reconcile.go internal/infra/web/reconcile_test.go
git commit -m "feat(visa): add reconcileView and parseReconcile for the config UI

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Web — wire the handler, template, and handler tests

**Files:**
- Modify: `internal/infra/web/server.go:44-54` (`handleIndex` payload), `:69-103` (`handleGenerate` wiring)
- Modify: `internal/infra/web/index.html` (add VISA section before `<h2>Exclusion rules</h2>`)
- Test: `internal/infra/web/server_test.go` (add VISA render + save + off subtests)
- Modify: `README.md` (minor note)

**Interfaces:**
- Consumes: `toReconcileView`, `parseReconcile`, `reconcileView` (Task 3); `cfg.VisaReconcile` (`config.File`).
- Produces: the rendered index carries a VISA section with fields `visa.description`/`visa.matchMode`/`visa.branch`; `POST /generate` treats the submitted VISA section as the source of truth for `cfg.VisaReconcile`, applies it to the run, and persists it under the `save` gate.

- [ ] **Step 1: Write the failing handler tests**

Add these subtests inside `TestHandleGenerate` in `internal/infra/web/server_test.go` (after the existing "saves rules when the checkbox is set" subtest), and one standalone render assertion. `multipartWith`, `tmpRules`, `csvHeader`, and `twoTxns` already exist.

```go
	t.Run("saves the VISA reconciliation config when the checkbox is set", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
			{"visa.description", "ΠΛΗΡΩΜΗ VΙSΑ"},
			{"visa.matchMode", "exact"},
			{"visa.branch", "96"},
			{"save", "on"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(path).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(data), `"visaReconcile"`)
		require.Contains(t, string(data), `"branch": "96"`)
	})

	t.Run("blank VISA description saves reconciliation off", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
			{"visa.description", ""},
			{"visa.matchMode", "exact"},
			{"visa.branch", "96"},
			{"save", "on"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(path).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NotContains(t, string(data), `"visaReconcile"`)
	})

	t.Run("rejects an invalid VISA match mode", func(t *testing.T) {
		fields := [][2]string{
			{"visa.description", "ΠΛΗΡΩΜΗ VΙSΑ"},
			{"visa.matchMode", "fuzzy"},
			{"visa.branch", "96"},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "VISA reconciliation")
	})
```

Add a standalone test (top level, alongside `TestHandleIndex`) asserting the section renders pre-filled from the seeded default:

```go
func TestHandleIndexRendersVISASection(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	New(tmpRules(t)).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `name="visa.description"`)
	require.Contains(t, body, `name="visa.branch"`)
	require.Contains(t, body, `value="96"`)                 // seeded default branch
	// VISA section appears before the exclusion-rules editor.
	require.Less(t, strings.Index(body, `name="visa.description"`), strings.Index(body, `name="rule.description"`))
}
```

This test uses `strings`; add `"strings"` to the `server_test.go` import block if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/web/ -run 'TestHandleGenerate|TestHandleIndexRendersVISASection'`
Expected: FAIL — the template has no `visa.*` fields yet and `handleGenerate` does not call `parseReconcile`; the invalid-mode subtest returns 200 instead of 400.

- [ ] **Step 3: Update `handleIndex` payload**

In `internal/infra/web/server.go`, replace the `indexTmpl.Execute` call (line 51):

```go
	payload := struct {
		Reconcile reconcileView
		Rules     []ruleView
	}{toReconcileView(cfg.VisaReconcile), toRuleViews(cfg.Exclusions)}
	if err := indexTmpl.Execute(w, payload); err != nil {
		slog.Error("rendering index", "error", err)
	}
```

- [ ] **Step 4: Wire `parseReconcile` into `handleGenerate`**

In `internal/infra/web/server.go`, after the `parseRules` block (lines 69-73) and before `config.Load` (line 75), add:

```go
	rc, err := parseReconcile(r.MultipartForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
```

Then, after `cfg.Exclusions = specs` (line 80) and before the save gate, add:

```go
	cfg.VisaReconcile = rc
```

The existing `report.NewService(..., cfg.VisaReconcile)` call (line 103) now
uses the freshly parsed config; no further change there.

- [ ] **Step 5: Add the VISA section to the template**

In `internal/infra/web/index.html`, insert this block **before** the
`<h2>Exclusion rules</h2>` line (currently line 42), directly after the
`<ul id="list"></ul>` element (line 40):

```html
        <h2>VISA reconciliation</h2>
        <p class="hint">
          When a bank export shows one lump “VISA payment” and you also attach the itemized
          VISA statement, this replaces the lump with the individual purchases (plus a single
          <code>VISA LEFTOVERS</code> row that keeps the month's total intact). A bank row is
          treated as the lump when its <strong>description</strong> matches
          (<code>exact</code> or <code>contains</code>) <em>and</em> its <strong>branch</strong>
          (Κατάστημα) equals the value below. <strong>Leave the description blank to turn VISA
          reconciliation off.</strong> Tick “Save…” below to keep your changes.
        </p>
        <div class="rule">
          <select name="visa.matchMode">
            <option value="exact" {{if eq .Reconcile.MatchMode "exact"}}selected{{end}}>exact</option>
            <option value="contains" {{if eq .Reconcile.MatchMode "contains"}}selected{{end}}>contains</option>
          </select>
          <input class="desc" name="visa.description" placeholder="description (blank = off)" value="{{.Reconcile.Description}}">
          <input class="src" name="visa.branch" placeholder="branch" value="{{.Reconcile.Branch}}">
        </div>

```

No add/remove buttons and no `rowTemplate` — it is a single fixed record. The
existing `{{range .Rules}}` block is unchanged and still renders below.

- [ ] **Step 6: Run the web tests to verify they pass**

Run: `go test ./internal/infra/web/`
Expected: PASS (new VISA subtests, the render test, and all pre-existing handler/rules tests).

- [ ] **Step 7: Update the README note (minor)**

In `README.md`, in the VISA reconciliation section (around lines 156-183),
add one sentence noting the matcher is now editable in the web UI, e.g.:

> The VISA matcher can be edited directly in the web UI's **VISA reconciliation**
> section (or in `exclusion-rules.json` under `visaReconcile`); leave the
> description blank to turn it off.

Keep the existing `exclusion-rules.json`/`visaReconcile` documentation intact.

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 9: Commit**

```bash
git add internal/infra/web/server.go internal/infra/web/index.html internal/infra/web/server_test.go README.md
git commit -m "feat(visa): expose VISA reconciliation as an editable config UI section

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:**
  - Single record, not a list → Task 3 (`reconcileView`, single `.rule` row in Task 4). ✓
  - Blank Description = off → Task 3 `parseReconcile` returns `(nil,nil)`; Task 4 off-test. ✓
  - Seeded default vs. persisted off-state → Task 1 (`DefaultReconcileConfig`), Task 2 (seed on missing file only), Task 3 (`toReconcileView(nil)` blank). ✓
  - Save gating mirrors exclusions → Task 4 (`cfg.VisaReconcile = rc` before the existing `save` gate). ✓
  - Web layer new file `reconcile.go` with the three declared symbols → Task 3. ✓
  - `server.go` payload + handler wiring → Task 4. ✓
  - Template VISA section FIRST → Task 4 Step 5 (inserted before `<h2>Exclusion rules</h2>`); asserted by `TestHandleIndexRendersVISASection`. ✓
  - Testing plan (domain default, config seeding, web parse/view, handler save+off) → Tasks 1-4. ✓
  - README note → Task 4 Step 7 (marked minor). ✓
- **Placeholder scan:** No TBD/TODO; every code step contains complete code. ✓
- **Type consistency:** `reconcileView{MatchMode, Description, Branch}` and `parseReconcile(*multipart.Form) (*transaction.ReconcileConfig, error)` are defined in Task 3 and consumed identically in Task 4; template bindings `.Reconcile.MatchMode/.Description/.Branch` and form names `visa.matchMode/.description/.branch` match across template, parser, and tests. `DefaultReconcileConfig() *ReconcileConfig` defined in Task 1, consumed in Task 2. ✓
