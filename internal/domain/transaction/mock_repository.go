package transaction

import (
	"context"

	"github.com/stretchr/testify/mock"
)

// MockRepository is a hand-written testify mock of Repository.
type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) GetAll(ctx context.Context) ([]Transaction, error) {
	args := m.Called(ctx)
	var txns []Transaction
	if v := args.Get(0); v != nil {
		txns = v.([]Transaction)
	}
	return txns, args.Error(1)
}
