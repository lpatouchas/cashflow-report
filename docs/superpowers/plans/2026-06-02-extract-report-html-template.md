# Extract `reportHTML` to Embedded `report.html` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the ~830-line `reportHTML` string constant out of `internal/infra/html/renderer.go` into a sibling `report.html` file embedded via `//go:embed`, with zero change to rendered output.

**Architecture:** Mirror the repo's existing `internal/infra/web/page.go` + `index.html` convention. `report.html` holds the HTML/CSS/JS (Go template directives stay inline); `renderer.go` keeps only Go logic plus a `//go:embed report.html` var that feeds the unchanged `tmpl` parser.

**Tech Stack:** Go 1.23, `embed` (stdlib), `html/template` (stdlib), testify.

---

## Background for the engineer

- The file `internal/infra/html/renderer.go` is 1119 lines. Lines 288–1119 are a single Go raw-string constant:
  - **Line 288** is exactly: `const reportHTML = ` followed by a backtick and `<!DOCTYPE html>` on the same line.
  - **Lines 289–1118** are the HTML/CSS/JS body, ending with `</html>` on line 1118.
  - **Line 1119** is a lone closing backtick `` ` ``.
- The constant is consumed once, by this line near the top of the file (currently lines 82–87):
  ```go
  var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
  	"euro":   formatEuro,
  	"pct":    formatPct,
  	"month":  monthLabel,
  	"months": monthsLabel,
  }).Parse(reportHTML))
  ```
- `//go:embed` into a `string` variable preserves bytes exactly, including the inline `{{ ... }}` template directives. `template.Parse` behaves identically whether its input is a const or an embedded string. So the rendered output must be byte-for-byte unchanged.
- The existing tests in `internal/infra/html/renderer_test.go` assert on rendered output bytes. They are the regression guard — no new tests are needed.
- Reference the existing pattern in `internal/infra/web/page.go`:
  ```go
  package web

  import (
  	_ "embed"
  	"html/template"
  )

  //go:embed index.html
  var indexHTML string

  var indexTmpl = template.Must(template.New("index").Parse(indexHTML))
  ```

All commands below are run from the repo root: `/Users/leonidas/GolandProjects/cashflow-report`.

---

## Task 1: Capture a golden baseline of the rendered output

This gives us a pre-change reference to diff against, proving the refactor is byte-identical. It also confirms the current tests pass before we touch anything.

**Files:**
- Create (temporary, deleted at end): `/tmp/report-old.html`

- [ ] **Step 1: Confirm the current build and tests are green**

Run:
```bash
go build ./... && go test ./internal/infra/html/ -v
```
Expected: build succeeds; tests PASS (you'll see subtests like `embeds per-month transactions as JSON`).

- [ ] **Step 2: Render a golden baseline file using the existing test fixtures via a throwaway program**

The renderer has no CLI entry that's convenient here, so capture the baseline by writing a tiny temporary test that dumps output. Create `internal/infra/html/golden_dump_test.go`:

```go
package html

import (
	"os"
	"testing"
	"time"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// TestDumpGolden is a temporary helper (deleted in the final task) that renders a
// fixed Summary to /tmp/report-baseline.html so we can prove the embed refactor is
// byte-identical. Run explicitly with -run TestDumpGolden.
func TestDumpGolden(t *testing.T) {
	s := transaction.Summary{
		TotalIncome:   1000,
		TotalExpenses: 600,
		Savings:       400,
		ByMonth: []transaction.MonthlyBreakdown{
			{
				Year:     2026,
				Month:    time.May,
				Income:   1000,
				Expenses: 600,
				Savings:  400,
				Transactions: []transaction.Transaction{
					{Date: time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC), Description: "Salary", Amount: 1000, IsDebit: false, SourceFile: "bank.csv"},
					{Date: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC), Description: "Rent", Amount: 600, IsDebit: true, SourceFile: "bank.csv"},
				},
				ByAccount: []transaction.AccountBreakdown{
					{Source: "bank.csv", Income: 1000, Expenses: 600},
				},
			},
		},
		Averages: transaction.MonthlyAverages{Months: 1, Income: 1000, Expenses: 600, Savings: 400},
	}
	f, err := os.Create("/tmp/report-baseline.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := NewWriter(f).Render(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}
```

> NOTE: Before writing this, open `internal/infra/html/renderer_test.go` and confirm the exact field names/types on `transaction.Summary`, `MonthlyBreakdown`, `AccountBreakdown`, `Averages`, and `Transaction`. If any field name differs from the struct literal above, copy the shape from an existing test in that file instead — the goal is only "a valid non-empty Summary that exercises rows, the chart, averages, and the modal." If `t.Context()` is unavailable on this Go version, use `context.Background()` and add `"context"` to the imports.

- [ ] **Step 3: Generate the baseline from the OLD (pre-change) code, then preserve it**

The dump test always writes to the hardcoded path `/tmp/report-baseline.html`. Generate it now (from current, unchanged code) and immediately rename it to `/tmp/report-old.html` so Task 4 can regenerate into the same hardcoded path without clobbering this reference.

Run:
```bash
go test ./internal/infra/html/ -run TestDumpGolden -v
mv /tmp/report-baseline.html /tmp/report-old.html
wc -l /tmp/report-old.html
```
Expected: PASS; `/tmp/report-old.html` exists with ~450+ lines of rendered HTML.

- [ ] **Step 4: Do NOT commit the dump test yet**

Leave `golden_dump_test.go` uncommitted on disk — it is scaffolding, removed in Task 4. Do not stage it.

---

## Task 2: Create `report.html` from the constant body

**Files:**
- Create: `internal/infra/html/report.html`

- [ ] **Step 1: Extract the raw-string body into `report.html`**

This pulls lines 288–1118 (the backtick line through the final `</html>`) and strips the `const reportHTML = ` + backtick prefix from the first line, leaving a clean `<!DOCTYPE html>` start. Line 1119 (the lone closing backtick) is intentionally excluded.

Run:
```bash
sed -n '288,1118p' internal/infra/html/renderer.go \
  | sed '1s/^const reportHTML = `//' \
  > internal/infra/html/report.html
```

- [ ] **Step 2: Verify the new file's boundaries are clean**

Run:
```bash
head -1 internal/infra/html/report.html
tail -1 internal/infra/html/report.html
```
Expected:
- First line: `<!DOCTYPE html>` (no `const` text, no backtick)
- Last line: `</html>`

- [ ] **Step 3: Verify the body is byte-identical to the constant body**

This re-extracts the constant body straight from the Go source (everything after the opening backtick on line 288, through line 1118) and diffs it against the new file. They must match exactly.

Run:
```bash
diff <(sed -n '288,1118p' internal/infra/html/renderer.go | sed '1s/^const reportHTML = `//') internal/infra/html/report.html && echo "IDENTICAL"
```
Expected: `IDENTICAL` (diff produces no output).

---

## Task 3: Replace the constant with a `//go:embed` directive

**Files:**
- Modify: `internal/infra/html/renderer.go` (import block ~lines 3–15; add embed var near line 82; delete const at lines 288–1119)

- [ ] **Step 1: Add the `embed` import**

Edit the import block. Change:
```go
import (
	"context"
	"encoding/json"
```
to:
```go
import (
	"context"
	_ "embed"
	"encoding/json"
```
(`_ "embed"` sorts alphabetically after `"context"` within the stdlib group, so gofmt will leave it here.)

- [ ] **Step 2: Add the embed directive directly above the `tmpl` var**

Find this block (currently starting at line 82):
```go
var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
```
Insert immediately above it:
```go
//go:embed report.html
var reportHTML string

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
```

- [ ] **Step 3: Delete the old constant block**

Remove the entire constant — line 288 through line 1119 inclusive — which begins with `const reportHTML = ` and ends with the lone closing backtick on its own line. After deletion, the file should end at the closing brace of the last Go function (`monthsLabel`/`formatPct` region), with no trailing raw-string content.

If doing this by hand is error-prone, delete it mechanically (note: the line numbers have NOT shifted, because Steps 1–2 used the Edit tool / in-place edits above the constant — but Steps 1–2 add 4 lines total, shifting the const down by 4. To avoid miscounting, delete by content instead):

```bash
# Delete from the line that starts the const to the matching closing backtick line.
# Safe because `const reportHTML = ` appears exactly once and the lone `` ` `` terminating
# line is the final line of the file.
perl -0pi -e 's/\nconst reportHTML = `.*`\n?\z//s' internal/infra/html/renderer.go
```

- [ ] **Step 4: Verify the constant is gone and the embed var is present**

Run:
```bash
grep -n 'const reportHTML' internal/infra/html/renderer.go; echo "exit:$?"
grep -n 'go:embed report.html' internal/infra/html/renderer.go
grep -n '_ "embed"' internal/infra/html/renderer.go
```
Expected: the `const reportHTML` grep prints nothing and `exit:1`; the other two each print a matching line.

- [ ] **Step 5: Format and build**

Run:
```bash
gofmt -w internal/infra/html/renderer.go && go build ./...
```
Expected: no output, exit 0. (If the build complains `reportHTML redeclared`, the const wasn't fully deleted — re-check Step 3.)

---

## Task 4: Verify byte-identical output and clean up

**Files:**
- Delete: `internal/infra/html/golden_dump_test.go`
- Read-only: `/tmp/report-old.html` (the pre-change baseline from Task 1)

- [ ] **Step 1: Render the NEW (post-change) output**

The dump test again writes to `/tmp/report-baseline.html`; rename it to `/tmp/report-new.html`.

Run:
```bash
go test ./internal/infra/html/ -run TestDumpGolden -v
mv /tmp/report-baseline.html /tmp/report-new.html
```
Expected: PASS; `/tmp/report-new.html` exists.

- [ ] **Step 2: Diff old vs new — must be identical**

Run:
```bash
diff /tmp/report-old.html /tmp/report-new.html && echo "BYTE-IDENTICAL"
```
Expected: `BYTE-IDENTICAL` (no diff output). If they differ, the extraction dropped or added a byte — revisit Task 2, Step 3's diff check.

- [ ] **Step 3: Remove the temporary golden dump test**

Run:
```bash
rm internal/infra/html/golden_dump_test.go
```

- [ ] **Step 4: Run the real test suite and full build**

Run:
```bash
go build ./... && go test ./...
```
Expected: build succeeds; all packages PASS, including `internal/infra/html` render tests and `internal/infra/web`.

- [ ] **Step 5: Confirm the line-count win**

Run:
```bash
wc -l internal/infra/html/renderer.go internal/infra/html/report.html
```
Expected: `renderer.go` ~290 lines; `report.html` ~830 lines.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/report.html
git status   # confirm golden_dump_test.go is NOT listed and /tmp files are untracked
git commit -m "refactor: extract reportHTML const to embedded report.html

Move the ~830-line HTML/CSS/JS blob out of renderer.go into a sibling
report.html embedded via //go:embed, matching the existing web/page.go +
index.html convention. Rendered output is byte-identical; existing render
tests are the regression guard.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

- [ ] **Step 7: Clean up temp files**

```bash
rm -f /tmp/report-old.html /tmp/report-new.html
```

---

## Done

- `internal/infra/html/report.html` holds the report markup with HTML/CSS/JS syntax highlighting.
- `internal/infra/html/renderer.go` is ~290 lines of Go only, with a `//go:embed report.html` var feeding the unchanged `tmpl`.
- Output proven byte-identical; full suite green.
