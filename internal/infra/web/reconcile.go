package web

import (
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// reconcileView is the VISA-reconciliation section of the config editor, with
// MatchMode defaulted to "exact" for the template. A nil stored config renders
// as a blank section (reconciliation off).
type reconcileView struct {
	MatchMode   string
	Description string
	Branch      string
}

// toReconcileView converts the stored config into an editor view. A nil config
// (reconciliation off) renders as a blank section with MatchMode "exact".
func toReconcileView(c *transaction.ReconcileConfig) reconcileView {
	if c == nil {
		return reconcileView{MatchMode: string(transaction.MatchExact)}
	}
	mode := string(c.MatchMode)
	if mode == "" {
		mode = string(transaction.MatchExact)
	}
	return reconcileView{
		MatchMode:   mode,
		Description: c.Description,
		Branch:      c.Branch,
	}
}

// parseReconcile reads the visa.* form fields into a config. A blank Description
// means reconciliation is off and returns (nil, nil). Otherwise the config is
// validated, returning a "VISA reconciliation: ..." error on the first problem.
func parseReconcile(form *multipart.Form) (*transaction.ReconcileConfig, error) {
	desc := strings.TrimSpace(valueAt(form.Value["visa.description"], 0))
	if desc == "" {
		return nil, nil
	}
	cfg := &transaction.ReconcileConfig{
		Description: desc,
		MatchMode:   transaction.MatchMode(valueAt(form.Value["visa.matchMode"], 0)),
		Branch:      strings.TrimSpace(valueAt(form.Value["visa.branch"], 0)),
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("VISA reconciliation: %s", err)
	}
	return cfg, nil
}