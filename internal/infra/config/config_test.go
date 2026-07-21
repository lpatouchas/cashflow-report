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
