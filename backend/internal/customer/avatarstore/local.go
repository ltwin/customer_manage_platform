// Package avatarstore 保留既有 import 路径，转发到中性 avatarmedia.Local。
package avatarstore

import "github.com/samson/customer-manage-platform/backend/internal/avatarmedia"

type Local = avatarmedia.Local

func NewLocal(root string) (*Local, error) {
	return avatarmedia.NewLocal(root)
}
