package creativeagent

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// readAccess is everything a read tool asks of whoever calls it. A run answers
// from its consent, canvas, conversation and library grant; an external agent
// connection's grant covers the whole account. The tools themselves are one
// implementation for both (epic creative-mcp-server X-I4).
type readAccess interface {
	// readable says whether this call may read a content revision. library
	// lets a library grant count, for reads that name an asset.
	readable(ctx context.Context, tx store.TxAccountScope, revisionID string, library bool) (bool, error)
	// consent gives the data classes the caller may receive.
	consent(ctx context.Context, tx store.TxAccountScope) (Consent, error)
	// imageCapable says whether the caller can look at images at all.
	imageCapable() bool
	// libraryAllowed refuses library reads the caller is not granted.
	libraryAllowed(ctx context.Context, metadata bool) error
}

// runReadAccess is a run's authorization, unchanged from before the split.
type runReadAccess struct{ h *runtimeHandler }

func (a runReadAccess) readable(ctx context.Context, tx store.TxAccountScope, id string, library bool) (bool, error) {
	return readableRevisionInTx(ctx, tx, a.h.held.RunID, a.h.held.ConsentID, id, library)
}

func (a runReadAccess) consent(ctx context.Context, tx store.TxAccountScope) (Consent, error) {
	return readConsentForUpdate(ctx, tx, a.h.held.ConsentID)
}

func (a runReadAccess) imageCapable() bool { return a.h.held.Model.Capability.ImageInput }

func (a runReadAccess) libraryAllowed(ctx context.Context, metadata bool) error {
	return a.h.requireLibrary(ctx, metadata)
}

// externalReadAccess is an external agent connection's authorization: the
// consent page granted the whole account, so everything the account owns and
// may display is readable. Ownership and display rights are still checked
// where content is read (creativecontent.RequireUsable runs under the
// account's scope), so another account's revision is simply not found.
type externalReadAccess struct{}

func (externalReadAccess) readable(context.Context, store.TxAccountScope, string, bool) (bool, error) {
	return true, nil
}

func (externalReadAccess) consent(context.Context, store.TxAccountScope) (Consent, error) {
	return Consent{Purpose: "external_agent", Scope: ConsentScope{Mode: ScopeAccountLibrary, DataClasses: dataClasses}}, nil
}

// The external agent decides itself whether its model can see images.
func (externalReadAccess) imageCapable() bool { return true }

func (externalReadAccess) libraryAllowed(context.Context, bool) error { return nil }
