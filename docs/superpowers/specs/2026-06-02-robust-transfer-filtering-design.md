# Robust transfer / duplicate filtering

**Date:** 2026-06-02
**Status:** Approved
**Area:** `internal/domain/transaction/transaction.go`

## Problem

`FilterTransfers` currently identifies inter-account transfers and duplicate
anomalies using the transaction **ID alone**: any ID that appears more than once
across the input is dropped entirely.

This is too aggressive. Bank CSV exports can reuse a transaction ID for unrelated
movements (across accounts or over time). When that happens, two legitimate,
unrelated transactions are wrongly discarded simply because they share an ID.

## Goal

Make pair detection more precise by matching on three fields together:

- **ID** — same transaction identifier.
- **Amount** — same value, compared to the whole cent.
- **Date** — same calendar day.

Direction (`IsDebit`) then only *classifies* a detected collision; it does not
change whether the records are dropped.

## Decisions

| Question | Decision |
| --- | --- |
| Same ID + amount + date, **same** direction (exact duplicate) | **Drop both.** Comment both logics clearly for the next developer. |
| Date match precision | **Exact same calendar day.** |
| Amount comparison | **Round to whole cents** before comparing (robust currency match). |

### What counts as an excludable collision

Two (or more) transactions sharing the same `(ID, amount-in-cents, calendar-day)`
form a collision and are **all excluded**. The collision is one of two kinds,
which differ only in `IsDebit`:

- **Inter-account transfer** — exactly two legs with **opposite** direction
  (one debit, one credit). The money leaves one account and enters another.
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
    day   int64 // calendar day (built from Year/Month/Day, UTC)
}
```

### Helpers

- `amountCents(f float64) int64` → `int64(math.Round(f * 100))`. Rounds to whole
  cents so float representation noise (e.g. `100.00` vs `100.001`) collapses to
  the same key.
- Day component is built from the transaction's own `Date.Year()`,
  `Date.Month()`, `Date.Day()` via `time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()`.
  This avoids timezone/time-of-day surprises and gives an exact calendar-day match.

### Algorithm

Same two-pass shape as today, only the key changes:

1. First pass — count occurrences of each `matchKey`.
2. Second pass — keep transactions whose key count is exactly 1; drop the rest.

### Documentation

A single doc comment on `FilterTransfers` explains that a key collision is one of
two things — an inter-account transfer (opposite directions) or a duplicate
anomaly (same direction) — and that both are excluded.

## Behavior changes

- Transactions sharing **only** an ID (different amount **or** different date) are
  now **kept**. This is the robustness fix.
- Genuine inter-account transfers (ID + amount + date match, opposite direction)
  are still **dropped**.
- Exact duplicates (ID + amount + date match, same direction) are still
  **dropped**.

Existing `TestFilterTransfers` cases ("cross-file transfer is excluded",
"single-file duplicate is excluded", "all unique are kept", "empty input")
continue to pass.

## Testing

Extend `TestFilterTransfers` with:

- Same ID, **different amount** → both kept (new behavior).
- Same ID, **different date** → both kept (new behavior).
- Amount differing only by float noise (`100.00` vs `100.001`) → rounds to same
  cents → still paired and dropped.
- (Retain) cross-file opposite-direction transfer → excluded.
- (Retain) single-file same-direction duplicate → excluded.

## Out of scope

- Date tolerance windows (settle-next-day matching).
- Per-category logging ("N transfers, M duplicates") in the service layer.
- Any change to `ApplyExclusions`, `Summarize`, or the rule system.
