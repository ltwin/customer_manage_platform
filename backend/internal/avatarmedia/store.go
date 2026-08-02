package avatarmedia

import (
	"context"
	"io"
	"time"
)

type ObjectMeta struct {
	MediaType  string
	Size       int64
	Checksum   string
	ModifiedAt time.Time
}

type PutResult struct {
	Meta    ObjectMeta
	Created bool
}

type ObjectItem struct {
	Key  Key
	Meta ObjectMeta
}

type ObjectPage struct {
	Items      []ObjectItem
	NextCursor Cursor
	Done       bool
}

// ObjectStore 是中性不可变头像对象端口；公开入口以 typed Key／Prefix／Cursor 为准。
type ObjectStore interface {
	PutImmutable(context.Context, Key, Content, ObjectMeta) (PutResult, error)
	Open(context.Context, Key) (io.ReadCloser, error)
	Stat(context.Context, Key) (ObjectMeta, error)
	List(context.Context, Prefix, Cursor, int) (ObjectPage, error)
	Delete(context.Context, Key) error
}

// ObjectInventory 供 exact-generation backup／verify 使用；不属于最小 ObjectStore port。
type ObjectInventory interface {
	Inventory(context.Context) ([]ObjectItem, error)
}

func SameObjectMeta(left, right ObjectMeta) bool {
	return left.MediaType == right.MediaType && left.Size == right.Size && left.Checksum == right.Checksum
}
