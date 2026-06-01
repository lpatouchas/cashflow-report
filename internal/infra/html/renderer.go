package html

import (
	"context"
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
	Amount float64 `json:"amt"` // signed: income +, expense −
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

const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>The Monthly Review — Finance</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@0,6..72,300;0,6..72,400;0,6..72,500;0,6..72,600;1,6..72,300;1,6..72,400&family=Archivo:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
:root {
  --serif: 'Newsreader', Georgia, serif;
  --sans: 'Archivo', system-ui, sans-serif;
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body { background: #2a2824; font-family: var(--sans); }

/* ---------- EDITION THEMES ---------- */
.paper {
  --bg: #f4f2ec; --sheet: #fbfaf6; --ink: #1c1a14; --muted: #746f63;
  --rule: #ddd8cc; --hair: #e9e5db; --accent: #1c1a14; --accent-soft: #eceae2;
  --pos: #2f5d3a; --neg: #8a3a2b;
  --c-exp: #1c1a14; --c-inc: #9a937f; --c-sav: #2f5d3a; --c-trend: #b7ae9c;
  --shadow: 0 1px 0 rgba(0,0,0,.04), 0 30px 60px -40px rgba(40,35,20,.4);
}
.paper[data-edition="almanac"] {
  --bg: #f3e7d4; --sheet: #fcf6ea; --ink: #261d15; --muted: #8a7a64;
  --rule: #e3d4ba; --hair: #ece0cb; --accent: #8a2b22; --accent-soft: #f0e2d0;
  --pos: #8a2b22; --neg: #b06a2a;
  --c-exp: #8a2b22; --c-inc: #c2a079; --c-sav: #3f6b46; --c-trend: #d2b48f;
  --shadow: 0 1px 0 rgba(0,0,0,.05), 0 30px 60px -40px rgba(80,40,20,.45);
}
.paper[data-edition="nocturne"] {
  --bg: #15140f; --sheet: #1c1b15; --ink: #efebdf; --muted: #948e7e;
  --rule: #34322a; --hair: #2a2922; --accent: #cba86a; --accent-soft: #2a271d;
  --pos: #cba86a; --neg: #c77b54;
  --c-exp: #cba86a; --c-inc: #6f6a59; --c-sav: #7fa886; --c-trend: #514d3e;
  --shadow: 0 30px 80px -40px rgba(0,0,0,.8);
}

.paper {
  min-height: 100vh;
  background: var(--bg);
  color: var(--ink);
  padding: clamp(20px, 4vw, 64px) 16px;
  transition: background .4s ease, color .4s ease;
}
.sheet {
  max-width: 980px; margin: 0 auto;
  background: var(--sheet);
  box-shadow: var(--shadow);
  padding: clamp(28px, 5vw, 72px);
}

/* ---------- MASTHEAD ---------- */
.masthead { margin-bottom: clamp(28px, 5vw, 52px); }
.mast-top {
  display: flex; justify-content: space-between; align-items: center;
  gap: 16px; flex-wrap: wrap; margin-bottom: 22px;
}
.kicker {
  font-family: var(--sans); font-weight: 600; font-size: 12px;
  letter-spacing: .22em; text-transform: uppercase; color: var(--muted);
}
.editions { display: flex; align-items: center; gap: 6px; }
.editions-lbl {
  font-family: var(--sans); font-size: 10px; letter-spacing: .18em;
  text-transform: uppercase; color: var(--muted); margin-right: 4px;
}
.ed-btn {
  font-family: var(--sans); font-size: 12px; font-weight: 600;
  letter-spacing: .04em; color: var(--muted);
  background: transparent; border: 1px solid var(--rule);
  padding: 6px 12px; cursor: pointer; border-radius: 999px;
  transition: all .2s ease;
}
.ed-btn:hover { color: var(--ink); border-color: var(--ink); }
.ed-btn.on { color: var(--sheet); background: var(--ink); border-color: var(--ink); }
.paper[data-edition="nocturne"] .ed-btn.on { color: var(--bg); background: var(--accent); border-color: var(--accent); }

.mast-title {
  font-family: var(--serif); font-weight: 400;
  font-size: clamp(42px, 8vw, 88px); line-height: .96;
  letter-spacing: -0.02em; margin: 0 0 20px;
  font-optical-sizing: auto;
}
.mast-rule {
  display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap;
  border-top: 2px solid var(--ink); border-bottom: 1px solid var(--rule);
  padding: 9px 0; font-family: var(--sans); font-size: 11.5px;
  letter-spacing: .04em; color: var(--muted); text-transform: uppercase;
}
.mast-rule span:nth-child(2) { color: var(--ink); text-transform: none; letter-spacing: 0; font-style: normal; }

/* ---------- HERO STATS ---------- */
.hero {
  display: grid; grid-template-columns: repeat(3, 1fr);
  border-bottom: 1px solid var(--rule);
  margin-bottom: clamp(30px, 5vw, 54px);
}
.stat { padding: 26px 0 30px; border-left: 1px solid var(--hair); padding-left: 22px; }
.stat:first-child { border-left: none; padding-left: 0; }
.stat-label {
  font-family: var(--sans); font-size: 11px; font-weight: 600;
  letter-spacing: .16em; text-transform: uppercase; color: var(--muted);
  margin-bottom: 14px;
}
.stat-value {
  font-family: var(--serif); font-weight: 400;
  font-size: clamp(28px, 4.6vw, 46px); line-height: 1;
  letter-spacing: -0.015em; font-variant-numeric: tabular-nums;
}
.stat.accent .stat-value { color: var(--accent); }
.stat-sub {
  font-family: var(--sans); font-size: 12.5px; color: var(--muted);
  margin-top: 10px; font-variant-numeric: tabular-nums;
}
.stat.lead { position: relative; }
.stat.lead::before {
  content: '★'; position: absolute; top: 26px; right: 0;
  font-size: 12px; color: var(--accent); opacity: .6;
}

/* ---------- FEATURE / CHART ---------- */
.feature { margin-bottom: clamp(30px, 5vw, 54px); }
.fig { margin: 0; }
.fig-cap {
  display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap;
  font-family: var(--sans); font-size: 13px; color: var(--muted);
  margin-bottom: 18px;
}
.fig-num {
  font-weight: 700; letter-spacing: .1em; text-transform: uppercase;
  font-size: 11px; color: var(--ink);
  border: 1px solid var(--rule); padding: 3px 7px;
}
.fig-cap > span:nth-child(2) { font-style: italic; font-family: var(--serif); font-size: 15px; color: var(--ink); }
.fig-trend { margin-left: auto; font-weight: 600; font-size: 11px; letter-spacing: .06em; text-transform: uppercase; }
.fig-trend.down { color: var(--pos); }
.fig-trend.up { color: var(--neg); }

.chart-controls { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; }
.toggle {
  display: inline-flex; align-items: center; gap: 7px;
  font-family: var(--sans); font-size: 12px; font-weight: 500;
  color: var(--muted); background: transparent;
  border: 1px solid var(--rule); border-radius: 999px;
  padding: 5px 12px 5px 9px; cursor: pointer; transition: all .18s ease;
}
.toggle:hover { border-color: var(--ink); color: var(--ink); }
.toggle.on { color: var(--ink); border-color: var(--ink); }
.toggle:not(.on) .swatch { opacity: .3; }
.swatch { width: 11px; height: 11px; border-radius: 50%; display: inline-block; background: var(--muted); }
.swatch.expenses { background: var(--c-exp); }
.swatch.income { background: var(--c-inc); }
.swatch.savings { background: var(--c-sav); }
.swatch.trendsw { border-radius: 0; height: 0; width: 13px; border-top: 2px dashed var(--c-trend); align-self: center; }
.toggle.trend .swatch.trendsw { border-top-color: var(--ink); }

.chart-wrap { position: relative; width: 100%; }
.chart { width: 100%; height: auto; display: block; aspect-ratio: 1000 / 440; overflow: visible; }
.grid { stroke: var(--hair); stroke-width: 1; }
.axis-y, .axis-x {
  font-family: var(--sans); font-size: 12px; fill: var(--muted);
  font-variant-numeric: tabular-nums;
}
.series-line { fill: none; stroke-width: 2.4; vector-effect: non-scaling-stroke; }
.series-line.expenses { stroke: var(--c-exp); }
.series-line.income { stroke: var(--c-inc); }
.series-line.savings { stroke: var(--c-sav); }
.series-area { opacity: .07; }
.series-area.expenses { fill: var(--c-exp); }
.series-area.income { fill: var(--c-inc); }
.series-area.savings { fill: var(--c-sav); }
.trendline { fill: none; stroke: var(--c-trend); stroke-width: 1.6; stroke-dasharray: 6 5; vector-effect: non-scaling-stroke; }
.dot { stroke: var(--sheet); stroke-width: 1.5; }
.dot.expenses { fill: var(--c-exp); }
.dot.income { fill: var(--c-inc); }
.dot.savings { fill: var(--c-sav); }
.hover-guide { stroke: var(--ink); stroke-width: 1; stroke-dasharray: 3 3; opacity: .35; }

.tip {
  position: absolute; top: 14%; pointer-events: none;
  background: var(--ink); color: var(--sheet);
  padding: 11px 13px; min-width: 168px; z-index: 5;
  box-shadow: 0 14px 30px -12px rgba(0,0,0,.5);
}
.paper[data-edition="nocturne"] .tip { background: var(--accent-soft); color: var(--ink); border: 1px solid var(--rule); }
.tip-m { font-family: var(--serif); font-size: 15px; margin-bottom: 8px; }
.tip-row {
  display: flex; justify-content: space-between; gap: 18px;
  font-family: var(--sans); font-size: 12px; padding: 3px 0;
  font-variant-numeric: tabular-nums;
}
.tip-row > span:first-child { display: inline-flex; align-items: center; gap: 6px; opacity: .85; }
.tip .swatch { width: 8px; height: 8px; }
.tip-v { font-weight: 600; }
.tip-row.rate { border-top: 1px solid rgba(255,255,255,.18); margin-top: 4px; padding-top: 6px; opacity: .8; }
.paper[data-edition="nocturne"] .tip-row.rate { border-top-color: var(--rule); }

/* ---------- AVERAGES ---------- */
.averages { margin-bottom: clamp(30px, 5vw, 50px); }
.avg-head, .block-head {
  display: flex; align-items: baseline; gap: 12px;
  border-bottom: 1px solid var(--rule); padding-bottom: 10px; margin-bottom: 4px;
}
.avg-head h2, .block-head h2 {
  font-family: var(--serif); font-weight: 400; font-size: 22px;
  margin: 0; letter-spacing: -0.01em;
}
.block-num, .avg-head .block-num {
  font-family: var(--sans); font-weight: 700; font-size: 10px;
  letter-spacing: .14em; text-transform: uppercase; color: var(--muted);
}
.block-hint { margin-left: auto; font-family: var(--sans); font-size: 11.5px; color: var(--muted); font-style: italic; }
.avg-row { display: grid; grid-template-columns: repeat(3, 1fr); }
.avg {
  display: flex; flex-direction: column; gap: 6px; padding: 18px 0;
  border-left: 1px solid var(--hair); padding-left: 20px;
}
.avg:first-child { border-left: none; padding-left: 0; }
.avg-l { font-family: var(--sans); font-size: 11px; letter-spacing: .12em; text-transform: uppercase; color: var(--muted); }
.avg-v { font-family: var(--serif); font-size: 26px; font-variant-numeric: tabular-nums; }
.avg-v.strong { color: var(--accent); }

/* ---------- TABLE ---------- */
.ledger-table { width: 100%; border-collapse: collapse; margin-top: 8px; }
.ledger-table th {
  font-family: var(--sans); font-size: 11px; font-weight: 600;
  letter-spacing: .1em; text-transform: uppercase; color: var(--muted);
  padding: 12px 14px; cursor: pointer; user-select: none;
  border-bottom: 1px solid var(--rule); transition: color .15s;
}
.ledger-table th.r { text-align: right; }
.ledger-table th.l { text-align: left; }
.ledger-table th:hover, .ledger-table th.active { color: var(--ink); }
.th-in { display: inline-flex; align-items: center; gap: 5px; }
.th-in :last-child { margin-left: 2px; }
.sort-arrow { font-size: 8px; opacity: 0; transition: opacity .15s; }
.sort-arrow.show { opacity: 1; color: var(--accent); }

.ledger-table td {
  padding: 13px 14px; font-family: var(--sans); font-size: 14px;
  border-bottom: 1px solid var(--hair); font-variant-numeric: tabular-nums;
}
.ledger-table td.l { text-align: left; }
.ledger-table td.r { text-align: right; }
.ledger-table td.num { color: var(--ink); }
.ledger-table td.strong { font-weight: 600; }
.row { transition: background .15s; }
.row:hover { background: var(--accent-soft); }
.mcell { font-weight: 500; }
.tag {
  font-size: 9px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase;
  padding: 2px 6px; margin-left: 9px; vertical-align: middle; border-radius: 3px;
}
.tag.best { color: var(--pos); border: 1px solid var(--pos); }
.tag.worst { color: var(--neg); border: 1px solid var(--neg); }

.rate-cell { white-space: nowrap; }
.rate-bar {
  display: inline-block; width: 64px; height: 5px; background: var(--hair);
  vertical-align: middle; margin-right: 10px; position: relative; border-radius: 3px; overflow: hidden;
}
.rate-fill { position: absolute; left: 0; top: 0; bottom: 0; background: var(--c-sav); border-radius: 3px; }
.rate-num { font-size: 13px; color: var(--muted); }
.rate-num.solo { color: var(--ink); font-weight: 600; }

.ledger-table tfoot td {
  border-bottom: none; border-top: 2px solid var(--ink);
  font-weight: 600; font-size: 13px; padding-top: 15px;
}
.ledger-table tfoot td.l { font-family: var(--sans); letter-spacing: .04em; text-transform: uppercase; font-size: 11px; color: var(--muted); }

/* ---------- COLOPHON ---------- */
.colophon {
  display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap;
  margin-top: clamp(30px, 5vw, 48px); padding-top: 16px;
  border-top: 1px solid var(--rule);
  font-family: var(--sans); font-size: 11px; letter-spacing: .04em;
  text-transform: uppercase; color: var(--muted);
}
.colophon span:first-child { font-family: var(--serif); text-transform: none; letter-spacing: 0; font-style: italic; font-size: 14px; color: var(--ink); }

.empty {
  font-family: var(--serif); font-style: italic; font-size: 19px;
  color: var(--muted); padding: 48px 0; margin: 0;
}

@media (max-width: 680px) {
  .hero { grid-template-columns: 1fr 1fr; }
  .stat.lead { grid-column: span 2; border-left: none; padding-left: 0; border-top: 1px solid var(--hair); }
  .avg-row { grid-template-columns: 1fr; }
  .avg { border-left: none; padding-left: 0; border-top: 1px solid var(--hair); }
  .avg:first-child { border-top: none; }
  .ledger-table th, .ledger-table td { padding: 10px 8px; font-size: 12.5px; }
  .rate-bar { display: none; }
}

/* ---------- TRANSACTION MODAL ---------- */
.row.clickable { cursor: pointer; }
.row.clickable:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.tx-modal {
  position: fixed; inset: 0; z-index: 50;
  display: flex; align-items: center; justify-content: center; padding: 20px;
}
.tx-modal[hidden] { display: none; }
.tx-backdrop { position: absolute; inset: 0; background: rgba(20,18,12,.55); }
.tx-dialog {
  position: relative; z-index: 1; width: min(680px, 100%); max-height: 84vh;
  display: flex; flex-direction: column; overflow: hidden;
  background: var(--sheet); color: var(--ink); border: 1px solid var(--rule);
  box-shadow: 0 30px 80px -30px rgba(0,0,0,.6);
}
.tx-head {
  display: flex; align-items: baseline; gap: 12px;
  padding: 22px 24px 14px; border-bottom: 1px solid var(--rule);
}
.tx-title {
  font-family: var(--serif); font-weight: 400; font-size: 24px;
  margin: 0; letter-spacing: -0.01em;
}
.tx-close {
  margin-left: auto; font-size: 22px; line-height: 1; color: var(--muted);
  background: transparent; border: none; cursor: pointer; padding: 0 4px;
}
.tx-close:hover { color: var(--ink); }
.tx-totals {
  display: flex; gap: 22px; flex-wrap: wrap; padding: 12px 24px;
  font-family: var(--sans); font-size: 12px; color: var(--muted);
  border-bottom: 1px solid var(--hair); font-variant-numeric: tabular-nums;
}
.tx-totals b { color: var(--ink); font-weight: 600; }
.tx-accounts { padding: 12px 24px 14px; border-bottom: 1px solid var(--hair); }
.tx-accounts[hidden] { display: none; }
.tx-acc-head {
  font-family: var(--sans); font-size: 11px; font-weight: 600; letter-spacing: .1em;
  text-transform: uppercase; color: var(--muted); margin-bottom: 8px;
}
.tx-acc-row {
  display: flex; justify-content: space-between; gap: 16px; padding: 4px 0;
  font-family: var(--sans); font-size: 13px; font-variant-numeric: tabular-nums;
}
.tx-acc-name { color: var(--ink); }
.tx-acc-figs { display: inline-flex; gap: 16px; }
.tx-scroll { overflow-y: auto; }
.tx-table { width: 100%; border-collapse: collapse; }
.tx-table th {
  font-family: var(--sans); font-size: 11px; font-weight: 600; letter-spacing: .1em;
  text-transform: uppercase; color: var(--muted); padding: 12px 24px; cursor: pointer;
  user-select: none; border-bottom: 1px solid var(--rule);
  position: sticky; top: 0; background: var(--sheet); z-index: 1;
}
.tx-table th.r { text-align: right; }
.tx-table th.l { text-align: left; }
.tx-table th:hover, .tx-table th.active { color: var(--ink); }
.tx-table td {
  padding: 11px 24px; font-family: var(--sans); font-size: 13.5px;
  border-bottom: 1px solid var(--hair); font-variant-numeric: tabular-nums;
}
.tx-table td.r { text-align: right; }
.tx-table td.l { text-align: left; }
.tx-amt.pos { color: var(--c-sav); }
.tx-amt.neg { color: var(--neg); }
.tx-src { color: var(--muted); font-size: 12px; }

/* ---------- PRINT (paper-friendly Ledger palette) ---------- */
@media print {
  body { background: #fff; }
  .paper {
    --bg: #fff; --sheet: #fff; --ink: #1c1a14; --muted: #746f63;
    --rule: #ddd8cc; --hair: #e9e5db; --accent: #1c1a14; --accent-soft: #eceae2;
    --pos: #2f5d3a; --neg: #8a3a2b;
    --c-exp: #1c1a14; --c-inc: #9a937f; --c-sav: #2f5d3a; --c-trend: #b7ae9c;
    --shadow: none; padding: 0;
  }
  .sheet { box-shadow: none; max-width: none; padding: 0; }
  .editions, .chart-controls, .block-hint, .tx-modal { display: none !important; }
}
</style>
</head>
<body>
<div class="paper" data-edition="nocturne" id="paper">
  <div class="sheet">

    <header class="masthead">
      <div class="mast-top">
        <span class="kicker">Cashflow Report</span>
        <div class="editions">
          <span class="editions-lbl">Edition</span>
          <button class="ed-btn on" data-ed="nocturne" title="Dark · gold">Nocturne</button>
          <button class="ed-btn" data-ed="ledger" title="Ink on paper">Ledger</button>
          <button class="ed-btn" data-ed="almanac" title="Warm · oxblood">Almanac</button>
        </div>
      </div>
      <h1 class="mast-title">The Monthly Review</h1>
      <div class="mast-rule">
        <span>Vol. I</span>
        <span>A statement of income, outflow &amp; savings</span>
        <span>Generated {{ .Generated }}</span>
      </div>
    </header>

{{ if .HasData }}
    <section class="hero">
      <div class="stat">
        <div class="stat-label">Total Income</div>
        <div class="stat-value">{{ euro .Summary.TotalIncome }}</div>
        <div class="stat-sub">{{ euro .Summary.Averages.Income }} / mo</div>
      </div>
      <div class="stat">
        <div class="stat-label">Total Expenses</div>
        <div class="stat-value">{{ euro .Summary.TotalExpenses }}</div>
        <div class="stat-sub">{{ euro .Summary.Averages.Expenses }} / mo</div>
      </div>
      <div class="stat lead accent">
        <div class="stat-label">Total Savings</div>
        <div class="stat-value">{{ euro .Summary.Savings }}</div>
        <div class="stat-sub">{{ pct .TotalRate }} saved</div>
      </div>
    </section>

    <section class="feature">
      <figure class="fig">
        <figcaption class="fig-cap">
          <span class="fig-num">Fig. 1</span>
          <span>Monthly expenses across the last {{ months (len .Rows) }}, with linear trend</span>
          <span id="fig-trend" class="fig-trend {{ if .TrendDown }}down{{ else }}up{{ end }}">{{ if .TrendDown }}▼ trending down{{ else }}▲ trending up{{ end }}</span>
        </figcaption>
        <div class="chart-controls">
          <button class="toggle expenses on" data-series="expenses" aria-pressed="true"><span class="swatch expenses"></span>Expenses</button>
          <button class="toggle income" data-series="income" aria-pressed="false"><span class="swatch income"></span>Income</button>
          <button class="toggle savings" data-series="savings" aria-pressed="false"><span class="swatch savings"></span>Savings</button>
          <button id="toggle-trend" class="toggle trend on" aria-pressed="true"><span class="swatch trendsw"></span>Trend</button>
        </div>
        <div class="chart-wrap" id="chart-wrap"></div>
      </figure>
    </section>

{{ if gt .Summary.Averages.Months 0 }}
    <section class="averages">
      <div class="avg-head">
        <span class="block-num">Avg.</span>
        <h2>Monthly Average</h2>
        <span class="block-hint">over {{ months .Summary.Averages.Months }}</span>
      </div>
      <div class="avg-row">
        <div class="avg"><span class="avg-l">Income</span><span class="avg-v">{{ euro .Summary.Averages.Income }}</span></div>
        <div class="avg"><span class="avg-l">Expenses</span><span class="avg-v">{{ euro .Summary.Averages.Expenses }}</span></div>
        <div class="avg"><span class="avg-l">Savings</span><span class="avg-v strong">{{ euro .Summary.Averages.Savings }}</span></div>
      </div>
    </section>
{{ end }}

    <section class="table-section">
      <div class="table-block">
        <div class="block-head">
          <span class="block-num">Tbl. 1</span>
          <h2>Monthly Breakdown</h2>
          <span class="block-hint">Click a column to sort</span>
        </div>
        <table class="ledger-table" id="ledger">
          <thead>
            <tr>
              <th class="l active" data-key="key"><span class="th-in">Month<span class="sort-arrow show">▼</span></span></th>
              <th class="r" data-key="income"><span class="th-in">Income<span class="sort-arrow">▼</span></span></th>
              <th class="r" data-key="expenses"><span class="th-in">Expenses<span class="sort-arrow">▼</span></span></th>
              <th class="r" data-key="savings"><span class="th-in">Savings<span class="sort-arrow">▼</span></span></th>
              <th class="r" data-key="rate"><span class="th-in">Rate<span class="sort-arrow">▼</span></span></th>
            </tr>
          </thead>
          <tbody>
{{ range .Rows }}
            <tr class="row clickable{{ if .Best }} best{{ end }}{{ if .Worst }} worst{{ end }}" data-key="{{ .Key }}" data-income="{{ .Income }}" data-expenses="{{ .Expenses }}" data-savings="{{ .Savings }}" data-rate="{{ .Rate }}" tabindex="0" role="button" aria-label="View {{ .Label }} transactions">
              <td class="l mcell">{{ .Label }}{{ if .Best }}<span class="tag best">best</span>{{ end }}{{ if .Worst }}<span class="tag worst">lean</span>{{ end }}</td>
              <td class="r num">{{ euro .Income }}</td>
              <td class="r num">{{ euro .Expenses }}</td>
              <td class="r num strong">{{ euro .Savings }}</td>
              <td class="r rate-cell"><span class="rate-bar"><span class="rate-fill" style="width: {{ printf "%.1f" .RateWidth }}%"></span></span><span class="rate-num">{{ pct .Rate }}</span></td>
            </tr>
{{ end }}
          </tbody>
          <tfoot>
            <tr>
              <td class="l">Total · {{ months (len .Rows) }}</td>
              <td class="r num">{{ euro .Summary.TotalIncome }}</td>
              <td class="r num">{{ euro .Summary.TotalExpenses }}</td>
              <td class="r num strong">{{ euro .Summary.Savings }}</td>
              <td class="r rate-cell"><span class="rate-num solo">{{ pct .TotalRate }}</span></td>
            </tr>
          </tfoot>
        </table>
      </div>
    </section>

    <footer class="colophon">
      <span>The Monthly Review</span>
      <span>Best month {{ .BestShort }} · leanest {{ .WorstShort }}</span>
      <span>€ figures in EU format</span>
    </footer>
{{ else }}
    <p class="empty">No transactions to report.</p>
{{ end }}

  </div>

  <div class="tx-modal" id="tx-modal" hidden>
    <div class="tx-backdrop" data-close></div>
    <div class="tx-dialog" role="dialog" aria-modal="true" aria-labelledby="tx-title">
      <div class="tx-head">
        <h2 class="tx-title" id="tx-title"></h2>
        <button class="tx-close" data-close aria-label="Close">&times;</button>
      </div>
      <div class="tx-totals" id="tx-totals"></div>
      <div class="tx-accounts" id="tx-accounts" hidden>
        <div class="tx-acc-head">By Account</div>
        <div class="tx-acc-rows" id="tx-acc-rows"></div>
      </div>
      <div class="tx-scroll">
        <table class="tx-table" id="tx-table">
          <thead>
            <tr>
              <th class="l active" data-key="k"><span class="th-in">Date<span class="sort-arrow show">▼</span></span></th>
              <th class="l" data-key="desc"><span class="th-in">Description<span class="sort-arrow">▼</span></span></th>
              <th class="r" data-key="amt"><span class="th-in">Amount<span class="sort-arrow">▼</span></span></th>
              <th class="l" data-key="src"><span class="th-in">Source<span class="sort-arrow">▼</span></span></th>
            </tr>
          </thead>
          <tbody id="tx-body"></tbody>
        </table>
      </div>
    </div>
  </div>
</div>

<script>window.FIN = {{ .ChartJSON }};</script>
<script>
(function () {
  var paper = document.getElementById('paper');

  // ---- edition switcher (persisted) ----
  var edButtons = document.querySelectorAll('.ed-btn');
  edButtons.forEach(function (b) {
    b.addEventListener('click', function () {
      var id = b.getAttribute('data-ed');
      paper.setAttribute('data-edition', id);
      try { localStorage.setItem('fin-edition', id); } catch (e) {}
      edButtons.forEach(function (x) { x.classList.toggle('on', x === b); });
    });
  });
  try {
    var saved = localStorage.getItem('fin-edition');
    if (saved) {
      var savedBtn = document.querySelector('.ed-btn[data-ed="' + saved + '"]');
      if (savedBtn) savedBtn.click();
    }
  } catch (e) {}

  // ---- sortable table ----
  var table = document.getElementById('ledger');
  if (table) {
    var tbody = table.querySelector('tbody');
    var sort = { key: 'key', dir: 'desc' };
    var applySort = function () {
      var rows = Array.prototype.slice.call(tbody.querySelectorAll('tr'));
      rows.sort(function (a, b) {
        var av, bv;
        if (sort.key === 'key') { av = a.getAttribute('data-key'); bv = b.getAttribute('data-key'); }
        else { av = parseFloat(a.getAttribute('data-' + sort.key)); bv = parseFloat(b.getAttribute('data-' + sort.key)); }
        var c = av > bv ? 1 : av < bv ? -1 : 0;
        return sort.dir === 'asc' ? c : -c;
      });
      rows.forEach(function (r) { tbody.appendChild(r); });
      table.querySelectorAll('th').forEach(function (th) {
        var on = th.getAttribute('data-key') === sort.key;
        th.classList.toggle('active', on);
        var ar = th.querySelector('.sort-arrow');
        if (ar) { ar.classList.toggle('show', on); ar.textContent = sort.dir === 'asc' ? '▲' : '▼'; }
      });
    };
    table.querySelectorAll('th').forEach(function (th) {
      th.addEventListener('click', function () {
        var k = th.getAttribute('data-key');
        if (sort.key === k) { sort.dir = sort.dir === 'asc' ? 'desc' : 'asc'; }
        else { sort = { key: k, dir: 'desc' }; }
        applySort();
      });
    });
    applySort();
  }

  // ---- trend chart ----
  var wrap = document.getElementById('chart-wrap');
  var M = (window.FIN && window.FIN.months) || [];
  if (!wrap || !M.length) return;

  var W = 1000, H = 440, PAD = { t: 28, r: 28, b: 44, l: 64 };
  var iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b;
  var SERIES = [
    { key: 'expenses', label: 'Expenses', cls: 'expenses' },
    { key: 'income', label: 'Income', cls: 'income' },
    { key: 'savings', label: 'Savings', cls: 'savings' }
  ];
  var state = { vis: { expenses: true, income: false, savings: false }, trend: true, hover: null };

  var eu = function (v) {
    return (v < 0 ? '−€' : '€') + Math.abs(v).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  };
  var pct = function (v) {
    return (v * 100).toLocaleString('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + ' %';
  };
  var linReg = function (ys) {
    var n = ys.length, xs = ys.map(function (_, i) { return i; });
    var mx = xs.reduce(function (a, b) { return a + b; }, 0) / n;
    var my = ys.reduce(function (a, b) { return a + b; }, 0) / n;
    var num = 0, den = 0;
    for (var i = 0; i < n; i++) { num += (xs[i] - mx) * (ys[i] - my); den += (xs[i] - mx) * (xs[i] - mx); }
    var slope = den ? num / den : 0, inter = my - slope * mx;
    return xs.map(function (x) { return inter + slope * x; });
  };
  var esc = function (s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); };

  var render = function () {
    var active = SERIES.filter(function (s) { return state.vis[s.key]; });
    var lo = Infinity, hi = -Infinity;
    active.forEach(function (s) { M.forEach(function (m) { lo = Math.min(lo, m[s.key]); hi = Math.max(hi, m[s.key]); }); });
    if (!isFinite(lo)) { lo = 0; hi = 1; }
    var span = (hi - lo) || 1;
    lo = Math.max(0, lo - span * 0.18);
    hi = hi + span * 0.18;

    var x = function (i) { return PAD.l + (M.length === 1 ? iw / 2 : (i / (M.length - 1)) * iw); };
    var y = function (v) { return PAD.t + ih - ((v - lo) / (hi - lo)) * ih; };
    var pathOf = function (key) { return M.map(function (m, i) { return (i ? 'L' : 'M') + x(i).toFixed(1) + ',' + y(m[key]).toFixed(1); }).join(' '); };
    var areaOf = function (key) { return pathOf(key) + ' L' + x(M.length - 1).toFixed(1) + ',' + (PAD.t + ih).toFixed(1) + ' L' + x(0).toFixed(1) + ',' + (PAD.t + ih).toFixed(1) + ' Z'; };

    var trendYs = linReg(M.map(function (m) { return m.expenses; }));
    var trendPath = trendYs.map(function (v, i) { return (i ? 'L' : 'M') + x(i).toFixed(1) + ',' + y(v).toFixed(1); }).join(' ');

    var ticks = 4, grid = [];
    for (var t = 0; t <= ticks; t++) { grid.push(lo + (t / ticks) * (hi - lo)); }

    var svg = '<svg viewBox="0 0 ' + W + ' ' + H + '" class="chart" role="img" preserveAspectRatio="none">';
    grid.forEach(function (g) {
      svg += '<line class="grid" x1="' + PAD.l + '" x2="' + (W - PAD.r) + '" y1="' + y(g).toFixed(1) + '" y2="' + y(g).toFixed(1) + '"/>';
      svg += '<text class="axis-y" x="' + (PAD.l - 12) + '" y="' + (y(g) + 4).toFixed(1) + '" text-anchor="end">€' + Math.round(g).toLocaleString('de-DE') + '</text>';
    });
    M.forEach(function (m, i) {
      svg += '<text class="axis-x" x="' + x(i).toFixed(1) + '" y="' + (H - 16) + '" text-anchor="middle">' + esc(m.short) + '</text>';
    });
    active.forEach(function (s) {
      svg += '<path class="series-area ' + s.cls + '" d="' + areaOf(s.key) + '"/>';
      svg += '<path class="series-line ' + s.cls + '" d="' + pathOf(s.key) + '"/>';
    });
    if (state.trend) { svg += '<path class="trendline" d="' + trendPath + '"/>'; }
    if (state.hover != null) {
      svg += '<line class="hover-guide" x1="' + x(state.hover).toFixed(1) + '" x2="' + x(state.hover).toFixed(1) + '" y1="' + PAD.t + '" y2="' + (PAD.t + ih) + '"/>';
    }
    active.forEach(function (s) {
      M.forEach(function (m, i) {
        svg += '<circle class="dot ' + s.cls + '" cx="' + x(i).toFixed(1) + '" cy="' + y(m[s.key]).toFixed(1) + '" r="' + (state.hover === i ? 5 : 3.2) + '"/>';
      });
    });
    M.forEach(function (m, i) {
      svg += '<rect class="hit" data-i="' + i + '" x="' + (x(i) - iw / (M.length * 2)).toFixed(1) + '" y="' + PAD.t + '" width="' + (iw / M.length).toFixed(1) + '" height="' + ih + '" fill="transparent"/>';
    });
    svg += '</svg>';

    var tip = '';
    if (state.hover != null) {
      var m = M[state.hover];
      var leftPct = (x(state.hover) / W) * 100;
      var tx = leftPct > 62 ? '-100%' : leftPct < 12 ? '0' : '-50%';
      tip += '<div class="tip" style="left:' + leftPct.toFixed(2) + '%;transform:translateX(' + tx + ') translateY(-50%)">';
      tip += '<div class="tip-m">' + esc(m.label) + '</div>';
      active.forEach(function (s) {
        tip += '<div class="tip-row"><span><span class="swatch ' + s.cls + '"></span>' + s.label + '</span><span class="tip-v">' + eu(m[s.key]) + '</span></div>';
      });
      tip += '<div class="tip-row rate"><span>Savings rate</span><span class="tip-v">' + pct(m.rate) + '</span></div></div>';
    }

    wrap.innerHTML = svg + tip;
    wrap.querySelectorAll('.hit').forEach(function (r) {
      r.addEventListener('mouseenter', function () { state.hover = +r.getAttribute('data-i'); render(); });
    });
    var svgEl = wrap.querySelector('svg');
    svgEl.addEventListener('mouseleave', function () { state.hover = null; render(); });

    var dir = trendYs[trendYs.length - 1] - trendYs[0];
    var ft = document.getElementById('fig-trend');
    if (ft) {
      ft.className = 'fig-trend ' + (dir <= 0 ? 'down' : 'up');
      ft.textContent = dir <= 0 ? '▼ trending down' : '▲ trending up';
    }
  };

  document.querySelectorAll('.toggle[data-series]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var k = btn.getAttribute('data-series');
      state.vis[k] = !state.vis[k];
      btn.classList.toggle('on', state.vis[k]);
      btn.setAttribute('aria-pressed', state.vis[k]);
      render();
    });
  });
  var trendBtn = document.getElementById('toggle-trend');
  if (trendBtn) {
    trendBtn.addEventListener('click', function () {
      state.trend = !state.trend;
      trendBtn.classList.toggle('on', state.trend);
      trendBtn.setAttribute('aria-pressed', state.trend);
      render();
    });
  }

  render();
})();
</script>
<script>
(function () {
  var TX = (window.FIN && window.FIN.tx) || {};
  var ACCT = (window.FIN && window.FIN.acct) || {};
  var modal = document.getElementById('tx-modal');
  var ledger = document.getElementById('ledger');
  if (!modal || !ledger) return;

  var titleEl = document.getElementById('tx-title');
  var totalsEl = document.getElementById('tx-totals');
  var acctWrap = document.getElementById('tx-accounts');
  var acctRowsEl = document.getElementById('tx-acc-rows');
  var bodyEl = document.getElementById('tx-body');
  var tableEl = document.getElementById('tx-table');
  var lastTrigger = null;
  var rows = [];
  var sort = { key: 'k', dir: 'desc' };

  var eu = function (v) {
    return (v < 0 ? '−€' : '€') + Math.abs(v).toLocaleString('de-DE', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  };
  var esc = function (s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); };

  var renderRows = function () {
    var sorted = rows.slice().sort(function (a, b) {
      var av = a[sort.key], bv = b[sort.key], c;
      if (sort.key === 'amt') { c = av > bv ? 1 : av < bv ? -1 : 0; }
      else { c = String(av).localeCompare(String(bv)); }
      return sort.dir === 'asc' ? c : -c;
    });
    var html = '';
    sorted.forEach(function (t) {
      var cls = t.amt < 0 ? 'neg' : 'pos';
      html += '<tr><td class="l">' + esc(t.date) + '</td>' +
              '<td class="l">' + esc(t.desc) + '</td>' +
              '<td class="r tx-amt ' + cls + '">' + eu(t.amt) + '</td>' +
              '<td class="l tx-src">' + esc(t.src) + '</td></tr>';
    });
    bodyEl.innerHTML = html;
    tableEl.querySelectorAll('th').forEach(function (th) {
      var on = th.getAttribute('data-key') === sort.key;
      th.classList.toggle('active', on);
      th.setAttribute('aria-sort', on ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none');
      var ar = th.querySelector('.sort-arrow');
      if (ar) { ar.classList.toggle('show', on); ar.textContent = sort.dir === 'asc' ? '▲' : '▼'; }
    });
  };

  var open = function (tr) {
    var key = tr.getAttribute('data-key');
    rows = TX[key] || [];
    lastTrigger = tr;
    var mcell = tr.querySelector('.mcell');
    titleEl.textContent = mcell ? mcell.textContent.replace(/best|lean/gi, '').trim() : key;
    totalsEl.innerHTML =
      '<span>Income <b>' + eu(parseFloat(tr.getAttribute('data-income'))) + '</b></span>' +
      '<span>Expenses <b>' + eu(parseFloat(tr.getAttribute('data-expenses'))) + '</b></span>' +
      '<span>Savings <b>' + eu(parseFloat(tr.getAttribute('data-savings'))) + '</b></span>';
    var accts = ACCT[key] || [];
    if (accts.length) {
      var ah = '';
      accts.forEach(function (a) {
        ah += '<div class="tx-acc-row"><span class="tx-acc-name">' + esc(a.src) + '</span>' +
              '<span class="tx-acc-figs">' +
              '<span class="tx-amt pos">' + eu(a.inc) + '</span>' +
              '<span class="tx-amt neg">' + eu(-a.exp) + '</span>' +
              '</span></div>';
      });
      acctRowsEl.innerHTML = ah;
      acctWrap.hidden = false;
    } else {
      acctRowsEl.innerHTML = '';
      acctWrap.hidden = true;
    }
    sort = { key: 'k', dir: 'desc' };
    renderRows();
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
    var closeBtn = modal.querySelector('[data-close]');
    if (closeBtn && closeBtn.focus) closeBtn.focus();
  };

  var close = function () {
    modal.hidden = true;
    document.body.style.overflow = '';
    if (lastTrigger) lastTrigger.focus();
  };

  ledger.querySelectorAll('tbody tr').forEach(function (tr) {
    tr.addEventListener('click', function () { open(tr); });
    tr.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(tr); }
    });
  });
  modal.querySelectorAll('[data-close]').forEach(function (el) {
    el.addEventListener('click', close);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !modal.hidden) close();
  });
  tableEl.querySelectorAll('th').forEach(function (th) {
    th.addEventListener('click', function () {
      var k = th.getAttribute('data-key');
      if (sort.key === k) { sort.dir = sort.dir === 'asc' ? 'desc' : 'asc'; }
      else { sort = { key: k, dir: k === 'amt' ? 'desc' : 'asc' }; }
      renderRows();
    });
  });
})();
</script>
</body>
</html>
`
