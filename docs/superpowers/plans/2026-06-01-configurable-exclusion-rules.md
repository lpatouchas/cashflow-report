# Configurable Exclusion Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users define transaction-exclusion rules from the web page or a JSON config file, seeded with today's hardcoded default, without editing Go.

**Architecture:** Domain (`transaction`) owns a serializable `RuleSpec` and pure compile-to-predicate logic. A new `internal/infra/config` adapter owns JSON load/save and first-run seeding. The web upload page edits rules inline (apply once or save back); the CLI takes `--config`. Both feed the existing `report.NewService(..., rules)` seam.

**Tech Stack:** Go 1.23 stdlib (`encoding/json`, `html/template`, `embed`, `net/http`, `flag`), testify.

---

## File Structure

- `internal/domain/transaction/transaction.go` — add `MatchMode`, `RuleSpec`, `Validate`, `CompileRule`, `CompileRules`, `DefaultRuleSpecs`; reimplement `DefaultExclusionRules`.
- `internal/domain/transaction/transaction_test.go` — tests for the above.
- `internal/infra/config/config.go` — new: `Load`, `Save`, `DefaultPath`.
- `internal/infra/config/config_test.go` — new: seeding, round-trip, error cases.
- `internal/infra/web/page.go` — parse `index.html` as an `html/template`.
- `internal/infra/web/index.html` — add the rules editor + save checkbox.
- `internal/infra/web/server.go` — `New(configPath)`, template index, parse/validate/compile/save rules in `POST /generate`.
- `internal/infra/web/rules.go` — new: `ruleView`, `toRuleViews`, `parseRules`.
- `internal/infra/web/server_test.go` — update `New` calls; add rules tests.
- `main.go` — `--config` flag on `generate`/`serve`; plumb into `runGenerate`/`runServe`.
- `main_test.go` — extend dispatch test for `--config`.
- `README.md` — document the rules file and web editor.

---

## Task 1: Domain rule model

**Files:**
- Modify: `internal/domain/transaction/transaction.go`
- Test: `internal/domain/transaction/transaction_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/domain/transaction/transaction_test.go`:

```go
func boolPtr(b bool) *bool { return &b }

func TestRuleSpecValidate(t *testing.T) {
	require.NoError(t, RuleSpec{MatchMode: MatchExact, Description: "x"}.Validate())
	require.NoError(t, RuleSpec{MatchMode: MatchContains, Description: "x"}.Validate())

	err := RuleSpec{MatchMode: MatchExact, Description: "  "}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "description")

	err = RuleSpec{MatchMode: "regex", Description: "x"}.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "match mode")
}

func TestCompileRule(t *testing.T) {
	debit := Transaction{Description: "PAY", IsDebit: true, SourceFile: "a.csv"}
	credit := Transaction{Description: "PAY", IsDebit: false, SourceFile: "b.csv"}

	// exact description only (isDebit any, no source)
	r := CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY"})
	require.True(t, r(debit))
	require.True(t, r(credit))
	require.False(t, r(Transaction{Description: "PAYMENT"}))

	// contains
	r = CompileRule(RuleSpec{MatchMode: MatchContains, Description: "AY"})
	require.True(t, r(Transaction{Description: "PAYMENT"}))
	require.False(t, r(Transaction{Description: "NOPE"}))

	// debit-only
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", IsDebit: boolPtr(true)})
	require.True(t, r(debit))
	require.False(t, r(credit))

	// credit-only
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", IsDebit: boolPtr(false)})
	require.False(t, r(debit))
	require.True(t, r(credit))

	// source-file scoped
	r = CompileRule(RuleSpec{MatchMode: MatchExact, Description: "PAY", SourceFile: "a.csv"})
	require.True(t, r(debit))
	require.False(t, r(credit)) // credit is on b.csv
}

func TestDefaultRuleSpecs(t *testing.T) {
	specs := DefaultRuleSpecs()
	require.Len(t, specs, 1)
	require.NoError(t, specs[0].Validate())

	rules := CompileRules(specs)
	hit := Transaction{Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", IsDebit: true, SourceFile: "invest.csv"}
	miss := Transaction{Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", IsDebit: true, SourceFile: "other.csv"}
	require.True(t, rules[0](hit))
	require.False(t, rules[0](miss))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/transaction/ -run 'RuleSpec|CompileRule|DefaultRuleSpecs' -v`
Expected: compile error / undefined `RuleSpec`, `MatchMode`, etc.

- [ ] **Step 3: Implement the model**

In `internal/domain/transaction/transaction.go`, update the import block to add `errors`, `fmt`, `strings`:

```go
import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)
```

Replace the existing `DefaultExclusionRules` function (the closure at ~lines 80-86) with:

```go
// MatchMode controls how a RuleSpec's Description is compared.
type MatchMode string

const (
	MatchExact    MatchMode = "exact"
	MatchContains MatchMode = "contains"
)

// RuleSpec is a serializable exclusion rule. A transaction matches when every
// specified field matches (AND); unspecified fields are wildcards.
// Description is required. IsDebit nil = any; true = debit only; false = credit
// only. SourceFile empty = all files.
type RuleSpec struct {
	MatchMode   MatchMode `json:"matchMode"`
	IsDebit     *bool     `json:"isDebit,omitempty"`
	Description string    `json:"description"`
	SourceFile  string    `json:"sourceFile,omitempty"`
}

// Validate reports whether the spec is well-formed.
func (s RuleSpec) Validate() error {
	if strings.TrimSpace(s.Description) == "" {
		return errors.New("description is required")
	}
	switch s.MatchMode {
	case MatchExact, MatchContains:
		return nil
	default:
		return fmt.Errorf("unknown match mode %q (use %q or %q)", s.MatchMode, MatchExact, MatchContains)
	}
}

// CompileRule turns a spec into a predicate. An unrecognised match mode is
// treated as exact (the safe default); callers should Validate first.
func CompileRule(s RuleSpec) ExclusionRule {
	return func(t Transaction) bool {
		if s.IsDebit != nil && t.IsDebit != *s.IsDebit {
			return false
		}
		if s.MatchMode == MatchContains {
			if !strings.Contains(t.Description, s.Description) {
				return false
			}
		} else if t.Description != s.Description {
			return false
		}
		if s.SourceFile != "" && t.SourceFile != s.SourceFile {
			return false
		}
		return true
	}
}

// CompileRules compiles specs into predicates. It does not validate; validate
// specs before compiling.
func CompileRules(specs []RuleSpec) []ExclusionRule {
	rules := make([]ExclusionRule, 0, len(specs))
	for _, s := range specs {
		rules = append(rules, CompileRule(s))
	}
	return rules
}

// DefaultRuleSpecs is the built-in rule set expressed as data: the single
// "external account move" rule (an instant-transfer debit on invest.csv).
func DefaultRuleSpecs() []RuleSpec {
	debit := true
	return []RuleSpec{{
		MatchMode:   MatchExact,
		IsDebit:     &debit,
		Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS",
		SourceFile:  "invest.csv",
	}}
}

// DefaultExclusionRules are the built-in rules applied until external
// configuration overrides them. Equivalent to CompileRules(DefaultRuleSpecs()).
func DefaultExclusionRules() []ExclusionRule {
	return CompileRules(DefaultRuleSpecs())
}
```

Keep the existing `ExclusionRule` type and `ApplyExclusions` function exactly as they are.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/transaction/ -v`
Expected: PASS, including the pre-existing `TestApplyExclusions` and `TestDefaultExclusionRules`.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git add internal/domain/transaction/
git commit -m "feat(transaction): add serializable RuleSpec and compile-to-predicate"
```

---

## Task 2: Config adapter

**Files:**
- Create: `internal/infra/config/config.go`
- Test: `internal/infra/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/infra/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestLoadSeedsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")

	specs, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, transaction.DefaultRuleSpecs(), specs)

	// the file was written
	require.FileExists(t, path)

	// a second load reads the saved file and matches
	again, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, specs, again)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	debit := false
	in := []transaction.RuleSpec{
		{MatchMode: transaction.MatchContains, IsDebit: &debit, Description: "FEE", SourceFile: "a.csv"},
		{MatchMode: transaction.MatchExact, Description: "RENT"},
	}
	require.NoError(t, Save(path, in))

	out, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestLoadMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), path)
}

func TestLoadInvalidSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"matchMode":"exact","description":""}]`), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule 1")
}

func TestDefaultPath(t *testing.T) {
	require.Contains(t, DefaultPath(), "exclusion-rules.json")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/config/ -v`
Expected: build failure — package/functions do not exist.

- [ ] **Step 3: Implement the adapter**

Create `internal/infra/config/config.go`:

```go
// Package config loads and saves user-defined exclusion rules as JSON.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// DefaultPath is exclusion-rules.json next to the executable, falling back to
// ./exclusion-rules.json when the executable path can't be resolved.
func DefaultPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "./exclusion-rules.json"
	}
	return filepath.Join(filepath.Dir(exe), "exclusion-rules.json")
}

// Load reads and validates rule specs from path. A missing file is seeded with
// DefaultRuleSpecs(), saved, and returned. A malformed file or invalid spec
// returns a descriptive error naming the path; it never silently falls back.
func Load(path string) ([]transaction.RuleSpec, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		specs := transaction.DefaultRuleSpecs()
		if err := Save(path, specs); err != nil {
			return nil, err
		}
		return specs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var specs []transaction.RuleSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, s := range specs {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("%s: rule %d: %w", path, i+1, err)
		}
	}
	return specs, nil
}

// Save writes specs to path as indented JSON.
func Save(path string, specs []transaction.RuleSpec) error {
	data, err := json.MarshalIndent(specs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/config/ -v`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/infra/config/
git add internal/infra/config/
git commit -m "feat(config): JSON exclusion-rules adapter with first-run seeding"
```

---

## Task 3: Web rules editor

**Files:**
- Modify: `internal/infra/web/page.go`
- Modify: `internal/infra/web/index.html`
- Modify: `internal/infra/web/server.go`
- Create: `internal/infra/web/rules.go`
- Test: `internal/infra/web/server_test.go`

- [ ] **Step 1: Write the failing tests**

Replace the body of `internal/infra/web/server_test.go` with the version below. Changes: `New(nil)` → `New(<temp path>)`; add a `tmpRules` helper; add rules-parsing and save tests.

```go
package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

// tmpRules returns a fresh rules-file path inside a temp dir (seeded on first Load).
func tmpRules(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "rules.json")
}

// multipartCSV builds a multipart body with one CSV file under field "files".
func multipartCSV(t *testing.T, name, body string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", name)
	require.NoError(t, err)
	_, err = fw.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

// multipartWith builds a multipart body with one CSV file plus extra form fields
// (key may repeat for parallel rule columns).
func multipartWith(t *testing.T, name, body string, fields [][2]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", name)
	require.NoError(t, err)
	_, err = fw.Write([]byte(body))
	require.NoError(t, err)
	for _, kv := range fields {
		require.NoError(t, w.WriteField(kv[0], kv[1]))
	}
	require.NoError(t, w.Close())
	return &buf, w.FormDataContentType()
}

func TestHandleIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	New(tmpRules(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `action="/generate"`)
	require.Contains(t, rec.Body.String(), `name="files"`)
	// the seeded default rule is pre-filled
	require.Contains(t, rec.Body.String(), "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS")
}

func TestHandleGenerate(t *testing.T) {
	twoTxns := csvHeader + "\n" +
		`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
		`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"

	t.Run("generates a report from an uploaded csv", func(t *testing.T) {
		buf, ct := multipartCSV(t, "acc.csv", twoTxns)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "May 2026")
		require.Contains(t, rec.Body.String(), "Generate another")
	})

	t.Run("applies a submitted exclusion rule", func(t *testing.T) {
		// Exclude the SHOP debit; only the SALARY credit should remain.
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "debit"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "1.550") // income kept
		require.NotContains(t, rec.Body.String(), "53,79") // SHOP expense excluded
	})

	t.Run("saves rules when the checkbox is set", func(t *testing.T) {
		path := tmpRules(t)
		fields := [][2]string{
			{"rule.matchMode", "contains"},
			{"rule.isDebit", "any"},
			{"rule.description", "SHOP"},
			{"rule.sourceFile", ""},
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
		require.Contains(t, string(data), `"description": "SHOP"`)
		require.Contains(t, string(data), `"matchMode": "contains"`)
	})

	t.Run("rejects an invalid rule row", func(t *testing.T) {
		fields := [][2]string{
			{"rule.matchMode", "exact"},
			{"rule.isDebit", "debit"},
			{"rule.description", ""},     // required
			{"rule.sourceFile", "x.csv"}, // makes the row non-blank
		}
		buf, ct := multipartWith(t, "acc.csv", twoTxns, fields)
		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "Rule 1")
	})

	t.Run("rejects when no files are uploaded", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.Close())

		req := httptest.NewRequest(http.MethodPost, "/generate", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "at least one CSV")
	})

	t.Run("rejects a malformed multipart body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader([]byte("garbage")))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=nope")
		rec := httptest.NewRecorder()
		New(tmpRules(t)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestParseRulesSkipsBlankRows(t *testing.T) {
	form := &multipart.Form{Value: map[string][]string{
		"rule.matchMode":   {"exact", "exact"},
		"rule.isDebit":     {"any", "debit"},
		"rule.description": {"", "RENT"},
		"rule.sourceFile":  {"", ""},
	}}
	specs, err := parseRules(form)
	require.NoError(t, err)
	require.Len(t, specs, 1)
	require.Equal(t, "RENT", specs[0].Description)
}

func TestBrowserURL(t *testing.T) {
	require.Equal(t, "http://localhost:8080", browserURL(":8080"))
	require.Equal(t, "http://127.0.0.1:9000", browserURL("127.0.0.1:9000"))
}

func TestWithBackLink(t *testing.T) {
	require.Contains(t, string(withBackLink([]byte("<html><body>x</body></html>"))), "Generate another")
	require.Equal(t, "no-body", string(withBackLink([]byte("no-body"))))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/infra/web/ -v`
Expected: build/compile failures — `New` takes a string now, `parseRules` undefined, template not wired.

- [ ] **Step 3: Rewrite `page.go` to parse the template**

Replace the entire contents of `internal/infra/web/page.go` with:

```go
package web

import (
	_ "embed"
	"html/template"
)

//go:embed index.html
var indexHTML string

// indexTmpl renders the upload page, pre-filling the exclusion-rules editor.
var indexTmpl = template.Must(template.New("index").Parse(indexHTML))
```

- [ ] **Step 4: Create `rules.go`**

Create `internal/infra/web/rules.go`:

```go
package web

import (
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// ruleView is one row in the rules editor, with IsDebit flattened to a select
// value ("any" | "debit" | "credit") for the template.
type ruleView struct {
	MatchMode   string
	Debit       string
	Description string
	SourceFile  string
}

// toRuleViews converts stored specs into editor rows.
func toRuleViews(specs []transaction.RuleSpec) []ruleView {
	views := make([]ruleView, 0, len(specs))
	for _, s := range specs {
		debit := "any"
		if s.IsDebit != nil {
			if *s.IsDebit {
				debit = "debit"
			} else {
				debit = "credit"
			}
		}
		mode := string(s.MatchMode)
		if mode == "" {
			mode = string(transaction.MatchExact)
		}
		views = append(views, ruleView{
			MatchMode:   mode,
			Debit:       debit,
			Description: s.Description,
			SourceFile:  s.SourceFile,
		})
	}
	return views
}

// parseRules reads the parallel rule.* form columns into specs. Fully blank
// rows (no description and no source file) are skipped; remaining rows are
// validated, returning a 1-based "Rule N" error on the first invalid row.
func parseRules(form *multipart.Form) ([]transaction.RuleSpec, error) {
	modes := form.Value["rule.matchMode"]
	debits := form.Value["rule.isDebit"]
	descs := form.Value["rule.description"]
	srcs := form.Value["rule.sourceFile"]

	var specs []transaction.RuleSpec
	for i := range descs {
		desc := strings.TrimSpace(descs[i])
		src := strings.TrimSpace(valueAt(srcs, i))
		if desc == "" && src == "" {
			continue
		}

		spec := transaction.RuleSpec{
			MatchMode:   transaction.MatchMode(valueAt(modes, i)),
			Description: desc,
			SourceFile:  src,
		}
		switch valueAt(debits, i) {
		case "debit":
			v := true
			spec.IsDebit = &v
		case "credit":
			v := false
			spec.IsDebit = &v
		}

		if err := spec.Validate(); err != nil {
			return nil, fmt.Errorf("Rule %d: %s", i+1, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// valueAt returns s[i] or "" when i is out of range.
func valueAt(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}
```

- [ ] **Step 5: Update `server.go`**

In `internal/infra/web/server.go`:

(a) Update the import block — drop nothing, add the `config` package:

```go
import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lpatouchas/personal-finance/internal/app/report"
	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/lpatouchas/personal-finance/internal/infra/config"
	"github.com/lpatouchas/personal-finance/internal/infra/csv"
	"github.com/lpatouchas/personal-finance/internal/infra/html"
)
```

(b) Replace the `Server` struct and `New` (lines ~24-36) with:

```go
// Server serves the local web UI: upload CSVs, edit exclusion rules, generate
// and view the report.
type Server struct {
	configPath string
	mux        *http.ServeMux
}

// New builds a Server backed by the exclusion-rules file at configPath.
func New(configPath string) *Server {
	s := &Server{configPath: configPath, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /generate", s.handleGenerate)
	return s
}
```

(c) Replace `handleIndex` (lines ~42-45) with:

```go
func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	specs, err := config.Load(s.configPath)
	if err != nil {
		http.Error(w, "Couldn't load exclusion rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, struct{ Rules []ruleView }{toRuleViews(specs)}); err != nil {
		slog.Error("rendering index", "error", err)
	}
}
```

(d) Replace `handleGenerate` (lines ~47-81) with:

```go
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "Couldn't read the upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "Please upload at least one CSV file.", http.StatusBadRequest)
		return
	}

	specs, err := parseRules(r.MultipartForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.FormValue("save") != "" {
		if err := config.Save(s.configPath, specs); err != nil {
			http.Error(w, "Couldn't save rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	tmpDir, err := os.MkdirTemp("", "pf-uploads-*")
	if err != nil {
		http.Error(w, "Server error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	for _, fh := range files {
		if err := saveUpload(tmpDir, fh); err != nil {
			http.Error(w, "Couldn't save "+fh.Filename+": "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	var buf bytes.Buffer
	svc := report.NewService(csv.New(tmpDir), html.NewWriter(&buf), transaction.CompileRules(specs))
	if err := svc.GenerateReport(context.Background()); err != nil {
		http.Error(w, "Couldn't generate the report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(withBackLink(buf.Bytes()))
}
```

Leave `saveUpload`, `withBackLink`, `browserURL`, and `Run` unchanged.

- [ ] **Step 6: Rewrite `index.html` with the rules editor**

Replace the entire contents of `internal/infra/web/index.html` with:

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Personal Finance — Generate Report</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; background:#f5f5f7; color:#111; margin:0; }
  .wrap { max-width:640px; margin:8vh auto; padding:0 20px; }
  .card { background:#fff; border-radius:14px; padding:32px; box-shadow:0 8px 30px rgba(0,0,0,.08); }
  h1 { margin:0 0 8px; font-size:24px; }
  h2 { margin:28px 0 6px; font-size:15px; text-transform:uppercase; letter-spacing:.05em; color:#888; }
  p { color:#555; margin:0 0 24px; }
  .drop { display:block; border:2px dashed #ccc; border-radius:10px; padding:28px; text-align:center; color:#777; cursor:pointer; transition:.15s; }
  .drop.hover { border-color:#111; color:#111; background:#fafafa; }
  #files { display:none; }
  ul { list-style:none; padding:0; margin:16px 0 0; font-size:14px; color:#333; }
  .rule { display:flex; gap:6px; align-items:center; margin:6px 0; }
  .rule select, .rule input { font:13px system-ui, sans-serif; padding:6px; border:1px solid #ccc; border-radius:6px; }
  .rule .desc { flex:2; } .rule .src { flex:1; }
  .rule .rm { flex:0 0 auto; width:30px; padding:6px; background:#eee; color:#900; cursor:pointer; }
  .addrule { margin-top:8px; background:#eee; color:#111; width:auto; padding:6px 12px; font-size:13px; }
  .save { margin-top:16px; font-size:14px; color:#444; display:flex; align-items:center; gap:8px; }
  button.primary { margin-top:20px; width:100%; padding:12px; font-size:16px; border:0; border-radius:10px; background:#111; color:#fff; cursor:pointer; }
  button { border:0; border-radius:8px; }
</style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <h1>Generate your report</h1>
      <p>Drop your bank CSV exports below, or click to choose them.</p>
      <form method="POST" action="/generate" enctype="multipart/form-data">
        <label class="drop" id="drop">
          Click or drop CSV files here
          <input type="file" id="files" name="files" multiple accept=".csv">
        </label>
        <ul id="list"></ul>

        <h2>Exclusion rules</h2>
        <div id="rules">
          {{range .Rules}}
          <div class="rule">
            <select name="rule.matchMode">
              <option value="exact" {{if eq .MatchMode "exact"}}selected{{end}}>exact</option>
              <option value="contains" {{if eq .MatchMode "contains"}}selected{{end}}>contains</option>
            </select>
            <select name="rule.isDebit">
              <option value="any" {{if eq .Debit "any"}}selected{{end}}>any</option>
              <option value="debit" {{if eq .Debit "debit"}}selected{{end}}>debit</option>
              <option value="credit" {{if eq .Debit "credit"}}selected{{end}}>credit</option>
            </select>
            <input class="desc" name="rule.description" placeholder="description" value="{{.Description}}">
            <input class="src" name="rule.sourceFile" placeholder="source (optional)" value="{{.SourceFile}}">
            <button type="button" class="rm" title="Remove">✕</button>
          </div>
          {{end}}
        </div>
        <button type="button" class="addrule" id="addrule">+ add rule</button>

        <label class="save"><input type="checkbox" name="save"> Save these rules for next time</label>
        <button type="submit" class="primary">Generate report</button>
      </form>
    </div>
  </div>

  <template id="rowTemplate">
    <div class="rule">
      <select name="rule.matchMode"><option value="exact">exact</option><option value="contains">contains</option></select>
      <select name="rule.isDebit"><option value="any">any</option><option value="debit">debit</option><option value="credit">credit</option></select>
      <input class="desc" name="rule.description" placeholder="description">
      <input class="src" name="rule.sourceFile" placeholder="source (optional)">
      <button type="button" class="rm" title="Remove">✕</button>
    </div>
  </template>

  <script>
    const drop = document.getElementById('drop');
    const input = document.getElementById('files');
    const list = document.getElementById('list');
    function show() {
      list.innerHTML = '';
      for (const f of input.files) {
        const li = document.createElement('li');
        li.textContent = '📄 ' + f.name;
        list.appendChild(li);
      }
    }
    input.addEventListener('change', show);
    ['dragenter','dragover'].forEach(e => drop.addEventListener(e, ev => { ev.preventDefault(); drop.classList.add('hover'); }));
    ['dragleave','drop'].forEach(e => drop.addEventListener(e, ev => { ev.preventDefault(); drop.classList.remove('hover'); }));
    drop.addEventListener('drop', ev => { input.files = ev.dataTransfer.files; show(); });

    const rules = document.getElementById('rules');
    const rowTemplate = document.getElementById('rowTemplate');
    document.getElementById('addrule').addEventListener('click', () => {
      rules.appendChild(rowTemplate.content.cloneNode(true));
    });
    rules.addEventListener('click', ev => {
      if (ev.target.classList.contains('rm')) ev.target.closest('.rule').remove();
    });
  </script>
</body>
</html>
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/infra/web/ -v`
Expected: PASS (index pre-fills the default rule, submitted-rule exclusion works, save writes the file, invalid row → 400, blank rows skipped).

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/infra/web/
git add internal/infra/web/
git commit -m "feat(web): inline exclusion-rules editor with save-to-config"
```

---

## Task 4: CLI --config flag

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

- [ ] **Step 1: Write the failing test**

Read `main_test.go` first to match its existing structure. Add this subcommand-dispatch test (adapt the surrounding table/helper names to the existing file). It asserts that `generate` accepts `--config` and still runs, and that an unknown flag still errors:

```go
func TestDispatchGenerateWithConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "rules.json")
	out := filepath.Join(dir, "report.html")
	data := filepath.Join(dir, "data")
	require.NoError(t, os.MkdirAll(data, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(data, "acc.csv"),
		[]byte("Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;\n"+
			`1;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;`+"\n"), 0o644))

	err := dispatch([]string{"generate", "--data", data, "--out", out, "--config", cfg})
	require.NoError(t, err)
	require.FileExists(t, out)
	require.FileExists(t, cfg) // seeded
}
```

Ensure `main_test.go` imports `os`, `path/filepath`, and `github.com/stretchr/testify/require` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestDispatchGenerateWithConfig -v`
Expected: FAIL — `--config` is not a defined flag (flag.Parse error).

- [ ] **Step 3: Wire `--config` through dispatch**

In `main.go`:

(a) Add the `config` import:

```go
	"github.com/lpatouchas/personal-finance/internal/infra/config"
```

(b) Replace `runGenerate` (lines ~41-51) with:

```go
// runGenerate produces the HTML report from a folder of CSV exports.
func runGenerate(dataDir, outputPath, configPath string) error {
	specs, err := config.Load(configPath)
	if err != nil {
		return err
	}
	repo := csv.New(dataDir)
	renderer := html.NewFile(outputPath)
	svc := report.NewService(repo, renderer, transaction.CompileRules(specs))
	if err := svc.GenerateReport(context.Background()); err != nil {
		return err
	}
	slog.Info("report generated", "path", outputPath)
	return nil
}
```

(c) Replace `runServe` (lines ~53-56) with:

```go
// runServe starts the local web app and blocks.
func runServe(addr, configPath string, open bool) error {
	return web.New(configPath).Run(addr, open)
}
```

(d) In the `generate` case of `dispatch`, add a `--config` flag and pass it:

```go
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		data := fs.String("data", defaultDataDir, "folder of CSV exports")
		out := fs.String("out", defaultOutput, "output HTML path")
		cfg := fs.String("config", config.DefaultPath(), "exclusion-rules JSON file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runGenerate(*data, *out, *cfg)
```

(e) In the `serve` case, add a `--config` flag and pass it:

```go
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		addr := fs.String("addr", defaultAddr, "address to listen on")
		noOpen := fs.Bool("no-open", false, "do not open the browser")
		cfg := fs.String("config", config.DefaultPath(), "exclusion-rules JSON file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runServe(*addr, *cfg, !*noOpen)
```

(f) Update the `usage` string: add a `--config` line under both `serve flags:` and `generate flags:`:

```
serve flags:
  --addr     address to listen on (default ":8080")
  --no-open  do not open the browser
  --config   exclusion-rules JSON file (default: beside the binary)

generate flags:
  --data    folder of CSV exports (default "./data")
  --out     output HTML path (default "./report.html")
  --config  exclusion-rules JSON file (default: beside the binary)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test . -v`
Expected: PASS, including the existing dispatch tests (no-args serve, generate, help, version, unknown).

- [ ] **Step 5: Commit**

```bash
gofmt -w main.go main_test.go
git add main.go main_test.go
git commit -m "feat(cli): --config flag selects the exclusion-rules file"
```

---

## Task 5: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document the rules file and editor**

In `README.md`, replace the bullet:

```
- Applies built-in exclusion rules (e.g. instant-transfer moves out of the
  investment account). External, user-defined rules are planned.
```

with:

```
- Applies user-defined exclusion rules. Rules live in `exclusion-rules.json`
  (created next to the binary on first run, pre-filled with the built-in
  instant-transfer rule). Each rule matches a transaction by description
  (exact or contains), optionally constrained to debit/credit and a single
  source file. Edit rules right on the web page (tick "Save these rules for
  next time" to persist them), or point at a different file with `--config`.
```

In the **Command line** section, add after the `generate` example:

```
Use `--config path/to/rules.json` with `generate` or `serve` to choose a
different exclusion-rules file.
```

- [ ] **Step 2: Verify the full suite still passes**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 3: Build the binary to confirm it compiles**

Run: `go build -o personal-finance .`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document configurable exclusion rules"
```

---

## Self-Review Notes

- **Spec coverage:** RuleSpec model + Validate + CompileRule (Task 1); config Load/Save/DefaultPath with seeding & loud failure (Task 2); web editor, parse, save checkbox, 400 on invalid (Task 3); CLI `--config` (Task 4); README (Task 5). All spec sections covered.
- **Type consistency:** `RuleSpec`, `MatchMode`, `MatchExact`/`MatchContains`, `CompileRules`, `DefaultRuleSpecs`, `config.Load/Save/DefaultPath`, `web.New(configPath)`, `runGenerate(data,out,config)`, `runServe(addr,config,open)` are used identically across tasks.
- **No placeholders:** every code step contains complete code.
- **Behavior preserved:** `DefaultExclusionRules()` keeps its signature and exact behavior (now via `CompileRules(DefaultRuleSpecs())`), so `report` and existing tests are untouched.
