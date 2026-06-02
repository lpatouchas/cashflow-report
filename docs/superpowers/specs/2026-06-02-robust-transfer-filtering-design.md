# Robust transfer / duplicate filtering

**Date:** 2026-06-02
**Status:** Approved
**Area:** `internal/domain/transaction/transaction.go`

## Problem

`FilterTransfers` originally identified inter-account transfers and duplicate
anomalies using the transaction **ID alone**: any ID that appears more than once
across the input is dropped entirely.

Matching on ID alone is fragile. We want pair detection to also confirm the
**amount**, so an ID collision must agree on the money moved before two records
are treated as the same movement.

## Goal

Match on two fields together:

- **ID** — same transaction identifier. IDs are unique per movement: the two legs
  of one transfer share a single ID, an accidental re-export duplicates it, and
  unrelated transactions never share one.
- **Amount** — same value, compared to the whole cent.

Direction (`IsDebit`) then only *classifies* a detected collision; it does not
change whether the records are dropped.

### Why the date is deliberately excluded

A bank can record the two legs of one transfer on **different days** (initiated
one day, posts/clears the next). Both legs still carry the same unique ID. If the
calendar day were part of the match key, those two legs would fall into separate
groups and the transfer would be **missed**. Because IDs are unique, the date
adds no precision — it can only cause that miss — so it is left out of the key.

## Decisions

| Question | Decision |
| --- | --- |
| Same ID + amount, **same** direction (exact duplicate) | **Drop both.** Comment both logics clearly for the next developer. |
| Date in the match key | **Excluded.** IDs are unique; including the date would miss transfers whose legs post on different days. |
| Amount comparison | **Round to whole cents** before comparing (robust currency match). |

### What counts as an excludable collision

Two (or more) transactions sharing the same `(ID, amount-in-cents)` form a
collision and are **all excluded**. The collision is one of two kinds, which
differ only in `IsDebit`:

- **Inter-account transfer** — exactly two legs with **opposite** direction
  (one debit, one credit). The money leaves one account and enters another,
  possibly posting on different days.
- **Duplicate anomaly** — two or more records with the **same** direction. A
  repeated export row.

Both are dropped. Direction is used only to explain *why* in code comments, not
to gate the drop decision.

## Design

### Signature

No change: `func FilterTransfers(txns []Transaction) []Transaction`. Callers
(`internal/app/report/service.go`) and the renderer are untouched.

### Matching key

Replace the ID-only count map with a composite key:

```go
type matchKey struct {
    id    string
    cents int64 // amount rounded to whole cents
}
```

### Helpers

- `amountCents(f float64) int64` → `int64(math.Round(f * 100))`. Rounds to whole
  cents so float representation noise (e.g. `100.00` vs `100.001`) collapses to
  the same key.
- `keyOf(t Transaction) matchKey` builds `{id, cents}` for a transaction.

### Algorithm

Same two-pass shape as before, only the key changes:

1. First pass — count occurrences of each `matchKey`.
2. Second pass — keep transactions whose key count is exactly 1; drop the rest.

### Documentation

A single doc comment on `FilterTransfers` explains that a key collision is one of
two things — an inter-account transfer (opposite directions) or a duplicate
anomaly (same direction) — and that both are excluded.

## Behavior changes

- Transactions sharing **only** an ID (different amount) are now **kept**.
- Genuine inter-account transfers (ID + amount match, opposite direction) are
  still **dropped**, including when the two legs post on different days.
- Exact duplicates (ID + amount match, same direction) are still **dropped**.

Existing `TestFilterTransfers` cases ("cross-file transfer is excluded",
"single-file duplicate is excluded", "all unique are kept", "empty input")
continue to pass.

## Testing

`TestFilterTransfers` covers:

- Same ID, **different amount** → both kept.
- Same ID, same amount, **different date** → excluded (date is not in the key).
- Amount differing only by float noise (`100.00` vs `100.001`) → rounds to same
  cents → still paired and dropped.
- (Retain) cross-file opposite-direction transfer → excluded.
- (Retain) single-file same-direction duplicate → excluded.

## Worktree isolation

All implementation work happens in a dedicated git worktree, not the main
checkout. A throwaway branch (`robust-transfer-filtering`) is created in its own
worktree directory; the change is built and tested there in isolation, and the
worktree is cleaned up after the work is merged/abandoned. The main working
directory stays untouched during development.

## Out of scope

- Date tolerance windows / fuzzy matching beyond the exact-ID + amount key.
- Per-category logging ("N transfers, M duplicates") in the service layer.
- Any change to `ApplyExclusions`, `Summarize`, or the rule system.
