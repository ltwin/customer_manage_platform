package creativeops_test

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// An explanation adds words, not a new kind of error: callers that map the
// error keep mapping it, and anyone who wants the words can find them.
func TestExplainKeepsTheErrorItWraps(t *testing.T) {
	err := creativeops.Explain(creativeops.ErrValidation, "节点 %s 太宽", "n1")
	var public creativeops.Public
	if !errors.Is(err, creativeops.ErrValidation) || !errors.As(err, &public) || public.PublicMessage() != "节点 n1 太宽" {
		t.Fatalf("%v", err)
	}
}
