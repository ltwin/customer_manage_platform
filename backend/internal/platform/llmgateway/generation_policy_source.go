package llmgateway

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GenerationPolicySource 只提供规则内容，不提供Adapter能力、发布权限或有效期。
// 后续配置平台实现此接口即可复用发布流程；实现应响应ctx并限制返回大小。
type GenerationPolicySource interface {
	Read(context.Context) ([]byte, error)
}

// GenerationPolicyFileSource 从服务端管理的本地普通文件读取配置。
// 路径不能来自外部请求；推荐临时文件写完后原子替换，避免读取到半份配置。
// 本实现没有轮询或文件监听，每次Read重新打开文件；文件系统I/O不能被ctx强制中断。
type GenerationPolicyFileSource struct{ path string }

var _ GenerationPolicySource = (*GenerationPolicyFileSource)(nil)

// NewGenerationPolicyFileSource 固定绝对路径，后续工作目录变化不会切换配置文件。
// 构造时不要求文件已存在，读取/发布时再检查；空路径不回退到内置示例。
func NewGenerationPolicyFileSource(path string) (*GenerationPolicyFileSource, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%w: generation policy file path required", ErrValidation)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve generation policy file: %w", err)
	}
	return &GenerationPolicyFileSource{path: absolute}, nil
}

func (s *GenerationPolicyFileSource) Read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.path == "" {
		return nil, fmt.Errorf("%w: generation policy file source required", ErrValidation)
	}
	// 打开前拒绝目录/管道/设备；部署目录由服务端管理，不能允许不受信写入者替换文件类型。
	info, err := os.Stat(s.path)
	if err != nil {
		return nil, fmt.Errorf("stat generation policy file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: generation policy requires a regular file", ErrValidation)
	}
	file, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open generation policy file: %w", err)
	}
	raw, readErr := readGenerationPolicyFile(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close generation policy file: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

func readGenerationPolicyFile(file *os.File) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect generation policy file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxGenerationPolicyBytes {
		return nil, fmt.Errorf("%w: generation policy file type or size", ErrValidation)
	}
	// 实际读取也设上限，不依赖可能在读取期间变化的文件大小。
	raw, err := io.ReadAll(io.LimitReader(file, maxGenerationPolicyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read generation policy file: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxGenerationPolicyBytes {
		return nil, fmt.Errorf("%w: generation policy file size", ErrValidation)
	}
	return raw, nil
}

// PublishFromSource 是文件和后续配置平台的共同入口，读取失败不会触碰活动目录。
// 不自动重试CAS冲突，也不续期；有效期和时钟由受信控制面在每次发布时明确提供。
func (c *GenerationPolicyCatalog) PublishFromSource(ctx context.Context, source GenerationPolicySource, target GenerationPolicyTarget, expectedRevision uint64, validUntil, now time.Time) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if c == nil || source == nil {
		return 0, fmt.Errorf("%w: generation policy catalog and source required", ErrValidation)
	}
	raw, err := source.Read(ctx)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Publish(raw, target, expectedRevision, validUntil, now)
}
