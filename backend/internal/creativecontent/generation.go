package creativecontent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// generationGate 是生成用途在声明层的权利检查。创作声明命名空间表达
// 不了授权素材的出站标志，所以在该契约扩展存在之前授权素材在这里一律
// 拒绝；摄影师自有素材通过。
func generationGate(in planningmedia.RightsDeclarationInput) error {
	return planningmedia.ValidatePurpose(in, planningmedia.PurposeGenerationReference)
}

// GrantGenerationReference 记录摄影师的显式同意：把该修订声明的内容用作
// 外部生成参考。授权绑定账户与声明；每次读取与后续每次派发都会重查，
// 撤回在下一次守卫生效。这是内容同意，不是按次派发的出站许可——后者
// 仍留在后续切片的 Gateway 最终准入。幂等：已存在的存活授权保持不动。
func GrantGenerationReference(ctx context.Context, scope store.AccountScope, contentRevisionID string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		r, _, err := requireRevisionState(ctx, tx, tx.QueryRowForUpdate, contentRevisionID, generationGate)
		if err != nil {
			return err
		}
		granted, err := tx.Exists(ctx, "creative_usage_grants", "declaration_id=$2 AND purpose='generation_reference' AND revoked_at IS NULL", r.DeclarationID)
		if err != nil {
			return err
		}
		if !granted {
			if err := tx.Insert(ctx, "creative_usage_grants", []string{"id", "declaration_id", "purpose", "evidence"}, "ccug_"+uuid.NewString(), r.DeclarationID, "generation_reference", json.RawMessage(`{"source":"generation_reference_grant"}`)); err != nil {
				return err
			}
		}
		return requireRoot(ctx, tx, r)
	})
}

// RevokeGenerationReference 撤回该修订声明的生成同意。已通过守卫的进行
// 中读取不受影响；下一次守卫调用被拒。幂等。声明行先加锁，并发授权
// 无法在撤回返回后再塞进一行存活授权。
func RevokeGenerationReference(ctx context.Context, scope store.AccountScope, contentRevisionID string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		// 与读取守卫同一锁序：先内容/声明，后修订。
		var declarationID string
		err := tx.QueryRow(ctx, "creative_content_revisions", "rights_declaration_id", "id=$2", contentRevisionID).Scan(&declarationID)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.QueryRowForUpdate(ctx, "creative_rights_declarations", "id", "id=$2", declarationID).Scan(new(string)); err != nil {
			return err
		}
		_, err = tx.Update(ctx, "creative_usage_grants", "revoked_at=clock_timestamp()", "declaration_id=$2 AND purpose='generation_reference' AND revoked_at IS NULL", declarationID)
		return err
	})
}
