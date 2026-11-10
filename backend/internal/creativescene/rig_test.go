package creativescene

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// data: 模型内容「骨架与人形映射」— the vectors are shared with the
// frontend's rig/humanoid.ts (make generate copies them to
// rig/humanoid_rig.gen.json).

func TestHumanoidRigVectors(t *testing.T) {
	var doc struct {
		Skeletons map[string]creativeglb.Skeleton `json:"skeletons"`
		Cases     []struct {
			Name     string `json:"name"`
			Skeleton string `json:"skeleton"`
			Rig      Rig    `json:"rig"`
			Valid    bool   `json:"valid"`
			Field    string `json:"field"`
			Reason   string `json:"reason"`
		} `json:"cases"`
	}
	readJSON(t, "humanoid_rig.json", &doc)
	if len(doc.Cases) < 30 {
		t.Fatalf("%d cases", len(doc.Cases))
	}
	for _, c := range doc.Cases {
		sk, ok := doc.Skeletons[c.Skeleton]
		if !ok {
			t.Fatalf("%s: no skeleton %q", c.Name, c.Skeleton)
		}
		err := c.Rig.Validate(&sk)
		if c.Valid {
			if err != nil {
				t.Errorf("%s: %v", c.Name, err)
			}
			continue
		}
		var field *creativeops.FieldError
		if !errors.As(err, &field) || !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("%s: got %v, want %s %s", c.Name, err, c.Field, c.Reason)
			continue
		}
		if field.Field() != c.Field || field.Reason != c.Reason {
			t.Errorf("%s: got %s %s (%s), want %s %s", c.Name, field.Field(), field.Reason, field.Message, c.Field, c.Reason)
		}
	}
}

func TestRigWithoutSkeleton(t *testing.T) {
	var field *creativeops.FieldError
	err := (&Rig{}).Validate(nil)
	if !errors.As(err, &field) || field.Field() != "rig" || field.Reason != rigNoSkeleton {
		t.Fatalf("got %v", err)
	}
}

func TestHumanoidBoneTable(t *testing.T) {
	if len(humanoidBones) != 55 {
		t.Fatalf("%d bones", len(humanoidBones))
	}
	for _, b := range requiredBones {
		if b != "hips" && humanoidParent[b] == "" {
			t.Errorf("required %s is not a bone", b)
		}
	}
	for _, b := range humanoidBones[1:] {
		if !standardAncestor("hips", b) {
			t.Errorf("%s does not reach hips", b)
		}
	}
}
