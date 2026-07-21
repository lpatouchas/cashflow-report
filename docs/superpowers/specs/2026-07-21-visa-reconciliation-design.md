# VISA Reconciliation — Design

**Date:** 2026-07-21
**Status:** Approved

## Problem

The bank CSV exports contain lump-sum debits that pay off a credit card:
`Αιτιολογία == "ΠΛΗΡΩΜΗ VΙSA"` with `Κατάστημα == 96`. A single line such as
`ΠΛΗΡΩΜΗ VΙSA 1000,00` hides all the individual card purchases behind it, so
the report shows *how much* went to the card but not *what it was spent on*.

The goal: replace each month's `ΠΛΗΡΩΜΗ VΙSA` lump with the itemized card
purchases from a separately-exported VISA statement, reconciling any difference
into a single `VISA LEFTOVERS` line so the month's net figure is preserved.

## VISA CSV format

Semicolon-separated, six columns, comma-decimal, signed amounts:

```
Ημ/νία συναλλαγής;Αιτιολογία;Κατηγορία δαπάνης;Είδος συναλλαγής;Ποσό (EUR);Κατάσταση συναλλαγής
21/07/2026 11:27;EVERYPAY*SKROUTZ;Λοιπές δαπάνες και πληρωμές;Αγορά;-22,19;Σε επεξεργασία
20/07/2026 11:07;PAYMENT EBANKING;Αφορά ηλεκτρονικές μεταφορές/πληρωμές;Πληρωμή Κάρτας;768,60;Σε επεξεργασία
19/07/2026 21:38;E GIANNAROU K SIA;Λοιπές δαπάνες και πληρωμές;Αγορά;-8,90;Σε επεξεργασία
18/07/2026 10:42;EFOOD;Supermarket / Διατροφή;Αγορά;-5,80;Εκτελεσμένη
13/07/2026 10:58;PAYMENT EBANKING;Αφορά ηλεκτρονικές μεταφορές/πληρωμές;Πληρωμή Κάρτας;411,19;Εκτελεσμένη
```

Columns: `[0]` datetime `DD/MM/YYYY HH:MM`, `[1]` description, `[2]` category,
`[3]` transaction type, `[4]` signed amount, `[5]` status.

- **Negative amounts = expenses** (real purchases). Only these are kept.
- **Positive amounts** are card payments (the mirror of the bank lump) and are
  **skipped entirely** — keeping them would double-count.
- The settled/pending status is not used for filtering. When status is
  `Σε επεξεργασία` (pending), a ` *` marker is appended to the description.

## Reconciliation model

- **Grouping:** per-month, by each purchase's own transaction date.
- **Scope:** for each month, reconcile that month's itemized purchases against
  that month's `ΠΛΗΡΩΜΗ VΙSA` lump(s).
- **Invariant:** for every month, `purchaseSum + leftover == paymentSum`, so the
  month's net VISA outflow always equals what actually left the bank. Over the
  full report span the leftovers net toward zero as purchases get paid off.

## Architecture (Approach A — domain-centric)

Pipeline:

```
GetAll
  → partition into bank vs VISA (by IsVISA)
  → FilterTransfers(bank only)
  → ReconcileVISA(bank + VISA, cfg)
  → ApplyExclusions
  → Summarize
```

VISA rows **bypass `FilterTransfers`**: that filter targets bank inter-account
transfers and duplicate export rows, concepts that do not apply to card
purchases (no bank IDs, no transfer legs). This also removes any dependency on
VISA-ID uniqueness for correctness.

### Data model

`transaction.Transaction` gains two fields:

```go
Branch string // Κατάστημα (bank col 3); "" for VISA rows
IsVISA bool   // true for rows parsed from a VISA file
```

### VISA parsing (csv repository)

- **Header auto-detection:** read each file's first row. If it matches the VISA
  header (leading columns `Ημ/νία συναλλαγής;Αιτιολογία;Κατηγορία δαπάνης;…`,
  tolerant of trailing whitespace), parse as VISA; otherwise use the existing
  8-column bank parser.
- **Bank parser change:** capture `rec[3]` into `Branch`; no other change.
- **VISA row mapping:**
  - Date: parse the `DD/MM/YYYY` part of `rec[0]` (drop the time), reusing
    `parseDate`.
  - Amount `rec[4]`: only negative amounts kept → `IsDebit=true`,
    `Amount=abs`. Positive rows skipped.
  - Description `rec[1]`: if status `rec[5] == "Σε επεξεργασία"`, append ` *`.
  - `ID`: synthetic, `"VISA-" + rec[0] + "-" + rec[1]` (datetime + description),
    for traceability/display only.
  - `IsVISA=true`; `SourceFile` = VISA filename (transient; re-tagged during
    reconciliation).

### `ReconcileVISA(txns []Transaction, cfg ReconcileConfig) []Transaction`

Pure function, run after `FilterTransfers`.

**Step 1 — Identify the paying account (once, globally).** Scan bank rows
(`!IsVISA`) matching the lump: `Αιτιολογία` matches `cfg.Description` per
`cfg.MatchMode` **AND** `Branch == cfg.Branch`. The `SourceFile` carrying these
is the VISA paying account.
- Multiple bank files carry lumps → pick the largest total, `slog.Warn` the rest.
- No bank match but VISA purchases exist → fall back to label `"VISA"`,
  `slog.Warn`.

**Partial-match logging.** Branch 96 is *probably* exclusive to these card
payments, but not guaranteed, and the mixed-script description can drift. So any
bank row that satisfies exactly **one** of the two criteria is `slog.Warn`ed
with its date, description, branch, and amount, and is **not** treated as a lump:
- `Branch == cfg.Branch` but description does not match → possible VISA payment
  we are failing to reconcile (homoglyph drift), or an unrelated branch-96 row.
- description matches but `Branch != cfg.Branch` → unexpected branch for a VISA
  payment.

This surfaces both homoglyph drift and any reuse of branch 96, without silently
mis-classifying rows.

**Step 2 — Partition & remove.** Remove matched lump rows. Pull out VISA
purchases (`IsVISA`) and re-tag each `SourceFile` = paying account. Bank
non-lump rows pass through untouched.

**Step 3 — Per-month reconciliation.** For each month with a lump or a purchase:
- `paymentSum` = Σ removed lumps dated that month.
- `purchaseSum` = Σ VISA purchases dated that month.
- `leftover = paymentSum − purchaseSum` (computed in cents via `amountCents`).
- Emit the itemized purchases (re-tagged) plus, when `leftover != 0`, a
  `VISA LEFTOVERS` row:
  - `leftover > 0` → `IsDebit=true` (expense remainder).
  - `leftover < 0` → `IsDebit=false`, `Amount=|leftover|` (credit).
  - `SourceFile` = paying account; `Date` = last lump date that month, or the
    month's last day when `paymentSum == 0`; `Description` = `"VISA LEFTOVERS"`;
    synthetic ID.

## Configuration

The lump matcher lives in the existing exclusion-rules JSON. The file moves to
an object shape only (no backward-compatible array fallback):

```json
{
  "exclusions": [ ... ],
  "visaReconcile": {
    "description": "ΠΛΗΡΩΜΗ VΙSA",
    "matchMode": "exact",
    "branch": "96"
  }
}
```

`ReconcileConfig{ Description, MatchMode, Branch string }`. When `visaReconcile`
is absent, reconciliation is a no-op and the app behaves as today. Matching:
`Αιτιολογία` per `matchMode` (default `exact`) AND `Branch == branch`. Both
criteria are kept (branch 96 is probably exclusive but not guaranteed); rows
matching only one are logged, not classified (see Step 1). The existing
`exclusion-rules.json` is migrated to the new shape.

**Exact description bytes.** The bank string is mixed-script and must be stored
byte-for-byte. Confirmed codepoints from a real export:

```
Π Λ Η Ρ Ω Μ Η  ·  V(U+0056,Latin) Ι(U+0399,Greek) S(U+0053,Latin) Α(U+0391,Greek)
U+03A0 U+039B U+0397 U+03A1 U+03A9 U+039C U+0397  U+0020  U+0056 U+0399 U+0053 U+0391
```

The `="…"` spreadsheet wrapper is stripped by the existing `unwrap()` before
matching, exactly as for the bank parser today.

## Rendering

No renderer changes required. Itemized purchases and `VISA LEFTOVERS` are
ordinary `Transaction`s, so they flow into the month detail modal
(Date/Desc/Amount/Source) and per-account totals automatically, under the
paying account's label. The pending `*` rides along in the description. Monthly
totals, the trend chart, and averages recompute naturally because the lumps are
replaced by equal-net detail.

## Edge cases

1. **Lump but no purchases that month** (payment settles a prior month):
   `leftover = paymentSum` → whole amount shows as one `VISA LEFTOVERS` debit.
2. **Purchases but no lump that month** (bought, not yet paid):
   `leftover = −purchaseSum` (credit); net month VISA expense = 0, correct for a
   cashflow report. The offsetting amount surfaces when the payment posts.
3. **Purchases exceed that month's payment** → negative leftover (credit), same
   handling as #2.
4. **VISA file present, no matching bank lump anywhere** → attribute purchases to
   label `"VISA"`, emit leftovers as usual, `slog.Warn`.
5. **Lumps present, no VISA file at all** → leave lumps untouched (current
   behavior). Stripping would delete real expenses with nothing to replace them.
6. **Positive-amount VISA rows that are not card payments** (e.g. a refund) are
   skipped under the negative-only rule. Known limitation; refund handling is
   out of scope (YAGNI).
7. **Rounding:** leftover computed in cents; a zero leftover emits no row.

## Testing

Table-driven, pure-function style matching the existing suite.

- **VISA parser:** header auto-detection vs bank; negative→expense; positive
  skipped; datetime parsing; pending `*`; synthetic ID format.
- **`ReconcileVISA`:** paying-account identification (single / multiple→largest
  +warn / none→`"VISA"`); per-month invariant across edge cases §1–§4; leftover
  sign; zero-leftover emits no row; cent rounding; lumps + no VISA file
  untouched (§5); branch gating (right description, wrong branch → no match);
  partial match (branch-only or description-only → not classified, warning
  emitted); multiple lumps in one month (e.g. the three 09/07 rows) sum
  correctly.
- **`config.Load`:** new object shape parses; `visaReconcile` absent → no-op.
- **Pipeline/service:** VISA rows bypass `FilterTransfers`; end-to-end ordering
  yields the expected summary.
- **Regression:** existing bank-only tests pass (Branch captured but inert).

## Sample data & docs

Add a small fictional VISA CSV to `sample-data/` and a `ΠΛΗΡΩΜΗ VΙSA` lump
(branch 96) to `sample-data/checking.csv` so the feature is demonstrable. Update
the README CSV-format section to document the VISA format and reconciliation.