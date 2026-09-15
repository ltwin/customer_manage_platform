package versionedfstest

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// VersionIdentity is the one thing adapters are allowed to disagree about, and
// callers have to know which they are holding: it decides whether two writers
// racing to publish the same bytes end up owning one object or two.
type VersionIdentity int

const (
	// ContentAddressed names a version after its bytes. One key plus one byte
	// sequence is one version however many times it is published, so a caller
	// deleting "its own" copy deletes the other writer's copy as well.
	ContentAddressed VersionIdentity = iota
	// PerWrite mints a version per publish. Identical bytes written twice are
	// two objects, each deleted on its own.
	//
	// The OSS adapter is one of these in this deployment, and not by accident:
	// it requires a versioning-enabled bucket to get a fixed version id at all,
	// and on such a bucket OSS ignores the ForbidOverwrite header, so a second
	// PutObject to one key adds a version rather than failing. See
	// docs/dev/object-storage.md. It is not a subject of this suite only
	// because running it needs credentials.
	PerWrite
)

const suiteKey = "conformance/objects/sample"

// RunAdapterSuite asserts the contract both consumers of the port rely on.
// open must return a fresh, empty adapter each call.
func RunAdapterSuite(t *testing.T, identity VersionIdentity, open func(*testing.T) versionedfs.Adapter) {
	t.Helper()
	t.Run("PublishedBytesReadBackAtTheirVersion", func(t *testing.T) {
		adapter := open(t)
		body := []byte("一份冻结的资源正文")
		version := publish(t, adapter, suiteKey, body)
		stat, err := adapter.StatVersion(t.Context(), suiteKey, version)
		if err != nil {
			t.Fatal(err)
		}
		if stat.Size != int64(len(body)) || stat.Version != version {
			t.Fatalf("stat %+v does not describe the published object", stat)
		}
		if got := read(t, adapter, suiteKey, version, nil); !bytes.Equal(got, body) {
			t.Fatalf("read back %q", got)
		}
	})

	t.Run("ADeclaredSizeThatDoesNotMatchTheBodyIsRefused", func(t *testing.T) {
		adapter := open(t)
		body := []byte("0123456789")
		// The declared size is the contract. Accepting either mismatch would
		// publish an object whose recorded length is a lie.
		if _, err := adapter.PublishVerified(t.Context(), suiteKey, bytes.NewReader(body), int64(len(body))+1, "text/plain"); err == nil {
			t.Fatal("a body shorter than its declaration was accepted")
		}
		if _, err := adapter.PublishVerified(t.Context(), suiteKey, bytes.NewReader(body), int64(len(body))-1, "text/plain"); err == nil {
			t.Fatal("a body longer than its declaration was accepted")
		}
	})

	t.Run("ADeletedVersionIsGoneAndDeletingAgainIsNotAnError", func(t *testing.T) {
		adapter := open(t)
		version := publish(t, adapter, suiteKey, []byte("待回收"))
		if err := adapter.DeleteExact(t.Context(), suiteKey, version); err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.StatVersion(t.Context(), suiteKey, version); !errors.Is(err, versionedfs.ErrNotFound) {
			t.Fatalf("deleted version still reported: %v", err)
		}
		// Reclamation reruns after a crash it cannot see the outcome of, so a
		// second delete has to be a no-op rather than a failure it must classify.
		if err := adapter.DeleteExact(t.Context(), suiteKey, version); err != nil {
			t.Fatalf("second delete: %v", err)
		}
	})

	t.Run("RangesAreInclusiveAndBoundedByTheObject", func(t *testing.T) {
		adapter := open(t)
		body := []byte("0123456789")
		version := publish(t, adapter, suiteKey, body)
		if got := read(t, adapter, suiteKey, version, &versionedfs.ByteRange{Start: 2, End: 5}); string(got) != "2345" {
			t.Fatalf("inclusive range returned %q", got)
		}
		for _, rng := range []versionedfs.ByteRange{
			{Start: 0, End: int64(len(body))},
			{Start: 5, End: 4},
			{Start: -1, End: 3},
		} {
			if _, _, err := adapter.OpenVersion(t.Context(), suiteKey, version, &rng); !errors.Is(err, versionedfs.ErrRange) {
				t.Fatalf("range %+v: %v", rng, err)
			}
		}
	})

	t.Run("OneKeysVersionsAreNotAnotherKeys", func(t *testing.T) {
		adapter := open(t)
		body := []byte("同样的字节，不同的键")
		mine := publish(t, adapter, suiteKey, body)
		theirs := publish(t, adapter, suiteKey+"-other", body)
		if err := adapter.DeleteExact(t.Context(), suiteKey, mine); err != nil {
			t.Fatal(err)
		}
		if got := read(t, adapter, suiteKey+"-other", theirs, nil); !bytes.Equal(got, body) {
			t.Fatal("deleting one key's object reached another key")
		}
	})

	t.Run("VersionIdentityFollowsTheDeclaredContract", func(t *testing.T) {
		adapter := open(t)
		body := []byte("两次发布，同样的字节")
		first := publish(t, adapter, suiteKey, body)
		second := publish(t, adapter, suiteKey, body)
		switch identity {
		case ContentAddressed:
			if first != second {
				t.Fatalf("content-addressed adapter minted %s and %s for one byte sequence", first, second)
			}
			// The consequence, stated as a fact rather than left to the reader:
			// there is one object, so one delete ends it for both writers. Any
			// caller that publishes before claiming ownership must reconcile
			// before it cleans up.
			if err := adapter.DeleteExact(t.Context(), suiteKey, first); err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.StatVersion(t.Context(), suiteKey, second); !errors.Is(err, versionedfs.ErrNotFound) {
				t.Fatalf("one delete left the shared object behind: %v", err)
			}
		case PerWrite:
			if first == second {
				t.Fatalf("per-write adapter reused version %s", first)
			}
			if err := adapter.DeleteExact(t.Context(), suiteKey, first); err != nil {
				t.Fatal(err)
			}
			if got := read(t, adapter, suiteKey, second, nil); !bytes.Equal(got, body) {
				t.Fatal("deleting one write's object took the other's with it")
			}
		}
	})

	t.Run("StagedPartsAssembleIntoOneReadableVersion", func(t *testing.T) {
		adapter := open(t)
		writer, ok := adapter.(versionedfs.LocalPartWriter)
		if !ok {
			t.Skip("adapter takes part writes over the network, not through this seam")
		}
		session, err := adapter.InitMultipart(t.Context(), suiteKey, "application/octet-stream")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.AuthorizePart(t.Context(), suiteKey, session, 1, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		chunks := [][]byte{[]byte("前半段"), []byte("后半段")}
		for i, chunk := range chunks {
			if _, err := writer.WritePart(t.Context(), suiteKey, session, i+1, bytes.NewReader(chunk), 1<<20); err != nil {
				t.Fatal(err)
			}
		}
		// Completion is checked against what the store observed, never against
		// what the client says it uploaded.
		observed, err := adapter.ListParts(t.Context(), suiteKey, session)
		if err != nil {
			t.Fatal(err)
		}
		if len(observed) != len(chunks) {
			t.Fatalf("store observed %d parts", len(observed))
		}
		version, err := adapter.CompleteMultipart(t.Context(), suiteKey, session, observed)
		if err != nil {
			t.Fatal(err)
		}
		want := append(append([]byte(nil), chunks[0]...), chunks[1]...)
		if got := read(t, adapter, suiteKey, version, nil); !bytes.Equal(got, want) {
			t.Fatalf("assembled %q", got)
		}
	})
}

func publish(t *testing.T, adapter versionedfs.Adapter, key string, body []byte) string {
	t.Helper()
	version, err := adapter.PublishVerified(t.Context(), key, bytes.NewReader(body), int64(len(body)), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func read(t *testing.T, adapter versionedfs.Adapter, key, version string, rng *versionedfs.ByteRange) []byte {
	t.Helper()
	stream, _, err := adapter.OpenVersion(t.Context(), key, version, rng)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	body, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
