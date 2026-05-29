package transaction

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMockRepository(t *testing.T) {
	ctx := context.Background()

	t.Run("returns configured transactions", func(t *testing.T) {
		want := []Transaction{{ID: "A"}}
		m := &MockRepository{}
		m.On("GetAll", ctx).Return(want, nil)

		got, err := m.GetAll(ctx)
		require.NoError(t, err)
		require.Equal(t, want, got)
		m.AssertExpectations(t)
	})

	t.Run("returns nil slice and error", func(t *testing.T) {
		m := &MockRepository{}
		m.On("GetAll", ctx).Return(nil, errors.New("boom"))

		got, err := m.GetAll(ctx)
		require.Error(t, err)
		require.Nil(t, got)
		m.AssertExpectations(t)
	})
}
