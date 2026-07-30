package digest

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const digestReminderLimit = 20

type DigestMessageBuilder struct {
	snapshots SnapshotRepository
	renderer  Renderer
}

func NewDigestMessageBuilder(snapshots SnapshotRepository, renderer Renderer) DigestMessageBuilder {
	return DigestMessageBuilder{snapshots: snapshots, renderer: renderer}
}

func (b DigestMessageBuilder) Build(
	ctx context.Context,
	scope store.AccountScope,
	delivery Delivery,
) (string, error) {
	switch delivery.MessageKind {
	case MessageKindBindingAck:
		return "Telegram 绑定成功。你将从这里收到每日经营摘要。", nil
	case MessageKindTemporaryUnavailable:
		return "摘要暂时不可用，请稍后重试。", nil
	case MessageKindDigest:
		if delivery.TargetLocalDate == nil || delivery.TimezoneAtEnqueue == "" {
			return "", errors.New("digest delivery missing frozen target")
		}
		window, err := WindowFor(LocalTarget{
			LocalDate: *delivery.TargetLocalDate,
			Timezone:  delivery.TimezoneAtEnqueue,
		})
		if err != nil {
			return "", err
		}
		snapshot, err := b.snapshots.Load(ctx, scope, window, digestReminderLimit)
		if err != nil {
			return "", err
		}
		return b.renderer.Render(snapshot), nil
	default:
		return "", errors.New("unknown delivery message kind")
	}
}

var _ MessageBuilder = DigestMessageBuilder{}
