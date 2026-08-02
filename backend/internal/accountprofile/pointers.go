package accountprofile

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// PointerSource 实现 avatarmedia 中性 PointerSource（account_profile 主体）。
type PointerSource struct {
	repo Repository
}

func NewPointerSource(repo Repository) PointerSource {
	return PointerSource{repo: repo}
}

// ListCurrent 列出当前账号资料头像 pointer；scope 必须是 store.AccountScope。
func (p PointerSource) ListCurrent(
	ctx context.Context,
	accountID string,
	scope any,
	cursor string,
	limit int,
) ([]avatarmedia.CurrentPointer, error) {
	accountScope, ok := scope.(store.AccountScope)
	if !ok {
		return nil, fmt.Errorf("account profile pointer source requires store.AccountScope")
	}
	if accountScope.AccountID() != accountID {
		return nil, fmt.Errorf("account scope mismatch")
	}
	profiles, err := p.repo.ListCurrentPointers(ctx, accountScope, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := make([]avatarmedia.CurrentPointer, 0, len(profiles))
	for _, profile := range profiles {
		if profile.AvatarVersion == nil || profile.AvatarObjectID == nil ||
			profile.AvatarMediaType == nil || profile.AvatarSize == nil {
			return nil, errors.New("incomplete current account profile avatar pointer")
		}
		out = append(out, avatarmedia.CurrentPointer{
			AccountID: accountID, SubjectKind: avatarmedia.SubjectAccountProfile, SubjectID: accountID,
			AvatarVersion: *profile.AvatarVersion, AvatarObjectID: *profile.AvatarObjectID,
			MediaType: *profile.AvatarMediaType, Size: *profile.AvatarSize,
		})
	}
	return out, nil
}

// AdaptPointerFunc 将账号资料 pointer 适配为 CustomerPointerFunc（供 mixed generate）。
func AdaptPointerFunc(
	accounts []store.ScopedAccount,
	repo Repository,
) avatarmedia.CustomerPointerFunc {
	scopes := make(map[string]store.AccountScope, len(accounts))
	for _, account := range accounts {
		scopes[account.AccountID] = account.Scope
	}
	source := NewPointerSource(repo)
	return func(ctx context.Context, accountID, cursor string, limit int) ([]avatarmedia.CurrentPointer, error) {
		scope, ok := scopes[accountID]
		if !ok {
			return nil, fmt.Errorf("unknown account scope: %s", accountID)
		}
		return source.ListCurrent(ctx, accountID, scope, cursor, limit)
	}
}
