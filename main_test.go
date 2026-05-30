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
		require.Contains(t, string(data), "The Monthly Review")
		require.Contains(t, string(data), "May 2026")
		require.Contains(t, string(data), "Monthly Average")
		require.Contains(t, string(data), "over 1 month")
	})

	t.Run("returns error when no data", func(t *testing.T) {
		err := run(t.TempDir(), filepath.Join(t.TempDir(), "report.html"))
		require.Error(t, err)
	})
}
