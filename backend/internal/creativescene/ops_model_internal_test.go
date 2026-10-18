package creativescene

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// A committed model must still be referenced by the scene the batch produces,
// not merely resolved by some operation: otherwise the document layer finds a
// retained revision outside the snapshot and fails with a type contract error.
func TestFinishRequiresCommittedModelsInTheResult(t *testing.T) {
	env := newOpEnv(t.Context(), store.TxAccountScope{}, nil, creativedocument.Current{})
	env.refs["pillar"] = creativecontent.Revision{ID: "ccrv_pillar", ContentID: "ccnt_pillar"}
	env.order = []string{"pillar"}
	placed := Scene{Objects: []Object{{ID: "sobj_a", Kind: KindModelInstance, Model: &ModelRef{ContentRevisionID: "ccrv_pillar"}}}}
	if res, err := env.finish(placed); err != nil || len(res["models"]) == 0 {
		t.Fatalf("finish with the model placed: %v %s", err, res["models"])
	}
	if _, err := env.finish(Scene{}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("finish without the model: %v", err)
	}
}
