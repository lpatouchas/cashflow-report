package report

import (
	"context"

	"github.com/lpatouchas/personal-finance/internal/domain/transaction"
)

// Renderer is the output port that writes a Summary to its destination.
type Renderer interface {
	Render(ctx context.Context, summary transaction.Summary) error
}
