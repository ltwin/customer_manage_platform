package creativeskill

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"slices"
	"strconv"
)

// framed writes length-prefixed fields so two different field splits can never
// produce the same hash input. hash.Hash never reports a write error.
func framed(h hash.Hash, fields ...string) {
	for _, f := range fields {
		h.Write([]byte(strconv.Itoa(len(f))))
		h.Write([]byte(":"))
		h.Write([]byte(f))
	}
}

// resourceFields returns the declared resources in one stable order. Reordering
// the same files is not a different version, so the digest must not say it is.
func resourceFields(resources []ResourceDeclaration) []string {
	sorted := slices.Clone(resources)
	slices.SortFunc(sorted, func(a, b ResourceDeclaration) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
	fields := make([]string, 0, len(sorted)*4)
	for _, r := range sorted {
		fields = append(fields, r.Path, r.Mime, strconv.FormatInt(r.ByteSize, 10), r.SHA256)
	}
	return fields
}

// contentDigest is the identity of a frozen version: the answer to "which text,
// declaration and files ran". Execution status is deliberately absent, because
// switching a version off does not change what it says.
func contentDigest(displayName, description, instructions string, manifest []byte, resources []ResourceDeclaration) string {
	h := sha256.New()
	framed(h, "creative-skill-version", strconv.Itoa(versionSchemaVersion),
		displayName, description, instructions, string(manifest),
		strconv.Itoa(len(resources)))
	framed(h, resourceFields(resources)...)
	return hex.EncodeToString(h.Sum(nil))
}

// requestHash covers the whole publication intent, not just the content: two
// imports that freeze identical text but disagree about which skill they extend
// or whether to activate it are different requests, and one operation id must
// not be able to mean both.
func requestHash(r ImportRequest, manifest []byte) string {
	h := sha256.New()
	activate := "0"
	if r.Activate {
		activate = "1"
	}
	framed(h, "creative-skill-import", r.Origin, r.Slug,
		strconv.FormatInt(int64(r.ExpectedSkillRevision), 10), activate,
		contentDigest(r.DisplayName, r.Description, r.Instructions, manifest, r.Resources))
	return hex.EncodeToString(h.Sum(nil))
}
