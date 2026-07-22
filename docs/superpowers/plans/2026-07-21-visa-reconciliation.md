# VISA Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace each month's `ΠΛΗΡΩΜΗ VΙSA` lump-sum bank debit with the itemized VISA-statement purchases behind it, reconciling any difference into a single `VISA LEFTOVERS` line so every month's net VISA outflow is preserved.

**Architecture:** VISA CSV files are auto-detected by header and parsed into ordinary `transaction.Transaction`s tagged `IsVISA=true`. A new pure function `ReconcileVISA` runs in the report pipeline *after* transfer filtering: it finds the paying bank account, removes the matched lump rows, re-tags the itemized purchases onto that account, and emits a per-month `VISA LEFTOVERS` row. The lump matcher is configured via a new object-shaped exclusion-rules JSON.

**Tech Stack:** Go 1.23, standard library (`encoding/csv`, `encoding/json`, `log/slog`, `time`, `sort`), `github.com/stretchr/testify/require` + `/mock` for tests. Hexagonal layout: `internal/domain/transaction` (pure logic), `internal/infra/csv` (parsing), `internal/infra/config` (JSON), `internal/app/report` (pipeline), `internal/infra/web` (UI).

## Global Constraints

- **Module path:** `github.com/lpatouchas/cashflow-report` — use this prefix for all internal imports.
- **Go version:** 1.23 (`go.mod`). No new third-party dependencies.
- **Test style:** table-driven / subtest (`t.Run`) style, `require` assertions, matching the existing suite. Money compared to the cent (`require.InDelta(..., 0.001)` for floats, or exact via `amountCents`).
- **Amounts:** `Transaction.Amount` is always **positive**; direction is carried by `IsDebit` (debit = expense). Reconciliation arithmetic is done in **integer cents** via the existing unexported `amountCents(float64) int64`.
- **Exact lump description bytes (mixed Greek/Latin — do NOT retype by hand):** the string `ΠΛΗΡΩΜΗ VΙSA` is codepoints
  `U+03A0 U+039B U+0397 U+03A1 U+03A9 U+039C U+0397  U+0020  U+0056(Latin V) U+0399(Greek Ι) U+0053(Latin S) U+0391(Greek Α)`
  i.e. UTF-8 bytes `ce a0 ce 9b ce 97 ce a1 ce a9 ce 9c ce 97 20 56 ce 99 53 ce 91`.
  In **Go source** always express it via escapes: `"ΠΛΗΡΩΜΗ VΙSΑ"`.
  In **shell** generate it via `printf 'ΠΛΗΡΩΜΗ VΙSΑ'` (zsh/bash 4+).
  In **JSON** write it via `\u` escapes (see Task 5).
- **Full suite must stay green:** run `go test ./...` at the end of every task. Existing bank-only behavior is a regression surface.
- **Commits:** commit at the end of each task (and at each `Commit` step). Do **not** open a PR — the user pushes and opens PRs themselves.

---

## File Structure

| File | Responsibility | Tasks |
| --- | --- | --- |
| `internal/domain/transaction/transaction.go` | `Transaction` gains `Branch`, `IsVISA`; new `ReconcileConfig` type + `ReconcileVISA` function | 1, 4 |
| `internal/domain/transaction/transaction_test.go` | `ReconcileConfig.Validate` + `ReconcileVISA` tests | 1, 4 |
| `internal/infra/csv/repository.go` | Bank parser captures `Branch`; VISA header auto-detection + `parseVISARow` | 2, 3 |
| `internal/infra/csv/repository_test.go` | Branch capture + VISA parsing tests | 2, 3 |
| `internal/infra/config/config.go` | Object-shaped `File{Exclusions, VisaReconcile}`, `Load`/`Save` | 5 |
| `internal/infra/config/config_test.go` | Rewritten for object shape + `visaReconcile` | 5 |
| `exclusion-rules.json` | Migrated to object shape with a `visaReconcile` block | 5 |
| `internal/app/report/service.go` | Pipeline: partition, VISA bypasses `FilterTransfers`, `ReconcileVISA` | 6 |
| `internal/app/report/service_test.go` | Updated `NewService` calls + bypass test | 6 |
| `main.go`, `internal/infra/web/server.go` | Adapt call sites to the new config shape + service signature | 5, 6 |
| `sample-data/visa.csv`, `sample-data/checking.csv`, `README.md` | Demonstrable sample data + docs | 7 |

---

## Task 1: Transaction model fields + `ReconcileConfig` type

**Files:**
- Modify: `internal/domain/transaction/transaction.go`
- Test: `internal/domain/transaction/transaction_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `Transaction.Branch string`, `Transaction.IsVISA bool` (new struct fields).
  - `type ReconcileConfig struct { Description string; MatchMode MatchMode; Branch string }` with JSON tags `description`/`matchMode`/`branch`.
  - `(ReconcileConfig).Validate() error` — Description required; MatchMode must be `""`, `MatchExact`, or `MatchContains`.
  - `(ReconcileConfig).descriptionMatches(desc string) bool` (unexported; used by Task 4).

- [ ] **Step 1: Add the two fields to `Transaction`**

In `internal/domain/transaction/transaction.go`, extend the struct (keep existing fields, add the two new ones at the end):

```go
// Transaction is a single bank movement loaded from a CSV export.
// Amount is always positive; direction is carried by IsDebit.
type Transaction struct {
	ID          string    // Αρ. συναλλαγής
	Date        time.Time // Ημερομηνία
	Description string    // Αιτιολογία
	Amount      float64   // Ποσό (positive)
	IsDebit     bool      // Πρόσημο ποσού: Χ = true (expense), Π = false (income)
	SourceFile  string    // originating CSV filename
	Branch      string    // Κατάστημα (bank col 3); "" for VISA rows
	IsVISA      bool      // true for rows parsed from a VISA file
}
```

- [ ] **Step 2: Write the failing test for `ReconcileConfig.Validate`**

Append to `internal/domain/transaction/transaction_test.go`:

```go
func TestReconcileConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ReconcileConfig
		wantErr bool
	}{
		{"exact ok", ReconcileConfig{Description: "X", MatchMode: MatchExact, Branch: "96"}, false},
		{"contains ok", ReconcileConfig{Description: "X", MatchMode: MatchContains, Branch: "96"}, false},
		{"empty mode defaults to exact", ReconcileConfig{Description: "X", Branch: "96"}, false},
		{"missing description", ReconcileConfig{MatchMode: MatchExact, Branch: "96"}, true},
		{"blank description", ReconcileConfig{Description: "   ", MatchMode: MatchExact}, true},
		{"unknown mode", ReconcileConfig{Description: "X", MatchMode: "fuzzy"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/domain/transaction/ -run TestReconcileConfigValidate -v`
Expected: compile error / FAIL — `ReconcileConfig` undefined.

- [ ] **Step 4: Implement `ReconcileConfig`, `Validate`, and `descriptionMatches`**

In `internal/domain/transaction/transaction.go`, add after the `RuleSpec`/`CompileRule` block (near the other config types). `errors`, `fmt`, `strings` are already imported.

```go
// ReconcileConfig configures VISA lump reconciliation. A bank row is a VISA
// lump when its Description matches per MatchMode AND its Branch equals Branch.
// An empty MatchMode means exact.
type ReconcileConfig struct {
	Description string    `json:"description"`
	MatchMode   MatchMode `json:"matchMode"`
	Branch      string    `json:"branch"`
}

// Validate reports whether the reconcile config is well-formed.
func (c ReconcileConfig) Validate() error {
	if strings.TrimSpace(c.Description) == "" {
		return errors.New("description is required")
	}
	switch c.MatchMode {
	case "", MatchExact, MatchContains:
		return nil
	default:
		return fmt.Errorf("unknown match mode %q (use %q or %q)", c.MatchMode, MatchExact, MatchContains)
	}
}

// descriptionMatches reports whether desc satisfies the config's description
// rule. An empty MatchMode is treated as exact.
func (c ReconcileConfig) descriptionMatches(desc string) bool {
	if c.MatchMode == MatchContains {
		return strings.Contains(desc, c.Description)
	}
	return desc == c.Description
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/domain/transaction/ -run TestReconcileConfigValidate -v`
Expected: PASS.

- [ ] **Step 6: Run the whole suite (regression — fields inert)**

Run: `go test ./...`
Expected: PASS (existing tests unaffected; `Branch`/`IsVISA` default to zero values).

- [ ] **Step 7: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat(transaction): add Branch/IsVISA fields and ReconcileConfig"
```

---

## Task 2: Bank parser captures `Branch`

**Files:**
- Modify: `internal/infra/csv/repository.go:112-119` (the `parseRow` return)
- Test: `internal/infra/csv/repository_test.go`

**Interfaces:**
- Consumes: `Transaction.Branch` (Task 1).
- Produces: bank rows now carry `Branch = unwrap(rec[3])` (the `Κατάστημα` column).

- [ ] **Step 1: Write the failing test**

Append to `internal/infra/csv/repository_test.go` (inside the file, a new top-level test):

```go
func TestBankBranchCaptured(t *testing.T) {
	dir := t.TempDir()
	body := header + "\n" +
		`1;29/05/2026;="SHOP";96;27/5/2026;="ID1";53,79;Χ;` + "\n"
	writeCSV(t, dir, "acc.csv", body)

	got, err := New(dir).GetAll(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "96", got[0].Branch)
	require.False(t, got[0].IsVISA)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/infra/csv/ -run TestBankBranchCaptured -v`
Expected: FAIL — `got[0].Branch` is `""`.

- [ ] **Step 3: Capture the branch in `parseRow`**

In `internal/infra/csv/repository.go`, change the returned struct in `parseRow` (currently ending at line 119) to set `Branch`:

```go
	return transaction.Transaction{
		ID:          unwrap(rec[5]),
		Date:        date,
		Description: unwrap(rec[2]),
		Amount:      amount,
		IsDebit:     isDebit,
		SourceFile:  file,
		Branch:      unwrap(rec[3]),
	}, true
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/infra/csv/ -run TestBankBranchCaptured -v`
Expected: PASS.

- [ ] **Step 5: Run the whole suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/infra/csv/repository.go internal/infra/csv/repository_test.go
git commit -m "feat(csv): capture bank Κατάστημα into Transaction.Branch"
```

---

## Task 3: VISA CSV parsing (header auto-detection + `parseVISARow`)

**Files:**
- Modify: `internal/infra/csv/repository.go` (constants block, `parseFile`, new `isVISAHeader` + `parseVISARow`)
- Test: `internal/infra/csv/repository_test.go`

**Interfaces:**
- Consumes: `Transaction.IsVISA` (Task 1); existing `parseDate`, `parseAmount`, `unwrap`.
- Produces:
  - `parseFile` routes to VISA parsing when the first row is a VISA header.
  - VISA rows: `IsVISA=true`, negative amounts only (`IsDebit=true`, `Amount=abs`), positive rows skipped, date = `DD/MM/YYYY` part of `rec[0]`, ` *` appended to description when `rec[5] == "Σε επεξεργασία"`, synthetic `ID = "VISA-" + rec[0] + "-" + rec[1]`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/infra/csv/repository_test.go`. Note the VISA header is built from its exact Greek column names.

```go
// visaHeader is the VISA statement header row (semicolon-separated).
const visaHeader = "Ημ/νία συναλλαγής;Αιτιολογία;Κατηγορία δαπάνης;Είδος συναλλαγής;Ποσό (EUR);Κατάσταση συναλλαγής"

func TestVISAParsing(t *testing.T) {
	ctx := context.Background()

	t.Run("negative rows kept as expenses, positive skipped", func(t *testing.T) {
		dir := t.TempDir()
		body := visaHeader + "\n" +
			`18/07/2026 10:42;EFOOD;Supermarket / Διατροφή;Αγορά;-5,80;Εκτελεσμένη` + "\n" +
			`13/07/2026 10:58;PAYMENT EBANKING;Αφορά μεταφορές;Πληρωμή Κάρτας;411,19;Εκτελεσμένη` + "\n"
		writeCSV(t, dir, "visa.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1) // the positive card-payment row is dropped
		require.True(t, got[0].IsVISA)
		require.True(t, got[0].IsDebit)
		require.Equal(t, "EFOOD", got[0].Description)
		require.InDelta(t, 5.80, got[0].Amount, 0.001)
		require.Equal(t, 2026, got[0].Date.Year())
		require.Equal(t, 7, int(got[0].Date.Month()))
		require.Equal(t, 18, got[0].Date.Day())
		require.Equal(t, "visa.csv", got[0].SourceFile)
		require.Equal(t, "VISA-18/07/2026 10:42-EFOOD", got[0].ID)
	})

	t.Run("pending status appends a marker to the description", func(t *testing.T) {
		dir := t.TempDir()
		body := visaHeader + "\n" +
			`21/07/2026 11:27;SKROUTZ;Λοιπές δαπάνες;Αγορά;-22,19;Σε επεξεργασία` + "\n"
		writeCSV(t, dir, "visa.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "SKROUTZ *", got[0].Description)
		require.Equal(t, "VISA-21/07/2026 11:27-SKROUTZ", got[0].ID) // ID uses the raw description
	})

	t.Run("bank files are unaffected by VISA detection", func(t *testing.T) {
		dir := t.TempDir()
		writeCSV(t, dir, "bank.csv", header+"\n"+`1;01/05/2026;="A";9;1/5/2026;="A1";1,00;Χ;`+"\n")
		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.False(t, got[0].IsVISA)
	})

	t.Run("skips malformed VISA rows", func(t *testing.T) {
		dir := t.TempDir()
		body := visaHeader + "\n" +
			`only;three;cols` + "\n" + // too few columns
			`notadate 10:00;X;C;Αγορά;-1,00;Εκτελεσμένη` + "\n" + // bad date
			`01/01/2026 10:00;Y;C;Αγορά;notanumber;Εκτελεσμένη` + "\n" + // bad amount
			`01/01/2026 10:00;OK;C;Αγορά;-2,50;Εκτελεσμένη` + "\n" // good
		writeCSV(t, dir, "visa.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "OK", got[0].Description)
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/infra/csv/ -run TestVISAParsing -v`
Expected: FAIL — VISA rows are currently parsed by the 8-column bank parser and dropped as malformed (so `got` is empty / wrong).

- [ ] **Step 3: Add the VISA constants and header detector**

In `internal/infra/csv/repository.go`, extend the `const` block (currently lines 17-21) and add package-level detection helpers:

```go
const (
	signDebit         = "Χ" // U+03A7 Greek capital Chi
	signCredit        = "Π" // U+03A0 Greek capital Pi
	columns           = 8
	visaColumns       = 6
	visaStatusPending = "Σε επεξεργασία"
)

// visaHeaderCols are the leading VISA-statement columns used to distinguish a
// VISA export from a bank export. Only the leading columns are checked, and
// trailing whitespace is tolerated.
var visaHeaderCols = []string{"Ημ/νία συναλλαγής", "Αιτιολογία", "Κατηγορία δαπάνης"}

// isVISAHeader reports whether a CSV header row is a VISA statement header.
func isVISAHeader(rec []string) bool {
	if len(rec) < len(visaHeaderCols) {
		return false
	}
	for i, w := range visaHeaderCols {
		if strings.TrimSpace(rec[i]) != w {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Route `parseFile` by header, add `parseVISARow`**

In `internal/infra/csv/repository.go`, replace the row loop in `parseFile` (currently lines 68-80) so it picks the parser once per file:

```go
	base := filepath.Base(path)
	isVISA := len(records) > 0 && isVISAHeader(records[0])
	var txns []transaction.Transaction
	for i, rec := range records {
		if i == 0 {
			continue // header
		}
		var (
			t  transaction.Transaction
			ok bool
		)
		if isVISA {
			t, ok = parseVISARow(rec, base, i+1)
		} else {
			t, ok = parseRow(rec, base, i+1)
		}
		if !ok {
			continue
		}
		txns = append(txns, t)
	}
	return txns, nil
```

Then add `parseVISARow` next to `parseRow`:

```go
// parseVISARow maps one VISA-statement row to a Transaction. Only negative
// amounts (real purchases) are kept; positive rows are card payments (the
// mirror of the bank lump) and are skipped so they are not double-counted.
func parseVISARow(rec []string, file string, line int) (transaction.Transaction, bool) {
	if len(rec) < visaColumns {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "too few columns")
		return transaction.Transaction{}, false
	}

	// rec[0] is "DD/MM/YYYY HH:MM"; keep the date part.
	fields := strings.Fields(rec[0])
	if len(fields) == 0 {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}
	date, err := parseDate(fields[0])
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}

	amount, err := parseAmount(unwrap(rec[4]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad amount")
		return transaction.Transaction{}, false
	}
	if amount >= 0 {
		return transaction.Transaction{}, false // card payment / non-expense: skip silently
	}

	desc := strings.TrimSpace(rec[1])
	if strings.TrimSpace(rec[5]) == visaStatusPending {
		desc += " *"
	}

	return transaction.Transaction{
		ID:          "VISA-" + strings.TrimSpace(rec[0]) + "-" + strings.TrimSpace(rec[1]),
		Date:        date,
		Description: desc,
		Amount:      -amount, // negative signed amount -> positive expense
		IsDebit:     true,
		SourceFile:  file,
		IsVISA:      true,
	}, true
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/infra/csv/ -run TestVISAParsing -v`
Expected: PASS.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/infra/csv/repository.go internal/infra/csv/repository_test.go
git commit -m "feat(csv): auto-detect and parse VISA statement exports"
```

---

## Task 4: `ReconcileVISA` pure function

**Files:**
- Modify: `internal/domain/transaction/transaction.go` (add `import "log/slog"`; add `ReconcileVISA` + `payingAccountFrom`)
- Test: `internal/domain/transaction/transaction_test.go`

**Interfaces:**
- Consumes: `Transaction` (with `Branch`/`IsVISA`), `ReconcileConfig`, `amountCents`, `Summarize` (tests).
- Produces: `func ReconcileVISA(txns []Transaction, cfg ReconcileConfig) []Transaction`.

**Behavior (from the design):**
1. Collect VISA purchases (`IsVISA` rows — the parser has already dropped positives). **If none, return `txns` unchanged** (edge §5: lumps present but no VISA file → leave untouched).
2. Scan bank rows (`!IsVISA`). A row is a **lump** when `descriptionMatches` AND `Branch == cfg.Branch`. Rows satisfying exactly one criterion are `slog.Warn`ed and **not** classified.
3. Paying account = the `SourceFile` with the largest lump total (warn the rest); or the label `"VISA"` when there are no lumps (warn).
4. Output = bank non-lump rows (untouched) + purchases re-tagged to the paying account + one `VISA LEFTOVERS` row per month where `leftover = paymentSum − purchaseSum != 0`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/transaction/transaction_test.go`. These use `amountCents` (same package) and build transactions directly.

```go
// visaLumpDesc is the exact mixed-script bank description "ΠΛΗΡΩΜΗ VΙSA".
const visaLumpDesc = "ΠΛΗΡΩΜΗ VΙSΑ"

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func reconcileCfg() ReconcileConfig {
	return ReconcileConfig{Description: visaLumpDesc, MatchMode: MatchExact, Branch: "96"}
}

func lumpTx(amount float64, date time.Time, file string) Transaction {
	return Transaction{Description: visaLumpDesc, Branch: "96", Amount: amount, IsDebit: true, Date: date, SourceFile: file}
}

func purchaseTx(desc string, amount float64, date time.Time) Transaction {
	return Transaction{Description: desc, Amount: amount, IsDebit: true, Date: date, SourceFile: "visa.csv", IsVISA: true}
}

// findLeftover returns the single VISA LEFTOVERS row for the given month, or a
// zero Transaction with ok=false when none was emitted.
func findLeftover(txns []Transaction, y int, m time.Month) (Transaction, bool) {
	for _, t := range txns {
		if t.Description == "VISA LEFTOVERS" && t.Date.Year() == y && t.Date.Month() == m {
			return t, true
		}
	}
	return Transaction{}, false
}

func TestReconcileVISA(t *testing.T) {
	t.Run("no VISA purchases leaves lumps untouched (§5)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			{ID: "R", Description: "RENT", Amount: 600, IsDebit: true, Date: day(2025, time.July, 1), SourceFile: "checking.csv", Branch: "99"},
		}
		out := ReconcileVISA(in, reconcileCfg())
		require.Equal(t, in, out)
	})

	t.Run("single month: positive leftover is a debit dated the lump date", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("SHOP", 50, day(2025, time.July, 3)),
			purchaseTx("CAFE", 20, day(2025, time.July, 10)),
			purchaseTx("GADGET", 150, day(2025, time.July, 14)),
		}
		out := ReconcileVISA(in, reconcileCfg())

		// lump removed
		for _, o := range out {
			require.NotEqual(t, visaLumpDesc, o.Description)
		}
		// purchases re-tagged onto the paying account
		var purchases int
		for _, o := range out {
			if o.IsVISA {
				require.Equal(t, "checking.csv", o.SourceFile)
				purchases++
			}
		}
		require.Equal(t, 3, purchases)
		// leftover = 300 - 220 = 80, debit, dated the lump date
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.True(t, lo.IsDebit)
		require.InDelta(t, 80, lo.Amount, 0.001)
		require.Equal(t, day(2025, time.July, 15), lo.Date)
		require.Equal(t, "checking.csv", lo.SourceFile)
	})

	t.Run("purchases but no lump that month: negative leftover is a credit on month-end (§2)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"), // sets paying account
			purchaseTx("SHOP", 220, day(2025, time.July, 3)),
			purchaseTx("AUG-BUY", 100, day(2025, time.August, 4)), // no August lump
		}
		out := ReconcileVISA(in, reconcileCfg())

		// July nets to zero leftover -> no row
		_, july := findLeftover(out, 2025, time.July)
		require.False(t, july)
		// August: leftover = 0 - 100 = -100 -> credit, dated 31 Aug
		aug, ok := findLeftover(out, 2025, time.August)
		require.True(t, ok)
		require.False(t, aug.IsDebit)
		require.InDelta(t, 100, aug.Amount, 0.001)
		require.Equal(t, day(2025, time.August, 31), aug.Date)
	})

	t.Run("lump but no purchases that month: whole amount is a leftover debit (§1)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.June, 20), "checking.csv"),
			purchaseTx("JULY-BUY", 50, day(2025, time.July, 2)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		june, ok := findLeftover(out, 2025, time.June)
		require.True(t, ok)
		require.True(t, june.IsDebit)
		require.InDelta(t, 300, june.Amount, 0.001)
		require.Equal(t, day(2025, time.June, 20), june.Date)
	})

	t.Run("no bank lump anywhere: purchases attributed to \"VISA\" (§4)", func(t *testing.T) {
		in := []Transaction{
			purchaseTx("SHOP", 40, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		for _, o := range out {
			if o.IsVISA {
				require.Equal(t, "VISA", o.SourceFile)
			}
		}
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.False(t, lo.IsDebit) // -40 -> credit
		require.InDelta(t, 40, lo.Amount, 0.001)
		require.Equal(t, "VISA", lo.SourceFile)
	})

	t.Run("zero leftover emits no row (§7)", func(t *testing.T) {
		in := []Transaction{
			lumpTx(220, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("SHOP", 220, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		_, ok := findLeftover(out, 2025, time.July)
		require.False(t, ok)
	})

	t.Run("multiple lumps in one month sum", func(t *testing.T) {
		in := []Transaction{
			lumpTx(100, day(2025, time.July, 9), "checking.csv"),
			lumpTx(200, day(2025, time.July, 9), "checking.csv"),
			purchaseTx("SHOP", 220, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.InDelta(t, 80, lo.Amount, 0.001) // 300 - 220
	})

	t.Run("branch gating: right description, wrong branch is not a lump", func(t *testing.T) {
		wrong := lumpTx(300, day(2025, time.July, 15), "checking.csv")
		wrong.Branch = "12" // not 96
		in := []Transaction{wrong, purchaseTx("SHOP", 50, day(2025, time.July, 3))}
		out := ReconcileVISA(in, reconcileCfg())
		// the branch-12 row is NOT removed; it passes through untouched
		var kept bool
		for _, o := range out {
			if o.Description == visaLumpDesc && o.Branch == "12" {
				kept = true
			}
		}
		require.True(t, kept)
		// leftover = 0 - 50 = -50 (no payment matched this month)
		lo, ok := findLeftover(out, 2025, time.July)
		require.True(t, ok)
		require.False(t, lo.IsDebit)
		require.InDelta(t, 50, lo.Amount, 0.001)
	})

	t.Run("per-month invariant: purchaseSum + leftover == paymentSum", func(t *testing.T) {
		in := []Transaction{
			lumpTx(300, day(2025, time.July, 15), "checking.csv"),
			purchaseTx("A", 50, day(2025, time.July, 3)),
			purchaseTx("B", 20, day(2025, time.July, 10)),
			purchaseTx("C", 150, day(2025, time.July, 14)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		var purchaseCents, leftoverCents int64
		for _, o := range out {
			if o.Date.Month() != time.July {
				continue
			}
			switch {
			case o.IsVISA:
				purchaseCents += amountCents(o.Amount)
			case o.Description == "VISA LEFTOVERS":
				if o.IsDebit {
					leftoverCents += amountCents(o.Amount)
				} else {
					leftoverCents -= amountCents(o.Amount)
				}
			}
		}
		require.Equal(t, amountCents(300), purchaseCents+leftoverCents)
	})

	t.Run("multiple bank files carry lumps: largest total wins", func(t *testing.T) {
		in := []Transaction{
			lumpTx(100, day(2025, time.July, 15), "small.csv"),
			lumpTx(300, day(2025, time.July, 15), "big.csv"),
			purchaseTx("SHOP", 50, day(2025, time.July, 3)),
		}
		out := ReconcileVISA(in, reconcileCfg())
		for _, o := range out {
			if o.IsVISA || o.Description == "VISA LEFTOVERS" {
				require.Equal(t, "big.csv", o.SourceFile)
			}
		}
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/transaction/ -run TestReconcileVISA -v`
Expected: FAIL — `ReconcileVISA` undefined.

- [ ] **Step 3: Add the `log/slog` import**

In `internal/domain/transaction/transaction.go`, add `"log/slog"` to the import block:

```go
import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 4: Implement `ReconcileVISA` and `payingAccountFrom`**

Add to `internal/domain/transaction/transaction.go` (e.g. after `FilterTransfers`):

```go
// ReconcileVISA replaces each month's matched VISA lump payment(s) with the
// itemized VISA purchases behind them, plus a single VISA LEFTOVERS row that
// preserves the month's net outflow (purchaseSum + leftover == paymentSum).
//
// It runs after FilterTransfers, on the combined bank+VISA slice. When no VISA
// purchases are present the input is returned unchanged (lumps stay intact).
func ReconcileVISA(txns []Transaction, cfg ReconcileConfig) []Transaction {
	// Itemized purchases (the parser keeps only negative VISA rows).
	var purchases []Transaction
	for _, t := range txns {
		if t.IsVISA {
			purchases = append(purchases, t)
		}
	}
	if len(purchases) == 0 {
		return txns // §5: lumps present, no VISA file — leave untouched.
	}

	// Step 1 — identify lumps among bank rows; warn on partial matches.
	isLump := make([]bool, len(txns))
	lumpTotals := make(map[string]int64) // SourceFile -> total cents
	for i, t := range txns {
		if t.IsVISA {
			continue
		}
		descMatch := cfg.descriptionMatches(t.Description)
		branchMatch := t.Branch == cfg.Branch
		switch {
		case descMatch && branchMatch:
			isLump[i] = true
			lumpTotals[t.SourceFile] += amountCents(t.Amount)
		case branchMatch && !descMatch:
			slog.Warn("branch matches VISA config but description does not; not treating as a VISA lump",
				"date", t.Date.Format("2006-01-02"), "description", t.Description, "branch", t.Branch, "amount", t.Amount)
		case descMatch && !branchMatch:
			slog.Warn("description matches VISA config but branch does not; not treating as a VISA lump",
				"date", t.Date.Format("2006-01-02"), "description", t.Description, "branch", t.Branch, "amount", t.Amount)
		}
	}

	payingAccount := payingAccountFrom(lumpTotals)

	type ym struct {
		year  int
		month time.Month
	}

	// Bank non-lump rows pass through untouched.
	out := make([]Transaction, 0, len(txns))
	for i, t := range txns {
		if t.IsVISA || isLump[i] {
			continue
		}
		out = append(out, t)
	}

	// Re-tagged purchases; accumulate per-month purchase sums.
	purchaseSum := make(map[ym]int64)
	for _, p := range purchases {
		p.SourceFile = payingAccount
		out = append(out, p)
		purchaseSum[ym{p.Date.Year(), p.Date.Month()}] += amountCents(p.Amount)
	}

	// Per-month payment sums and the last lump date in each month.
	paymentSum := make(map[ym]int64)
	lastLump := make(map[ym]time.Time)
	for i, t := range txns {
		if !isLump[i] {
			continue
		}
		k := ym{t.Date.Year(), t.Date.Month()}
		paymentSum[k] += amountCents(t.Amount)
		if d, ok := lastLump[k]; !ok || t.Date.After(d) {
			lastLump[k] = t.Date
		}
	}

	// Union of months, sorted for deterministic output.
	monthSet := make(map[ym]bool)
	for k := range purchaseSum {
		monthSet[k] = true
	}
	for k := range paymentSum {
		monthSet[k] = true
	}
	months := make([]ym, 0, len(monthSet))
	for k := range monthSet {
		months = append(months, k)
	}
	sort.Slice(months, func(i, j int) bool {
		if months[i].year != months[j].year {
			return months[i].year < months[j].year
		}
		return months[i].month < months[j].month
	})

	for _, k := range months {
		leftover := paymentSum[k] - purchaseSum[k]
		if leftover == 0 {
			continue // §7
		}
		row := Transaction{
			Description: "VISA LEFTOVERS",
			SourceFile:  payingAccount,
			ID:          fmt.Sprintf("VISA-LEFTOVERS-%04d-%02d", k.year, int(k.month)),
		}
		if leftover > 0 {
			row.IsDebit = true
			row.Amount = float64(leftover) / 100
		} else {
			row.IsDebit = false
			row.Amount = float64(-leftover) / 100
		}
		if d, ok := lastLump[k]; ok {
			row.Date = d
		} else {
			// No payment this month: date on the month's last day.
			row.Date = time.Date(k.year, k.month+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		}
		out = append(out, row)
	}
	return out
}

// payingAccountFrom picks the SourceFile carrying the largest lump total,
// warning about the rest. With no lumps it falls back to the label "VISA".
func payingAccountFrom(lumpTotals map[string]int64) string {
	if len(lumpTotals) == 0 {
		slog.Warn("VISA purchases found but no matching bank lump; attributing to \"VISA\"")
		return "VISA"
	}
	files := make([]string, 0, len(lumpTotals))
	for f := range lumpTotals {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if lumpTotals[files[i]] != lumpTotals[files[j]] {
			return lumpTotals[files[i]] > lumpTotals[files[j]] // largest first
		}
		return files[i] < files[j] // stable tie-break
	})
	for _, f := range files[1:] {
		slog.Warn("multiple bank files carry VISA lumps; using the largest, ignoring this one",
			"file", f, "total", float64(lumpTotals[f])/100)
	}
	return files[0]
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/domain/transaction/ -run TestReconcileVISA -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/transaction/transaction.go internal/domain/transaction/transaction_test.go
git commit -m "feat(transaction): add ReconcileVISA per-month reconciliation"
```

---

## Task 5: Object-shaped config + `exclusion-rules.json` migration

**Files:**
- Modify: `internal/infra/config/config.go`
- Test: `internal/infra/config/config_test.go` (rewrite)
- Modify: `main.go:49-62` (`runGenerate`), `internal/infra/web/server.go` (`handleIndex`, `handleGenerate`)
- Migrate: `exclusion-rules.json`

**Interfaces:**
- Consumes: `transaction.RuleSpec`, `transaction.ReconcileConfig` (+ its `Validate`), `transaction.DefaultRuleSpecs`.
- Produces:
  - `type File struct { Exclusions []transaction.RuleSpec; VisaReconcile *transaction.ReconcileConfig }` with JSON tags `exclusions` / `visaReconcile,omitempty`.
  - `func Load(path string) (File, error)` — object shape only; seeds `File{Exclusions: DefaultRuleSpecs()}` when the file is missing; validates each exclusion spec and, when present, `VisaReconcile`.
  - `func Save(path string, f File) error`.

- [ ] **Step 1: Rewrite the config tests (failing)**

Replace the entire contents of `internal/infra/config/config_test.go` with:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestLoadSeedsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")

	f, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, transaction.DefaultRuleSpecs(), f.Exclusions)
	require.Nil(t, f.VisaReconcile)
	require.FileExists(t, path)

	again, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, f, again)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	debit := false
	in := File{
		Exclusions: []transaction.RuleSpec{
			{MatchMode: transaction.MatchContains, IsDebit: &debit, Description: "FEE", SourceFile: "a.csv"},
			{MatchMode: transaction.MatchExact, Description: "RENT"},
		},
		VisaReconcile: &transaction.ReconcileConfig{
			Description: "ΠΛΗΡΩΜΗ VΙSA", MatchMode: transaction.MatchExact, Branch: "96",
		},
	}
	require.NoError(t, Save(path, in))

	out, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestLoadVisaReconcileAbsentIsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"exclusions":[{"matchMode":"exact","description":"RENT"}]}`), 0o644))

	f, err := Load(path)
	require.NoError(t, err)
	require.Nil(t, f.VisaReconcile)
	require.Len(t, f.Exclusions, 1)
}

func TestLoadMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), path)
}

func TestLoadInvalidExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"exclusions":[{"matchMode":"exact","description":""}]}`), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule 1")
	require.Contains(t, err.Error(), path)
}

func TestLoadInvalidVisaReconcile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"exclusions":[],"visaReconcile":{"description":"","branch":"96"}}`), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "visaReconcile")
	require.Contains(t, err.Error(), path)
}

func TestDefaultPath(t *testing.T) {
	require.Contains(t, DefaultPath(), "exclusion-rules.json")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/infra/config/ -v`
Expected: FAIL — `Load` returns `[]transaction.RuleSpec`, so `f.Exclusions` / `File` don't compile.

- [ ] **Step 3: Rewrite `config.go` to the object shape**

Replace `Load` and `Save` in `internal/infra/config/config.go` (keep the package comment, imports, and `DefaultPath` as-is; `errors`, `fmt`, `encoding/json`, `os` are already imported):

```go
// File is the on-disk configuration: exclusion rules plus optional VISA
// reconciliation settings. It is stored as a JSON object.
type File struct {
	Exclusions    []transaction.RuleSpec       `json:"exclusions"`
	VisaReconcile *transaction.ReconcileConfig `json:"visaReconcile,omitempty"`
}

// Load reads and validates the config object from path. A missing file is
// seeded with DefaultRuleSpecs() (and no VISA reconciliation), saved, and
// returned. A malformed file or invalid entry returns a descriptive error
// naming the path; it never silently falls back.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		f := File{Exclusions: transaction.DefaultRuleSpecs()}
		if err := Save(path, f); err != nil {
			return File{}, err
		}
		return f, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	for i, s := range f.Exclusions {
		if err := s.Validate(); err != nil {
			return File{}, fmt.Errorf("%s: rule %d: %w", path, i+1, err)
		}
	}
	if f.VisaReconcile != nil {
		if err := f.VisaReconcile.Validate(); err != nil {
			return File{}, fmt.Errorf("%s: visaReconcile: %w", path, err)
		}
	}
	return f, nil
}

// Save writes the config object to path as indented JSON.
func Save(path string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the config tests to verify they pass**

Run: `go test ./internal/infra/config/ -v`
Expected: PASS.

- [ ] **Step 5: Adapt `main.go` `runGenerate` to the new shape**

In `main.go`, update `runGenerate` (lines 49-62) to read `cfg.Exclusions` (the `NewService` signature does **not** change in this task — the reconcile argument arrives in Task 6):

```go
func runGenerate(dataDir, outputPath, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	repo := csv.New(dataDir)
	renderer := html.NewFile(outputPath)
	svc := report.NewService(repo, renderer, transaction.CompileRules(cfg.Exclusions))
	if err := svc.GenerateReport(context.Background()); err != nil {
		return err
	}
	slog.Info("report generated", "path", outputPath)
	return nil
}
```

- [ ] **Step 6: Adapt the web layer to the new shape**

In `internal/infra/web/server.go`, update `handleIndex` (the `config.Load` call, lines 45-51) to use `cfg.Exclusions`:

```go
func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		http.Error(w, "Couldn't load exclusion rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, struct{ Rules []ruleView }{toRuleViews(cfg.Exclusions)}); err != nil {
		slog.Error("rendering index", "error", err)
	}
}
```

In `handleGenerate`, the save path must now write a `config.File`. Load the existing config first to preserve `VisaReconcile` (the form only edits exclusions). Replace the save block (currently lines 74-79) with:

```go
	if r.FormValue("save") != "" {
		existing, err := config.Load(s.configPath)
		if err != nil {
			http.Error(w, "Couldn't load rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
		existing.Exclusions = specs
		if err := config.Save(s.configPath, existing); err != nil {
			http.Error(w, "Couldn't save rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
```

(The `NewService` call in `handleGenerate` is updated in Task 6.)

- [ ] **Step 7: Run the whole suite**

Run: `go test ./...`
Expected: PASS (existing `server_test.go` and `main_test.go` still pass — the saved-file substring checks `"description": "SHOP"` / `"matchMode": "contains"` remain present in the nested object).

- [ ] **Step 8: Migrate the repo's `exclusion-rules.json` to the object shape**

The existing file is an array holding one rule whose description is mixed-script (`ΕΝΤΟΛΗ ΙΝSΤΑΝΤ ΤRΑΝS`). Preserve those bytes verbatim by wrapping the array with `jq` rather than retyping it; inject the exact VISA description via `printf`:

```bash
VISA_DESC=$(printf 'ΠΛΗΡΩΜΗ VΙSΑ')
jq --arg d "$VISA_DESC" \
  '{exclusions: ., visaReconcile: {description: $d, matchMode: "exact", branch: "96"}}' \
  exclusion-rules.json > exclusion-rules.tmp.json
mv exclusion-rules.tmp.json exclusion-rules.json
```

Verify the shape and the exact VISA bytes:

```bash
jq -e '.exclusions and .visaReconcile.branch == "96"' exclusion-rules.json
jq -r '.visaReconcile.description' exclusion-rules.json | xxd | head -2
```
Expected: the `jq -e` prints `true`; the `xxd` output begins `cea0 ce9b ce97 cea1 cea9 ce9c ce97 2056  ...` then `ce99 53ce 91`.

- [ ] **Step 9: Confirm the migrated file loads**

Run: `go run . generate --data sample-data --out "$TMPDIR/mig.html" --config exclusion-rules.json`
Expected: exits 0 and logs `report generated`. (Sample reconciliation output is added in Task 7; here we only confirm the object-shaped config parses.)

- [ ] **Step 10: Commit**

```bash
git add internal/infra/config/config.go internal/infra/config/config_test.go main.go internal/infra/web/server.go exclusion-rules.json
git commit -m "feat(config): object-shaped rules file with visaReconcile block"
```

---

## Task 6: Service pipeline wiring (partition, bypass, reconcile)

**Files:**
- Modify: `internal/app/report/service.go`
- Test: `internal/app/report/service_test.go`
- Modify: `main.go:49-62` (`runGenerate`), `internal/infra/web/server.go` (`handleGenerate`)

**Interfaces:**
- Consumes: `transaction.ReconcileVISA`, `transaction.ReconcileConfig`, `config.File.VisaReconcile`.
- Produces: `func NewService(repo transaction.Repository, renderer Renderer, rules []transaction.ExclusionRule, reconcile *transaction.ReconcileConfig) *Service`. The pipeline partitions bank vs VISA, runs `FilterTransfers` on **bank only**, then `ReconcileVISA(bank+VISA, *reconcile)` when `reconcile != nil`.

- [ ] **Step 1: Update existing `NewService` calls + add the bypass test (failing)**

In `internal/app/report/service_test.go`, add `nil` as the fourth argument to every existing `NewService(...)` call (there are four: lines 37, 54, 68, 93). Then append a new subtest inside `TestGenerateReport` proving VISA rows bypass `FilterTransfers` and get reconciled:

```go
	t.Run("VISA rows bypass FilterTransfers and reconcile", func(t *testing.T) {
		d := time.Date(2025, time.July, 1, 0, 0, 0, 0, time.UTC)
		lumpDesc := "ΠΛΗΡΩΜΗ VΙSΑ"
		txns := []transaction.Transaction{
			// A bank inter-account transfer pair (same ID+amount) must be filtered out.
			{ID: "T", SourceFile: "checking.csv", Amount: 100, IsDebit: true, Date: d, Branch: "12"},
			{ID: "T", SourceFile: "savings.csv", Amount: 100, IsDebit: false, Date: d, Branch: "12"},
			// The VISA lump (branch 96) is replaced by its itemized purchases.
			{ID: "L", SourceFile: "checking.csv", Amount: 200, IsDebit: true, Date: time.Date(2025, time.July, 15, 0, 0, 0, 0, time.UTC), Branch: "96", Description: lumpDesc},
			// Two VISA purchases; note they share ID+amount but must NOT be filtered as a transfer.
			{ID: "VISA-a", SourceFile: "visa.csv", Amount: 80, IsDebit: true, Date: time.Date(2025, time.July, 3, 0, 0, 0, 0, time.UTC), IsVISA: true, Description: "SHOP"},
			{ID: "VISA-b", SourceFile: "visa.csv", Amount: 80, IsDebit: true, Date: time.Date(2025, time.July, 4, 0, 0, 0, 0, time.UTC), IsVISA: true, Description: "SHOP"},
		}
		repo := &transaction.MockRepository{}
		repo.On("GetAll", ctx).Return(txns, nil)

		var captured transaction.Summary
		renderer := &MockRenderer{}
		renderer.On("Render", ctx, mock.Anything).
			Run(func(args mock.Arguments) { captured = args.Get(1).(transaction.Summary) }).
			Return(nil)

		cfg := &transaction.ReconcileConfig{Description: lumpDesc, MatchMode: transaction.MatchExact, Branch: "96"}
		svc := NewService(repo, renderer, nil, cfg)
		require.NoError(t, svc.GenerateReport(ctx))

		// Transfer pair filtered (income 0). Expenses = 80+80 purchases + 40 leftover = 200.
		require.InDelta(t, 0, captured.TotalIncome, 0.001)
		require.InDelta(t, 200, captured.TotalExpenses, 0.001)
	})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/app/report/ -v`
Expected: FAIL — `NewService` takes 3 args, not 4.

- [ ] **Step 3: Update `Service`, `NewService`, and the pipeline**

Replace the struct, constructor, and `GenerateReport` in `internal/app/report/service.go`:

```go
// Service orchestrates report generation:
// load → partition bank/VISA → filter transfers (bank only) →
// reconcile VISA → apply exclusion rules → summarize → render.
type Service struct {
	repo      transaction.Repository
	renderer  Renderer
	rules     []transaction.ExclusionRule
	reconcile *transaction.ReconcileConfig // nil = VISA reconciliation disabled
}

func NewService(repo transaction.Repository, renderer Renderer, rules []transaction.ExclusionRule, reconcile *transaction.ReconcileConfig) *Service {
	return &Service{repo: repo, renderer: renderer, rules: rules, reconcile: reconcile}
}

func (s *Service) GenerateReport(ctx context.Context) error {
	all, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("loading transactions: %w", err)
	}

	// VISA rows bypass FilterTransfers: that filter targets bank inter-account
	// transfers and duplicate export rows, which do not apply to card purchases.
	var bank, visa []transaction.Transaction
	for _, t := range all {
		if t.IsVISA {
			visa = append(visa, t)
		} else {
			bank = append(bank, t)
		}
	}

	bankKept := transaction.FilterTransfers(bank)
	if excluded := len(bank) - len(bankKept); excluded > 0 {
		slog.Info("excluded inter-account transfers and duplicates", "count", excluded)
	}

	combined := append(bankKept, visa...)
	if s.reconcile != nil {
		combined = transaction.ReconcileVISA(combined, *s.reconcile)
	}

	before := len(combined)
	kept := transaction.ApplyExclusions(combined, s.rules)
	if dropped := before - len(kept); dropped > 0 {
		slog.Info("excluded transactions by exclusion rule", "count", dropped)
	}

	summary := transaction.Summarize(kept)

	if err := s.renderer.Render(ctx, summary); err != nil {
		return fmt.Errorf("rendering report: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the service tests to verify they pass**

Run: `go test ./internal/app/report/ -v`
Expected: PASS (including the new bypass subtest).

- [ ] **Step 5: Pass the reconcile config from `main.go`**

In `main.go` `runGenerate`, add the fourth argument:

```go
	svc := report.NewService(repo, renderer, transaction.CompileRules(cfg.Exclusions), cfg.VisaReconcile)
```

- [ ] **Step 6: Pass the reconcile config from the web layer**

In `internal/infra/web/server.go` `handleGenerate`, load the config once (needed for `VisaReconcile`) and pass it to the service. Replace the current parse/save/service section so it reads the config, applies the parsed exclusions, optionally saves, then builds the service with the reconcile config:

```go
	specs, err := parseRules(r.MultipartForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg, err := config.Load(s.configPath)
	if err != nil {
		http.Error(w, "Couldn't load rules: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cfg.Exclusions = specs
	if r.FormValue("save") != "" {
		if err := config.Save(s.configPath, cfg); err != nil {
			http.Error(w, "Couldn't save rules: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
```

and further down, the service construction (currently line 96) becomes:

```go
	svc := report.NewService(csv.New(tmpDir), html.NewWriter(&buf), transaction.CompileRules(specs), cfg.VisaReconcile)
```

(Remove the standalone save block added in Task 5 Step 6 — it is now folded into this combined section. There must be exactly one `config.Load` and one save path in `handleGenerate`.)

- [ ] **Step 7: Run the whole suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/report/service.go internal/app/report/service_test.go main.go internal/infra/web/server.go
git commit -m "feat(report): partition VISA, bypass FilterTransfers, reconcile in pipeline"
```

---

## Task 7: Sample data + README documentation

**Files:**
- Create: `sample-data/visa.csv`
- Modify: `sample-data/checking.csv` (append a `ΠΛΗΡΩΜΗ VΙSA` branch-96 lump)
- Modify: `README.md` (CSV-format section)

**Interfaces:**
- Consumes: the full pipeline (Tasks 2–6) and the migrated `exclusion-rules.json` (Task 5).
- Produces: a demonstrable July-2025 reconciliation (lump €300 → three itemized purchases €220 + €80 leftover).

- [ ] **Step 1: Create `sample-data/visa.csv`**

The Greek category/status strings below are ordinary Greek (no homoglyphs), so a normal editor is fine. Write:

```
Ημ/νία συναλλαγής;Αιτιολογία;Κατηγορία δαπάνης;Είδος συναλλαγής;Ποσό (EUR);Κατάσταση συναλλαγής
03/07/2025 12:15;SUPERMARKET ALFA;Supermarket / Διατροφή;Αγορά;-50,00;Εκτελεσμένη
10/07/2025 09:30;CAFE NUMERO;Λοιπές δαπάνες και πληρωμές;Αγορά;-20,00;Εκτελεσμένη
14/07/2025 18:45;ELECTRONICS STORE;Λοιπές δαπάνες και πληρωμές;Αγορά;-150,00;Σε επεξεργασία
15/07/2025 11:00;PAYMENT EBANKING;Αφορά ηλεκτρονικές μεταφορές/πληρωμές;Πληρωμή Κάρτας;300,00;Εκτελεσμένη
```

- [ ] **Step 2: Append the VISA lump to `sample-data/checking.csv`**

The lump description must be byte-exact mixed-script — append it with `printf` (do not type it in an editor). The row is `Α/Α=87`, date `15/07/2025`, `Κατάστημα=96`, amount `300,00`, sign `Χ`:

```bash
{
  printf '87;15/07/2025;="'
  printf 'ΠΛΗΡΩΜΗ VΙSΑ'
  printf '";96;15/7/2025;="202507150096000887";300,00;Χ;\n'
} >> sample-data/checking.csv
```

Verify the appended description bytes:

```bash
tail -1 sample-data/checking.csv | xxd | grep -i 'cea0 ce9b'
```
Expected: a line containing `cea0 ce9b ce97 cea1 cea9 ce9c ce97 2056` (the `ΠΛΗΡΩΜΗ V…` bytes). Also confirm the row parses by re-running the end-to-end check in Step 3.

- [ ] **Step 3: End-to-end verification against the sample data**

Run:

```bash
go run . generate --data sample-data --out "$TMPDIR/sample.html" --config exclusion-rules.json
grep -c "VISA LEFTOVERS" "$TMPDIR/sample.html"
grep -c "SUPERMARKET ALFA" "$TMPDIR/sample.html"
grep -c "ELECTRONICS STORE \*" "$TMPDIR/sample.html"
```
Expected:
- command exits 0,
- `VISA LEFTOVERS` count ≥ 1 (the €80 July remainder),
- `SUPERMARKET ALFA` and the pending-marked `ELECTRONICS STORE *` both appear (count ≥ 1), i.e. the itemized purchases replaced the lump.

If any count is 0, the description bytes in `checking.csv` and `exclusion-rules.json` do not match — regenerate both from the same `printf`/`\u` codepoints and retry.

- [ ] **Step 4: Update the README CSV-format section**

In `README.md`, after the existing bank-format table (ending at line 154, "…are skipped with a warning."), add a VISA subsection. Insert:

```markdown
### VISA statement reconciliation

The bank export records a credit-card bill as a single lump debit
(`ΠΛΗΡΩΜΗ VΙSA`, `Κατάστημα` 96) — it shows *how much* went to the card but not
*what it was spent on*. Drop the card's own VISA statement export into the same
folder and the report replaces each month's lump with the itemized purchases
behind it, plus a single `VISA LEFTOVERS` line that keeps the month's net figure
intact.

VISA exports are auto-detected by their header and are semicolon-separated with
signed, comma-decimal amounts:

```csv
Ημ/νία συναλλαγής;Αιτιολογία;Κατηγορία δαπάνης;Είδος συναλλαγής;Ποσό (EUR);Κατάσταση συναλλαγής
03/07/2025 12:15;SUPERMARKET ALFA;Supermarket / Διατροφή;Αγορά;-50,00;Εκτελεσμένη
15/07/2025 11:00;PAYMENT EBANKING;Αφορά ηλεκτρονικές μεταφορές/πληρωμές;Πληρωμή Κάρτας;300,00;Εκτελεσμένη
```

- **Negative amounts** are real purchases and are kept as expenses; **positive
  amounts** are card payments (the mirror of the bank lump) and are skipped so
  they are not double-counted.
- Pending transactions (`Κατάσταση` = `Σε επεξεργασία`) get a ` *` appended to
  their description.
- The lump matcher is configured in `exclusion-rules.json` under
  `visaReconcile` (`description`, `matchMode`, `branch`); when that block is
  absent, reconciliation is disabled and the app behaves as before.

See `sample-data/visa.csv` for a runnable example.
```

- [ ] **Step 5: Verify the README renders and the suite is green**

Run: `go test ./...`
Expected: PASS.

Confirm the README addition is present:

```bash
grep -c "VISA statement reconciliation" README.md
```
Expected: `1`.

- [ ] **Step 6: Commit**

```bash
git add sample-data/visa.csv sample-data/checking.csv README.md
git commit -m "docs: sample VISA data and CSV-format reconciliation docs"
```

---

## Self-Review

**1. Spec coverage.**

| Design section | Covered by |
| --- | --- |
| Data model (`Branch`, `IsVISA`) | Task 1 |
| VISA parsing: header auto-detection, negative→expense, positive skipped, datetime, pending `*`, synthetic ID | Task 3 |
| Bank parser change (capture `Branch`) | Task 2 |
| `ReconcileVISA` step 1 (paying account: single/multiple→largest+warn/none→"VISA"), partial-match logging | Task 4 |
| `ReconcileVISA` step 2 (partition & remove, re-tag) | Task 4 |
| `ReconcileVISA` step 3 (per-month leftover, sign, date, zero-leftover, cents) | Task 4 |
| Edge cases §1–§7 | Task 4 tests (§1 lump-no-purchases, §2 purchases-no-lump, §3 exceed→credit = same as §2, §4 no-lump→"VISA", §5 no-VISA-file untouched, §6 positives skipped = Task 3, §7 zero leftover) |
| Configuration (object shape, `ReconcileConfig`, absent→no-op, migration, exact bytes) | Task 5 |
| Rendering (no renderer changes) | none needed — verified by Task 7 end-to-end |
| Pipeline (VISA bypasses `FilterTransfers`, ordering) | Task 6 |
| Testing (parser, reconcile, config, pipeline, regression) | Tasks 1–7 |
| Sample data & docs | Task 7 |

**2. Placeholder scan.** No `TBD`/`TODO`/"add error handling"/"similar to Task N" — every code and test block is complete.

**3. Type consistency.** `ReconcileConfig{Description, MatchMode, Branch}` (Task 1) is consumed unchanged by `ReconcileVISA` (Task 4), `config.File.VisaReconcile` (Task 5), and `NewService(..., reconcile *transaction.ReconcileConfig)` (Task 6). `config.File{Exclusions, VisaReconcile}` (Task 5) is consumed as `cfg.Exclusions` / `cfg.VisaReconcile` in `main.go` and `server.go` (Tasks 5–6). `ReconcileVISA(txns, cfg)` signature is identical across the service call (Task 6) and its tests (Task 4). `parseVISARow`/`isVISAHeader` (Task 3) are internal to the `csv` package. The `VISA LEFTOVERS` description literal and the `visaLumpDesc` escape sequence are identical everywhere they appear.

> **Note on `handleGenerate` (web):** Task 5 Step 6 adds a save-only `config.Load`; Task 6 Step 6 **replaces** it with a single combined load/save/service section. After Task 6 there must be exactly one `config.Load` call in `handleGenerate`.