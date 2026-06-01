package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const csvHeader = "Α/Α;Ημερομηνία;Αιτιολογία;Κατάστημα;Τοκισμός από;Αρ. συναλλαγής;Ποσό;Πρόσημο ποσού;"

func TestRunGenerate(t *testing.T) {
	t.Run("generates report end to end", func(t *testing.T) {
		dataDir := t.TempDir()
		body := csvHeader + "\n" +
			`1;29/05/2026;="SHOP";9;27/5/2026;="ID1";53,79;Χ;` + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))

		out := filepath.Join(t.TempDir(), "report.html")
		cfg := filepath.Join(t.TempDir(), "rules.json")
		require.NoError(t, runGenerate(dataDir, out, cfg))

		data, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(data), "May 2026")
	})

	t.Run("returns error when no data", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "rules.json")
		require.Error(t, runGenerate(t.TempDir(), filepath.Join(t.TempDir(), "report.html"), cfg))
	})
}

func TestDispatch(t *testing.T) {
	t.Run("generate writes a report", func(t *testing.T) {
		dataDir := t.TempDir()
		body := csvHeader + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))
		out := filepath.Join(t.TempDir(), "report.html")

		require.NoError(t, dispatch([]string{"generate", "--data", dataDir, "--out", out}))
		_, err := os.Stat(out)
		require.NoError(t, err)
	})

	t.Run("generate accepts --config and seeds the file", func(t *testing.T) {
		dataDir := t.TempDir()
		body := csvHeader + "\n" +
			`2;18/05/2026;="SALARY";9;18/5/2026;="ID2";1.550,00;Π;` + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, "acc.csv"), []byte(body), 0o644))
		out := filepath.Join(t.TempDir(), "report.html")
		cfg := filepath.Join(t.TempDir(), "rules.json")

		require.NoError(t, dispatch([]string{"generate", "--data", dataDir, "--out", out, "--config", cfg}))
		require.FileExists(t, out)
		require.FileExists(t, cfg) // seeded on first load
	})

	t.Run("help prints usage", func(t *testing.T) {
		require.NoError(t, dispatch([]string{"help"}))
	})

	t.Run("version prints", func(t *testing.T) {
		require.NoError(t, dispatch([]string{"--version"}))
	})

	t.Run("unknown command errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"bogus"}))
	})

	t.Run("generate with bad flag errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"generate", "--nope"}))
	})

	t.Run("serve with bad flag errors", func(t *testing.T) {
		require.Error(t, dispatch([]string{"serve", "--nope"}))
	})
}
