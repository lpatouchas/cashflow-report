# CLI + Local Web App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the `go run .` report generator into a single binary with a real CLI (`generate`) and a no-terminal local web app (default / `serve`), while extracting the hardcoded transaction-exclusion rule into an injectable seam for future config.

**Architecture:** One binary, two modes. `main.go` is a thin dispatcher over stdlib `flag`. The existing pipeline (`GetAll → FilterTransfers → ApplyExclusions → Summarize → Render`) is reused unchanged for both modes. The HTML renderer gains a writer-based constructor so the web layer can render to memory; the web layer saves uploads to a temp dir and reuses the CSV repo. The line-68 business rule becomes a `transaction.ExclusionRule` supplied at the composition root.

**Tech Stack:** Go 1.23 (stdlib `net/http` with method-based routing, `flag`, `embed`, `mime/multipart`), `testify/require` + `testify/mock` for tests.

---

## File Structure

**Modify:**
- `internal/domain/transaction/transaction.go` — narrow `FilterTransfers` to dedup-only; add `ExclusionRule`, `ApplyExclusions`, `DefaultExclusionRules`
- `internal/domain/transaction/transaction_test.go` — add tests for the two new functions
- `internal/infra/html/renderer.go` — extract `render(io.Writer, …)`; replace `New` with `NewFile` + add `NewWriter`
- `internal/infra/html/renderer_test.go` — `New` → `NewFile`; add a writer test
- `internal/app/report/service.go` — `NewService` takes `rules`; `GenerateReport` applies them
- `internal/app/report/service_test.go` — update `NewService` calls; add a rule-application test
- `main.go` — dispatcher (`dispatch`, `runGenerate`, `runServe`)
- `main_test.go` — `run` → `runGenerate`; add `dispatch` tests
- `README.md` — three usage paths

**Create:**
- `internal/infra/web/server.go` — `Server`, handlers, `withBackLink`, `browserURL`, `Run`
- `internal/infra/web/index.html` — embedded upload page
- `internal/infra/web/page.go` — `//go:embed` of the page
- `internal/infra/web/browser.go` — per-OS `openBrowser`
- `internal/infra/web/server_test.go` — `httptest` tests

---

## Task 1: Exclusion-rules seam in the domain

**Files:**
- Modify: `internal/domain/transaction/transaction.go:59-75`
- Test: `internal/domain/transaction/transaction_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/transaction/transaction_test.go`:

```go
func TestApplyExclusions(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	t.Run("nil rules keep everything", func(t *testing.T) {
		in := []Transaction{tx("A", "f.csv", 10, true, d)}
		require.Equal(t, in, ApplyExclusions(in, nil))
	})

	t.Run("matching transactions are dropped", func(t *testing.T) {
		rule := func(tr Transaction) bool { return tr.SourceFile == "drop.csv" }
		in := []Transaction{
			tx("A", "keep.csv", 10, true, d),
			tx("B", "drop.csv", 20, true, d),
		}
		got := ApplyExclusions(in, []ExclusionRule{rule})
		require.Len(t, got, 1)
		require.Equal(t, "A", got[0].ID)
	})
}

func TestDefaultExclusionRules(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	rules := DefaultExclusionRules()

	// NOTE: copy the Description literal verbatim from transaction.go line 68 —
	// it mixes Greek and Latin look-alike letters and must match byte-for-byte.
	move := Transaction{ID: "M", SourceFile: "invest.csv", Description: "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS", Amount: 100, IsDebit: true, Date: d}
	normal := Transaction{ID: "N", SourceFile: "invest.csv", Description: "DIVIDEND", Amount: 50, IsDebit: false, Date: d}

	got := ApplyExclusions([]Transaction{move, normal}, rules)
	require.Len(t, got, 1)
	require.Equal(t, "N", got[0].ID)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/transaction/ -run 'TestApplyExclusions|TestDefaultExclusionRules' -v`
Expected: FAIL — `undefined: ApplyExclusions`, `undefined: ExclusionRule`, `undefined: DefaultExclusionRules`.

- [ ] **Step 3: Narrow `FilterTransfers` and add the new API**

In `internal/domain/transaction/transaction.go`, replace the whole `FilterTransfers` function (lines 56-75) with:

```go
// FilterTransfers removes inter-account transfers and duplicate anomalies.
// Any ID appearing more than once across the input is dropped entirely;
// only transactions whose ID occurs exactly once are returned.
func FilterTransfers(txns []Transaction) []Transaction {
	counts := make(map[string]int, len(txns))
	for _, t := range txns {
		counts[t.ID]++
	}
	var kept []Transaction
	for _, t := range txns {
		if counts[t.ID] == 1 {
			kept = append(kept, t)
		}
	}
	return kept
}

// ExclusionRule reports whether a transaction should be excluded from the
// report totals. Rules are applied after transfer/duplicate filtering.
type ExclusionRule func(Transaction) bool

// DefaultExclusionRules are the built-in rules applied until external
// configuration exists. Today this is the single "external account move" rule:
// a debit on invest.csv describing an instant transfer out.
func DefaultExclusionRules() []ExclusionRule {
	return []ExclusionRule{
		func(t Transaction) bool {
			return t.IsDebit && t.Description == "ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS" && t.SourceFile == "invest.csv"
		},
	}
}

// ApplyExclusions drops every transaction matching any of the rules.
// With no rules the input is returned unchanged.
func ApplyExclusions(txns []Transaction, rules []ExclusionRule) []Transaction {
	if len(rules) == 0 {
		return txns
	}
	var kept []Transaction
	for _, t := range txns {
		excluded := false
		for _, rule := range rules {
			if rule(t) {
				excluded = true
				break
			}
		}
		if !excluded {
			kept = append(kept, t)
		}
	}
	return kept
}
```

NOTE: the magic Description literal must be the exact bytes that were on the old line 68 — copy them rather than retyping.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/domain/transaction/ -v`
Expected: PASS (existing `TestFilterTransfers` still passes — its fixtures never used the magic description; new tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: extract exclusion rules into injectable ExclusionRule seam"
```

---

## Task 2: Renderer gains a writer constructor

**Files:**
- Modify: `internal/infra/html/renderer.go:16-23,84-92`
- Test: `internal/infra/html/renderer_test.go`
- Modify (caller): `main.go:20`

- [ ] **Step 1: Write the failing test**

Add this subtest inside the existing `TestRender` in `internal/infra/html/renderer_test.go` (after the last `t.Run` block, before `TestRender` closes), and add `"bytes"` to the imports:

```go
	t.Run("renders to a writer", func(t *testing.T) {
		var buf bytes.Buffer
		summary := transaction.Summary{
			TotalIncome:   1500,
			TotalExpenses: 500,
			Savings:       1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000},
			},
		}
		err := NewWriter(&buf).Render(ctx, summary)
		require.NoError(t, err)
		require.Contains(t, buf.String(), "€1.500,00")
		require.Contains(t, buf.String(), "May 2026")
	})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infra/html/ -run TestRender -v`
Expected: FAIL — `undefined: NewWriter`.

- [ ] **Step 3: Refactor the renderer**

In `internal/infra/html/renderer.go`, add `"io"` to the imports. Replace the `Renderer` struct + `New` constructor (lines 16-23):

```go
// Renderer writes a Summary as HTML to a destination: either a file path
// (NewFile) or an arbitrary io.Writer (NewWriter).
type Renderer struct {
	w    io.Writer // when non-nil, render here
	path string    // otherwise, create this file
}

// NewFile returns a Renderer that writes the report to a file at path.
func NewFile(path string) *Renderer { return &Renderer{path: path} }

// NewWriter returns a Renderer that writes the report to w.
func NewWriter(w io.Writer) *Renderer { return &Renderer{w: w} }
```

Then replace the `Render` method (lines 84-92):

```go
func (r *Renderer) Render(ctx context.Context, summary transaction.Summary) error {
	if r.w != nil {
		return render(r.w, summary)
	}
	f, err := os.Create(r.path)
	if err != nil {
		return err
	}
	defer f.Close()
	return render(f, summary)
}

func render(w io.Writer, summary transaction.Summary) error {
	return tmpl.Execute(w, buildView(summary))
}
```

- [ ] **Step 4: Update the existing file-renderer tests and the caller**

In `internal/infra/html/renderer_test.go`, replace every `New(out)` with `NewFile(out)` (4 occurrences in `TestRender`).

In `main.go`, change line 20 from `renderer := html.New(outputPath)` to `renderer := html.NewFile(outputPath)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/infra/html/ ./... -run TestRender` then `go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go main.go
git commit -m "feat: add NewWriter renderer alongside file renderer"
```

---

## Task 3: Service applies exclusion rules

**Files:**
- Modify: `internal/app/report/service.go:12-38`
- Test: `internal/app/report/service_test.go`
- Modify (caller): `main.go:18-23`

- [ ] **Step 1: Write the failing test**

Add this subtest inside `TestGenerateReport` in `internal/app/report/service_test.go` (after the first `t.Run` block):

```go
	t.Run("applies exclusion rules before summarizing", func(t *testing.T) {
		txns := []transaction.Transaction{
			{ID: "INC", SourceFile: "a.csv", Amount: 500, IsDebit: false, Date: d},
			{ID: "DROP", SourceFile: "a.csv", Amount: 200, IsDebit: true, Date: d},
		}
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(txns, nil)

		var captured transaction.Summary
		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).
			Run(func(args mock.Arguments) {
				captured = args.Get(1).(transaction.Summary)
			}).
			Return(nil)

		rules := []transaction.ExclusionRule{
			func(t transaction.Transaction) bool { return t.ID == "DROP" },
		}
		svc := NewService(repo, renderer, rules)
		require.NoError(t, svc.GenerateReport(ctx))
		require.InDelta(t, 500, captured.TotalIncome, 0.001)
		require.InDelta(t, 0, captured.TotalExpenses, 0.001)
	})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/report/ -run TestGenerateReport -v`
Expected: FAIL — `too many arguments in call to NewService` (the existing 2-arg calls also won't compile yet, which is expected; we fix them in Step 3).

- [ ] **Step 3: Make `NewService` rules-aware**

In `internal/app/report/service.go`, replace the struct + constructor (lines 12-19):

```go
// Service orchestrates report generation:
// load → filter transfers → apply exclusion rules → summarize → render.
type Service struct {
	repo     transaction.Repository
	renderer Renderer
	rules    []transaction.ExclusionRule
}

func NewService(repo transaction.Repository, renderer Renderer, rules []transaction.ExclusionRule) *Service {
	return &Service{repo: repo, renderer: renderer, rules: rules}
}
```

Then, in `GenerateReport`, insert the exclusion step right after the transfer-filter logging block (after the `if excluded := …` block, before `summary := transaction.Summarize(kept)`):

```go
	kept = transaction.ApplyExclusions(kept, s.rules)
```

- [ ] **Step 4: Update existing service tests and the caller**

In `internal/app/report/service_test.go`, change the three existing `NewService(repo, renderer)` calls to `NewService(repo, renderer, nil)`.

In `main.go`: add `"github.com/lpatouchas/personal-finance/internal/domain/transaction"` to the imports, and change the `NewService` call (line 21) to:

```go
	svc := report.NewService(repo, renderer, transaction.DefaultExclusionRules())
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./... && go build ./...`
Expected: PASS across all packages, clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/app/report/service.go internal/app/report/service_test.go main.go
git commit -m "feat: apply injectable exclusion rules in report service"
```

---

## Task 4: Local web app package

**Files:**
- Create: `internal/infra/web/server.go`
- Create: `internal/infra/web/index.html`
- Create: `internal/infra/web/page.go`
- Create: `internal/infra/web/browser.go`
- Test: `internal/infra/web/server_test.go`

NOTE: the CSV parser skips malformed rows rather than erroring (`internal/infra/csv/repository.go:75`), so a junk upload yields an empty report (200, "No transactions") rather than a failure. The reachable user errors are: malformed multipart and no files → 400. The generic pipeline/IO error branch returns 500 and is defensive (not exercised by a test).

- [ ] **Step 1: Create the embedded upload page**

Create `internal/infra/web/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Personal Finance — Generate Report</title>
<style>
  body { font-family: system-ui, -apple-system, sans-serif; background:#f5f5f7; color:#111; margin:0; }
  .wrap { max-width:560px; margin:10vh auto; padding:0 20px; }
  .card { background:#fff; border-radius:14px; padding:32px; box-shadow:0 8px 30px rgba(0,0,0,.08); }
  h1 { margin:0 0 8px; font-size:24px; }
  p { color:#555; margin:0 0 24px; }
  .drop { display:block; border:2px dashed #ccc; border-radius:10px; padding:28px; text-align:center; color:#777; cursor:pointer; transition:.15s; }
  .drop.hover { border-color:#111; color:#111; background:#fafafa; }
  #files { display:none; }
  ul { list-style:none; padding:0; margin:16px 0 0; font-size:14px; color:#333; }
  button { margin-top:24px; width:100%; padding:12px; font-size:16px; border:0; border-radius:10px; background:#111; color:#fff; cursor:pointer; }
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
        <button type="submit">Generate report</button>
      </form>
    </div>
  </div>
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
  </script>
</body>
</html>
```

- [ ] **Step 2: Create the embed binding**

Create `internal/infra/web/page.go`:

```go
package web

import _ "embed"

//go:embed index.html
var indexHTML []byte
```

- [ ] **Step 3: Create the per-OS browser opener**

Create `internal/infra/web/browser.go`:

```go
package web

import (
	"os/exec"
	"runtime"
)

// openBrowser opens url in the user's default browser. Best-effort.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
```

- [ ] **Step 4: Write the failing server tests**

Create `internal/infra/web/server_test.go`:

```go
package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

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

func TestHandleIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	New(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `action="/generate"`)
	require.Contains(t, rec.Body.String(), `name="files"`)
}

func TestHandleGenerate(t *testing.T) {
	t.Run("generates a report from an uploaded csv", func(t *testing.T) {
		body := csvHeader + "\n" +
			`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		buf, ct := multipartCSV(t, "acc.csv", body)

		req := httptest.NewRequest(http.MethodPost, "/generate", buf)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), "May 2026")
		require.Contains(t, rec.Body.String(), "Generate another")
	})

	t.Run("rejects when no files are uploaded", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		require.NoError(t, w.Close())

		req := httptest.NewRequest(http.MethodPost, "/generate", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "at least one CSV")
	})

	t.Run("rejects a malformed multipart body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewReader([]byte("garbage")))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=nope")
		rec := httptest.NewRecorder()
		New(nil).ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
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

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/infra/web/ -v`
Expected: FAIL — `undefined: New`, `undefined: browserURL`, `undefined: withBackLink` (build error).

- [ ] **Step 6: Implement the server**

Create `internal/infra/web/server.go`:

```go
package web

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
	"github.com/lpatouchas/personal-finance/internal/infra/csv"
	"github.com/lpatouchas/personal-finance/internal/infra/html"
)

// maxUploadBytes caps the in-memory portion of a multipart upload.
const maxUploadBytes = 32 << 20 // 32 MiB

// Server serves the local web UI: upload CSVs, generate and view the report.
type Server struct {
	rules []transaction.ExclusionRule
	mux   *http.ServeMux
}

// New builds a Server that applies the given exclusion rules to each report.
func New(rules []transaction.ExclusionRule) *Server {
	s := &Server{rules: rules, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("POST /generate", s.handleGenerate)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

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
	svc := report.NewService(csv.New(tmpDir), html.NewWriter(&buf), s.rules)
	if err := svc.GenerateReport(context.Background()); err != nil {
		http.Error(w, "Couldn't generate the report: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(withBackLink(buf.Bytes()))
}

// saveUpload writes one uploaded file into dir under its base filename.
func saveUpload(dir string, fh *multipart.FileHeader) error {
	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(filepath.Join(dir, filepath.Base(fh.Filename)))
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// withBackLink injects a fixed "Generate another" link before </body> so the
// served report links back to the upload page. If </body> is absent the page
// is returned unchanged.
func withBackLink(page []byte) []byte {
	marker := []byte("</body>")
	if !bytes.Contains(page, marker) {
		return page
	}
	link := []byte(`<a href="/" style="position:fixed;top:12px;right:16px;z-index:9999;` +
		`font:14px sans-serif;background:#111;color:#fff;padding:8px 12px;` +
		`border-radius:6px;text-decoration:none">&#8635; Generate another</a></body>`)
	return bytes.Replace(page, marker, link, 1)
}

// browserURL derives a clickable localhost URL from a listen address such as
// ":8080" or "localhost:8080".
func browserURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}

// Run starts the HTTP server on addr, optionally opening the browser. It
// blocks until the server stops.
func (s *Server) Run(addr string, open bool) error {
	url := browserURL(addr)
	if open {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = openBrowser(url)
		}()
	}
	slog.Info("serving", "url", url)
	return http.ListenAndServe(addr, s)
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/infra/web/ -v`
Expected: PASS for `TestHandleIndex`, `TestHandleGenerate` (all 3 subtests), `TestBrowserURL`, `TestWithBackLink`.

- [ ] **Step 8: Commit**

```bash
git add internal/infra/web/
git commit -m "feat: add local web app for uploading CSVs and viewing the report"
```

---

## Task 5: main.go dispatcher (CLI + serve)

**Files:**
- Modify: `main.go` (full rewrite)
- Test: `main_test.go`

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `main_test.go` with:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

func TestRunGenerate(t *testing.T) {
	t.Run("generates report end to end", func(t *testing.T) {
		dataDir := t.TempDir()
		body := csvHeader + "\n" +
			`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))

		out := filepath.Join(t.TempDir(), "report.html")
		require.NoError(t, runGenerate(dataDir, out))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "May 2026")
	})

	t.Run("returns error when no data", func(t *testing.T) {
		require.Error(t, runGenerate(t.TempDir(), filepath.Join(t.TempDir(), "report.html")))
	})
}

func TestDispatch(t *testing.T) {
	t.Run("generate writes a report", func(t *testing.T) {
		dataDir := t.TempDir()
		body := csvHeader + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))
		out := filepath.Join(t.TempDir(), "report.html")

		require.NoError(t, dispatch([]string{"generate", "--data", dataDir, "--out", out}))
		_, err := os.Stat(out)
		require.NoError(t, err)
	})

	t.Run("help prints usage", func(t *testing.T) {
		require.NoError(t, dispatch([]string{"help"}))
	})

	t.Run("version prints", func(t *testing.T) {
		require.NoError(t, dispatch([]string{"--version"}))
	})

	t.Run("unknown command errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"bogus"}))
	})

	t.Run("generate with bad flag errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"generate", "--nope"}))
	})

	t.Run("serve with bad flag errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"serve", "--nope"}))
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test . -v`
Expected: FAIL — `undefined: runGenerate`, `undefined: dispatch` (build error; old `run`/`main` still present).

- [ ] **Step 3: Rewrite main.go as a dispatcher**

Replace the entire contents of `main.go` with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/lpatouchas/personal-finance/internal/app/report"
	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/lpatouchas/personal-finance/internal/infra/csv"
	"github.com/lpatouchas/personal-finance/internal/infra/html"
	"github.com/lpatouchas/personal-finance/internal/infra/web"
)

const (
	defaultDataDir = "./data"
	defaultOutput  = "./report.html"
	defaultAddr    = ":8080"
)

const usage = `personal-finance — summarise bank CSV exports into an HTML report

Usage:
  personal-finance                   Start the web app (opens browser, upload CSVs)
  personal-finance serve [flags]     Start the web app
  personal-finance generate [flags]  Generate report.html from a data folder

serve flags:
  --addr     address to listen on (default ":8080")
  --no-open  do not open the browser

generate flags:
  --data  folder of CSV exports (default "./data")
  --out   output HTML path (default "./report.html")
`

// runGenerate produces the HTML report from a folder of CSV exports.
func runGenerate(dataDir, outputPath string) error {
	repo := csv.New(dataDir)
	renderer := html.NewFile(outputPath)
	svc := report.NewService(repo, renderer, transaction.DefaultExclusionRules())
	if err := svc.GenerateReport(context.Background()); err != nil {
		return err
	}
	slog.Info("report generated", "path", outputPath)
	return nil
}

// runServe starts the local web app and blocks.
func runServe(addr string, open bool) error {
	return web.New(transaction.DefaultExclusionRules()).Run(addr, open)
}

// dispatch routes CLI args to a subcommand. With no command it serves the web
// app. Returns an error for unknown commands, flag errors, or generation
// failures.
func dispatch(args []string) error {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "version", "--version":
		fmt.Println("personal-finance dev")
		return nil
	case "generate":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		data := fs.String("data", defaultDataDir, "folder of CSV exports")
		out := fs.String("out", defaultOutput, "output HTML path")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runGenerate(*data, *out)
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		addr := fs.String("addr", defaultAddr, "address to listen on")
		noOpen := fs.Bool("no-open", false, "do not open the browser")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return runServe(*addr, !*noOpen)
	default:
		return fmt.Errorf("unknown command %q (try 'personal-finance help')", cmd)
	}
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... && go build ./...`
Expected: PASS across all packages; clean build.

- [ ] **Step 5: Smoke-test the binary manually**

Run:
```bash
go build -o personal-finance . && ./personal-finance help && ./personal-finance generate --data ./data --out /tmp/report.html && ls -la /tmp/report.html
```
Expected: usage text prints; report is generated at `/tmp/report.html`.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: dispatch CLI subcommands (serve default, generate one-shot)"
```

---

## Task 6: Update the README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Replace the Usage section**

Replace the entire contents of `README.md` with:

```markdown
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

## What it does

- Loads every `*.csv` in the data folder (semicolon-separated Greek bank export
  format).
- Excludes inter-account transfers: any transaction ID (`Αρ. συναλλαγής`)
  appearing more than once across the loaded files is treated as a transfer or
  duplicate and left out of the totals.
- Applies built-in exclusion rules (e.g. instant-transfer moves out of the
  investment account). External, user-defined rules are planned.
- Reports total income, expenses, and savings, plus a per-month breakdown.
- The report's monthly table is interactive: click a month to open a modal
  listing that month's individual transactions, sortable by any column.

## Development

```bash
go test ./...   # run the test suite
```
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document web app and CLI usage"
```

---

## Self-Review notes

- **Spec coverage:** §1 command structure → Task 5; §2 renderer refactor → Task 2; §3 rules seam → Tasks 1 & 3; §4 web mode → Task 4; §5 error handling → Task 4 (with the documented 422→500 correction, since the parser skips bad rows); §6 testing → tests in every task; §7 README → Task 6. Future config (out of scope) is left as the `DefaultExclusionRules` attach point.
- **Type consistency:** `NewService(repo, renderer, rules)`, `html.NewFile`/`html.NewWriter`, `transaction.ExclusionRule`/`ApplyExclusions`/`DefaultExclusionRules`, `web.New(rules).Run(addr, open)`, and `dispatch`/`runGenerate`/`runServe` are used identically across tasks.
- **Deviation from spec:** the spec's "unparseable CSV → 422" is not reachable (parser skips malformed rows); the plan uses a defensive 500 and tests the reachable 400 paths instead.
```
