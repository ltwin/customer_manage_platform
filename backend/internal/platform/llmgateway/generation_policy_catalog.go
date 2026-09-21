package llmgateway

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// GenerationPolicyCatalog 管理单个受信部署的活动策略，配置源只负责向Publish提供JSON。
// 调用方负责发布权限及持久版本仓库；实例最多记住256个版本摘要，达到上限后须更换目录实例。
// 已获得的编译策略仍可用于历史恢复；Snapshot只决定新请求能否使用活动版本。
type GenerationPolicyCatalog struct {
	mu         sync.RWMutex
	current    *CompiledGenerationPolicy
	revision   uint64
	validUntil time.Time
	versions   map[string][32]byte
}

// Publish 先编译再按目录修订CAS发布，防止慢编译覆盖其他发布者的新配置。
// 首次发布expectedRevision为0；now由控制面传入，求值器不隐式读取墙钟。
func (c *GenerationPolicyCatalog) Publish(raw []byte, target GenerationPolicyTarget, expectedRevision uint64, validUntil, now time.Time) (uint64, error) {
	if now.IsZero() || !validUntil.After(now) {
		return 0, fmt.Errorf("%w: generation policy validity", ErrValidation)
	}
	candidate, err := CompileGenerationPolicy(raw, target)
	if err != nil {
		return 0, err
	}
	hash := sha256.Sum256(candidate.raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revision != expectedRevision {
		return 0, fmt.Errorf("%w: generation policy publication revision", ErrConflict)
	}
	if c.current != nil {
		previous := c.current.target
		if previous.ModelKey != target.ModelKey || previous.ModelRevision != target.ModelRevision || previous.DeploymentID != target.DeploymentID || previous.AdapterVersion != target.AdapterVersion {
			return 0, fmt.Errorf("%w: generation catalog target", ErrConflict)
		}
		// 同一Adapter版本的硬保证不可悄悄变化；修改保证须发布新Adapter版本和目录。
		if !generationTargetsEqual(previous, candidate.target) {
			return 0, fmt.Errorf("%w: generation catalog capabilities", ErrConflict)
		}
	}
	previous, exists := c.versions[candidate.Version()]
	if exists && previous != hash {
		return 0, fmt.Errorf("%w: immutable generation policy version", ErrConflict)
	}
	if !exists && len(c.versions) >= 256 {
		return 0, fmt.Errorf("%w: generation policy version capacity", ErrValidation)
	}
	if c.versions == nil {
		c.versions = make(map[string][32]byte)
	}
	c.versions[candidate.Version()] = hash
	c.current = candidate
	c.validUntil = validUntil
	c.revision++
	return c.revision, nil
}
func (c *GenerationPolicyCatalog) Snapshot(now time.Time) (*CompiledGenerationPolicy, uint64, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if now.IsZero() || c.current == nil || !now.Before(c.validUntil) {
		return nil, c.revision, fmt.Errorf("%w: generation policy unavailable or expired", ErrCapability)
	}
	return c.current, c.revision, nil
}
func generationTargetsEqual(a, b GenerationPolicyTarget) bool {
	if len(a.Modes) != len(b.Modes) || len(a.Usage) != len(b.Usage) {
		return false
	}
	modes := make(map[string]bool, len(a.Modes))
	for _, m := range a.Modes {
		modes[m] = true
	}
	for _, m := range b.Modes {
		if !modes[m] {
			return false
		}
	}
	for k, v := range a.Usage {
		other, ok := b.Usage[k]
		if !ok || other.Unit != v.Unit || (other.UpperBound == nil) != (v.UpperBound == nil) || other.UpperBound != nil && *other.UpperBound != *v.UpperBound {
			return false
		}
	}
	return true
}
