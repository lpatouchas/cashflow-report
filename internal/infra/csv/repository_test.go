package csv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
			`1;2;3` + "\n" + // too few columns
			`x;notadate;="X";1;1;="ID1";10,00;Χ;` + "\n" + // bad date (no slashes)
			`x;aa/bb/cccc;="X";1;1;="ID5";10,00;Χ;` + "\n" + // bad date (non-numeric parts)
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

	t.Run("wraps open error for unopenable file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("permission bits are bypassed when running as root")
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "locked.csv")
		require.NoError(t, os.WriteFile(path, []byte(header+"\n"), 0o000))

		_, err := New(dir).GetAll(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, "locked.csv")
	})
}

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
		require.Equal(t, "Supermarket / Διατροφή", got[0].Category) // Κατηγορία δαπάνης captured
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

	t.Run("leading UTF-8 BOM does not defeat VISA header detection", func(t *testing.T) {
		dir := t.TempDir()
		// Real bank/VISA exports commonly begin with a UTF-8 BOM (EF BB BF).
		body := "\ufeff" + visaHeader + "\n" +
			`21/07/2026 11:27;EVERYPAY*SKROUTZ;Λοιπές δαπάνες;Αγορά;-22,19;Σε επεξεργασία` + "\n" +
			`21/07/2026 09:16;FD4 COFFEE I K E;Εστίαση;Αγορά;-36,00;Σε επεξεργασία` + "\n"
		writeCSV(t, dir, "visa-gold.csv", body)

		got, err := New(dir).GetAll(ctx)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.True(t, got[0].IsVISA)
		require.Equal(t, "EVERYPAY*SKROUTZ *", got[0].Description)
		require.InDelta(t, 22.19, got[0].Amount, 0.001)
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
