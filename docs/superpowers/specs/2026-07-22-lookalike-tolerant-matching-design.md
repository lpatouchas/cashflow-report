# Lookalike-tolerant text matching (Greek↔Latin homoglyphs)

**Date:** 2026-07-22
**Status:** Design — approved for planning

## Problem

Greek CSV exports and hand-typed configuration rules can contain characters
that look identical on screen but are different Unicode codepoints. The
canonical example: `VΙSΑ` (Latin V, **Greek** Ι U+0399, Latin S, **Greek** Α
U+0391) renders identically to `VISA` (all Latin) but is not byte-equal.

Because the codebase compares strings by raw bytes, any header, status value,
or exclusion/consolidation rule can silently fail to match when the export or
the rule carries a Greek letter where a Latin lookalike was expected (or vice
versa). This is the same class of defect as the leading-BOM bug fixed in
`d4c5fbc`: text that looks right but does not compare equal.

Five comparison sites are affected:

| Site | Comparison | Consequence if it mis-matches |
|------|------------|-------------------------------|
| `internal/infra/csv/repository.go:36` | VISA header columns | VISA file mis-classified as a bank export |
| `internal/infra/csv/repository.go:191` | `Σε επεξεργασία` pending status | pending VISA row treated as settled |
| `internal/domain/transaction/transaction.go:160` | exclusion rule, `contains` | exclusion silently stops matching |
| `internal/domain/transaction/transaction.go:163` | exclusion rule, `exact` | exclusion silently stops matching |
| `internal/domain/transaction/transaction.go:209` | consolidation rule, `contains` | VISA lump not consolidated |

## Goal

A single, shared, dependency-free helper that makes all five sites tolerant of
Greek↔Latin lookalike characters, without introducing false merges of text
that is genuinely different.

## Non-goals

- **Cyrillic, full-width, and other exotic scripts.** The data is Greek + Latin
  only. The full Unicode UTS-39 confusables table was considered and rejected:
  it folds digits and letter clusters (`0↔O`, `1↔l`, `rn↔m`), which would risk
  wrongly merging distinct descriptions, and it requires embedding/​maintaining
  an external data table.
- **Case-insensitive matching.** Folding is case-preserving. `COOP` must not
  match `coop`. Only lookalikes are newly equated; nothing about case changes.
- **NFC accent normalization.** True NFC requires `golang.org/x/text`, which
  would be the project's first runtime dependency. Real Greek bank/VISA exports
  emit precomposed (NFC) text already, so the homoglyph map alone solves every
  case observed. If a decomposed export ever appears, adding `norm.NFC` is a
  localized one-line change at that point.
- **Branch matching in `ReconcileVISA` (`transaction.go:282`).** The
  `Κατάστημα` column header is Greek, but its *values* are numeric branch codes
  (e.g. `99`, `12`, `81`). Digits have no Greek↔Latin homoglyph, so folding the
  `branchMatch` comparison would be a strict no-op — it stays byte-exact. If
  branch codes ever become alphabetic Greek text, this becomes a sixth fold
  site (the change is one line and non-breaking).

## Design

### New package `internal/textfold`

A small, dependency-free package exposing one function:

```go
// Fold returns a canonical form of s for lookalike-tolerant comparison.
// It maps each Greek letter that has a visually identical Latin counterpart
// to that Latin letter, case-preserving (Α→A, α→a, Ο→O, ο→o, …). Digits and
// letter case are left untouched, so Fold never merges genuinely different
// text. Two strings that look identical to a human are equal after Fold.
func Fold(s string) string
```

Implementation:

- A package-level `map[rune]rune` (the fold table) listing only the Greek
  letters with an identical-looking Latin twin, in both cases:

  | Uppercase | Lowercase |
  |-----------|-----------|
  | Α→A Β→B Ε→E Ζ→Z Η→H Ι→I Κ→K Μ→M Ν→N Ο→O Ρ→P Τ→T Υ→Y Χ→X | ο→o ρ→p υ→y χ→x |

  (Lowercase Greek letters mostly do **not** look like their Latin sound-alikes
  — `α` vs `a`, `β` vs `b` differ visibly — so only the genuinely
  indistinguishable lowercase pairs are included: ο/o, ρ/p, υ/y, χ/x. The exact
  table is finalized in the plan; the rule is "include a pair only if the two
  glyphs are visually indistinguishable in common fonts.")

- Greek-only letters with no Latin twin (λ, δ, ψ, ω, …) pass through unchanged.
- A single `strings.Map(fold, s)` — O(n), one result allocation.

### Applying Fold at the five sites

The rule everywhere: **fold both operands, then compare as before.**

- `repository.go:36` — `textfold.Fold(strings.TrimSpace(rec[i])) != textfold.Fold(w)`
- `repository.go:191` — fold both sides of the `visaStatusPending` compare
- `transaction.go:160` — `strings.Contains(textfold.Fold(t.Description), textfold.Fold(s.Description))`
- `transaction.go:163` — fold both sides of the exact `!=` compare
- `transaction.go:209` — `strings.Contains(textfold.Fold(desc), textfold.Fold(c.Description))`

For the `contains` sites, folding both operands preserves substring semantics:
a folded needle still matches inside a folded haystack.

### Import direction

`internal/infra/csv` already imports `internal/domain/transaction`, and both
will import `internal/textfold`. `textfold` imports nothing from the project, so
there is no import cycle.

### Performance

Rules are few and descriptions are short. Folding runs per comparison; the cost
is a single linear pass over strings that are already being scanned by
`Contains`/`==`. No precomputation or caching is warranted at this scale. (If
profiling ever shows rule matching to be hot, the rule side of each compare can
be folded once at `CompileRule`/`ReconcileConfig` construction — noted, not
implemented.)

## Testing

`internal/textfold/fold_test.go` — table-driven unit tests:

- `Fold("VΙSΑ") == Fold("VISA")` (the canonical case)
- mixed Greek/Latin merchant name folds to a stable Latin form
- digits are not folded: `Fold("CO0P") != Fold("COOP")`
- case is preserved: `Fold("COOP") != Fold("coop")`
- pure-Greek words with no Latin twins stay distinct from each other
- idempotence: `Fold(Fold(s)) == Fold(s)`

Regression tests at each patched site:

- a VISA header whose columns carry Greek lookalike letters is still detected
  as a VISA header
- a pending-status row written with a lookalike still counts as pending
- an exclusion rule with a homoglyph still matches the intended transaction
  (both `exact` and `contains`)
- a consolidation rule with a homoglyph still consolidates the VISA lump

## Files touched

- **new** `internal/textfold/fold.go`
- **new** `internal/textfold/fold_test.go`
- `internal/infra/csv/repository.go` (2 sites + import)
- `internal/domain/transaction/transaction.go` (3 sites + import)
- existing test files at both sites gain regression cases