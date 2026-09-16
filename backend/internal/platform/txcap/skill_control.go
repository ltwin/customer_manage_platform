package txcap

import "context"

// SkillControlOps is the trusted set of skill-version control operations the
// store binds to one physical transaction.
//
// These rows do carry an account column — unlike the shared provider admission
// record — but the account they carry is the *publisher's*, and the transaction
// asking about them belongs to a reader. A platform skill exists precisely so
// that one account can run what another published, so an account-scoped query
// could never answer this question, and an account-scoped lock key could never
// serialise the two sides against each other.
//
// What the view exposes is therefore deliberately narrow: the control facts
// that decide whether a version may start work, and nothing that a reader is
// not already entitled to see. Instructions, resources and object addresses
// stay behind the skill package's own ports.
type SkillControlOps struct {
	// Lock takes the version's control lock. The key is the version id alone,
	// because the publisher disabling a version and the reader dispatching it
	// are in different accounts by construction.
	Lock func(ctx context.Context, versionID string) error
	// Read returns the control facts of one version, or ok=false when no such
	// version exists at all.
	Read func(ctx context.Context, versionID string) (SkillControl, bool, error)
}

// SkillControl is what the control row says about starting work right now. It
// is not a snapshot of the version: a run already holds the frozen content, and
// what it is missing is only whether that content may still be started.
type SkillControl struct {
	SkillID        string
	OwnerAccountID string
	Origin         string
	// ExecutionStatus is the version's own switch; SkillAvailability is the
	// owning skill's. Either one being off stops new work, and they are
	// reported separately because they are two different decisions.
	ExecutionStatus   string
	SkillAvailability string
}

// SkillControlView is a sealed capability; only the store can produce one.
type SkillControlView interface {
	Lock(ctx context.Context, versionID string) error
	Read(ctx context.Context, versionID string) (SkillControl, bool, error)
	skillControlViewSeal()
}

type skillControlView struct{ ops SkillControlOps }

// BindSkillControlView is the store's entry point for the sealed capability.
func BindSkillControlView(ops SkillControlOps) SkillControlView { return skillControlView{ops: ops} }

func (skillControlView) skillControlViewSeal() {}

func (v skillControlView) Lock(ctx context.Context, versionID string) error {
	return v.ops.Lock(ctx, versionID)
}

func (v skillControlView) Read(ctx context.Context, versionID string) (SkillControl, bool, error) {
	return v.ops.Read(ctx, versionID)
}
