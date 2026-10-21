package creativeagent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func imageBytesLabel(size int64) string {
	return fmt.Sprintf("%.2f MiB（%d 字节）", float64(size)/(1<<20), size)
}

// omitLimitedImage tells both the photographer and the model what was omitted.
// The persisted notice does not depend on the model repeating the explanation.
// One source key per revision makes retries and repeated tool reads idempotent.
func (h *runtimeHandler) omitLimitedImage(ctx context.Context, source *schema.Message, message *llmgateway.Message, id, code, reason string) error {
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitDispatchInTx(ctx, tx, h.held, nil); err != nil {
			return err
		}
		_, err := writeBlockInTx(ctx, tx, h.held.RunID, "image-limit:"+id, blockChange{
			Type: BlockNotice, State: "complete",
			Set: noticePayload{Code: code, Message: reason + "该图片未发送给助手。请压缩、替换图片或减少本轮图片引用；对话可以继续。"},
		})
		return err
	})
	if err != nil {
		return err
	}
	delete(source.Extra, imageRefsKey)
	if facts, ok := withoutThumbnail(source.Content, "image_limit"); ok {
		source.Content = facts
	} else {
		source.Content = referenceLabel(id) + "（" + reason + "该图片未加载，无法判断画面内容；在图片或引用发生变化前，不要重复读取。）"
	}
	message.Blocks = []llmgateway.Block{{Kind: llmgateway.BlockText, Text: source.Content}}
	return nil
}
