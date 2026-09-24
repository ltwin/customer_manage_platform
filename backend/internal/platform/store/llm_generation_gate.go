package store

import (
	"context"
	"fmt"
)

// LockGenerationDeploymentGate 是生成部署的准入/阻断同步门（llmgateway G2）：
// 新准入的阻断检查与超界争议的阻断写入在同一把 advisory transaction lock 上
// 串行。预算桶按月分行，跨月的核算事务与新准入不共享行锁，这把门是两者
// 唯一的稳定同步边界。它只提供串行性，不是权限判断；哈希冲突只会额外串行，
// 不影响正确性。锁随事务结束释放。
func (sc TxAccountScope) LockGenerationDeploymentGate(ctx context.Context, deploymentID string) error {
	if sc.AccountID() == "" {
		return ErrEmptyAccountScope
	}
	_, err := sc.scope.execRunner().Exec(ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended('llm-generation-deployment:' || $1::text || ':' || $2::text, 0))",
		sc.AccountID(), deploymentID)
	if err != nil {
		return fmt.Errorf("lock generation deployment gate: %w", err)
	}
	return nil
}
