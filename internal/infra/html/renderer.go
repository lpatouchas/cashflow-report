package html

import (
	"context"
	"html/template"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Renderer writes a Summary to an HTML file at a fixed path.
type Renderer struct {
	path string
}

func New(path string) *Renderer {
	return &Renderer{path: path}
}

type viewData struct {
	GeneratedAt string
	Summary     transaction.Summary
}

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"euro":   formatEuro,
	"month":  monthLabel,
	"months": monthsLabel,
}).Parse(reportHTML))

func (r *Renderer) Render(ctx context.Context, summary transaction.Summary) error {
	f, err := os.Create(r.path)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, viewData{
		GeneratedAt: time.Now().Format("2006-01-02 15:04"),
		Summary:     summary,
	})
}

func monthLabel(mb transaction.MonthlyBreakdown) string {
	return mb.Month.String() + " " + strconv.Itoa(mb.Year)
}

// monthsLabel renders a month count with correct singular/plural wording,
// e.g. "1 month" or "3 months".
func monthsLabel(n int) string {
	if n == 1 {
		return "1 month"
	}
	return strconv.Itoa(n) + " months"
}

// formatEuro renders a value as Greek-locale currency: €1.234,56.
func formatEuro(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	dot := strings.IndexByte(s, '.')
	intPart, dec := s[:dot], s[dot+1:]

	var b strings.Builder
	n := len(intPart)
	for i, ch := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(ch)
	}
	return sign + "€" + b.String() + "," + dec
}

const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Finance Report</title>
<style>
body { font-family: system-ui, sans-serif; margin: 2rem; color: #1a1a1a; }
.cards { display: flex; gap: 1rem; margin: 1rem 0 2rem; }
.card { flex: 1; padding: 1rem 1.5rem; border-radius: 8px; background: #f4f4f5; }
.card .value { font-size: 1.6rem; font-weight: 700; }
.card.savings { background: #e8f5e9; }
table { border-collapse: collapse; width: 100%; }
th, td { padding: .5rem .75rem; text-align: right; border-bottom: 1px solid #e4e4e7; }
th:first-child, td:first-child { text-align: left; }
caption { text-align: left; font-weight: 600; margin-bottom: .5rem; }
</style>
</head>
<body>
<h1>Finance Report</h1>
<p>Generated {{ .GeneratedAt }}</p>

<div class="cards">
  <div class="card"><div>Total Income</div><div class="value">{{ euro .Summary.TotalIncome }}</div></div>
  <div class="card"><div>Total Expenses</div><div class="value">{{ euro .Summary.TotalExpenses }}</div></div>
  <div class="card savings"><div>Savings</div><div class="value">{{ euro .Summary.Savings }}</div></div>
</div>

{{ if gt .Summary.Averages.Months 0 }}
<h2>Monthly Average <small>(over {{ months .Summary.Averages.Months }})</small></h2>
<div class="cards">
  <div class="card"><div>Avg Income / mo</div><div class="value">{{ euro .Summary.Averages.Income }}</div></div>
  <div class="card"><div>Avg Expenses / mo</div><div class="value">{{ euro .Summary.Averages.Expenses }}</div></div>
  <div class="card savings"><div>Avg Savings / mo</div><div class="value">{{ euro .Summary.Averages.Savings }}</div></div>
</div>
{{ end }}

{{ if .Summary.ByMonth }}
<table>
  <caption>Monthly Breakdown</caption>
  <thead><tr><th>Month</th><th>Income</th><th>Expenses</th><th>Savings</th></tr></thead>
  <tbody>
  {{ range .Summary.ByMonth }}
    <tr>
      <td>{{ month . }}</td>
      <td>{{ euro .Income }}</td>
      <td>{{ euro .Expenses }}</td>
      <td>{{ euro .Savings }}</td>
    </tr>
  {{ end }}
  </tbody>
</table>
{{ else }}
<p>No transactions to report.</p>
{{ end }}
</body>
</html>
`
