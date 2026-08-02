package avatarmedia

import "errors"

var (
	ErrObjectNotFound  = errors.New("avatar object not found")
	ErrObjectKey       = errors.New("invalid avatar object key")
	ErrObjectIntegrity = errors.New("avatar object integrity mismatch")
	ErrObjectTemporary = errors.New("temporary avatar object error")
	ErrValidation      = errors.New("validation_failed")
)

// ValidationError 是可映射到 HTTP 校验失败的中性错误；文案由调用方／characterization 锁定。
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }
