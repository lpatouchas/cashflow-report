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
	signDebit  = "Χ" // U+03A7 Greek capital Chi
	signCredit = "Π" // U+03A0 Greek capital Pi
	columns    = 8
)

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

	base := filepath.Base(path)
	var txns []transaction.Transaction
	for i, rec := range records {
		if i == 0 {
			continue // header
		}
		t, ok := parseRow(rec, base, i+1)
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
