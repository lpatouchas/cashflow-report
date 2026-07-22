# Lookalike-tolerant text matching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all five Greek-text comparison sites tolerant of Greek↔Latin lookalike characters (e.g. `VΙSΑ` vs `VISA`) so text that looks identical to a human compares equal.

**Architecture:** A new zero-dependency package `internal/textfold` exposes one function, `Fold(string) string`, that maps each Greek letter with a visually identical Latin twin onto that twin (case-preserving, strictly 1:1). The five affected comparison sites fold both operands before comparing, preserving existing exact/substring semantics.

**Tech Stack:** Go 1.23, standard library only (`strings`), testify for tests.

## Global Constraints

- **Zero new runtime dependencies.** Only `github.com/stretchr/testify` may be imported in tests; no `golang.org/x/text`. `textfold` uses `strings` only.
- **Case-preserving.** Folding never changes letter case. `COOP` must not match `coop`.
- **Strictly 1:1 mapping.** Each Greek rune maps to exactly one Latin rune; digits, punctuation, and Greek-only letters pass through unchanged. This guarantees Fold never merges two strings that look different to a human.
- **`textfold` imports nothing from the project** (no import cycle): `internal/infra/csv` and `internal/domain/transaction` both import it.
- **Fold table (final):**
  - Uppercase: `Α→A Β→B Ε→E Ζ→Z Η→H Ι→I Κ→K Μ→M Ν→N Ο→O Ρ→P Τ→T Υ→Y Χ→X`
  - Lowercase: `ο→o ρ→p χ→x`
  - Intentionally excluded (not reliably indistinguishable): lowercase `ν/v`, `υ/y`, `κ/k`. Adding one later is safe and non-breaking because the map is 1:1.
- Commit message trailer on every commit: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- Do NOT open a PR. Commit (and push only if asked); the user opens PRs.

## File Structure

- **Create** `internal/textfold/fold.go` — the `Fold` function + fold table. Single responsibility: lookalike normalization.
- **Create** `internal/textfold/fold_test.go` — unit tests for `Fold`.
- **Modify** `internal/infra/csv/repository.go` — fold both operands in `isVISAHeader` and the pending-status check; add import.
- **Modify** `internal/infra/csv/repository_test.go` — regression tests for the two folded sites.
- **Modify** `internal/domain/transaction/transaction.go` — fold both operands in `CompileRule` (contains + exact) and `descriptionMatches` (contains); add import.
- **Modify** `internal/domain/transaction/transaction_test.go` — regression tests for the three folded sites.

---

### Task 1: `internal/textfold` package

**Files:**
- Create: `internal/textfold/fold.go`
- Test: `internal/textfold/fold_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func Fold(s string) string` in package `textfold` (import path `github.com/lpatouchas/cashflow-report/internal/textfold`).

- [ ] **Step 1: Write the failing test**

Create `internal/textfold/fold_test.go`:

```go
package textfold

import "testing"

func TestFold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Greek uppercase lookalikes fold to Latin.
		{"greek visa", "VΙSΑ", "VISA"},          // Greek Ι U+0399, Α U+0391
		{"pure latin unchanged", "VISA", "VISA"},
		{"greek header word", "Ημ/νία", "Hμ/νία"}, // only Η has a Latin twin here
		{"lowercase lookalikes", "χρονο", "xpoνo"}, // χ→x, ρ→p, ο→o; ν stays
		// No false merges.
		{"digits not folded", "CO0P", "CO0P"},
		{"case preserved", "coop", "coop"},
		// Greek-only letters pass through.
		{"greek only", "λδψω", "λδψω"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fold(tt.in); got != tt.want {
				t.Errorf("Fold(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFoldMakesLookalikesEqual(t *testing.T) {
	if Fold("VΙSΑ") != Fold("VISA") {
		t.Errorf("Fold should equate VΙSΑ and VISA")
	}
}

func TestFoldDoesNotMergeDifferentText(t *testing.T) {
	// Genuinely different text must stay different after folding.
	if Fold("CO0P") == Fold("COOP") {
		t.Errorf("Fold must not merge digit 0 with letter O")
	}
	if Fold("COOP") == Fold("coop") {
		t.Errorf("Fold must not merge different case")
	}
}

func TestFoldIdempotent(t *testing.T) {
	for _, s := range []string{"VΙSΑ", "Ημ/νία συναλλαγής", "χρονο", "λδψω"} {
		if Fold(Fold(s)) != Fold(s) {
			t.Errorf("Fold not idempotent for %q", s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/textfold/...`
Expected: FAIL — build error, `undefined: Fold` (package/function does not exist yet).

- [ ] **Step 3: Write the implementation**

Create `internal/textfold/fold.go`:

```go
// Package textfold provides lookalike-tolerant text comparison for Greek/Latin
// data, where visually identical characters can be distinct Unicode codepoints
// (e.g. Greek Ι U+0399 vs Latin I U+0049). Fold maps Greek letters that have an
// identical-looking Latin twin onto that twin, so such strings compare equal.
package textfold

import "strings"

// greekToLatin maps each Greek letter that is visually indistinguishable from a
// Latin letter in common fonts onto that Latin letter. It is case-preserving and
// strictly 1:1, so folding can never merge two strings that look different to a
// human — only ones that look identical. Lowercase ν/υ/κ are deliberately absent:
// they are not reliably identical to v/y/k. Adding a pair later stays 1:1 and
// non-breaking.
var greekToLatin = map[rune]rune{
	// Uppercase
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H', 'Ι': 'I',
	'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P', 'Τ': 'T',
	'Υ': 'Y', 'Χ': 'X',
	// Lowercase — only the genuinely indistinguishable pairs.
	'ο': 'o', 'ρ': 'p', 'χ': 'x',
}

// Fold returns a canonical form of s for lookalike-tolerant comparison. Each
// Greek letter with an identical-looking Latin counterpart is replaced by that
// Latin letter; every other rune (Greek-only letters, digits, punctuation,
// existing Latin text) passes through unchanged. Case is preserved, so Fold
// never equates text that differs only by case. Two strings that look identical
// to a human are equal after Fold.
func Fold(s string) string {
	return strings.Map(func(r rune) rune {
		if latin, ok := greekToLatin[r]; ok {
			return latin
		}
		return r
	}, s)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/textfold/...`
Expected: PASS (`ok  github.com/lpatouchas/cashflow-report/internal/textfold`).

- [ ] **Step 5: Commit**

```bash
git add internal/textfold/fold.go internal/textfold/fold_test.go
git commit -m "feat(textfold): add Greek↔Latin lookalike folding

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Apply Fold in `internal/infra/csv/repository.go`

**Files:**
- Modify: `internal/infra/csv/repository.go` (import block; `isVISAHeader` line 36; `parseVISARow` line 191)
- Test: `internal/infra/csv/repository_test.go`

**Interfaces:**
- Consumes: `textfold.Fold(string) string` from Task 1.
- Produces: no new exported symbols; behavior change only.

- [ ] **Step 1: Write the failing regression tests**

Add to `internal/infra/csv/repository_test.go`. Two tests: one proves folding on a token that carries a real uppercase Greek↔Latin twin (`Κ` U+039A vs `K` U+004B); one is a regression guard that the pending exact-match still fires after folding is wired in.

```go
func TestIsVISAHeaderToleratesLookalikes(t *testing.T) {
	// "Κατηγορία δαπάνης" typed with a Latin 'K' (U+004B) in place of the
	// Greek 'Κ' (U+039A). Both fold to the same form, so it is still detected.
	rec := []string{"Ημ/νία συναλλαγής", "Αιτιολογία", "Kατηγορία δαπάνης"}
	if !isVISAHeader(rec) {
		t.Errorf("VISA header with a Latin-lookalike leading letter should be detected")
	}
}

func TestParseVISARowPendingStillFlags(t *testing.T) {
	// Regression guard: after folding is wired in, the canonical pending
	// status must still flag the description with a trailing " *".
	rec := []string{
		"01/02/2026 10:00", // date
		"COOP PURCHASE",    // description
		"Supermarket",      // category (col 2)
		"",                 // col 3 (unused by parseVISARow)
		"-12,50",           // amount (negative -> expense kept)
		"Σε επεξεργασία",   // pending status (col 5)
	}
	got, ok := parseVISARow(rec, "visa.csv", 2)
	if !ok {
		t.Fatalf("expected VISA row to parse")
	}
	if !strings.HasSuffix(got.Description, " *") {
		t.Errorf("pending row should be flagged with trailing *, got %q", got.Description)
	}
}
```

> Why no lookalike variant for the pending token: `Σε επεξεργασία` contains no uppercase Greek letter with a Latin twin (its only lowercase twin-bearing letter is `ρ`). The header test carries the real twin (`Κ`/`K`) and is the folding proof; this test only guards that exact matching survives the change. Ensure `strings` is imported in the test file (it is used by the existing tests).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/csv/... -run 'TestIsVISAHeaderToleratesLookalikes|TestParseVISARowPendingWithLookalike' -v`
Expected: `TestIsVISAHeaderToleratesLookalikes` FAILS ("should be detected") because `isVISAHeader` still does a byte-exact compare and the Latin `K` does not equal Greek `Κ`.

- [ ] **Step 3: Apply the folding**

Add the import to `internal/infra/csv/repository.go` (in the existing import block, after the transaction import):

```go
	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/lpatouchas/cashflow-report/internal/textfold"
```

Change `isVISAHeader` (line 36) from:

```go
		if strings.TrimSpace(rec[i]) != w {
```

to:

```go
		if textfold.Fold(strings.TrimSpace(rec[i])) != textfold.Fold(w) {
```

Change the pending check in `parseVISARow` (line 191) from:

```go
	if strings.TrimSpace(rec[5]) == visaStatusPending {
```

to:

```go
	if textfold.Fold(strings.TrimSpace(rec[5])) == textfold.Fold(visaStatusPending) {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/infra/csv/...`
Expected: PASS (all existing tests plus the two new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/infra/csv/repository.go internal/infra/csv/repository_test.go
git commit -m "feat(csv): fold Greek↔Latin lookalikes in VISA header and status checks

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Apply Fold in `internal/domain/transaction/transaction.go`

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (import block; `CompileRule` lines 160 & 163; `descriptionMatches` line 209)
- Test: `internal/domain/transaction/transaction_test.go`

**Interfaces:**
- Consumes: `textfold.Fold(string) string` from Task 1.
- Produces: no new exported symbols; behavior change only. `CompileRule`, `descriptionMatches`, `ReconcileVISA` semantics unchanged except lookalike tolerance.

- [ ] **Step 1: Write the failing regression tests**

Add to `internal/domain/transaction/transaction_test.go`:

```go
func TestCompileRuleContainsToleratesLookalike(t *testing.T) {
	// Rule typed with Latin "VISA"; transaction description carries Greek
	// lookalikes "VΙSΑ" (Greek Ι, Α). Contains match must still fire.
	spec := RuleSpec{MatchMode: MatchContains, Description: "VISA"}
	rule := CompileRule(spec)
	txn := Transaction{Description: "MONTHLY VΙSΑ PAYMENT"}
	if !rule(txn) {
		t.Errorf("contains rule should match across Greek↔Latin lookalikes")
	}
}

func TestCompileRuleExactToleratesLookalike(t *testing.T) {
	spec := RuleSpec{MatchMode: MatchExact, Description: "VISA"}
	rule := CompileRule(spec)
	txn := Transaction{Description: "VΙSΑ"} // Greek Ι, Α
	if !rule(txn) {
		t.Errorf("exact rule should match across Greek↔Latin lookalikes")
	}
}

func TestCompileRuleStillRejectsDifferentText(t *testing.T) {
	// Folding must not create false matches: digit 0 vs letter O stays distinct.
	spec := RuleSpec{MatchMode: MatchExact, Description: "COOP"}
	rule := CompileRule(spec)
	if rule(Transaction{Description: "CO0P"}) {
		t.Errorf("exact rule must not match CO0P against COOP")
	}
}

func TestReconcileDescriptionMatchesLookalike(t *testing.T) {
	// Consolidation config typed with Latin "VISA"; bank lump description
	// carries Greek lookalikes. The lump must be consolidated.
	cfg := ReconcileConfig{Description: "VISA", MatchMode: MatchContains, Branch: "HQ"}
	lump := Transaction{Description: "VΙSΑ CARD", Branch: "HQ", Amount: 100, IsDebit: true, SourceFile: "bank.csv"}
	purchase := Transaction{Description: "SHOP", Amount: 40, IsDebit: true, IsVISA: true, Date: lump.Date}
	out := ReconcileVISA([]Transaction{lump, purchase}, cfg)
	for _, txn := range out {
		if txn.Description == "VΙSΑ CARD" {
			t.Errorf("VISA lump with lookalike description should have been consolidated, not passed through")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/transaction/... -run 'Lookalike' -v`
Expected: `TestCompileRuleContainsToleratesLookalike`, `TestCompileRuleExactToleratesLookalike`, and `TestReconcileDescriptionMatchesLookalike` FAIL (byte-exact compares don't match Greek vs Latin). `TestCompileRuleStillRejectsDifferentText` passes already (guard).

- [ ] **Step 3: Apply the folding**

Add the import to `internal/domain/transaction/transaction.go` (existing import block):

```go
	"time"

	"github.com/lpatouchas/cashflow-report/internal/textfold"
```

Change `CompileRule` (lines 159-165) from:

```go
		if s.MatchMode == MatchContains {
			if !strings.Contains(t.Description, s.Description) {
				return false
			}
		} else if t.Description != s.Description {
			return false
		}
```

to:

```go
		if s.MatchMode == MatchContains {
			if !strings.Contains(textfold.Fold(t.Description), textfold.Fold(s.Description)) {
				return false
			}
		} else if textfold.Fold(t.Description) != textfold.Fold(s.Description) {
			return false
		}
```

Change `descriptionMatches` (lines 207-212) from:

```go
func (c ReconcileConfig) descriptionMatches(desc string) bool {
	if c.MatchMode == MatchContains {
		return strings.Contains(desc, c.Description)
	}
	return desc == c.Description
}
```

to:

```go
func (c ReconcileConfig) descriptionMatches(desc string) bool {
	if c.MatchMode == MatchContains {
		return strings.Contains(textfold.Fold(desc), textfold.Fold(c.Description))
	}
	return textfold.Fold(desc) == textfold.Fold(c.Description)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/transaction/...`
Expected: PASS (all existing tests plus the four new ones).

- [ ] **Step 5: Run the full suite and commit**

Run: `go test ./...`
Expected: PASS across all packages.

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat(transaction): fold Greek↔Latin lookalikes in rule and reconcile matching

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- New `internal/textfold` package with `Fold` — Task 1. ✅
- Fold table (uppercase + finalized lowercase) — Task 1, Global Constraints. ✅
- Site 1 `repository.go:36` VISA header — Task 2. ✅
- Site 2 `repository.go:191` pending status — Task 2. ✅
- Site 3 `transaction.go:160` exclusion contains — Task 3. ✅
- Site 4 `transaction.go:163` exclusion exact — Task 3. ✅
- Site 5 `transaction.go:209` consolidation contains — Task 3. ✅
- Non-goals (no NFC, no x/text, case-preserving, no exotic scripts) — Global Constraints + fold table. ✅
- Unit tests (canonical case, mixed name, digits, case, greek-only, idempotence) — Task 1 tests. ✅
- Regression tests at each site — Tasks 2 & 3. ✅
- Import direction / no cycle — Global Constraints; `textfold` imports only `strings`. ✅

**Placeholder scan:** No TBD/TODO. The one explanatory note in Task 2 Step 1 documents why the pending test has no lookalike variant (the token has no uppercase twin) — it is rationale, not a deferral. All code steps show complete code.

**Type consistency:** `Fold(string) string` is referenced identically in Tasks 2 and 3. Import path `github.com/lpatouchas/cashflow-report/internal/textfold` consistent throughout. Existing signatures (`isVISAHeader`, `parseVISARow`, `CompileRule`, `descriptionMatches`, `ReconcileVISA`, `RuleSpec`, `ReconcileConfig`, `Transaction`) match the current source.

**Open decision for user review:** lowercase fold table excludes `ν/v`, `υ/y`, `κ/k` (Global Constraints). Flagged; safe to add later.