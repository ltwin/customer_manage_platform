package creativeworkspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	db    *store.Store
	scope store.AccountScope
	svc   *Service
	pilot *Pilot
	ctx   context.Context
	now   time.Time
}

func newFixture(t *testing.T, account string) fixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.CreateAccount(ctx, account, "hash"); err != nil {
		t.Fatal(err)
	}
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	repo := NewPostgresRepository()
	pilot := NewPilot(repo).WithClock(clock)
	svc := NewService(repo, objects, pilot).WithClock(clock)
	return fixture{db: db, scope: db.ScopeFor(auth.AccountContext{AccountID: account}), svc: svc, pilot: pilot, ctx: ctx, now: now}
}

func (f fixture) enroll(t *testing.T) {
	t.Helper()
	acct, pre, err := f.pilot.Enroll(f.ctx, f.scope, "w1")
	if err != nil {
		t.Fatalf("enroll: %v (%+v)", err, pre)
	}
	if acct.State != PilotNewWrite {
		t.Fatalf("state = %s", acct.State)
	}
}

func (f fixture) seedCustomerAndOrder(t *testing.T) (customerID, orderID string) {
	t.Helper()
	customerID = "cus_test"
	if err := f.scope.Insert(f.ctx, "customers", []string{"id", "display_name", "channel"}, customerID, "阿宁", "other"); err != nil {
		t.Fatal(err)
	}
	orderID = "ord_test"
	if err := f.scope.Insert(f.ctx, "orders", []string{"id", "customer_id", "title"}, orderID, customerID, "汉服外拍"); err != nil {
		t.Fatal(err)
	}
	return
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPilotPreflightGateAndEnroll(t *testing.T) {
	f := newFixture(t, "acct-pilot")
	// legacy_write 默认态：新模型写被拒
	if _, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{}); !errors.Is(err, ErrPilotRequired) {
		t.Fatalf("expected pilot_required, got %v", err)
	}
	// 干净账号 preflight 合格
	pre, err := f.pilot.Preflight(f.ctx, f.scope)
	if err != nil || !pre.Eligible {
		t.Fatalf("preflight: %v %+v", err, pre)
	}
	if len(pre.Items) < 11 {
		t.Fatalf("expected full checklist, got %d items", len(pre.Items))
	}
	// 制造一个活跃旧计划 → 阻断 + 清场清单
	if err := f.scope.Insert(f.ctx, "shoot_plans", []string{"id", "title", "subject", "status"}, "plan_1", "旧策划", "主题", "draft"); err != nil {
		t.Fatal(err)
	}
	pre, err = f.pilot.Preflight(f.ctx, f.scope)
	if err != nil || pre.Eligible {
		t.Fatalf("expected ineligible: %v %+v", err, pre)
	}
	var found bool
	for _, it := range pre.Items {
		if it.Key == "active_plans" && it.Count == 1 && it.Blocking && it.HowTo != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("active_plans item missing: %+v", pre.Items)
	}
	if _, _, err := f.pilot.Enroll(f.ctx, f.scope, "w1"); !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("expected preflight failure, got %v", err)
	}
	// 清场（归档）后可准入
	if _, err := f.scope.Update(f.ctx, "shoot_plans", "status = 'archived', archived_at = $2", "id = $3", f.now, "plan_1"); err != nil {
		t.Fatal(err)
	}
	f.enroll(t)
	blocked, err := f.pilot.LegacyWriteBlocked(f.ctx, f.scope)
	if err != nil || !blocked {
		t.Fatalf("legacy write should be blocked after enroll: %v %v", blocked, err)
	}
	// 幂等：再次 enroll 不报错
	if _, _, err := f.pilot.Enroll(f.ctx, f.scope, "w1"); err != nil {
		t.Fatal(err)
	}
	// stop 后新写被拒，旧写不再被本 gate 阻断（恢复由 owner 决定）
	if _, err := f.pilot.Stop(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{}); !errors.Is(err, ErrPilotRequired) {
		t.Fatalf("expected pilot_required after stop, got %v", err)
	}
}

func TestWorkspaceLifecycleInboxLinksAndCards(t *testing.T) {
	f := newFixture(t, "acct-ws")
	f.enroll(t)
	customerID, orderID := f.seedCustomerAndOrder(t)

	// 未归类唯一且自动存在
	list, err := f.svc.ListWorkspaces(f.ctx, f.scope, false)
	if err != nil || len(list) != 1 || list[0].Kind != KindInbox {
		t.Fatalf("expected only inbox: %v %+v", err, list)
	}
	inbox := list[0]
	if _, err := f.svc.UpdateWorkspace(f.ctx, f.scope, inbox.ID, UpdateWorkspaceInput{Link: &LinkInput{Kind: LinkOrder, ID: orderID}}); !errors.Is(err, ErrInboxImmutable) {
		t.Fatalf("inbox must not be linkable: %v", err)
	}

	// 一步创建（无必填）
	ws, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{})
	if err != nil || ws.Kind != KindProject || ws.Name != "" {
		t.Fatalf("create: %v %+v", err, ws)
	}
	// 关联订单 → 显示名回落为订单标题
	ws, err = f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{ExpectedRevision: ws.Revision, Link: &LinkInput{Kind: LinkOrder, ID: orderID}})
	if err != nil || ws.Link == nil || ws.Link.Name != "汉服外拍" || ws.DisplayName() != "汉服外拍" {
		t.Fatalf("link order: %v %+v", err, ws)
	}
	// 换成客户；订单侧零写：订单行不变
	ws, err = f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Link: &LinkInput{Kind: LinkCustomer, ID: customerID}})
	if err != nil || ws.Link == nil || ws.Link.Kind != LinkCustomer || ws.Link.Name != "阿宁" {
		t.Fatalf("link customer: %v %+v", err, ws)
	}
	var orderTitle string
	if err := f.scope.QueryRow(f.ctx, "orders", "title", "id = $2", orderID).Scan(&orderTitle); err != nil || orderTitle != "汉服外拍" {
		t.Fatalf("order must be untouched: %v %q", err, orderTitle)
	}
	// 反向入口
	back, err := f.svc.WorkspacesLinkedTo(f.ctx, f.scope, LinkInput{Kind: LinkCustomer, ID: customerID})
	if err != nil || len(back) != 1 || back[0].ID != ws.ID {
		t.Fatalf("reverse lookup: %v %+v", err, back)
	}
	// 关联对象删除 → 外键 SET NULL，空间不受影响
	if _, err := f.scope.Delete(f.ctx, "orders", "id = $2", orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.scope.Delete(f.ctx, "customers", "id = $2", customerID); err != nil {
		t.Fatal(err)
	}
	d, err := f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if err != nil || d.Workspace.Link != nil {
		t.Fatalf("link should be cleared on delete: %v %+v", err, d.Workspace)
	}
	// 解除 / 不存在目标
	if _, err := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Link: &LinkInput{Kind: LinkOrder, ID: "ord_nope"}}); !errors.Is(err, ErrLinkTargetNotFound) {
		t.Fatalf("expected link target not found: %v", err)
	}
	// revision 冲突
	if _, err := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{ExpectedRevision: 1, Name: strPtr("x")}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict: %v", err)
	}

	// 批量搬入：多行文本按行成卡，链接成链接卡；批次保留原文
	raw := "阿宁：这次想拍暗一点的\n\nhttps://www.xiaohongshu.com/explore/abc\n  背光加一点烟？  \n"
	var res ImportCardsResult
	err = f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		res, err = f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: ws.ID, RawText: raw})
		return err
	})
	if err != nil || len(res.Cards) != 3 {
		t.Fatalf("import: %v %+v", err, res)
	}
	if res.Cards[1].Type != CardLink || res.Cards[2].Text != "背光加一点烟？" || res.Cards[2].BatchSeq != 3 {
		t.Fatalf("cards = %+v", res.Cards)
	}
	var storedRaw string
	if err := f.scope.QueryRow(f.ctx, "creative_import_batches", "raw_text", "id = $2", res.Batch.ID).Scan(&storedRaw); err != nil || storedRaw != raw {
		t.Fatalf("batch raw text lost: %v %q", err, storedRaw)
	}

	// 图片上传 → 成卡；来源分类默认 unknown_web；视频拒绝
	if _, err := f.svc.UploadAsset(f.ctx, f.scope, UploadAssetInput{WorkspaceID: ws.ID, DeclaredMediaType: "video/mp4", Bytes: []byte{0, 1}}); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("video must be rejected: %v", err)
	}
	asset, err := f.svc.UploadAsset(f.ctx, f.scope, UploadAssetInput{WorkspaceID: ws.ID, DeclaredMediaType: "image/png", Bytes: pngBytes(t, 64, 32)})
	if err != nil || asset.SourceClass != SourceUnknownWeb || asset.RightsBasis != "citation_or_display" || asset.GenerationReferenceGranted {
		t.Fatalf("upload: %v %+v", err, asset)
	}
	err = f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		res, err = f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: ws.ID, AssetIDs: []string{asset.ID}})
		return err
	})
	if err != nil || len(res.Cards) != 1 || res.Cards[0].Type != CardImage || res.Cards[0].Checksum != asset.DisplayChecksum {
		t.Fatalf("image import: %v %+v", err, res)
	}
	imgCard := res.Cards[0]
	stream, err := f.svc.OpenDisplay(f.ctx, f.scope, ws.ID, asset.ID, asset.DisplayChecksum)
	if err != nil {
		t.Fatalf("open display: %v", err)
	}
	_ = stream.Body.Close()

	// 列表汇总：卡片数 4、封面为图片卡
	list, err = f.svc.ListWorkspaces(f.ctx, f.scope, false)
	if err != nil {
		t.Fatal(err)
	}
	var proj Workspace
	for _, w := range list {
		if w.ID == ws.ID {
			proj = w
		}
	}
	if proj.CardCount != 4 || proj.CoverAssetID == nil || *proj.CoverAssetID != asset.ID {
		t.Fatalf("summary = %+v", proj)
	}
	if list[0].Kind != KindInbox {
		t.Fatalf("inbox must be first: %+v", list[0])
	}

	// 分组与排序
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, true)
	g, err := f.svc.CreateGroup(f.ctx, f.scope, CreateGroupInput{WorkspaceID: ws.ID, Name: "走廊", CardIDs: []string{d.Cards[0].ID, imgCard.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReorderCards(f.ctx, f.scope, ReorderInput{WorkspaceID: ws.ID, CardIDs: []string{imgCard.ID}}); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if d.Cards[0].ID != imgCard.ID || d.Cards[0].GroupID == nil || *d.Cards[0].GroupID != g.ID || len(d.Cards) != 4 {
		t.Fatalf("after reorder/group: %+v", d.Cards)
	}
	if err := f.svc.DeleteGroup(f.ctx, f.scope, ws.ID, g.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if d.Cards[0].GroupID != nil {
		t.Fatal("group_id must be cleared after group delete")
	}

	// 从未归类挪进项目空间：只改一条成员记录
	var inboxRes ImportCardsResult
	_ = f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		inboxRes, err = f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: inbox.ID, RawText: "逆光奔跑"})
		return err
	})
	moved, err := f.svc.MoveCard(f.ctx, f.scope, MoveCardInput{FromWorkspaceID: inbox.ID, CardID: inboxRes.Cards[0].ID, ToWorkspaceID: ws.ID})
	if err != nil || moved.WorkspaceID != ws.ID || moved.Position != 4 {
		t.Fatalf("move: %v %+v", err, moved)
	}
	n, _ := f.scope.Count(f.ctx, "creative_card_memberships", "card_id = $2", moved.ID)
	if n != 1 {
		t.Fatalf("membership rows = %d", n)
	}

	// 拍摄项：值拷贝 + 来源两态 + tombstone；来源移除不影响拍摄项
	var items []ShootItem
	err = f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		items, err = f.svc.CreateShootItemsInScope(f.ctx, tx, CreateShootItemsInput{WorkspaceID: ws.ID, CardIDs: []string{imgCard.ID, d.Cards[1].ID}, Merge: true})
		return err
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("create shoot items: %v %+v", err, items)
	}
	it := items[0]
	if it.RefAssetID == nil || *it.RefAssetID != asset.ID || it.Title != d.Cards[1].Text || it.SourceState != SourceAvailable || len(it.SourceCardIDs) != 2 {
		t.Fatalf("shoot item = %+v", it)
	}
	if err := f.svc.RemoveCard(f.ctx, f.scope, ws.ID, imgCard.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if len(d.ShootItems) != 1 || d.ShootItems[0].SourceState != SourceUnavailable || d.ShootItems[0].Title != it.Title || d.ShootItems[0].RefChecksum == nil {
		t.Fatalf("shoot item after source removal = %+v", d.ShootItems)
	}
	// 卡片行仍由账号持有（归档，不删除）
	var archived *time.Time
	if err := f.scope.QueryRow(f.ctx, "creative_cards", "archived_at", "id = $2", imgCard.ID).Scan(&archived); err != nil || archived == nil {
		t.Fatalf("card must be archived not deleted: %v %v", err, archived)
	}
	// 现场结果 + 撤销 + tombstone
	done := ResultDone
	upd, err := f.svc.UpdateShootItem(f.ctx, f.scope, ws.ID, it.ID, UpdateShootItemInput{Result: &done, ResultNote: strPtr("换了侧逆光")})
	if err != nil || upd.Result == nil || *upd.Result != ResultDone || upd.ResultNote != "换了侧逆光" {
		t.Fatalf("result: %v %+v", err, upd)
	}
	upd, err = f.svc.UpdateShootItem(f.ctx, f.scope, ws.ID, it.ID, UpdateShootItemInput{ClearResult: true})
	if err != nil || upd.Result != nil {
		t.Fatalf("clear result: %v %+v", err, upd)
	}
	if _, err := f.svc.UpdateShootItem(f.ctx, f.scope, ws.ID, it.ID, UpdateShootItemInput{Tombstone: true}); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if len(d.ShootItems) != 0 {
		t.Fatalf("tombstoned item must leave current list: %+v", d.ShootItems)
	}
	all, _ := f.svc.repo.ListShootItems(f.ctx, f.scope, ws.ID, true)
	if len(all) != 1 || all[0].Status != ShootTombstone {
		t.Fatalf("history must remain readable: %+v", all)
	}

	// 备忘：批量粘贴、打勾、排序、删除；无任何阻断
	var memos []Memo
	_ = f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		memos, err = f.svc.CreateMemosInScope(f.ctx, tx, CreateMemosInput{WorkspaceID: ws.ID, Text: "备用电池\n反光板\n\n租的伞记得还"})
		return err
	})
	if len(memos) != 3 {
		t.Fatalf("memos = %+v", memos)
	}
	if err := f.svc.UpdateMemo(f.ctx, f.scope, ws.ID, memos[0].ID, UpdateMemoInput{Checked: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReorderMemos(f.ctx, f.scope, ReorderMemosInput{WorkspaceID: ws.ID, MemoIDs: []string{memos[2].ID}}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteMemo(f.ctx, f.scope, ws.ID, memos[1].ID); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, false)
	if len(d.Memos) != 2 || d.Memos[0].ID != memos[2].ID || !d.Memos[1].Checked {
		t.Fatalf("memos after ops = %+v", d.Memos)
	}

	// 归档空间：卡片不删除，列表默认隐藏
	if _, err := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Archived: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	list, _ = f.svc.ListWorkspaces(f.ctx, f.scope, false)
	if len(list) != 1 {
		t.Fatalf("archived must be hidden: %+v", list)
	}
	cards, _ := f.scope.Count(f.ctx, "creative_card_memberships", "workspace_id = $2", ws.ID)
	if cards != 4 {
		t.Fatalf("archiving workspace must keep cards: %d", cards)
	}
}

func TestAccountIsolation(t *testing.T) {
	f := newFixture(t, "acct-a")
	f.enroll(t)
	ws, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{Name: "A 的空间"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateAccount(f.ctx, "acct-b", "hash"); err != nil {
		t.Fatal(err)
	}
	other := f.db.ScopeFor(auth.AccountContext{AccountID: "acct-b"})
	if _, err := f.svc.GetWorkspaceDetail(f.ctx, other, ws.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account read must be not_found: %v", err)
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestStoppedAndArchivedSpacesRejectEveryWrite(t *testing.T) {
	f := newFixture(t, "acct-readonly")
	f.enroll(t)
	ws, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	var cards ImportCardsResult
	var items []ShootItem
	var memos []Memo
	if err := f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var err error
		cards, err = f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: ws.ID, RawText: "逆光"})
		if err != nil {
			return err
		}
		items, err = f.svc.CreateShootItemsInScope(f.ctx, tx, CreateShootItemsInput{WorkspaceID: ws.ID, CardIDs: []string{cards.Cards[0].ID}})
		if err != nil {
			return err
		}
		memos, err = f.svc.CreateMemosInScope(f.ctx, tx, CreateMemosInput{WorkspaceID: ws.ID, Text: "电池"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	group, err := f.svc.CreateGroup(f.ctx, f.scope, CreateGroupInput{WorkspaceID: ws.ID, Name: "第一组"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Archived: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() error
	}{
		{"workspace rename", func() error {
			_, e := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Name: strPtr("changed")})
			return e
		}},
		{"workspace unlink", func() error {
			_, e := f.svc.UpdateWorkspace(f.ctx, f.scope, ws.ID, UpdateWorkspaceInput{Link: &LinkInput{}})
			return e
		}},
		{"card edit", func() error {
			_, e := f.svc.UpdateCard(f.ctx, f.scope, ws.ID, cards.Cards[0].ID, UpdateCardInput{Text: strPtr("改变")})
			return e
		}},
		{"card remove", func() error { return f.svc.RemoveCard(f.ctx, f.scope, ws.ID, cards.Cards[0].ID) }},
		{"shot result", func() error {
			r := ResultDone
			_, e := f.svc.UpdateShootItem(f.ctx, f.scope, ws.ID, items[0].ID, UpdateShootItemInput{Result: &r})
			return e
		}},
		{"memo edit", func() error {
			return f.svc.UpdateMemo(f.ctx, f.scope, ws.ID, memos[0].ID, UpdateMemoInput{Checked: boolPtr(true)})
		}},
		{"memo remove", func() error { return f.svc.DeleteMemo(f.ctx, f.scope, ws.ID, memos[0].ID) }},
		{"group rename", func() error { return f.svc.RenameGroup(f.ctx, f.scope, ws.ID, group.ID, "改名") }},
		{"group remove", func() error { return f.svc.DeleteGroup(f.ctx, f.scope, ws.ID, group.ID) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, ErrWorkspaceArchived) {
				t.Fatalf("want archived, got %v", err)
			}
		})
	}
	if _, err := f.pilot.Stop(f.ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.pilot.Enroll(f.ctx, f.scope, "w2"); !errors.Is(err, ErrPilotRequired) {
		t.Fatalf("stop must not silently reopen: %v", err)
	}
	if blocked, err := f.pilot.LegacyWriteBlocked(f.ctx, f.scope); err != nil || !blocked {
		t.Fatalf("old history must stay readonly: %v", err)
	}
	if _, err := f.svc.GetWorkspaceDetail(f.ctx, f.scope, ws.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		_, e := f.svc.CreateWorkspaceInScope(f.ctx, tx, CreateWorkspaceInput{})
		return e
	}); !errors.Is(err, ErrPilotRequired) {
		t.Fatalf("idempotency callback bypassed stop: %v", err)
	}
}

func TestLegacyTransactionAndEnrollCannotCross(t *testing.T) {
	f := newFixture(t, "acct-cutover-race")
	locked := make(chan struct{})
	release := make(chan struct{})
	legacyDone := make(chan error, 1)
	enrollDone := make(chan error, 1)
	go func() {
		legacyDone <- f.scope.WithLegacyPlanningWrite().WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
			close(locked)
			<-release
			return tx.Insert(f.ctx, "shoot_plans", []string{"id", "title", "subject", "status"}, "race-plan", "仍在创作", "人像", "draft")
		})
	}()
	<-locked
	go func() { _, _, err := f.pilot.Enroll(f.ctx, f.scope, "w1"); enrollDone <- err }()
	close(release)
	if err := <-legacyDone; err != nil {
		t.Fatal(err)
	}
	if err := <-enrollDone; !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("must see preceding legacy write: %v", err)
	}
	if _, err := f.scope.Update(f.ctx, "shoot_plans", "status='archived', archived_at=$2", "id=$3", f.now, "race-plan"); err != nil {
		t.Fatal(err)
	}
	f.enroll(t)
	ran := false
	err := f.scope.WithLegacyPlanningWrite().WithTxScope(f.ctx, func(tx store.TxAccountScope) error { ran = true; return nil })
	if !errors.Is(err, store.ErrLegacyReadOnly) || ran {
		t.Fatalf("late old callback ran=%v err=%v", ran, err)
	}
}

func TestExecutionHistoryAndConcurrentInbox(t *testing.T) {
	f := newFixture(t, "acct-history")
	f.enroll(t)
	results := make(chan Workspace, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() { w, e := f.svc.EnsureInbox(f.ctx, f.scope); results <- w; errs <- e }()
	}
	a, b := <-results, <-results
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if a.ID != b.ID {
		t.Fatal("more than one inbox")
	}
	var shots []ShootItem
	if err := f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		cards, e := f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: a.ID, RawText: "窗边"})
		if e != nil {
			return e
		}
		shots, e = f.svc.CreateShootItemsInScope(f.ctx, tx, CreateShootItemsInput{WorkspaceID: a.ID, CardIDs: []string{cards.Cards[0].ID}})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	r := ResultDone
	done, e := f.svc.UpdateShootItem(f.ctx, f.scope, a.ID, shots[0].ID, UpdateShootItemInput{ExpectedRevision: shots[0].Revision, Result: &r})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.svc.UpdateShootItem(f.ctx, f.scope, a.ID, shots[0].ID, UpdateShootItemInput{ExpectedRevision: done.Revision, ClearResult: true}); e != nil {
		t.Fatal(e)
	}
	count, e := f.scope.Count(f.ctx, "creative_execution_events", "shoot_item_id=$2", shots[0].ID)
	if e != nil || count != 2 {
		t.Fatalf("history lost: %d %v", count, e)
	}
}

// The request may be cancelled after the first immutable object is published.
// Cleanup must use a detached bounded context; cancellation cannot strand it.
type cancellingObjects struct {
	immutablefs.ObjectStore
	cancel   context.CancelFunc
	firstKey string
}

func (o *cancellingObjects) PutImmutable(ctx context.Context, key string, body []byte, meta immutablefs.Metadata) (immutablefs.Metadata, bool, error) {
	m, created, err := o.ObjectStore.PutImmutable(ctx, key, body, meta)
	if created && o.firstKey == "" {
		o.firstKey = key
		o.cancel()
	}
	return m, created, err
}
func TestCancelledUploadCleansPublishedObject(t *testing.T) {
	f := newFixture(t, "acct-cancel-media")
	f.enroll(t)
	ws, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	objects := &cancellingObjects{ObjectStore: f.svc.objects, cancel: cancel}
	f.svc.objects = objects
	_, err = f.svc.UploadAsset(ctx, f.scope, UploadAssetInput{WorkspaceID: ws.ID, DeclaredMediaType: "image/png", Bytes: pngBytes(t, 64, 32)})
	if err == nil || objects.firstKey == "" {
		t.Fatalf("did not exercise cancellation: %v", err)
	}
	if _, err := objects.Stat(f.ctx, objects.firstKey); !errors.Is(err, immutablefs.ErrNotFound) {
		t.Fatalf("stranded object: %v", err)
	}
	n, err := f.scope.Count(f.ctx, "creative_workspace_assets", "")
	if err != nil || n != 0 {
		t.Fatalf("cancelled asset committed: %d %v", n, err)
	}
}
func TestUnknownCommitPreservesObjects(t *testing.T) {
	f := newFixture(t, "acct-unknown-commit")
	key := "creative/test/original"
	body := []byte("asset")
	// Use a real object and deliberately signal a lost commit acknowledgement.
	sum := sha256.Sum256(body)
	_, _, err := f.svc.objects.PutImmutable(f.ctx, key, body, immutablefs.Metadata{MediaType: "application/octet-stream", Size: int64(len(body)), Checksum: "sha256-" + hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	err = f.svc.cleanupFailedUpload(f.ctx, []string{key}, fmt.Errorf("lost acknowledgement: %w", store.ErrCommitOutcomeUnknown))
	if !errors.Is(err, store.ErrCommitOutcomeUnknown) {
		t.Fatalf("lost uncertainty marker: %v", err)
	}
	if _, err := f.svc.objects.Stat(f.ctx, key); err != nil {
		t.Fatalf("possibly committed object deleted: %v", err)
	}
}
func TestMoveCardRejectsStaleSource(t *testing.T) {
	f := newFixture(t, "acct-stale-move")
	f.enroll(t)
	a, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.CreateWorkspace(f.ctx, f.scope, CreateWorkspaceInput{})
	if err != nil {
		t.Fatal(err)
	}
	var cards ImportCardsResult
	if err := f.scope.WithTxScope(f.ctx, func(tx store.TxAccountScope) error {
		var e error
		cards, e = f.svc.ImportCardsInScope(f.ctx, tx, ImportCardsInput{WorkspaceID: a.ID, RawText: "一张素材"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	id := cards.Cards[0].ID
	if _, err := f.svc.MoveCard(f.ctx, f.scope, MoveCardInput{FromWorkspaceID: a.ID, ToWorkspaceID: b.ID, CardID: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.MoveCard(f.ctx, f.scope, MoveCardInput{FromWorkspaceID: a.ID, ToWorkspaceID: a.ID, CardID: id}); !errors.Is(err, ErrCardNotInWorkspace) {
		t.Fatalf("stale source moved current card: %v", err)
	}
}
