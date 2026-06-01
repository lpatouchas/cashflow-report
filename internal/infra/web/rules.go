package web

import (
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// ruleView is one row in the rules editor, with IsDebit flattened to a select
// value ("any" | "debit" | "credit") for the template.
type ruleView struct {
	MatchMode   string
	Debit       string
	Description string
	SourceFile  string
}

// toRuleViews converts stored specs into editor rows.
func toRuleViews(specs []transaction.RuleSpec) []ruleView {
	views := make([]ruleView, 0, len(specs))
	for _, s := range specs {
		debit := "any"
		if s.IsDebit != nil {
			if *s.IsDebit {
				debit = "debit"
			} else {
				debit = "credit"
			}
		}
		mode := string(s.MatchMode)
		if mode == "" {
			mode = string(transaction.MatchExact)
		}
		views = append(views, ruleView{
			MatchMode:   mode,
			Debit:       debit,
			Description: s.Description,
			SourceFile:  s.SourceFile,
		})
	}
	return views
}

// parseRules reads the parallel rule.* form columns into specs. Fully blank
// rows (no description and no source file) are skipped; remaining rows are
// validated, returning a 1-based "Rule N" error on the first invalid row.
func parseRules(form *multipart.Form) ([]transaction.RuleSpec, error) {
	modes := form.Value["rule.matchMode"]
	debits := form.Value["rule.isDebit"]
	descs := form.Value["rule.description"]
	srcs := form.Value["rule.sourceFile"]

	var specs []transaction.RuleSpec
	for i := range descs {
		desc := strings.TrimSpace(descs[i])
		src := strings.TrimSpace(valueAt(srcs, i))
		if desc == "" && src == "" {
			continue
		}

		spec := transaction.RuleSpec{
			MatchMode:   transaction.MatchMode(valueAt(modes, i)),
			Description: desc,
			SourceFile:  src,
		}
		switch valueAt(debits, i) {
		case "debit":
			v := true
			spec.IsDebit = &v
		case "credit":
			v := false
			spec.IsDebit = &v
		}

		if err := spec.Validate(); err != nil {
			return nil, fmt.Errorf("Rule %d: %s", i+1, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// valueAt returns s[i] or "" when i is out of range.
func valueAt(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}
