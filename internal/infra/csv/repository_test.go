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
