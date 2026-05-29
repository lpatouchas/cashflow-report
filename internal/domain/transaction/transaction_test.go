package transaction

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func tx(id, file string, amount float64, debit bool, date time.Time) Transaction {
	return Transaction{ID: id, SourceFile: file, Amount: amount, IsDebit: debit, Date: date}
}

func TestFilterTransfers(t *testing.T) {
	d := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   []Transaction
		wantIDs []string
	}{
		{
			name:    "empty input",
			input:   nil,
			wantIDs: nil,
		},
		{
			name: "all unique are kept",
			input: []Transaction{
				tx("A", "f1.csv", 10, true, d),
				tx("B", "f2.csv", 20, false, d),
			},
			wantIDs: []string{"A", "B"},
		},
		{
			name: "cross-file transfer is excluded",
			input: []Transaction{
				tx("T", "f1.csv", 100, true, d),
				tx("T", "f2.csv", 100, false, d),
				tx("K", "f1.csv", 5, true, d),
			},
			wantIDs: []string{"K"},
		},
		{
			name: "single-file duplicate is excluded",
			input: []Transaction{
				tx("D", "f1.csv", 7, true, d),
				tx("D", "f1.csv", 7, true, d),
				tx("U", "f1.csv", 9, false, d),
			},
			wantIDs: []string{"U"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterTransfers(tc.input)
			var gotIDs []string
			for _, x := range got {
				gotIDs = append(gotIDs, x.ID)
			}
			require.Equal(t, tc.wantIDs, gotIDs)
		})
	}
}
