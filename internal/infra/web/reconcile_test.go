package web

import (
	"mime/multipart"
	"testing"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/stretchr/testify/require"
)

func TestToReconcileView(t *testing.T) {
	t.Run("nil renders blank with exact default", func(t *testing.T) {
		v := toReconcileView(nil)
		require.Equal(t, reconcileView{MatchMode: "exact"}, v)
	})

	t.Run("populated config mirrors fields", func(t *testing.T) {
		v := toReconcileView(&transaction.ReconcileConfig{
			Description: "ΠΛΗΡΩΜΗ VΙSΑ", MatchMode: transaction.MatchContains, Branch: "96",
		})
		require.Equal(t, reconcileView{MatchMode: "contains", Description: "ΠΛΗΡΩΜΗ VΙSΑ", Branch: "96"}, v)
	})

	t.Run("empty match mode renders as exact", func(t *testing.T) {
		v := toReconcileView(&transaction.ReconcileConfig{Description: "X", Branch: "96"})
		require.Equal(t, "exact", v.MatchMode)
	})
}

// reconcileForm builds a multipart.Form carrying only visa.* fields.
func reconcileForm(desc, mode, branch string) *multipart.Form {
	return &multipart.Form{Value: map[string][]string{
		"visa.description": {desc},
		"visa.matchMode":   {mode},
		"visa.branch":      {branch},
	}}
}

func TestParseReconcile(t *testing.T) {
	t.Run("blank description is off", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm("   ", "exact", "96"))
		require.NoError(t, err)
		require.Nil(t, rc)
	})

	t.Run("missing fields are off", func(t *testing.T) {
		rc, err := parseReconcile(&multipart.Form{Value: map[string][]string{}})
		require.NoError(t, err)
		require.Nil(t, rc)
	})

	t.Run("valid fields build a config", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm(" ΠΛΗΡΩΜΗ VΙSΑ ", "exact", " 96 "))
		require.NoError(t, err)
		require.NotNil(t, rc)
		require.Equal(t, "ΠΛΗΡΩΜΗ VΙSΑ", rc.Description)
		require.Equal(t, transaction.MatchExact, rc.MatchMode)
		require.Equal(t, "96", rc.Branch)
	})

	t.Run("invalid match mode is a VISA-prefixed error", func(t *testing.T) {
		rc, err := parseReconcile(reconcileForm("X", "fuzzy", "96"))
		require.Error(t, err)
		require.Nil(t, rc)
		require.Contains(t, err.Error(), "VISA reconciliation:")
	})
}