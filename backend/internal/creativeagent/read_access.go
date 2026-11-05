package creativeagent

import (
	"context"
	"errors"

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
	// search_assets is offered without the grant for its built-in source; an
	// account search in a run that started without it is refused, not a
	// run-ending egress request. A grant revoked mid-run still ends it.
	err := a.h.requireLibrary(ctx, metadata)
	if errors.Is(err, ErrEgressRequired) && !a.h.libraryEnabled {
		return refuseTool("这个对话没有资产库授权，查不了账号里的素材；内置材质用 source: \"builtin\" 查")
	}
	return err
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
