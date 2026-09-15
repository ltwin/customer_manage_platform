package creativeskill

import "testing"

// The digest answers "which text, declaration and files ran". Listing the same
// files in another order is not a different answer, and two different splits of
// the same characters must never fold into one.
func TestContentDigestIsOrderStableAndUnambiguous(t *testing.T) {
	manifest := []byte(`{"manifest_schema_version":1}`)
	a := declarations("b.md", "a.md")
	b := declarations("a.md", "b.md")
	if contentDigest("名称", "说明", "正文", manifest, a) != contentDigest("名称", "说明", "正文", manifest, b) {
		t.Fatal("reordering the same files must not change the version identity")
	}
	if contentDigest("名称", "说明", "正文", manifest, a) == contentDigest("名称", "说明", "正文", manifest, a[:1]) {
		t.Fatal("dropping a declared file must change the version identity")
	}
	// Without length framing these two would hash the same bytes.
	if contentDigest("ab", "c", "正文", manifest, nil) == contentDigest("a", "bc", "正文", manifest, nil) {
		t.Fatal("a different split of the same characters must be a different digest")
	}
	// The manifest decides which tools a run may reach, so a version that
	// widened its allowlist must not be able to keep the old identity.
	widened := []byte(`{"manifest_schema_version":1,"tool_allowlist":["write_nodes@1"]}`)
	if contentDigest("名称", "说明", "正文", manifest, a) == contentDigest("名称", "说明", "正文", widened, a) {
		t.Fatal("changing the declaration must change the version identity")
	}
}

// The request hash covers intent, not just content: the same frozen text aimed
// at a different skill line or with a different activation decision is a
// different request and must not replay under one operation id.
func TestRequestHashSeparatesIntentFromContent(t *testing.T) {
	manifest := []byte(`{"manifest_schema_version":1}`)
	base := ImportRequest{Origin: "platform", Slug: "a", DisplayName: "n", Description: "d", Instructions: "i"}
	activated := base
	activated.Activate = true
	if requestHash(base, manifest) == requestHash(activated, manifest) {
		t.Fatal("activation is part of the request")
	}
	extended := base
	extended.ExpectedSkillRevision = 3
	if requestHash(base, manifest) == requestHash(extended, manifest) {
		t.Fatal("which revision is being extended is part of the request")
	}
}

// declarations keeps every field a function of the path, so listing the same
// files in another order really is the same set and the test measures ordering
// rather than a difference it introduced itself.
func declarations(paths ...string) []ResourceDeclaration {
	list := make([]ResourceDeclaration, 0, len(paths))
	for _, p := range paths {
		list = append(list, ResourceDeclaration{Path: p, Mime: "text/markdown", ByteSize: int64(len(p)), SHA256: p})
	}
	return list
}
