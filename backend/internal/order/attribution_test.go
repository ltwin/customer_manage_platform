package order_test

import (
	"context"
	"testing"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
)

// TestCreateFreezesAttributionSnapshot 锁住归因快照语义（dashboard-v2 ITEM-3）：
// 创建事务内固化下单时客户渠道与套系拍摄类型；此后客户渠道后改、套系类型后改、
// 订单改挂客户（merge 语义）都不重算；无套系订单 shoot_type_snapshot 为 NULL。
func TestCreateFreezesAttributionSnapshot(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-attrib")
	svc := orderService()

	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "channel", "status"},
		"cus_attr", "归因客户", "douyin", "active",
	); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "channel", "status"},
		"cus_merge_target", "合并目标", "weibo", "active",
	); err != nil {
		t.Fatalf("seed merge target: %v", err)
	}
	if err := scope.Insert(ctx, "packages",
		[]string{"id", "name", "shoot_type", "pricing_mode", "base_price", "status"},
		"pkg_attr", "Cosplay 套系", "cosplay", "fixed", 68000, "active",
	); err != nil {
		t.Fatalf("seed package: %v", err)
	}

	withPackage, err := svc.Create(ctx, scope, orderdomain.CreateInput{
		CustomerID: "cus_attr",
		PackageID:  strPtr("pkg_attr"),
		Price:      intPtr(1000),
	})
	if err != nil {
		t.Fatalf("create order with package: %v", err)
	}
	if withPackage.ChannelSnapshot != "douyin" {
		t.Fatalf("channel_snapshot 应固化下单时渠道 douyin: %q", withPackage.ChannelSnapshot)
	}
	if withPackage.ShootTypeSnapshot == nil || *withPackage.ShootTypeSnapshot != "cosplay" {
		t.Fatalf("shoot_type_snapshot 应固化套系拍摄类型 cosplay: %v", withPackage.ShootTypeSnapshot)
	}

	noPackage, err := svc.Create(ctx, scope, orderdomain.CreateInput{
		CustomerID: "cus_attr",
		Price:      intPtr(500),
	})
	if err != nil {
		t.Fatalf("create order without package: %v", err)
	}
	if noPackage.ChannelSnapshot != "douyin" {
		t.Fatalf("无套系订单也应有渠道快照: %q", noPackage.ChannelSnapshot)
	}
	if noPackage.ShootTypeSnapshot != nil {
		t.Fatalf("无套系订单 shoot_type_snapshot 应为 NULL（矩阵未归因桶）: %v", *noPackage.ShootTypeSnapshot)
	}

	// 客户渠道后改、套系类型后改、订单改挂客户（merge 语义模拟）都不重算快照。
	if _, err := scope.Update(ctx, "customers", "channel = 'weibo'", "id = $2", "cus_attr"); err != nil {
		t.Fatalf("change customer channel: %v", err)
	}
	if _, err := scope.Update(ctx, "packages", "shoot_type = 'portrait'", "id = $2", "pkg_attr"); err != nil {
		t.Fatalf("change package shoot_type: %v", err)
	}
	if _, err := scope.Update(ctx, "orders", "customer_id = 'cus_merge_target'", "id = $2", withPackage.ID); err != nil {
		t.Fatalf("re-point order customer: %v", err)
	}

	revised, err := svc.Update(ctx, scope, withPackage.ID, orderdomain.UpdateInput{Title: strPtr("改名不重算")})
	if err != nil {
		t.Fatalf("update order: %v", err)
	}
	if revised.ChannelSnapshot != "douyin" || revised.ShootTypeSnapshot == nil || *revised.ShootTypeSnapshot != "cosplay" {
		t.Fatalf("归因快照不可变被破坏: channel=%q shoot_type=%v",
			revised.ChannelSnapshot, revised.ShootTypeSnapshot)
	}

	channelChanged, err := svc.Update(ctx, scope, noPackage.ID, orderdomain.UpdateInput{Title: strPtr("渠道后改也不重算")})
	if err != nil {
		t.Fatalf("update packageless order: %v", err)
	}
	if channelChanged.ChannelSnapshot != "douyin" {
		t.Fatalf("客户渠道后改不得重算快照: %q", channelChanged.ChannelSnapshot)
	}
}
