# Extract `reportHTML` to an Embedded `report.html`

**Date:** 2026-06-02
**Status:** Approved

## Problem

`internal/infra/html/renderer.go` is 1119 lines. Lines 288–1119 (~830 lines, 74% of
the file) are a single `const reportHTML` string holding the report's HTML, CSS, and
JavaScript. The Go logic — view-model construction, formatters, trend math — is only
the first ~287 lines. The large embedded blob:

- gets no HTML/CSS/JS syntax highlighting in the editor,
- bloats the Go file and obscures its actual logic,
- diverges from a pattern already established elsewhere in this repo.

## Existing Precedent

`internal/infra/web/page.go` already solves the same problem the intended way:

```go
//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))
```

The HTML lives in a sibling `index.html` and the Go file holds only template wiring.
This design applies the same convention to the report renderer.

## Goal

Move the HTML/CSS/JS blob out of `renderer.go` into a sibling `report.html` embedded
via `//go:embed`, leaving `renderer.go` to hold only Go logic. No behavioural change.

## Changes

### 1. New file `internal/infra/html/report.html`

Contains the verbatim contents of the current `reportHTML` const body (the text
between the backtick delimiters, current lines 289–1118), including all Go template
directives (`{{ .Generated }}`, `{{ range .Rows }}`, `{{ .ChartJSON }}`, etc.). The
editor now provides HTML/CSS/JS highlighting.

### 2. `renderer.go`

- Add `_ "embed"` to the import block.
- Replace the `const reportHTML = ` ... `` ` block (current lines 288–1119) with:

  ```go
  //go:embed report.html
  var reportHTML string
  ```

  Placed near the top of the file, adjacent to the `tmpl` var it feeds, matching
  `page.go`'s layout.

- The existing `var tmpl = template.Must(template.New("report").Funcs(...).Parse(reportHTML))`
  is unchanged — it still consumes a `string` named `reportHTML`.

## What Does NOT Change

- The `template.FuncMap` wiring (`euro`, `pct`, `month`, `months`).
- `Render`, `render`, `buildView`, all formatters and view-model types.
- Template directives remain inline in the HTML. `//go:embed` of a `string` preserves
  the bytes untouched, and `template.Parse` behaves identically whether its input came
  from a const or an embedded file.

## Verification

- `go build ./...` succeeds.
- `go test ./internal/infra/html/` passes. The existing render tests assert on output
  bytes (e.g. "embeds per-month transactions as JSON"), so a byte-identical move keeps
  them green. These tests are the regression guard — no new tests are required.

## Trade-off Accepted

`report.html` is not a standalone valid HTML document — it carries Go template syntax.
This is identical to the existing `index.html` and is consistent with the repo, so it
is acceptable.

## Result

`renderer.go` drops from 1119 to ~290 lines, all Go. One new file, `report.html`.
