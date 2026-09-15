package creativeskill_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
)

// The digest of a frozen version is computed from the manifest as it was
// declared, and the same bytes reach the request_hash that a retried import is
// compared against. So the encoding is not an implementation detail: changing
// it retroactively re-hashes every import already recorded, and replaying a
// pinned operation id — which is how the seed import stays idempotent across
// deployments — would report a conflict that no retry can clear.
//
// This pins the encoding to a literal so that a change to the Manifest struct,
// its json tags, or the framing in digest.go fails here and has to be a
// decision rather than a side effect. If this test goes red, the question is
// not "what is the new digest" but "what happens to the imports already stored
// under the old one".
//
// The manifest below omits required_model_capabilities deliberately: an absent
// optional array is the case where a rendering rule is most tempting to apply
// before hashing, and Manifest.normalized documents why it is not.
func TestTheManifestEncodingIsAPersistedContract(t *testing.T) {
	f := setup(t)
	body := []byte("# 固定内容\n")
	req := creativeskill.ImportRequest{
		OperationID:  uuid.NewString(),
		Origin:       "platform",
		Slug:         "encoding-contract",
		DisplayName:  "编码契约",
		Description:  "这份请求的每个字段都是字面量，所以下面的 digest 只跟着编码走。",
		Instructions: "不要改这段文字。",
		Manifest: creativeskill.Manifest{
			ManifestSchemaVersion: 1,
			InputKinds:            []string{"text"},
			MaxInputs:             20,
			ToolAllowlist:         []string{"search_assets@1"},
			OutputContract:        "一段文字。",
			CompletionCheckKey:    "encoding_contract_v1",
		},
		Resources: []creativeskill.ResourceDeclaration{declare("references/fixed.md", "text/markdown", body)},
		Activate:  true,
	}
	const want = "3ef4103137d443a2d1ce3965b779ea1e4059129fa446ee904eabd4235c8060bd"
	version := publishBody(t, f, f.a, req, body)
	if version.Digest != want {
		t.Fatalf("the manifest encoding changed: digest=%q want=%q\n"+
			"every import recorded under the old encoding can no longer be replayed by its operation id",
			version.Digest, want)
	}
}

// An absent optional array and an explicitly empty one are the same
// declaration, so they must freeze as the same version identity. Without this
// the two spellings of "needs nothing special" would be two different skills.
func TestAnOmittedOptionalArrayIsStillRenderedAsAnArray(t *testing.T) {
	f := setup(t)
	req := newRequest()
	req.Manifest.RequiredModelCapabilities = nil
	version := publish(t, f, f.a, req)

	snapshot, err := f.svc.ResolveVersion(t.Context(), f.a, version.SkillID, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The API declares an array. A nil slice would serialise as null and the
	// client could not parse it, so the read path has to fill it in even though
	// the stored bytes — and the digest over them — keep the field absent.
	if snapshot.Manifest.RequiredModelCapabilities == nil {
		t.Fatal("an omitted optional array reached the client as null")
	}
	if len(snapshot.Manifest.RequiredModelCapabilities) != 0 {
		t.Fatalf("the read path invented capabilities: %+v", snapshot.Manifest.RequiredModelCapabilities)
	}

	page, err := f.svc.ListAccessibleSkills(t.Context(), f.a, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ToolAllowlist == nil {
		t.Fatalf("the directory read path did not normalise: %+v", page.Items)
	}
}
