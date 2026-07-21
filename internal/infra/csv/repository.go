package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
)

const (
	signDebit         = "Χ" // U+03A7 Greek capital Chi
	signCredit        = "Π" // U+03A0 Greek capital Pi
	columns           = 8
	visaColumns       = 6
	visaStatusPending = "Σε επεξεργασία"
)

// visaHeaderCols are the leading VISA-statement columns used to distinguish a
// VISA export from a bank export. Only the leading columns are checked, and
// trailing whitespace is tolerated.
var visaHeaderCols = []string{"Ημ/νία συναλλαγής", "Αιτιολογία", "Κατηγορία δαπάνης"}

// isVISAHeader reports whether a CSV header row is a VISA statement header.
func isVISAHeader(rec []string) bool {
	if len(rec) < len(visaHeaderCols) {
		return false
	}
	for i, w := range visaHeaderCols {
		if strings.TrimSpace(rec[i]) != w {
			return false
		}
	}
	return true
}

// Repository loads transactions from semicolon-separated Greek CSV exports
// found in a directory.
type Repository struct {
	dir string
}

func New(dir string) *Repository {
	return &Repository{dir: dir}
}

func (r *Repository) GetAll(ctx context.Context) ([]transaction.Transaction, error) {
	// Pattern is a fixed literal, so Glob never returns ErrBadPattern here.
	matches, _ := filepath.Glob(filepath.Join(r.dir, "*.csv"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("no CSV files found in %s", r.dir)
	}

	var out []transaction.Transaction
	for _, path := range matches {
		txns, err := r.parseFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
		}
		out = append(out, txns...)
	}
	return out, nil
}

func (r *Repository) parseFile(path string) ([]transaction.Transaction, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = ';'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Bank/VISA exports are frequently saved with a leading UTF-8 BOM
	// (EF BB BF). It lands on the first field of the first record and is not
	// stripped by TrimSpace, so it would defeat VISA header detection and
	// corrupt the first column. Remove it once, up front.
	if len(records) > 0 && len(records[0]) > 0 {
		records[0][0] = strings.TrimPrefix(records[0][0], "\ufeff")
	}

	base := filepath.Base(path)
	isVISA := len(records) > 0 && isVISAHeader(records[0])
	var txns []transaction.Transaction
	for i, rec := range records {
		if i == 0 {
			continue // header
		}
		var (
			t  transaction.Transaction
			ok bool
		)
		if isVISA {
			t, ok = parseVISARow(rec, base, i+1)
		} else {
			t, ok = parseRow(rec, base, i+1)
		}
		if !ok {
			continue
		}
		txns = append(txns, t)
	}
	return txns, nil
}

func parseRow(rec []string, file string, line int) (transaction.Transaction, bool) {
	if len(rec) < columns {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "too few columns")
		return transaction.Transaction{}, false
	}

	date, err := parseDate(unwrap(rec[1]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}

	amount, err := parseAmount(unwrap(rec[6]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad amount")
		return transaction.Transaction{}, false
	}

	var isDebit bool
	switch unwrap(rec[7]) {
	case signDebit:
		isDebit = true
	case signCredit:
		isDebit = false
	default:
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad sign")
		return transaction.Transaction{}, false
	}

	return transaction.Transaction{
		ID:          unwrap(rec[5]),
		Date:        date,
		Description: unwrap(rec[2]),
		Amount:      amount,
		IsDebit:     isDebit,
		SourceFile:  file,
		Branch:      unwrap(rec[3]),
	}, true
}

// parseVISARow maps one VISA-statement row to a Transaction. Only negative
// amounts (real purchases) are kept; positive rows are card payments (the
// mirror of the bank lump) and are skipped so they are not double-counted.
func parseVISARow(rec []string, file string, line int) (transaction.Transaction, bool) {
	if len(rec) < visaColumns {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "too few columns")
		return transaction.Transaction{}, false
	}

	// rec[0] is "DD/MM/YYYY HH:MM"; keep the date part.
	fields := strings.Fields(rec[0])
	if len(fields) == 0 {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}
	date, err := parseDate(fields[0])
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad date")
		return transaction.Transaction{}, false
	}

	amount, err := parseAmount(unwrap(rec[4]))
	if err != nil {
		slog.Warn("skipping malformed row", "file", file, "line", line, "reason", "bad amount")
		return transaction.Transaction{}, false
	}
	if amount >= 0 {
		return transaction.Transaction{}, false // card payment / non-expense: skip silently
	}

	desc := strings.TrimSpace(rec[1])
	if strings.TrimSpace(rec[5]) == visaStatusPending {
		desc += " *"
	}

	return transaction.Transaction{
		ID:          "VISA-" + strings.TrimSpace(rec[0]) + "-" + strings.TrimSpace(rec[1]),
		Date:        date,
		Description: desc,
		Amount:      -amount, // negative signed amount -> positive expense
		IsDebit:     true,
		SourceFile:  file,
		IsVISA:      true,
		Kind:        strings.TrimSpace(rec[3]), // Είδος συναλλαγής
	}, true
}

// unwrap strips the spreadsheet ="..." wrapper from a CSV field.
func unwrap(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	return strings.Trim(s, `"`)
}

// parseAmount converts a Greek-formatted amount (1.550,00) to a float.
func parseAmount(s string) (float64, error) {
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}

// parseDate parses DD/MM/YYYY, tolerating non-zero-padded day/month.
func parseDate(s string) (time.Time, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}
	day, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	year, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}
