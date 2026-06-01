package report

import (
	"context"

	"github.com/lpatouchas/cashflow-report/internal/domain/transaction"
	"github.com/stretchr/testify/mock"
)

// MockRenderer is a hand-written testify mock of Renderer.
type MockRenderer struct {
	mock.Mock
}

func (m *MockRenderer) Render(ctx context.Context, summary transaction.Summary) error {
	args := m.Called(ctx, summary)
	return args.Error(0)
}
