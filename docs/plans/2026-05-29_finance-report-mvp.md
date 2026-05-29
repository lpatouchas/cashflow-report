# Finance Report MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that scans `./data/` for bank CSV exports, excludes inter-account transfers, and writes an HTML report of total + monthly income, expenses, and savings to `./report.html`.

**Architecture:** Clean Architecture per `AGENTS.md` (`domain ← app ← infra`). Pure business logic (transfer filtering, aggregation) lives in `internal/domain/transaction`. The `internal/app/report` service orchestrates: load → filter → summarize → render. Infrastructure adapters (`internal/infra/csv`, `internal/infra/html`) implement the repository and renderer ports. `main.go` wires concrete adapters via a testable `run()` function.

**Tech Stack:** Go 1.23 (toolchain 1.25), stdlib `encoding/csv`, `html/template`, `log/slog`, testify (`require` + hand-written `mock.Mock` mocks).

---

## File Structure

| File | Responsibility |
|---|---|
| `go.mod` | Module `github.com/lpatouchas/personal-finance`, `go 1.23`, testify dep |
| `.gitignore` | Ignore generated `report.html` and dropped CSVs |
| `data/.gitkeep` | Keep the input folder in git |
| `internal/domain/transaction/transaction.go` | `Transaction` entity, `Summary`, `MonthlyBreakdown`, `FilterTransfers`, `Summarize` |
| `internal/domain/transaction/transaction_test.go` | Unit tests for `FilterTransfers` + `Summarize` |
| `internal/domain/transaction/repository.go` | `Repository` port interface |
| `internal/domain/transaction/mock_repository.go` | Hand-written testify mock of `Repository` |
| `internal/app/report/renderer.go` | `Renderer` output port interface |
| `internal/app/report/mock_renderer.go` | Hand-written testify mock of `Renderer` |
| `internal/app/report/service.go` | `Service.GenerateReport` use case |
| `internal/app/report/service_test.go` | Unit tests with mocked repo + renderer |
| `internal/infra/csv/repository.go` | Scans `./data/`, parses Greek semicolon CSVs |
| `internal/infra/csv/repository_test.go` | Table-driven tests with temp-dir fixtures |
| `internal/infra/html/renderer.go` | Renders `Summary` → HTML file, Greek money formatting |
| `internal/infra/html/renderer_test.go` | Tests output file contents + `formatEuro` |
| `main.go` | `run()` wiring + thin `main()` |
| `main_test.go` | End-to-end test of `run()` |
| `README.md` | Usage docs |

---

## Task 1: Project Bootstrap

**Files:**
- Modify: `go.mod`
- Create: `.gitignore`, `data/.gitkeep`

- [ ] **Step 1: Initialise git**

Run:
```bash
git init
```
Expected: `Initialized empty Git repository in .../personal-finance/.git/`

- [ ] **Step 2: Fix the module name and Go version in `go.mod`**

Replace the entire contents of `go.mod` with:
```
module github.com/lpatouchas/personal-finance

go 1.23
```

- [ ] **Step 3: Add testify dependency**

Run:
```bash
go get github.com/stretchr/testify@v1.9.0 && go mod tidy
```
Expected: `go.mod` now lists `github.com/stretchr/testify v1.9.0`; a `go.sum` is created.

- [ ] **Step 4: Create the input folder placeholder**

Create `data/.gitkeep` with empty content (the folder must exist so the CLI can scan it; the placeholder keeps it in git).

- [ ] **Step 5: Create `.gitignore`**

Create `.gitignore`:
```gitignore
# Generated report
/report.html

# Dropped bank exports (user data — never commit)
/data/*.csv

# Go build artifacts
/personal-finance
*.out
```

- [ ] **Step 6: Verify the module builds**

Run:
```bash
go build ./...
```
Expected: exits 0 with no output (no packages yet beyond `main.go`; if the stock GoLand `main.go` is still present it will build).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum .gitignore data/.gitkeep
git commit -m "chore: bootstrap go module and project layout"
```

---

## Task 2: Domain Types and Repository Port

**Files:**
- Create: `internal/domain/transaction/transaction.go`
- Create: `internal/domain/transaction/repository.go`

No behaviour yet — these are pure declarations consumed by later tasks. They are exercised (and covered) by the tests in Tasks 3–4.

- [ ] **Step 1: Create the entity and value objects**

Create `internal/domain/transaction/transaction.go`:
```go
package transaction

import "time"

// Transaction is a single bank movement loaded from a CSV export.
// Amount is always positive; direction is carried by IsDebit.
type Transaction struct {
	ID          string    // Αρ. συναλλαγής
	Date        time.Time // Ημερομηνία
	Description string    // Αιτιολογία
	Amount      float64   // Ποσό (positive)
	IsDebit     bool      // Πρόσημο ποσού: Χ = true (expense), Π = false (income)
	SourceFile  string    // originating CSV filename
}

// Summary is the aggregated report over a set of transactions.
type Summary struct {
	TotalIncome   float64
	TotalExpenses float64
	Savings       float64 // TotalIncome - TotalExpenses
	ByMonth       []MonthlyBreakdown
}

// MonthlyBreakdown holds income/expenses/savings for a single calendar month.
type MonthlyBreakdown struct {
	Year     int
	Month    time.Month
	Income   float64
	Expenses float64
	Savings  float64
}
```

- [ ] **Step 2: Create the repository port**

Create `internal/domain/transaction/repository.go`:
```go
package transaction

import "context"

// Repository loads all transactions from a data source.
type Repository interface {
	GetAll(ctx context.Context) ([]Transaction, error)
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./internal/domain/transaction/
```
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/repository.go
git commit -m "feat: add transaction domain types and repository port"
```

---

## Task 3: Domain — FilterTransfers (TDD)

**Files:**
- Test: `internal/domain/transaction/transaction_test.go`
- Modify: `internal/domain/transaction/transaction.go`

**Rule:** A transaction ID that appears more than once across the loaded set is excluded. This covers both cases from the spec: the same `Αρ. συναλλαγής` appearing in 2+ files (an inter-account transfer) and the same ID appearing 2+ times in one file (a data anomaly). Only IDs occurring exactly once are kept.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/transaction/transaction_test.go`:
```go
package transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func tx(id, file string, amount float64, debit bool, date time.Time) Transaction {
	return Transaction{ID: id, SourceFile: file, Amount: amount, IsDebit: debit, Date: date}
}

func TestFilterTransfers(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   []Transaction
		wantIDs []string
	}{
		{
			name:    "empty input",
			input:   nil,
			wantIDs: nil,
		},
		{
			name: "all unique are kept",
			input: []Transaction{
				tx("A", "f1.csv", 10, true, d),
				tx("B", "f2.csv", 20, false, d),
			},
			wantIDs: []string{"A", "B"},
		},
		{
			name: "cross-file transfer is excluded",
			input: []Transaction{
				tx("T", "f1.csv", 100, true, d),
				tx("T", "f2.csv", 100, false, d),
				tx("K", "f1.csv", 5, true, d),
			},
			wantIDs: []string{"K"},
		},
		{
			name: "single-file duplicate is excluded",
			input: []Transaction{
				tx("D", "f1.csv", 7, true, d),
				tx("D", "f1.csv", 7, true, d),
				tx("U", "f1.csv", 9, false, d),
			},
			wantIDs: []string{"U"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterTransfers(tc.input)
			var gotIDs []string
			for _, x := range got {
				gotIDs = append(gotIDs, x.ID)
			}
			require.Equal(t, tc.wantIDs, gotIDs)
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/domain/transaction/ -run TestFilterTransfers -v
```
Expected: compile error / FAIL — `undefined: FilterTransfers`.

- [ ] **Step 3: Implement `FilterTransfers`**

Append to `internal/domain/transaction/transaction.go`:
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/domain/transaction/ -run TestFilterTransfers -v
```
Expected: PASS (all four subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: filter inter-account transfers and duplicate IDs"
```

---

## Task 4: Domain — Summarize (TDD)

**Files:**
- Modify: `internal/domain/transaction/transaction_test.go`
- Modify: `internal/domain/transaction/transaction.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/transaction/transaction_test.go`:
```go
func TestSummarize(t *testing.T) {
	may := time.Date(2026, time.May, 10, 0, 0, 0, 0, time.UTC)
	may2 := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	apr := time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC)

	t.Run("empty input yields zero summary", func(t *testing.T) {
		got := Summarize(nil)
		require.Equal(t, Summary{}, got)
	})

	t.Run("aggregates totals and savings", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "f", 100, false, may), // income
			tx("b", "f", 30, true, may),   // expense
		})
		require.InDelta(t, 100, got.TotalIncome, 0.001)
		require.InDelta(t, 30, got.TotalExpenses, 0.001)
		require.InDelta(t, 70, got.Savings, 0.001)
	})

	t.Run("groups by month sorted oldest first", func(t *testing.T) {
		got := Summarize([]Transaction{
			tx("a", "f", 50, false, may),
			tx("b", "f", 20, true, may2),
			tx("c", "f", 200, false, apr),
		})
		require.Len(t, got.ByMonth, 2)

		require.Equal(t, 2026, got.ByMonth[0].Year)
		require.Equal(t, time.April, got.ByMonth[0].Month)
		require.InDelta(t, 200, got.ByMonth[0].Income, 0.001)
		require.InDelta(t, 0, got.ByMonth[0].Expenses, 0.001)
		require.InDelta(t, 200, got.ByMonth[0].Savings, 0.001)

		require.Equal(t, time.May, got.ByMonth[1].Month)
		require.InDelta(t, 50, got.ByMonth[1].Income, 0.001)
		require.InDelta(t, 20, got.ByMonth[1].Expenses, 0.001)
		require.InDelta(t, 30, got.ByMonth[1].Savings, 0.001)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/domain/transaction/ -run TestSummarize -v
```
Expected: compile error / FAIL — `undefined: Summarize`.

- [ ] **Step 3: Implement `Summarize`**

Append to `internal/domain/transaction/transaction.go` (add `"sort"` to the existing import block, making it `import ("sort"; "time")`):
```go
// Summarize aggregates transactions into totals and a chronological
// per-month breakdown. Debits are expenses; credits are income.
func Summarize(txns []Transaction) Summary {
	type key struct {
		year  int
		month time.Month
	}
	months := make(map[key]*MonthlyBreakdown)

	var s Summary
	for _, t := range txns {
		if t.IsDebit {
			s.TotalExpenses += t.Amount
		} else {
			s.TotalIncome += t.Amount
		}

		k := key{t.Date.Year(), t.Date.Month()}
		mb := months[k]
		if mb == nil {
			mb = &MonthlyBreakdown{Year: k.year, Month: k.month}
			months[k] = mb
		}
		if t.IsDebit {
			mb.Expenses += t.Amount
		} else {
			mb.Income += t.Amount
		}
	}
	s.Savings = s.TotalIncome - s.TotalExpenses

	for _, mb := range months {
		mb.Savings = mb.Income - mb.Expenses
		s.ByMonth = append(s.ByMonth, *mb)
	}
	sort.Slice(s.ByMonth, func(i, j int) bool {
		if s.ByMonth[i].Year != s.ByMonth[j].Year {
			return s.ByMonth[i].Year < s.ByMonth[j].Year
		}
		return s.ByMonth[i].Month < s.ByMonth[j].Month
	})

	return s
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/domain/transaction/ -v
```
Expected: PASS for both `TestFilterTransfers` and `TestSummarize`.

- [ ] **Step 5: Confirm full domain coverage**

Run:
```bash
go test ./internal/domain/transaction/ -cover
```
Expected: `coverage: 100.0% of statements`.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat: aggregate transactions into monthly summary"
```

---

## Task 5: Domain — Repository Mock

**Files:**
- Create: `internal/domain/transaction/mock_repository.go`

Per `AGENTS.md` rule 6, mocks live next to the interface they mock. This mock is used by the app-layer service tests in Task 7.

- [ ] **Step 1: Create the mock**

Create `internal/domain/transaction/mock_repository.go`:
```go
package transaction

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockRepository is a hand-written testify mock of Repository.
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) GetAll(ctx context.Context) ([]Transaction, error) {
	args := m.Called(ctx)
	var txns []Transaction
	if v := args.Get(0); v != nil {
		txns = v.([]Transaction)
	}
	return txns, args.Error(1)
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
go build ./internal/domain/transaction/
```
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/transaction/mock_repository.go
git commit -m "test: add repository mock for app-layer tests"
```

---

## Task 6: App — Renderer Port and Mock

**Files:**
- Create: `internal/app/report/renderer.go`
- Create: `internal/app/report/mock_renderer.go`

- [ ] **Step 1: Create the renderer port**

Create `internal/app/report/renderer.go`:
```go
package report

import (
	"context"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Renderer is the output port that writes a Summary to its destination.
type Renderer interface {
	Render(ctx context.Context, summary transaction.Summary) error
}
```

- [ ] **Step 2: Create the renderer mock**

Create `internal/app/report/mock_renderer.go`:
```go
package report

import (
	"context"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/mock"
)

// MockRenderer is a hand-written testify mock of Renderer.
type MockRenderer struct {
	mock.Mock
}

func (m *MockRenderer) Render(ctx context.Context, summary transaction.Summary) error {
	args := m.Called(ctx, summary)
	return args.Error(0)
}
```

- [ ] **Step 3: Verify it compiles**

Run:
```bash
go build ./internal/app/report/
```
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add internal/app/report/renderer.go internal/app/report/mock_renderer.go
git commit -m "feat: add report renderer port and mock"
```

---

## Task 7: App — GenerateReport Service (TDD)

**Files:**
- Test: `internal/app/report/service_test.go`
- Create: `internal/app/report/service.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/report/service_test.go`:
```go
package report

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGenerateReport(t *testing.T) {
	ctx := context.Background()
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	t.Run("filters transfers then renders summary", func(t *testing.T) {
		txns := []transaction.Transaction{
			{ID: "T", SourceFile: "a.csv", Amount: 100, IsDebit: true, Date: d},
			{ID: "T", SourceFile: "b.csv", Amount: 100, IsDebit: false, Date: d},
			{ID: "INC", SourceFile: "a.csv", Amount: 500, IsDebit: false, Date: d},
			{ID: "EXP", SourceFile: "a.csv", Amount: 200, IsDebit: true, Date: d},
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

		svc := NewService(repo, renderer)
		err := svc.GenerateReport(ctx)

		require.NoError(t, err)
		require.InDelta(t, 500, captured.TotalIncome, 0.001)
		require.InDelta(t, 200, captured.TotalExpenses, 0.001)
		require.InDelta(t, 300, captured.Savings, 0.001)
		repo.AssertExpectations(t)
		renderer.AssertExpectations(t)
	})

	t.Run("returns repo error without rendering", func(t *testing.T) {
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(nil, errors.New("boom"))

		renderer := &MockRenderer{}

		svc := NewService(repo, renderer)
		err := svc.GenerateReport(ctx)

		require.Error(t, err)
		renderer.AssertNotCalled(t, "Render", mock.Anything, mock.Anything)
	})

	t.Run("propagates renderer error", func(t *testing.T) {
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return([]transaction.Transaction{}, nil)

		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).Return(errors.New("write failed"))

		svc := NewService(repo, renderer)
		err := svc.GenerateReport(ctx)

		require.Error(t, err)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/app/report/ -run TestGenerateReport -v
```
Expected: compile error / FAIL — `undefined: NewService`.

- [ ] **Step 3: Implement the service**

Create `internal/app/report/service.go`:
```go
package report

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Service orchestrates report generation: load → filter → summarize → render.
type Service struct {
	repo     transaction.Repository
	renderer Renderer
}

func NewService(repo transaction.Repository, renderer Renderer) *Service {
	return &Service{repo: repo, renderer: renderer}
}

func (s *Service) GenerateReport(ctx context.Context) error {
	all, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("loading transactions: %w", err)
	}

	kept := transaction.FilterTransfers(all)
	if excluded := len(all) - len(kept); excluded > 0 {
		slog.Info("excluded inter-account transfers and duplicates", "count", excluded)
	}

	summary := transaction.Summarize(kept)

	if err := s.renderer.Render(ctx, summary); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/app/report/ -v
```
Expected: PASS for all three subtests.

- [ ] **Step 5: Confirm full app coverage**

Run:
```bash
go test ./internal/app/report/ -cover
```
Expected: `coverage: 100.0% of statements`.

- [ ] **Step 6: Commit**

```bash
git add internal/app/report/service.go internal/app/report/service_test.go
git commit -m "feat: add GenerateReport use case"
```

---

## Task 8: Infra — CSV Repository (TDD)

**Files:**
- Test: `internal/infra/csv/repository_test.go`
- Create: `internal/infra/csv/repository.go`

The repository scans a directory for `*.csv`, parses each semicolon-separated, UTF-8 Greek export, strips `="..."` wrappers, converts comma-decimal amounts, parses `DD/MM/YYYY` dates, maps `Χ`→debit / `Π`→credit, and logs+skips malformed rows. The note constants `"Χ"` and `"Π"` are the Greek capital letters Chi (U+03A7) and Pi (U+03A0).

- [ ] **Step 1: Write the failing test**

Create `internal/infra/csv/repository_test.go`:
```go
package csv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const header = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

func writeCSV(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

func TestGetAll(t *testing.T) {
	ctx := context.Background()

	t.Run("empty folder returns error", func(t *testing.T) {
		dir := t.TempDir()
		_, err := New(dir).GetAll(ctx)
		require.ErrorContains(t, err, "no CSV files found")
	})

	t.Run("parses rows, unwraps strings, converts amounts and dates", func(t *testing.T) {
		dir := t.TempDir()
		body := header + "\n" +
			`1;29/05/2026;="ΒUΤCΗΕRΙΕS";99;27/5/2026;="202605290990022734";53,79;Χ;` + "\n" +
			`27;18/05/2026;="ΑWΒ John DOE";96;18/5/2026;="202605180960379907";1.550,00;Π;` + "\n"
		writeCSV(t, dir, "acc1.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 2)

		require.Equal(t, "202605290990022734", got[0].ID)
		require.Equal(t, "ΒUΤCΗΕRΙΕS", got[0].Description)
		require.InDelta(t, 53.79, got[0].Amount, 0.001)
		require.True(t, got[0].IsDebit)
		require.Equal(t, 2026, got[0].Date.Year())
		require.Equal(t, 5, int(got[0].Date.Month()))
		require.Equal(t, 29, got[0].Date.Day())
		require.Equal(t, "acc1.csv", got[0].SourceFile)

		require.InDelta(t, 1550.00, got[1].Amount, 0.001)
		require.False(t, got[1].IsDebit)
	})

	t.Run("skips blank and malformed rows", func(t *testing.T) {
		dir := t.TempDir()
		body := header + "\n" +
			"\n" + // blank line
			`x;notadate;="X";1;1;="ID1";10,00;Χ;` + "\n" + // bad date
			`1;01/05/2026;="Y";1;1;="ID2";notanumber;Χ;` + "\n" + // bad amount
			`1;01/05/2026;="Z";1;1;="ID3";5,00;Q;` + "\n" + // bad sign
			`1;01/05/2026;="OK";1;1;="ID4";5,00;Π;` + "\n" // good
		writeCSV(t, dir, "acc.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "ID4", got[0].ID)
	})

	t.Run("reads multiple files", func(t *testing.T) {
		dir := t.TempDir()
		writeCSV(t, dir, "a.csv", header+"\n"+`1;01/05/2026;="A";1;1;="A1";1,00;Χ;`+"\n")
		writeCSV(t, dir, "b.csv", header+"\n"+`1;01/05/2026;="B";1;1;="B1";2,00;Π;`+"\n")

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 2)
	})

	t.Run("wraps read error for unreadable file", func(t *testing.T) {
		dir := t.TempDir()
		// A directory named like a CSV: Open succeeds, ReadAll fails.
		require.NoError(t, os.Mkdir(filepath.Join(dir, "bad.csv"), 0o755))

		_, err := New(dir).GetAll(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, "bad.csv")
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/infra/csv/ -run TestGetAll -v
```
Expected: compile error / FAIL — `undefined: New`.

- [ ] **Step 3: Implement the repository**

Create `internal/infra/csv/repository.go`:
```go
package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

const (
	signDebit  = "Χ" // U+03A7 Greek capital Chi
	signCredit = "Π" // U+03A0 Greek capital Pi
	columns    = 8
)

// Repository loads transactions from semicolon-separated Greek CSV exports
// found in a directory.
type Repository struct {
	dir string
}

func New(dir string) *Repository {
	return &Repository{dir: dir}
}

func (r *Repository) GetAll(ctx context.Context) ([]transaction.Transaction, error) {
	// Pattern is a fixed literal, so Glob never returns ErrBadPattern here.
	matches, _ := filepath.Glob(filepath.Join(r.dir, "*.csv"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no CSV files found in %s", r.dir)
	}

	var out []transaction.Transaction
	for _, path := range matches {
		txns, err := r.parseFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
		}
		out = append(out, txns...)
	}
	return out, nil
}

func (r *Repository) parseFile(path string) ([]transaction.Transaction, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = ';'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	base := filepath.Base(path)
	var txns []transaction.Transaction
	for i, rec := range records {
		if i == 0 {
			continue // header
		}
		t, ok := parseRow(rec, base, i+1)
		if !ok {
			continue
		}
		txns = append(txns, t)
	}
	return txns, nil
}

func parseRow(rec []string, file string, line int) (transaction.Transaction, bool) {
	if len(rec) < columns {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "too few columns")
		return transaction.Transaction{}, false
	}

	date, err := parseDate(unwrap(rec[1]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}

	amount, err := parseAmount(unwrap(rec[6]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad amount")
		return transaction.Transaction{}, false
	}

	var isDebit bool
	switch unwrap(rec[7]) {
	case signDebit:
		isDebit = true
	case signCredit:
		isDebit = false
	default:
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad sign")
		return transaction.Transaction{}, false
	}

	return transaction.Transaction{
		ID:          unwrap(rec[5]),
		Date:        date,
		Description: unwrap(rec[2]),
		Amount:      amount,
		IsDebit:     isDebit,
		SourceFile:  file,
	}, true
}

// unwrap strips the spreadsheet ="..." wrapper from a CSV field.
func unwrap(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	return strings.Trim(s, `"`)
}

// parseAmount converts a Greek-formatted amount (1.550,00) to a float.
func parseAmount(s string) (float64, error) {
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

// parseDate parses DD/MM/YYYY, tolerating non-zero-padded day/month.
func parseDate(s string) (time.Time, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}
	day, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	year, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/infra/csv/ -v
```
Expected: PASS for all subtests.

- [ ] **Step 5: Confirm full CSV coverage**

Run:
```bash
go test ./internal/infra/csv/ -cover
```
Expected: `coverage: 100.0% of statements`.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/csv/repository.go internal/infra/csv/repository_test.go
git commit -m "feat: add CSV repository for Greek bank exports"
```

---

## Task 9: Infra — HTML Renderer (TDD)

**Files:**
- Test: `internal/infra/html/renderer_test.go`
- Create: `internal/infra/html/renderer.go`

- [ ] **Step 1: Write the failing test**

Create `internal/infra/html/renderer_test.go`:
```go
package html

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestFormatEuro(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "€0,00"},
		{53.79, "€53,79"},
		{1234.56, "€1.234,56"},
		{1000000, "€1.000.000,00"},
		{-1234.5, "-€1.234,50"},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, formatEuro(tc.in))
	}
}

func TestRender(t *testing.T) {
	ctx := context.Background()

	t.Run("writes report with totals and month rows", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		summary := transaction.Summary{
			TotalIncome:   1500,
			TotalExpenses: 500,
			Savings:       1000,
			ByMonth: []transaction.MonthlyBreakdown{
				{Year: 2026, Month: time.May, Income: 1500, Expenses: 500, Savings: 1000},
			},
		}

		err := New(out).Render(ctx, summary)
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		content := string(data)

		require.Contains(t, content, "Total Income")
		require.Contains(t, content, "€1.500,00")
		require.Contains(t, content, "€1.000,00")
		require.Contains(t, content, "May 2026")
	})

	t.Run("renders empty summary without month rows", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "report.html")

		err := New(out).Render(ctx, transaction.Summary{})
		require.NoError(t, err)

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "No transactions")
	})

	t.Run("returns error when path is unwritable", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "nonexistent-dir", "report.html")
		err := New(out).Render(ctx, transaction.Summary{})
		require.Error(t, err)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/infra/html/ -run 'TestFormatEuro|TestRender' -v
```
Expected: compile error / FAIL — `undefined: formatEuro` / `undefined: New`.

- [ ] **Step 3: Implement the renderer**

Create `internal/infra/html/renderer.go`:
```go
package html

import (
	"context"
	"html/template"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Renderer writes a Summary to an HTML file at a fixed path.
type Renderer struct {
	path string
}

func New(path string) *Renderer {
	return &Renderer{path: path}
}

type viewData struct {
	GeneratedAt string
	Summary     transaction.Summary
}

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"euro":  formatEuro,
	"month": monthLabel,
}).Parse(reportHTML))

func (r *Renderer) Render(ctx context.Context, summary transaction.Summary) error {
	f, err := os.Create(r.path)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, viewData{
		GeneratedAt: time.Now().Format("2006-01-02 15:04"),
		Summary:     summary,
	})
}

func monthLabel(mb transaction.MonthlyBreakdown) string {
	return mb.Month.String() + " " + strconv.Itoa(mb.Year)
}

// formatEuro renders a value as Greek-locale currency: €1.234,56.
func formatEuro(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	dot := strings.IndexByte(s, '.')
	intPart, dec := s[:dot], s[dot+1:]

	var b strings.Builder
	n := len(intPart)
	for i, ch := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(ch)
	}
	return sign + "€" + b.String() + "," + dec
}

const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Finance Report</title>
<style>
body { font-family: system-ui, sans-serif; margin: 2rem; color: #1a1a1a; }
.cards { display: flex; gap: 1rem; margin: 1rem 0 2rem; }
.card { flex: 1; padding: 1rem 1.5rem; border-radius: 8px; background: #f4f4f5; }
.card .value { font-size: 1.6rem; font-weight: 700; }
.card.savings { background: #e8f5e9; }
table { border-collapse: collapse; width: 100%; }
th, td { padding: .5rem .75rem; text-align: right; border-bottom: 1px solid #e4e4e7; }
th:first-child, td:first-child { text-align: left; }
caption { text-align: left; font-weight: 600; margin-bottom: .5rem; }
</style>
</head>
<body>
<h1>Finance Report</h1>
<p>Generated {{ .GeneratedAt }}</p>

<div class="cards">
  <div class="card"><div>Total Income</div><div class="value">{{ euro .Summary.TotalIncome }}</div></div>
  <div class="card"><div>Total Expenses</div><div class="value">{{ euro .Summary.TotalExpenses }}</div></div>
  <div class="card savings"><div>Savings</div><div class="value">{{ euro .Summary.Savings }}</div></div>
</div>

{{ if .Summary.ByMonth }}
<table>
  <caption>Monthly Breakdown</caption>
  <thead><tr><th>Month</th><th>Income</th><th>Expenses</th><th>Savings</th></tr></thead>
  <tbody>
  {{ range .Summary.ByMonth }}
    <tr>
      <td>{{ month . }}</td>
      <td>{{ euro .Income }}</td>
      <td>{{ euro .Expenses }}</td>
      <td>{{ euro .Savings }}</td>
    </tr>
  {{ end }}
  </tbody>
</table>
{{ else }}
<p>No transactions to report.</p>
{{ end }}
</body>
</html>
`
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/infra/html/ -v
```
Expected: PASS for `TestFormatEuro` and all `TestRender` subtests.

- [ ] **Step 5: Confirm full HTML coverage**

Run:
```bash
go test ./internal/infra/html/ -cover
```
Expected: `coverage: 100.0% of statements`.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/html/renderer.go internal/infra/html/renderer_test.go
git commit -m "feat: add HTML renderer with Greek currency formatting"
```

---

## Task 10: Wiring — run() and main (TDD)

**Files:**
- Test: `main_test.go`
- Modify: `main.go` (replace the stock GoLand template entirely)

The logic lives in a testable `run(dataDir, outputPath string) error`. `func main()` only supplies the fixed paths and handles process exit; it contains no logic and is excluded from the coverage gate.

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	header := "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

	t.Run("generates report end to end", func(t *testing.T) {
		dataDir := t.TempDir()
		body := header + "\n" +
			`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))

		out := filepath.Join(t.TempDir(), "report.html")
		require.NoError(t, run(dataDir, out))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "Finance Report")
		require.Contains(t, string(data), "May 2026")
	})

	t.Run("returns error when no data", func(t *testing.T) {
		err := run(t.TempDir(), filepath.Join(t.TempDir(), "report.html"))
		require.Error(t, err)
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test . -run TestRun -v
```
Expected: compile error / FAIL — `undefined: run` (the stock `main.go` has no `run`).

- [ ] **Step 3: Replace `main.go`**

Replace the entire contents of `main.go` with:
```go
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/lpatouchas/personal-finance/internal/app/report"
	"github.com/lpatouchas/personal-finance/internal/infra/csv"
	"github.com/lpatouchas/personal-finance/internal/infra/html"
)

const (
	dataDir    = "./data"
	outputPath = "./report.html"
)

func run(dataDir, outputPath string) error {
	repo := csv.New(dataDir)
	renderer := html.New(outputPath)
	svc := report.NewService(repo, renderer)
	return svc.GenerateReport(context.Background())
}

func main() {
	if err := run(dataDir, outputPath); err != nil {
		slog.Error("report generation failed", "error", err)
		os.Exit(1)
	}
	slog.Info("report generated", "path", outputPath)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test . -run TestRun -v
```
Expected: PASS for both subtests.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: wire CLI entrypoint"
```

---

## Task 11: Docs and Full Verification

**Files:**
- Create: `README.md`
- Modify: `AGENTS.md` (add plan reference under the Plans section)

- [ ] **Step 1: Create `README.md`**

Create `README.md`:
```markdown
# personal-finance

A CLI that summarises bank transactions into an HTML report.

## Usage

1. Drop one or more bank CSV exports into `./data/`.
2. Run:

   ```bash
   go run .
   ```

3. Open the generated `./report.html`.

## What it does

- Loads every `*.csv` in `./data/` (semicolon-separated Greek bank export format).
- Excludes inter-account transfers: any transaction ID (`Αρ. συναλλαγής`)
  appearing more than once across the loaded files is treated as a transfer
  or duplicate and left out of the totals.
- Reports total income, expenses, and savings, plus a per-month breakdown.

## Development

```bash
go test ./... -cover   # all packages must stay at 100%
```
```

- [ ] **Step 2: Add the plan reference to `AGENTS.md`**

In `AGENTS.md`, under the `## Plans` section, add this line directly beneath the numbered "When creating a new plan" guidance list (it is the project's running list of plans):
```markdown
- [Finance Report MVP](docs/plans/2026-05-29_finance-report-mvp.md) — CLI that summarises bank CSVs into an HTML report, excluding inter-account transfers.
```

- [ ] **Step 3: Run the full test suite with coverage**

Run:
```bash
go test ./... -cover
```
Expected: every package PASS. Domain, app, infra/csv, infra/html report `100.0%`. The root `main` package reports near-100% (only the wiring-only `func main()` is uncovered, by design).

- [ ] **Step 4: Run go vet**

Run:
```bash
go vet ./...
```
Expected: exits 0 with no output.

- [ ] **Step 5: Smoke-test the binary**

Run:
```bash
printf 'Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;\n1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;\n2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;\n' > data/sample.csv
go run .
```
Expected: log line `report generated path=./report.html`; `report.html` exists and opens to a summary card (Income €1.550,00, Expenses €53,79, Savings €1.496,21) and a May 2026 row. Then clean up: `rm data/sample.csv report.html`.

- [ ] **Step 6: Commit**

```bash
git add README.md AGENTS.md
git commit -m "docs: add README and register implementation plan"
```

---

## Self-Review Notes

- **Spec coverage:** Architecture/structure → Tasks 1–2, 5, 6, 10. Transaction entity + transfer rule → Tasks 2–3. Summary/monthly aggregation → Task 4. Repository port → Task 2; CSV adapter → Task 8. Renderer port → Task 6; HTML adapter (Greek money format, header, summary card, monthly table) → Task 9. App service flow → Task 7. Error-handling table (empty folder, unreadable file, malformed row, unwritable output) → Tasks 7–9. Testing strategy (100% coverage, table-driven, hand-written testify mocks) → every task. go.mod module-name fix → Task 1.
- **Single-file duplicate handling** (spec: "log & skip"): implemented as part of the count-based `FilterTransfers` (any ID seen >1 time is dropped) with an aggregate log line in the service. This satisfies both the transfer rule and the duplicate-anomaly rule with one mechanism.
- **`func main()` coverage:** logic extracted to `run()`, which is fully tested; the 3-line `main()` (paths + `os.Exit`) is the one intentional exception to the 100% gate, consistent with standard Go practice.
- **Type consistency:** constructors are `csv.New` / `html.New` / `report.NewService`; ports are `transaction.Repository` and `report.Renderer`; domain functions are `FilterTransfers` and `Summarize` — used identically across Tasks 7–10.
