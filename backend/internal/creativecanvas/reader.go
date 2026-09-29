package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// canvasReader is shared by read snapshots and command transactions. It cannot
// acquire write locks or mutate the graph.
type canvasReader interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
}
