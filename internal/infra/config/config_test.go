package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestLoadSeedsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")

	specs, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, transaction.DefaultRuleSpecs(), specs)

	// the file was written
	require.FileExists(t, path)

	// a second load reads the saved file and matches
	again, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, specs, again)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	debit := false
	in := []transaction.RuleSpec{
		{MatchMode: transaction.MatchContains, IsDebit: &debit, Description: "FEE", SourceFile: "a.csv"},
		{MatchMode: transaction.MatchExact, Description: "RENT"},
	}
	require.NoError(t, Save(path, in))

	out, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestLoadMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), path)
}

func TestLoadInvalidSpec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	require.NoError(t, os.WriteFile(path, []byte(`[{"matchMode":"exact","description":""}]`), 0o644))

	_, err := Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rule 1")
	require.Contains(t, err.Error(), path)
}

func TestDefaultPath(t *testing.T) {
	require.Contains(t, DefaultPath(), "exclusion-rules.json")
}
