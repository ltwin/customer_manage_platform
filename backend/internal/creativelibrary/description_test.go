package creativelibrary_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestDescriptionWriteLimitAndLegacyMetadata(t *testing.T) {
	db, a, _ := setup(t)
	for _, n := range []int{1000, 1001} {
		desc := strings.Repeat("🙂", n)
		_, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "描述", Description: desc, Content: textDraft("正文")}))
		if n == 1000 && err != nil || n == 1001 && !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("create %d: %v", n, err)
		}
		err = creativelibrary.ValidateImportTarget(creativelibrary.ImportTarget{Title: "描述", Description: desc})
		if n == 1000 && err != nil || n == 1001 && !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("import %d: %v", n, err)
		}
	}
	asset := createAsset(t, a, "旧素材", nil)
	legacy := strings.Repeat("长", 2000)
	if _, err := db.Exec("UPDATE creative_assets SET description=$1 WHERE account_id='library-a' AND id=$2", legacy, asset.ID); err != nil {
		t.Fatal(err)
	}
	title := "改标题"
	r, err := creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: asset.ID, ExpectedRevision: asset.Revision, Title: &title}))
	result := decode[creativelibrary.LibraryResult](t, r, err)
	if current(t, a, asset.ID).Description != legacy {
		t.Fatal("legacy description changed")
	}
	_, err = creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: asset.ID, ExpectedRevision: result.Revision, Description: &legacy}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("explicit long update: %v", err)
	}
}
