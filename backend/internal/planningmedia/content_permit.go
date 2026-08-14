package planningmedia

// ContentPermit is an opaque, process-local grant to stream display content.
// It is never serialized onto the wire and cannot be reconstructed from refs.
type ContentPermit struct {
	_  [0]func()
	id string
}

// BindContentPermit constructs a permit for trusted planningmedia adapters.
// Anonymous HTTP handlers must not call this.
func BindContentPermit(id string) ContentPermit {
	return ContentPermit{id: id}
}

// ID returns the internal pin identity for trusted adapters only.
func (p ContentPermit) ID() string { return p.id }
