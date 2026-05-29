package transaction

import "context"

// Repository loads all transactions from a data source.
type Repository interface {
	GetAll(ctx context.Context) ([]Transaction, error)
}
