# Robust Transfer/Duplicate Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `FilterTransfers` match transfer/duplicate pairs on ID + amount (to the cent) + calendar day instead of ID alone, so unrelated transactions that merely share an ID are no longer wrongly dropped.

**Architecture:** Keep the existing two-pass count-map shape. Replace the `string` (ID) map key with a composite `matchKey` struct (id + cents + day). Direction is documented in comments as the distinction between an inter-account transfer and a duplicate anomaly, but both kinds of collision are dropped. No public signature changes, so callers and tests outside this file are untouched.

**Tech Stack:** Go (standard library `math`, `time`), tests with `github.com/stretchr/testify/require`.

**Spec:** `docs/superpowers/specs/2026-06-02-robust-transfer-filtering-design.md`

---

## File Structure

- `internal/domain/transaction/transaction.go` — add `matchKey` type, `amountCents` and `keyOf` helpers, rewrite `FilterTransfers`. Add `math` to imports.
- `internal/domain/transaction/transaction_test.go` — extend the existing `TestFilterTransfers` table with three new cases.

No new files. No changes to `internal/app/report/service.go` (the `[]Transaction → []Transaction` signature is preserved).

---

### Task 1: Set up isolated worktree

**Files:** none (git/worktree setup only)

- [ ] **Step 1: Create the worktree on a new branch**

REQUIRED SUB-SKILL: Use `superpowers:using-git-worktrees` to create the worktree. If invoking that skill, follow it; otherwise the equivalent commands are:

```bash
cd /Users/leonidas/GolandProjects/cashflow-report
git worktree add -b robust-transfer-filtering ../cashflow-report-robust-transfer-filtering main
```

Expected: `Preparing worktree (new branch 'robust-transfer-filtering')` and a new directory `../cashflow-report-robust-transfer-filtering`.

- [ ] **Step 2: Confirm the worktree builds clean before any changes**

Run:
```bash
cd ../cashflow-report-robust-transfer-filtering && go build ./... && go test ./internal/domain/transaction/ 2>&1 | tail -5
```
Expected: build succeeds, `ok  github.com/lpatouchas/cashflow-report/internal/domain/transaction`.

**All subsequent tasks run inside `../cashflow-report-robust-transfer-filtering`.**

---

### Task 2: Add failing tests (red)

**Files:**
- Test: `internal/domain/transaction/transaction_test.go` (extend `TestFilterTransfers` table, ~line 44–52)

- [ ] **Step 1: Add three new cases to the `TestFilterTransfers` table**

Insert these table entries immediately after the existing `"single-file duplicate is excluded"` case (after its closing `},` around line 52), before the closing `}` of the `tests` slice:

```go
		{
			name: "same id different amount are kept",
			input: []Transaction{
				tx("X", "f1.csv", 10, true, d),
				tx("X", "f2.csv", 20, false, d),
			},
			wantIDs: []string{"X", "X"},
		},
		{
			name: "same id different date are kept",
			input: []Transaction{
				tx("Y", "f1.csv", 10, true, d),
				tx("Y", "f2.csv", 10, false, d.AddDate(0, 0, 1)),
			},
			wantIDs: []string{"Y", "Y"},
		},
		{
			name: "float-noise amounts still excluded",
			input: []Transaction{
				tx("Z", "f1.csv", 100.00, true, d),
				tx("Z", "f2.csv", 100.001, false, d),
			},
			wantIDs: nil,
		},
```

- [ ] **Step 2: Run the tests to verify the two "kept" cases fail**

Run:
```bash
go test ./internal/domain/transaction/ -run TestFilterTransfers -v 2>&1 | tail -25
```
Expected: FAIL. The current ID-only implementation drops `X`/`X` and `Y`/`Y` because they share an ID, so `same id different amount are kept` and `same id different date are kept` fail (got `nil`, want `["X","X"]` / `["Y","Y"]`). The `float-noise` case passes (current code drops on ID alone anyway).

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/domain/transaction/transaction_test.go
git commit -m "test: add robust transfer-matching cases for FilterTransfers"
```

---

### Task 3: Composite-key implementation (green)

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (imports ~line 3–9; `FilterTransfers` ~line 59–74)

- [ ] **Step 1: Add `math` to the import block**

Change the import block (lines 3–9) to:

```go
import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 2: Replace `FilterTransfers` and add the helpers**

Replace the entire current `FilterTransfers` function and its doc comment (lines 59–74) with:

```go
// matchKey identifies transactions that represent the same underlying movement.
// Two transactions collide when they share an ID, the same amount (to the cent)
// and the same calendar day.
type matchKey struct {
	id    string
	cents int64
	day   int64
}

// amountCents rounds an amount to whole cents for robust comparison, collapsing
// float-representation noise (e.g. 100.00 vs 100.001) onto a single value.
func amountCents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// keyOf builds the composite match key for a transaction. The day component is
// built from the calendar date in UTC so time-of-day and timezone never affect
// the match.
func keyOf(t Transaction) matchKey {
	day := time.Date(t.Date.Year(), t.Date.Month(), t.Date.Day(), 0, 0, 0, 0, time.UTC).Unix()
	return matchKey{id: t.ID, cents: amountCents(t.Amount), day: day}
}

// FilterTransfers removes inter-account transfers and duplicate anomalies.
//
// Transactions are grouped by (ID, amount-in-cents, calendar-day). Any group
// with more than one member is dropped entirely; only transactions whose key is
// unique are returned. A collision is one of two kinds, distinguished solely by
// direction — both are excluded:
//
//   - Inter-account transfer: two legs with opposite direction (one debit, one
//     credit). The money leaves one account and enters another.
//   - Duplicate anomaly: two or more records with the same direction. A repeated
//     export row.
//
// Transactions that share only an ID but differ in amount or date are unrelated
// and kept.
func FilterTransfers(txns []Transaction) []Transaction {
	counts := make(map[matchKey]int, len(txns))
	for _, t := range txns {
		counts[keyOf(t)]++
	}
	var kept []Transaction
	for _, t := range txns {
		if counts[keyOf(t)] == 1 {
			kept = append(kept, t)
		}
	}
	return kept
}
```

- [ ] **Step 3: Run the FilterTransfers tests to verify they pass**

Run:
```bash
go test ./internal/domain/transaction/ -run TestFilterTransfers -v 2>&1 | tail -25
```
Expected: PASS for all cases, including the retained `cross-file transfer is excluded` and `single-file duplicate is excluded`, and the three new cases.

- [ ] **Step 4: Commit the implementation**

```bash
git add internal/domain/transaction/transaction.go
git commit -m "feat: match transfers on id, amount and date in FilterTransfers"
```

---

### Task 4: Verify the whole package and module

**Files:** none (verification only)

- [ ] **Step 1: Run vet, build, and the full test suite**

Run:
```bash
go vet ./... && go build ./... && go test ./... 2>&1 | tail -20
```
Expected: no vet output, build succeeds, every package reports `ok` (or `no test files`). In particular `internal/domain/transaction` and `internal/app/report` pass.

- [ ] **Step 2: Confirm the working tree is clean**

Run:
```bash
git status --short
```
Expected: empty output (all changes committed across Tasks 2–3).

---

## Notes for the executor

- After the plan is fully implemented and reviewed, merge `robust-transfer-filtering` into `main` and remove the worktree:
  ```bash
  cd /Users/leonidas/GolandProjects/cashflow-report
  git merge robust-transfer-filtering
  git worktree remove ../cashflow-report-robust-transfer-filtering
  git branch -d robust-transfer-filtering
  ```
- Do not change the `FilterTransfers` signature; `internal/app/report/service.go:29` depends on it.
