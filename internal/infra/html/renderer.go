package html

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

// Renderer writes a Summary as HTML to a destination: either a file path
// (NewFile) or an arbitrary io.Writer (NewWriter).
type Renderer struct {
	w    io.Writer // when non-nil, render here
	path string    // otherwise, create this file
}

// NewFile returns a Renderer that writes the report to a file at path.
func NewFile(path string) *Renderer { return &Renderer{path: path} }

// NewWriter returns a Renderer that writes the report to w.
func NewWriter(w io.Writer) *Renderer { return &Renderer{w: w} }

// rowVM is one month rendered into the breakdown table. The data-* attributes
// it carries let the client sort rows without re-querying the model.
type rowVM struct {
	Key       string
	Label     string
	Income    float64
	Expenses  float64
	Savings   float64
	Rate      float64
	RateWidth float64 // savings-rate bar width, percent of the best month
	Best      bool
	Worst     bool
}

// chartMonth is one point in the trend chart, serialized to JS as window.FIN.
type chartMonth struct {
	Label    string  `json:"label"`
	Short    string  `json:"short"`
	Income   float64 `json:"income"`
	Expenses float64 `json:"expenses"`
	Savings  float64 `json:"savings"`
	Rate     float64 `json:"rate"`
}

// txVM is one transaction line inside a month's detail modal, serialized to JS.
type txVM struct {
	Date   string  `json:"date"` // "12 May 2026", for display
	Sort   string  `json:"k"`    // "2026-05-12", for date sorting
	Desc   string  `json:"desc"`
	Cat    string  `json:"cat,omitempty"` // Κατηγορία δαπάνης (VISA only), shown beside the description
	Amount float64 `json:"amt"`           // signed: income +, expense −
	Source string  `json:"src"`
}

// acctVM is one account's income/expense totals inside a month's modal, serialized to JS.
type acctVM struct {
	Source   string  `json:"src"` // display label, .csv stripped
	Income   float64 `json:"inc"`
	Expenses float64 `json:"exp"`
}

type viewData struct {
	Generated  string
	Summary    transaction.Summary
	HasData    bool
	Rows       []rowVM
	TotalRate  float64
	BestShort  string
	WorstShort string
	TrendDown  bool
	ChartJSON  template.JS
}

//go:embed report.html
var reportHTML string

var tmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"euro":   formatEuro,
	"pct":    formatPct,
	"month":  monthLabel,
	"months": monthsLabel,
}).Parse(reportHTML))

func (r *Renderer) Render(ctx context.Context, summary transaction.Summary) error {
	if r.w != nil {
		return render(r.w, summary)
	}
	f, err := os.Create(r.path)
	if err != nil {
		return err
	}
	defer f.Close()
	return render(f, summary)
}

func render(w io.Writer, summary transaction.Summary) error {
	return tmpl.Execute(w, buildView(summary))
}

func buildView(summary transaction.Summary) viewData {
	vd := viewData{
		Generated: time.Now().Format("02 January 2006 · 15:04"),
		Summary:   summary,
		HasData:   len(summary.ByMonth) > 0,
		TotalRate: rateOf(summary.TotalIncome, summary.Savings),
	}

	// Chart series run oldest-to-newest; ByMonth is newest-first, so reverse it.
	n := len(summary.ByMonth)
	chart := make([]chartMonth, n)
	for i, mb := range summary.ByMonth {
		chart[n-1-i] = chartMonth{
			Label:    monthLabel(mb),
			Short:    monthShort(mb.Month),
			Income:   mb.Income,
			Expenses: mb.Expenses,
			Savings:  mb.Savings,
			Rate:     rateOf(mb.Income, mb.Savings),
		}
	}
	txByMonth := make(map[string][]txVM, n)
	for _, mb := range summary.ByMonth {
		key := fmt.Sprintf("%04d-%02d", mb.Year, int(mb.Month))
		lines := make([]txVM, len(mb.Transactions))
		for j, t := range mb.Transactions {
			amt := t.Amount
			if t.IsDebit {
				amt = -amt
			}
			lines[j] = txVM{
				Date:   t.Date.Format("02 January 2006"),
				Sort:   t.Date.Format("2006-01-02"),
				Desc:   t.Description,
				Cat:    t.Category,
				Amount: amt,
				Source: accountLabel(t.SourceFile),
			}
		}
		txByMonth[key] = lines
	}
	acctByMonth := make(map[string][]acctVM, n)
	for _, mb := range summary.ByMonth {
		key := fmt.Sprintf("%04d-%02d", mb.Year, int(mb.Month))
		accs := make([]acctVM, len(mb.ByAccount))
		for j, a := range mb.ByAccount {
			accs[j] = acctVM{
				Source:   accountLabel(a.Source),
				Income:   a.Income,
				Expenses: a.Expenses,
			}
		}
		acctByMonth[key] = accs
	}
	payload, _ := json.Marshal(map[string]any{"months": chart, "tx": txByMonth, "acct": acctByMonth})
	vd.ChartJSON = template.JS(payload)
	vd.TrendDown = trendingDown(chart)

	if !vd.HasData {
		return vd
	}

	bestI, worstI := 0, 0
	maxRate := 0.0
	for i, mb := range summary.ByMonth {
		if mb.Savings > summary.ByMonth[bestI].Savings {
			bestI = i
		}
		if mb.Savings < summary.ByMonth[worstI].Savings {
			worstI = i
		}
		if r := rateOf(mb.Income, mb.Savings); r > maxRate {
			maxRate = r
		}
	}
	vd.BestShort = monthShort(summary.ByMonth[bestI].Month)
	vd.WorstShort = monthShort(summary.ByMonth[worstI].Month)

	vd.Rows = make([]rowVM, n)
	for i, mb := range summary.ByMonth {
		rate := rateOf(mb.Income, mb.Savings)
		width := 0.0
		if maxRate > 0 && rate > 0 {
			width = rate / maxRate * 100
		}
		vd.Rows[i] = rowVM{
			Key:       fmt.Sprintf("%04d-%02d", mb.Year, int(mb.Month)),
			Label:     monthLabel(mb),
			Income:    mb.Income,
			Expenses:  mb.Expenses,
			Savings:   mb.Savings,
			Rate:      rate,
			RateWidth: width,
			Best:      i == bestI,
			Worst:     i == worstI && worstI != bestI,
		}
	}

	return vd
}

// rateOf returns savings as a fraction of income, guarding against income of 0.
func rateOf(income, savings float64) float64 {
	if income <= 0 {
		return 0
	}
	return savings / income
}

// trendingDown reports whether a least-squares fit of monthly expenses slopes
// downward (flat counts as down). Fewer than two points are treated as down.
func trendingDown(months []chartMonth) bool {
	n := len(months)
	if n < 2 {
		return true
	}
	var sx, sy, sxx, sxy float64
	for i, m := range months {
		x := float64(i)
		sx += x
		sy += m.Expenses
		sxx += x * x
		sxy += x * m.Expenses
	}
	// den is positive for any n >= 2 distinct indices, so no zero guard is needed.
	den := float64(n)*sxx - sx*sx
	slope := (float64(n)*sxy - sx*sy) / den
	return slope <= 0
}

func monthLabel(mb transaction.MonthlyBreakdown) string {
	return mb.Month.String() + " " + strconv.Itoa(mb.Year)
}

func monthShort(m time.Month) string {
	return m.String()[:3]
}

// accountLabel renders a source filename for display, dropping a trailing
// ".csv" extension (case-insensitive). Other names pass through unchanged.
func accountLabel(src string) string {
	if len(src) >= 4 && strings.EqualFold(src[len(src)-4:], ".csv") {
		return src[:len(src)-4]
	}
	return src
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

// formatPct renders a ratio as a Greek-locale percentage: 0.453 -> "45,3 %".
func formatPct(v float64) string {
	s := strconv.FormatFloat(v*100, 'f', 1, 64)
	return strings.Replace(s, ".", ",", 1) + " %"
}
