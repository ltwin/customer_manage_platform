package shootplanning_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func TestPostgresRepositoryAccountIsolationAndStableList(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scopeA := createPlanningAccount(t, db, "shoot-plan-acct-a")
	scopeB := createPlanningAccount(t, db, "shoot-plan-acct-b")
	repo := shootplanning.NewPostgresRepository()

	first, err := repo.Create(ctx, scopeA, shootplanning.CreatePlanInput{Title: "第一份", Subject: "模特 A"})
	if err != nil {
		t.Fatalf("create first plan: %v", err)
	}
	if _, err := repo.Create(ctx, scopeA, shootplanning.CreatePlanInput{Title: "第二份", Subject: "模特 B"}); err != nil {
		t.Fatalf("create second plan: %v", err)
	}
	if _, err := repo.Create(ctx, scopeB, shootplanning.CreatePlanInput{Title: "别的账号", Subject: "模特 C"}); err != nil {
		t.Fatalf("create account B plan: %v", err)
	}

	list, err := repo.List(ctx, scopeA, shootplanning.ListPlansFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if list.Total != 2 || len(list.Items) != 2 || list.Items[0].ID == list.Items[1].ID {
		t.Fatalf("unexpected stable list: %+v", list)
	}
	if _, err := repo.Detail(ctx, scopeB, first.ID, false); !errors.Is(err, shootplanning.ErrPlanNotFound) {
		t.Fatalf("cross-account detail error = %v, want not found", err)
	}
}

func TestPostgresRepositoryListKeywordAndSort(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-list-query")
	other := createPlanningAccount(t, db, "shoot-plan-list-query-other")
	repo := shootplanning.NewPostgresRepository()

	for _, input := range []shootplanning.CreatePlanInput{
		{Title: "夜景人像", Subject: "模特 A"},
		{Title: "晨雾外拍", Subject: "夜景补拍备选"},
		{Title: "Studio Portrait", Subject: "模特 B"},
		{Title: "折扣 100% 交付", Subject: "模特 D"},
		{Title: "命名 a_b 规则", Subject: "模特 E"},
	} {
		if _, err := repo.Create(ctx, scope, input); err != nil {
			t.Fatalf("create plan %q: %v", input.Title, err)
		}
	}
	// 关键词不得越过账号边界。
	if _, err := repo.Create(ctx, other, shootplanning.CreatePlanInput{Title: "夜景别的账号", Subject: "模特 C"}); err != nil {
		t.Fatalf("create other-account plan: %v", err)
	}

	// 关键词同时命中 title 与 subject，且过滤发生在分页之前（total 与页内容同口径）。
	byKeyword, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: "夜景", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list by keyword: %v", err)
	}
	if byKeyword.Total != 2 || len(byKeyword.Items) != 2 {
		t.Fatalf("keyword list = total %d items %d, want 2/2: %+v", byKeyword.Total, len(byKeyword.Items), byKeyword.Items)
	}
	for _, item := range byKeyword.Items {
		if !strings.Contains(item.Title, "夜景") && !strings.Contains(item.Subject, "夜景") {
			t.Fatalf("keyword list leaked non-matching plan: %+v", item)
		}
	}

	// 大小写不敏感。
	lower, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: "studio", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list by lowercase keyword: %v", err)
	}
	if lower.Total != 1 || len(lower.Items) != 1 || lower.Items[0].Title != "Studio Portrait" {
		t.Fatalf("case-insensitive keyword list = %+v", lower)
	}

	// 首尾空白不参与匹配。
	padded, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: "  夜景  ", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list by padded keyword: %v", err)
	}
	if padded.Total != byKeyword.Total {
		t.Fatalf("padded keyword total = %d, want %d", padded.Total, byKeyword.Total)
	}

	// LIKE 元字符按字面量匹配：搜「%」不能通配成全部，搜「_」不能通配成任意单字符。
	for _, tt := range []struct {
		keyword   string
		wantTitle string
	}{
		{keyword: "100%", wantTitle: "折扣 100% 交付"},
		{keyword: "%", wantTitle: "折扣 100% 交付"},
		{keyword: "a_b", wantTitle: "命名 a_b 规则"},
		{keyword: "_", wantTitle: "命名 a_b 规则"},
	} {
		literal, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: tt.keyword, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("list by literal %q: %v", tt.keyword, err)
		}
		if literal.Total != 1 || len(literal.Items) != 1 || literal.Items[0].Title != tt.wantTitle {
			t.Fatalf("literal %q = total %d items %+v, want only %q", tt.keyword, literal.Total, literal.Items, tt.wantTitle)
		}
	}
	// 反斜杠本身也必须是字面量，且不能让模式变成非法转义序列。
	backslash, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: `\`, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list by literal backslash: %v", err)
	}
	if backslash.Total != 0 {
		t.Fatalf("literal backslash matched %d plans, want 0", backslash.Total)
	}

	desc, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Sort: shootplanning.PlanListSortUpdatedDesc, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list updated desc: %v", err)
	}
	asc, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Sort: shootplanning.PlanListSortUpdatedAsc, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list updated asc: %v", err)
	}
	if len(desc.Items) != 5 || len(asc.Items) != len(desc.Items) {
		t.Fatalf("sorted lists have unexpected sizes: desc=%d asc=%d", len(desc.Items), len(asc.Items))
	}
	// id 兜底让两个方向严格互为倒序，即使 updated_at 在同一刻撞值。
	for i := range desc.Items {
		if desc.Items[i].ID != asc.Items[len(asc.Items)-1-i].ID {
			t.Fatalf("updated_at asc is not the exact reverse of desc: desc=%+v asc=%+v", desc.Items, asc.Items)
		}
	}

	created, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Sort: shootplanning.PlanListSortCreatedDesc, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list created desc: %v", err)
	}
	if created.Total != 5 || len(created.Items) != 5 {
		t.Fatalf("created desc list = total %d items %d, want 5/5", created.Total, len(created.Items))
	}

	if _, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Sort: "title_asc", Page: 1, PageSize: 20}); err == nil {
		t.Fatal("unknown sort key must be rejected")
	}
	if _, err := repo.List(ctx, scope, shootplanning.ListPlansFilter{Q: strings.Repeat("夜", 121), Page: 1, PageSize: 20}); err == nil {
		t.Fatal("over-long keyword must be rejected")
	}
}

func TestApplicationPlanCommandsAndLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-command-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	created, err := app.CreatePlan(ctx, scope, "create-plan-command", shootplanning.CreatePlanInput{Title: "命令测试", Subject: "主体"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	replayed, err := app.CreatePlan(ctx, scope, "create-plan-command", shootplanning.CreatePlanInput{Title: "命令测试", Subject: "主体"})
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("create replay=%+v err=%v", replayed, err)
	}

	mutation, err := app.ApplyPlanCommand(ctx, scope, "add-shot-command", created.ID, created.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: stringPointer("镜头一")}})
	if err != nil {
		t.Fatalf("add shot: %v", err)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "add-readiness-command", created.ID, mutation.Revision,
		shootplanning.UpsertReadinessCommand{Item: shootplanning.ReadinessWrite{
			Category: stringPointer("styling"), Title: stringPointer("服装"), Requirement: stringPointer("required"),
			PreflightStatus: stringPointer("unchecked"), ResponsibilityHint: stringPointer("photographer"),
		}})
	if err != nil {
		t.Fatalf("add readiness: %v", err)
	}
	detail, err := app.GetPlan(ctx, scope, created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Shots) != 1 || len(detail.ReadinessItems) != 1 {
		t.Fatalf("unexpected command projection: %+v", detail)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "link-readiness-command", created.ID, mutation.Revision,
		shootplanning.LinkReadinessCommand{ShotID: detail.Shots[0].ID, ReadinessID: detail.ReadinessItems[0].ID})
	if err != nil {
		t.Fatalf("link readiness: %v", err)
	}
	if _, err := app.TransitionPlan(ctx, scope, "mark-ready-blocked", created.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	}); !errors.Is(err, shootplanning.ErrReadinessIncomplete) {
		t.Fatalf("mark ready error=%v", err)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "check-readiness-command", created.ID, mutation.Revision,
		shootplanning.SetPreflightCommand{ReadinessID: detail.ReadinessItems[0].ID, PreflightStatus: "checked"})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.TransitionPlan(ctx, scope, "mark-ready-command", created.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	})
	if err != nil || ready.Status != shootplanning.PlanStatusReady {
		t.Fatalf("mark ready=%+v err=%v", ready, err)
	}
	unchecked, err := app.ApplyPlanCommand(ctx, scope, "uncheck-ready-command", created.ID, ready.Revision,
		shootplanning.SetPreflightCommand{ReadinessID: detail.ReadinessItems[0].ID, PreflightStatus: "unchecked"})
	if err != nil || unchecked.Status != shootplanning.PlanStatusDraft {
		t.Fatalf("uncheck ready=%+v err=%v", unchecked, err)
	}
}

func TestApplicationPatchNoopWindowAndBatchSemantics(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-patch-batch-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "patch-plan-create", shootplanning.CreatePlanInput{Title: "Patch", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := app.ApplyPlanCommand(ctx, scope, "patch-shot-create", plan.ID, plan.Revision, shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{
		Title: stringPointer("镜头"),
		Scene: shootplanning.Optional[string]{Specified: true, Value: "场景"},
		Notes: shootplanning.Optional[string]{Specified: true, Value: "保留备注"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	shotID := detail.Shots[0].ID
	mutation, err = app.ApplyPlanCommand(ctx, scope, "patch-shot-clear", plan.ID, mutation.Revision, shootplanning.UpsertShotCommand{
		ShotID: &shotID,
		Shot:   shootplanning.ShotWrite{Scene: shootplanning.Optional[string]{Specified: true, Null: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Shots[0].Scene != nil || detail.Shots[0].Notes == nil || *detail.Shots[0].Notes != "保留备注" {
		t.Fatalf("presence-aware shot patch failed: %+v", detail.Shots[0])
	}
	workTitle := "作品"
	mutation, err = app.ApplyPlanCommand(ctx, scope, "brief-update-command", plan.ID, mutation.Revision, shootplanning.UpdateBriefCommand{
		CreativeBrief: &shootplanning.CreativeBriefPatch{WorkTitle: shootplanning.Optional[string]{Specified: true, Value: workTitle}},
	})
	if err != nil {
		t.Fatal(err)
	}

	look, scene := 2, 3
	mutation, err = app.ApplyPlanCommand(ctx, scope, "public-scale-both", plan.ID, mutation.Revision, shootplanning.SetPublicScaleCommand{
		PlannedLookCount:  shootplanning.Optional[int]{Specified: true, Value: look},
		PlannedSceneCount: shootplanning.Optional[int]{Specified: true, Value: scene},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "public-scale-clear-look", plan.ID, mutation.Revision, shootplanning.SetPublicScaleCommand{
		PlannedLookCount: shootplanning.Optional[int]{Specified: true, Null: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if detail.PublicScale.PlannedLookCount != nil || detail.PublicScale.PlannedSceneCount == nil || *detail.PublicScale.PlannedSceneCount != scene {
		t.Fatalf("presence-aware public scale failed: %+v", detail.PublicScale)
	}

	startsAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	window := shootplanning.SetExecutionWindowCommand{
		StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour), Timezone: "Asia/Shanghai",
		LiveWindowStartsAt: startsAt.Add(-time.Hour), LiveWindowEndsAt: startsAt.Add(2 * time.Hour),
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "window-first", plan.ID, mutation.Revision, window)
	if err != nil {
		t.Fatal(err)
	}
	window.EndsAt = window.EndsAt.Add(time.Hour)
	window.LiveWindowEndsAt = window.LiveWindowEndsAt.Add(time.Hour)
	mutation, err = app.ApplyPlanCommand(ctx, scope, "window-second", plan.ID, mutation.Revision, window)
	if err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ExecutionWindow == nil || detail.ExecutionWindow.Revision != 2 {
		t.Fatalf("execution window revision = %+v, want 2", detail.ExecutionWindow)
	}

	readinessTitle := "服装"
	mutation, err = app.ApplyPlanCommand(ctx, scope, "noop-ready-create", plan.ID, mutation.Revision, shootplanning.UpsertReadinessCommand{
		Item: shootplanning.ReadinessWrite{Title: &readinessTitle},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	readinessID := detail.ReadinessItems[0].ID
	linked, err := app.ApplyPlanCommand(ctx, scope, "noop-link-first", plan.ID, mutation.Revision,
		shootplanning.LinkReadinessCommand{ShotID: shotID, ReadinessID: readinessID})
	if err != nil {
		t.Fatal(err)
	}
	noOp, err := app.ApplyPlanCommand(ctx, scope, "noop-link-second", plan.ID, linked.Revision,
		shootplanning.LinkReadinessCommand{ShotID: shotID, ReadinessID: readinessID})
	if err != nil {
		t.Fatal(err)
	}
	if noOp.Revision != linked.Revision {
		t.Fatalf("no-op link advanced revision: %d -> %d", linked.Revision, noOp.Revision)
	}
	unlinked, err := app.ApplyPlanCommand(ctx, scope, "unlink-ready-first", plan.ID, noOp.Revision,
		shootplanning.UnlinkReadinessCommand{ShotID: shotID, ReadinessID: readinessID})
	if err != nil {
		t.Fatal(err)
	}
	noOpUnlink, err := app.ApplyPlanCommand(ctx, scope, "unlink-ready-second", plan.ID, unlinked.Revision,
		shootplanning.UnlinkReadinessCommand{ShotID: shotID, ReadinessID: readinessID})
	if err != nil || noOpUnlink.Revision != unlinked.Revision {
		t.Fatalf("no-op unlink=%+v err=%v", noOpUnlink, err)
	}
	removedReadiness, err := app.ApplyPlanCommand(ctx, scope, "remove-ready-command", plan.ID, noOpUnlink.Revision,
		shootplanning.RemoveReadinessCommand{ReadinessID: readinessID})
	if err != nil {
		t.Fatal(err)
	}
	clearedWindow, err := app.ApplyPlanCommand(ctx, scope, "clear-window-command", plan.ID, removedReadiness.Revision,
		shootplanning.ClearExecutionWindowCommand{})
	if err != nil {
		t.Fatal(err)
	}
	secondTitle := "镜头二"
	secondShot, err := app.ApplyPlanCommand(ctx, scope, "second-shot-create", plan.ID, clearedWindow.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &secondTitle}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Shots) != 2 || detail.ExecutionWindow != nil || len(detail.ReadinessItems) != 0 {
		t.Fatalf("clear/remove projection mismatch: %+v", detail)
	}
	secondShotID := detail.Shots[1].ID
	if _, err := app.ApplyPlanCommand(ctx, scope, "reorder-shot-duplicate", plan.ID, secondShot.Revision,
		shootplanning.ReorderShotsCommand{OrderedShotIDs: []string{shotID, shotID}}); !errors.Is(err, shootplanning.ErrValidation) {
		t.Fatalf("duplicate reorder error=%v want validation", err)
	}
	afterInvalidReorder, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if afterInvalidReorder.Revision != secondShot.Revision || afterInvalidReorder.Shots[0].ID != shotID || afterInvalidReorder.Shots[1].ID != secondShotID {
		t.Fatalf("duplicate reorder changed plan: %+v", afterInvalidReorder)
	}
	reordered, err := app.ApplyPlanCommand(ctx, scope, "reorder-shot-command", plan.ID, secondShot.Revision,
		shootplanning.ReorderShotsCommand{OrderedShotIDs: []string{secondShotID, shotID}})
	if err != nil {
		t.Fatal(err)
	}
	removedShot, err := app.ApplyPlanCommand(ctx, scope, "remove-shot-command", plan.ID, reordered.Revision,
		shootplanning.RemoveShotCommand{ShotID: secondShotID, AcknowledgeExecutionHistory: false})
	if err != nil {
		t.Fatal(err)
	}
	if removedShot.Revision != reordered.Revision+1 {
		t.Fatalf("remove shot revision=%d want %d", removedShot.Revision, reordered.Revision+1)
	}
	thirdTitle := "镜头三"
	appendedShot, err := app.ApplyPlanCommand(ctx, scope, "third-shot-after-remove", plan.ID, removedShot.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &thirdTitle}})
	if err != nil {
		t.Fatalf("append after removing first position: %v", err)
	}
	afterAppend, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAppend.Shots) != 2 || afterAppend.Shots[0].ID != shotID || afterAppend.Shots[1].Title != thirdTitle ||
		afterAppend.Shots[0].Position >= afterAppend.Shots[1].Position || appendedShot.Revision != removedShot.Revision+1 {
		t.Fatalf("positions after remove and append: detail=%+v mutation=%+v", afterAppend.Shots, appendedShot)
	}

	batchPlan, err := app.CreatePlan(ctx, scope, "batch-plan-create", shootplanning.CreatePlanInput{Title: "Batch", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	batchShotTitle, batchReadinessTitle := "批量镜头", "批量准备"
	batchInput := shootplanning.PlanBatchInput{PlanID: batchPlan.ID, ExpectedPlanRevision: batchPlan.Revision, Candidates: []shootplanning.PlanBatchCandidate{
		shootplanning.CreateShotBatchCandidate{ClientRef: "shot-client-1", Shot: shootplanning.ShotWrite{Title: &batchShotTitle}},
		shootplanning.CreateReadinessBatchCandidate{ClientRef: "ready-client-1", Item: shootplanning.ReadinessWrite{Title: &batchReadinessTitle}},
		shootplanning.LinkReadinessBatchCandidate{ShotRef: "shot-client-1", ReadinessRef: "ready-client-1"},
	}}
	batch, err := app.CommitPlanBatch(ctx, scope, "batch-commit-one", batchInput)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Revision != batchPlan.Revision+1 || len(batch.CreatedIDs) != 2 || len(batch.CurrentLinks) != 1 {
		t.Fatalf("unexpected batch result: %+v", batch)
	}
	replayed, err := app.CommitPlanBatch(ctx, scope, "batch-commit-one", batchInput)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CreatedIDs[0].ServerID != batch.CreatedIDs[0].ServerID || replayed.Revision != batch.Revision {
		t.Fatalf("batch replay mismatch: first=%+v replay=%+v", batch, replayed)
	}
	crossA, err := app.CreatePlan(ctx, scope, "cross-resource-create-a", shootplanning.CreatePlanInput{Title: "Cross A", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	crossB, err := app.CreatePlan(ctx, scope, "cross-resource-create-b", shootplanning.CreatePlanInput{Title: "Cross B", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	newTitle := "同一请求体"
	if _, err := app.ApplyPlanCommand(ctx, scope, "cross-resource-command-key", crossA.ID, crossA.Revision,
		shootplanning.UpdateBriefCommand{Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyPlanCommand(ctx, scope, "cross-resource-command-key", crossB.ID, crossB.Revision,
		shootplanning.UpdateBriefCommand{Title: &newTitle}); !errors.Is(err, idempotency.ErrConflict) {
		t.Fatalf("same operation/key across plan resources error=%v", err)
	}
	sharedOperationPlan, err := app.CreatePlan(ctx, scope, "same-key-different-operation", shootplanning.CreatePlanInput{Title: "Shared", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyPlanCommand(ctx, scope, "same-key-different-operation", sharedOperationPlan.ID, sharedOperationPlan.Revision,
		shootplanning.UpdateBriefCommand{Title: &newTitle}); err != nil {
		t.Fatalf("different operations must allow same key: %v", err)
	}
}

type configurableReadinessGuard struct {
	err   error
	calls int
}

func (g *configurableReadinessGuard) AssertRemovableInScope(context.Context, store.TxAccountScope, string, string) error {
	g.calls++
	return g.err
}

type configurableArchiveParticipant struct {
	err   error
	calls int
}

type wrongArchiveImpactPolicy struct{}

func (wrongArchiveImpactPolicy) Capability() planningcapability.ArchiveCapability {
	return planningcapability.ArchiveCapabilityCore
}

func (wrongArchiveImpactPolicy) RequiredAcknowledgement() shootplanning.ArchiveAcknowledgement {
	return shootplanning.ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityPlanningShare)
}

func (p *configurableArchiveParticipant) OnPlanArchivedInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	_ int64,
	generation int64,
	_ time.Time,
) error {
	p.calls++
	if _, err := tx.Update(ctx, "shoot_plans", "subject = $2", "id = $3", "participant touched", planID); err != nil {
		return err
	}
	if p.err != nil {
		return p.err
	}
	// S3 archive participant owns resolution + MarkApplied (engine no longer stubs them).
	if err := crm.WriteGenerationResolution(ctx, tx, planID, generation); err != nil {
		return err
	}
	locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
	if err != nil {
		return err
	}
	return locked.MarkApplied(ctx, generation)
}

func TestApplicationRemovalGuardAndReminderArchiveRollback(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-guard-archive-acct")
	guard := &configurableReadinessGuard{err: shootplanning.ErrReadinessAssignmentActive}
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithReadinessRemovalGuard(guard))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "guard-plan-create", shootplanning.CreatePlanInput{Title: "Guard", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	readinessTitle := "服装"
	mutation, err := app.ApplyPlanCommand(ctx, scope, "guard-ready-create", plan.ID, plan.Revision,
		shootplanning.UpsertReadinessCommand{Item: shootplanning.ReadinessWrite{Title: &readinessTitle}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	readinessID := detail.ReadinessItems[0].ID
	if _, err := app.ApplyPlanCommand(ctx, scope, "guard-ready-remove", plan.ID, mutation.Revision,
		shootplanning.RemoveReadinessCommand{ReadinessID: readinessID}); !errors.Is(err, shootplanning.ErrReadinessAssignmentActive) {
		t.Fatalf("active guard error = %v", err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil || len(detail.ReadinessItems) != 1 {
		t.Fatalf("guard failure did not roll back readiness: detail=%+v err=%v", detail, err)
	}
	guard.err = nil
	removed, err := app.ApplyPlanCommand(ctx, scope, "guard-ready-remove", plan.ID, mutation.Revision,
		shootplanning.RemoveReadinessCommand{ReadinessID: readinessID})
	if err != nil || removed.Revision != mutation.Revision+1 || guard.calls != 2 {
		t.Fatalf("guard retry=%+v calls=%d err=%v", removed, guard.calls, err)
	}

	corePlan, err := app.CreatePlan(ctx, scope, "archive-core-create", shootplanning.CreatePlanInput{Title: "Core archive", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	coreAcknowledgement := shootplanning.ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityCore)
	coreArchived, err := app.TransitionPlan(ctx, scope, "archive-core-transition", corePlan.ID, shootplanning.PlanTransition{
		ExpectedRevision: corePlan.Revision, Kind: shootplanning.TransitionArchive, ArchiveAcknowledgement: &coreAcknowledgement,
	})
	if err != nil || coreArchived.Status != shootplanning.PlanStatusArchived {
		t.Fatalf("core archive=%+v err=%v", coreArchived, err)
	}

	promoteArchiveCapability(t, ctx, db, 1, planningcapability.ArchiveCapabilityPlanningShare)
	if _, err := app.GetPlan(ctx, scope, plan.ID, false); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("lower core wiring after share promotion error=%v", err)
	}
	shareApp, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithReadinessRemovalGuard(&configurableReadinessGuard{}),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{}))
	if err != nil {
		t.Fatal(err)
	}
	sharePlan, err := shareApp.CreatePlan(ctx, scope, "archive-share-create", shootplanning.CreatePlanInput{Title: "Share archive", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	if detail, err := shareApp.GetPlan(ctx, scope, sharePlan.ID, false); err != nil || detail.RequiredArchiveAcknowledgement.Version != "planning-share-v1" {
		t.Fatalf("share required acknowledgement=%+v err=%v", detail.RequiredArchiveAcknowledgement, err)
	}
	if _, err := shareApp.TransitionPlan(ctx, scope, "archive-share-transition", sharePlan.ID, shootplanning.PlanTransition{
		ExpectedRevision: sharePlan.Revision, Kind: shootplanning.TransitionArchive, ArchiveAcknowledgement: &coreAcknowledgement,
	}); !errors.Is(err, shootplanning.ErrArchiveAcknowledgement) {
		t.Fatalf("wrong share acknowledgement error=%v", err)
	}
	shareAcknowledgement := shootplanning.ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityPlanningShare)
	shareArchived, err := shareApp.TransitionPlan(ctx, scope, "archive-share-transition", sharePlan.ID, shootplanning.PlanTransition{
		ExpectedRevision: sharePlan.Revision, Kind: shootplanning.TransitionArchive, ArchiveAcknowledgement: &shareAcknowledgement,
	})
	if err != nil || shareArchived.Status != shootplanning.PlanStatusArchived {
		t.Fatalf("share archive=%+v err=%v", shareArchived, err)
	}

	promoteArchiveCapability(t, ctx, db, 2, planningcapability.ArchiveCapabilityReminder)
	if _, err := shareApp.GetPlan(ctx, scope, sharePlan.ID, false); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("lower share wiring after reminder promotion error=%v", err)
	}
	participant := &configurableArchiveParticipant{err: errors.New("participant failure")}
	reminderApp, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithReadinessRemovalGuard(&configurableReadinessGuard{}),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareReminderArchiveImpactPolicyV1{}),
		shootplanning.WithArchiveReminderParticipant(participant))
	if err != nil {
		t.Fatal(err)
	}
	archivePlan, err := reminderApp.CreatePlan(ctx, scope, "archive-plan-create", shootplanning.CreatePlanInput{Title: "Archive", Subject: "原始主体"})
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement := shootplanning.ArchiveAcknowledgementRegistryV1(planningcapability.ArchiveCapabilityReminder)
	transition := shootplanning.PlanTransition{ExpectedRevision: archivePlan.Revision, Kind: shootplanning.TransitionArchive, ArchiveAcknowledgement: &acknowledgement}
	if _, err := reminderApp.TransitionPlan(ctx, scope, "archive-reminder-transition", archivePlan.ID, transition); err == nil || err.Error() != "participant failure" {
		t.Fatalf("participant failure = %v", err)
	}
	detail, err = reminderApp.GetPlan(ctx, scope, archivePlan.ID, false)
	if err != nil || detail.Status != shootplanning.PlanStatusDraft || detail.Subject != "原始主体" {
		t.Fatalf("archive participant failure did not roll back: detail=%+v err=%v", detail, err)
	}
	var generationRows int64
	generationRows, err = scope.Count(ctx, "planning_reminder_account_generations", "TRUE")
	if err != nil || generationRows != 0 {
		t.Fatalf("failed archive left fence row count=%d err=%v", generationRows, err)
	}
	participant.err = nil
	archived, err := reminderApp.TransitionPlan(ctx, scope, "archive-reminder-transition", archivePlan.ID, transition)
	if err != nil || archived.Status != shootplanning.PlanStatusArchived || participant.calls != 2 {
		t.Fatalf("archive retry=%+v calls=%d err=%v", archived, participant.calls, err)
	}
	var target, applied int64
	if err := scope.QueryRow(ctx, "planning_reminder_account_generations", "target_generation, applied_generation", "TRUE").Scan(&target, &applied); err != nil {
		t.Fatal(err)
	}
	if target != 1 || applied != 1 {
		t.Fatalf("archive generation watermark=%d/%d want 1/1", target, applied)
	}
}

func TestApplicationArchiveCompositionFailsClosed(t *testing.T) {
	guard := &configurableReadinessGuard{}
	participant := &configurableArchiveParticipant{}
	if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareArchiveImpactPolicyV1{})); !errors.Is(err, shootplanning.ErrReadinessGuardWiringMismatch) {
		t.Fatalf("share without guard error=%v", err)
	}
	if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithReadinessRemovalGuard(guard),
		shootplanning.WithArchiveImpactPolicy(shootplanning.PlanningShareReminderArchiveImpactPolicyV1{})); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("reminder without participant error=%v", err)
	}
	if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithArchiveReminderParticipant(participant)); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("core with reminder participant error=%v", err)
	}
	if _, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor(),
		shootplanning.WithArchiveImpactPolicy(wrongArchiveImpactPolicy{})); !errors.Is(err, shootplanning.ErrArchiveWiringMismatch) {
		t.Fatalf("wrong policy acknowledgement error=%v", err)
	}
}

func TestCommitPreparedPlanBatchInScopeSharesLedgerAndProbeTransaction(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-prepared-batch-acct")
	executor := idempotency.NewExecutor()
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), executor)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "prepared-plan-create", shootplanning.CreatePlanInput{Title: "Prepared", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	shotTitle := "Prepared shot"
	prepared, err := shootplanning.PreparePlanBatch(shootplanning.PlanBatchInput{
		PlanID: plan.ID, ExpectedPlanRevision: plan.Revision,
		Candidates: []shootplanning.PlanBatchCandidate{
			shootplanning.CreateShotBatchCandidate{ClientRef: "prepared-shot", Shot: shootplanning.ShotWrite{Title: &shotTitle}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(map[string]any{
		"plan_id": plan.ID, "expected_plan_revision": plan.Revision,
		"candidates": []any{map[string]any{"kind": "create_shot", "client_ref": "prepared-shot", "shot": map[string]any{"title": shotTitle}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := idempotency.Request{
		Operation: idempotency.OperationShootPlanBatch, Key: "prepared-batch-scope-key",
		ResourceIdentity: idempotency.BatchResource(plan.ID), CanonicalBody: canonical,
	}
	const probeReadinessID = "spri_prepared_batch_probe"
	insertProbe := func(tx store.TxAccountScope) error {
		return tx.Insert(ctx, "shoot_plan_readiness_items",
			[]string{"id", "plan_id", "category", "title", "requirement", "preflight_status", "responsibility_hint"},
			probeReadinessID, plan.ID, "other", "prepared batch transaction probe", "optional", "unchecked", "unassigned")
	}
	injected := errors.New("probe write failure")
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := executor.ExecuteInScope(ctx, tx, request, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
			result, err := app.CommitPreparedPlanBatchInScope(ctx, tx, prepared)
			if err != nil {
				return idempotency.StoredResponse{}, err
			}
			if err := insertProbe(tx); err != nil {
				return idempotency.StoredResponse{}, err
			}
			_ = result
			return idempotency.StoredResponse{}, injected
		})
		return err
	})
	if !errors.Is(err, injected) {
		t.Fatalf("injected outer transaction error=%v", err)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Revision != plan.Revision || len(detail.Shots) != 0 {
		t.Fatalf("failed prepared batch left core mutation: %+v", detail)
	}
	probeRows, err := scope.Count(ctx, "shoot_plan_readiness_items", "id = $2", probeReadinessID)
	if err != nil || probeRows != 0 {
		t.Fatalf("failed prepared batch left probe rows=%d err=%v", probeRows, err)
	}
	ledgerRows, err := scope.Count(ctx, "idempotency_records", "operation = $2 AND key = $3", string(idempotency.OperationShootPlanBatch), request.Key)
	if err != nil || ledgerRows != 0 {
		t.Fatalf("failed prepared batch left ledger rows=%d err=%v", ledgerRows, err)
	}

	callbackCalls := 0
	commit := func() error {
		return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := executor.ExecuteInScope(ctx, tx, request, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
				callbackCalls++
				result, err := app.CommitPreparedPlanBatchInScope(ctx, tx, prepared)
				if err != nil {
					return idempotency.StoredResponse{}, err
				}
				if err := insertProbe(tx); err != nil {
					return idempotency.StoredResponse{}, err
				}
				body, err := json.Marshal(result)
				return idempotency.StoredResponse{Status: 200, Body: body}, err
			})
			return err
		})
	}
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if callbackCalls != 1 {
		t.Fatalf("prepared batch replay callback calls=%d want 1", callbackCalls)
	}
	probeRows, err = scope.Count(ctx, "shoot_plan_readiness_items", "id = $2", probeReadinessID)
	if err != nil || probeRows != 1 {
		t.Fatalf("prepared batch probe rows=%d err=%v, want 1", probeRows, err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil || detail.Revision != plan.Revision+1 || len(detail.Shots) != 1 {
		t.Fatalf("prepared batch commit detail=%+v err=%v", detail, err)
	}
}

func TestApplicationRunModeHistoryVoidAndFinalizationLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-run-lifecycle-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "run-plan-create", shootplanning.CreatePlanInput{Title: "Run", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	shotTitle := "镜头"
	mutation, err := app.ApplyPlanCommand(ctx, scope, "run-shot-create", plan.ID, plan.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &shotTitle}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	mutation, err = app.ApplyPlanCommand(ctx, scope, "run-window-create", plan.ID, mutation.Revision,
		shootplanning.SetExecutionWindowCommand{
			StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Timezone: "Asia/Shanghai",
			LiveWindowStartsAt: now.Add(-2 * time.Hour), LiveWindowEndsAt: now.Add(2 * time.Hour),
		})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.TransitionPlan(ctx, scope, "run-mark-ready", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := app.OpenRunSession(ctx, scope, "run-session-open", plan.ID, ready.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if session.Session.CaptureMode != shootplanning.CaptureModeLive || session.PlanRevision != ready.Revision+1 || len(session.Input.Shots) != 1 {
		t.Fatalf("unexpected live session: %+v", session)
	}
	var storedFingerprint []byte
	if err := scope.QueryRow(ctx, "shoot_plan_run_sessions", "idempotency_key_fingerprint", "id = $2", session.Session.ID).Scan(&storedFingerprint); err != nil {
		t.Fatal(err)
	}
	expectedFingerprint := sha256.Sum256([]byte("shoot-plan-run-lifecycle-acct\x00shoot-plan.run-session.open.v1\x00run-session-open"))
	if !bytes.Equal(storedFingerprint, expectedFingerprint[:]) {
		t.Fatalf("run session fingerprint=%x want account/operation-bound %x", storedFingerprint, expectedFingerprint)
	}
	replayedSession, err := app.OpenRunSession(ctx, scope, "run-session-open", plan.ID, ready.Revision)
	if err != nil || replayedSession.Session.ID != session.Session.ID {
		t.Fatalf("session replay=%+v err=%v", replayedSession, err)
	}
	secondSession, err := app.OpenRunSession(ctx, scope, "run-session-second", plan.ID, session.PlanRevision)
	if err != nil || secondSession.PlanRevision != session.PlanRevision || secondSession.Session.ID == session.Session.ID {
		t.Fatalf("in-progress session=%+v err=%v", secondSession, err)
	}
	shotID := session.Input.Shots[0].ID
	zeroExecutionFacts := int64(0)
	if _, err := app.TransitionPlan(ctx, scope, "run-complete-pending", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: session.PlanRevision, Kind: shootplanning.TransitionComplete,
		ExpectedExecutionFactRevision: &zeroExecutionFacts,
	}); !errors.Is(err, shootplanning.ErrShotsIncomplete) {
		t.Fatalf("pending shot completion error=%v", err)
	}
	skipReason := "preparation_missing"
	skipped, err := app.AppendShotResult(ctx, scope, "run-capture-skipped", plan.ID, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 0, SessionID: &session.Session.ID,
		Result: shootplanning.ShotResultSkipped, SkipReason: &skipReason,
	})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Event.Sequence != 1 || skipped.Event.CaptureMode != shootplanning.CaptureModeLive || skipped.CurrentOutcome == nil {
		t.Fatalf("unexpected skipped event: %+v", skipped)
	}
	if _, err := app.TransitionPlan(ctx, scope, "run-complete-stale-facts", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: session.PlanRevision, Kind: shootplanning.TransitionComplete,
		ExpectedExecutionFactRevision: &zeroExecutionFacts,
	}); !errors.Is(err, shootplanning.ErrExecutionRevisionConflict) {
		t.Fatalf("stale execution fact completion error=%v", err)
	}
	cleared, err := app.AppendShotResult(ctx, scope, "run-capture-cleared", plan.ID, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 1, SessionID: &session.Session.ID,
		Result: shootplanning.ShotResultCleared, SupersedesEventID: &skipped.Event.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Event.Sequence != 2 || cleared.CurrentOutcome != nil {
		t.Fatalf("cleared projection=%+v", cleared)
	}
	voidedClear, err := app.VoidExecutionEvent(ctx, scope, "run-void-cleared", plan.ID, cleared.Event.ID,
		shootplanning.VoidExecutionEventInput{ExpectedExecutionRevision: 2, Reason: "误清空"})
	if err != nil {
		t.Fatal(err)
	}
	if voidedClear.VoidEvent.Sequence != 3 || voidedClear.CurrentOutcome == nil || voidedClear.CurrentOutcome.EventID != skipped.Event.ID {
		t.Fatalf("void cleared did not restore skipped event: %+v", voidedClear)
	}
	executionFactRevision := voidedClear.ExecutionFactRevision
	completed, err := app.TransitionPlan(ctx, scope, "run-complete-first", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: session.PlanRevision, Kind: shootplanning.TransitionComplete,
		ExpectedExecutionFactRevision: &executionFactRevision,
	})
	if err != nil || completed.FinalizationRevision == nil || *completed.FinalizationRevision != 1 {
		t.Fatalf("first completion=%+v err=%v", completed, err)
	}
	var closedAt *time.Time
	if err := scope.QueryRow(ctx, "shoot_plan_run_sessions", "closed_at", "plan_id = $2 AND id = $3", plan.ID, session.Session.ID).Scan(&closedAt); err != nil || closedAt == nil {
		t.Fatalf("completion did not close run session: closed_at=%v err=%v", closedAt, err)
	}
	if _, err := app.AppendShotResult(ctx, scope, "run-capture-completed", plan.ID, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 3, Result: shootplanning.ShotResultCaptured, SupersedesEventID: &skipped.Event.ID,
	}); !errors.Is(err, shootplanning.ErrInvalidPlanTransition) {
		t.Fatalf("capture while completed error=%v", err)
	}
	reopened, err := app.TransitionPlan(ctx, scope, "run-reopen", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: completed.Revision, Kind: shootplanning.TransitionReopen,
	})
	if err != nil || reopened.Status != shootplanning.PlanStatusInProgress {
		t.Fatalf("reopen=%+v err=%v", reopened, err)
	}
	captured, err := app.AppendShotResult(ctx, scope, "run-capture-backfill", plan.ID, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 3, Result: shootplanning.ShotResultCaptured, SupersedesEventID: &skipped.Event.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Event.Sequence != 4 || captured.Event.CaptureMode != shootplanning.CaptureModeBackfill {
		t.Fatalf("unexpected backfill capture: %+v", captured)
	}
	voidedSkip, err := app.VoidExecutionEvent(ctx, scope, "run-void-skipped", plan.ID, skipped.Event.ID,
		shootplanning.VoidExecutionEventInput{ExpectedExecutionRevision: 4, Reason: "误记缺失"})
	if err != nil {
		t.Fatal(err)
	}
	if voidedSkip.CurrentOutcome == nil || voidedSkip.CurrentOutcome.EventID != captured.Event.ID {
		t.Fatalf("void non-current event changed latest outcome: %+v", voidedSkip)
	}
	if _, err := app.VoidExecutionEvent(ctx, scope, "run-void-skipped-again", plan.ID, skipped.Event.ID,
		shootplanning.VoidExecutionEventInput{ExpectedExecutionRevision: 5, Reason: "重复"}); !errors.Is(err, shootplanning.ErrExecutionEventAlreadyVoid) {
		t.Fatalf("double void error=%v", err)
	}
	executionFactRevision = voidedSkip.ExecutionFactRevision
	recompleted, err := app.TransitionPlan(ctx, scope, "run-complete-second", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: reopened.Revision, Kind: shootplanning.TransitionComplete,
		ExpectedExecutionFactRevision: &executionFactRevision,
	})
	if err != nil || recompleted.FinalizationRevision == nil || *recompleted.FinalizationRevision != 2 {
		t.Fatalf("second completion=%+v err=%v", recompleted, err)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != shootplanning.PlanStatusCompleted || len(detail.ExecutionFacts) != 5 || len(detail.Finalizations) != 2 {
		t.Fatalf("unexpected history detail: %+v", detail)
	}
	if len(detail.Finalizations[0].PreparationMissingEventIDs) != 1 || detail.Finalizations[0].PreparationMissingEventIDs[0] != skipped.Event.ID {
		t.Fatalf("first finalization lost live preparation gap: %+v", detail.Finalizations[0])
	}
	if len(detail.Finalizations[1].PreparationMissingEventIDs) != 0 || detail.Finalizations[0].OutcomeEventRefs[0] != skipped.Event.ID || detail.Finalizations[1].OutcomeEventRefs[0] != captured.Event.ID {
		t.Fatalf("finalization revisions drifted: %+v", detail.Finalizations)
	}
	reopenedForRemoval, err := app.TransitionPlan(ctx, scope, "run-reopen-remove", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: recompleted.Revision, Kind: shootplanning.TransitionReopen,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyPlanCommand(ctx, scope, "run-remove-shot-history", plan.ID, reopenedForRemoval.Revision,
		shootplanning.RemoveShotCommand{ShotID: shotID, AcknowledgeExecutionHistory: true}); err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, true)
	if err != nil || len(detail.Shots) != 0 || len(detail.ExecutionFacts) != 5 || len(detail.Finalizations) != 2 {
		t.Fatalf("soft-removed shot history was not retained: detail=%+v err=%v", detail, err)
	}
}

func TestConcurrentShotResultsWithSameExecutionRevisionAllowOneWinner(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-concurrent-result-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "concurrent-plan-create", shootplanning.CreatePlanInput{Title: "Concurrent", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	title := "镜头"
	mutation, err := app.ApplyPlanCommand(ctx, scope, "concurrent-shot-create", plan.ID, plan.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &title}})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.TransitionPlan(ctx, scope, "concurrent-mark-ready", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := app.OpenRunSession(ctx, scope, "concurrent-session-open", plan.ID, ready.Revision)
	if err != nil {
		t.Fatal(err)
	}
	shotID := session.Input.Shots[0].ID
	errorsCh := make(chan error, 2)
	for _, key := range []string{"concurrent-capture-a", "concurrent-capture-b"} {
		key := key
		go func() {
			_, err := app.AppendShotResult(ctx, scope, key, plan.ID, shotID, shootplanning.AppendShotResultInput{
				ExpectedExecutionRevision: 0, Result: shootplanning.ShotResultCaptured,
			})
			errorsCh <- err
		}()
	}
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-errorsCh
		switch {
		case err == nil:
			successes++
		case errors.Is(err, shootplanning.ErrExecutionRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent capture error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent capture outcomes success=%d conflict=%d", successes, conflicts)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, true)
	if err != nil || len(detail.ExecutionFacts) != 1 || detail.Shots[0].ExecutionRevision != 1 {
		t.Fatalf("concurrent capture detail=%+v err=%v", detail, err)
	}
}

func TestConcurrentCaptureAndCompleteCannotCreateSnapshotGap(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-capture-complete-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "capture-complete-create", shootplanning.CreatePlanInput{Title: "Race", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	title := "镜头"
	mutation, err := app.ApplyPlanCommand(ctx, scope, "capture-complete-shot", plan.ID, plan.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: &title}})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.TransitionPlan(ctx, scope, "capture-complete-ready", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := app.OpenRunSession(ctx, scope, "capture-complete-session", plan.ID, ready.Revision)
	if err != nil {
		t.Fatal(err)
	}
	shotID := session.Input.Shots[0].ID
	first, err := app.AppendShotResult(ctx, scope, "capture-complete-first", plan.ID, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 0, Result: shootplanning.ShotResultCaptured,
	})
	if err != nil {
		t.Fatal(err)
	}
	type raceResult struct {
		kind string
		err  error
	}
	results := make(chan raceResult, 2)
	go func() {
		_, err := app.AppendShotResult(ctx, scope, "capture-complete-second", plan.ID, shotID, shootplanning.AppendShotResultInput{
			ExpectedExecutionRevision: 1, Result: shootplanning.ShotResultCaptured, SupersedesEventID: &first.Event.ID,
		})
		results <- raceResult{kind: "capture", err: err}
	}()
	go func() {
		expectedFacts := first.ExecutionFactRevision
		_, err := app.TransitionPlan(ctx, scope, "capture-complete-finish", plan.ID, shootplanning.PlanTransition{
			ExpectedRevision: session.PlanRevision, Kind: shootplanning.TransitionComplete,
			ExpectedExecutionFactRevision: &expectedFacts,
		})
		results <- raceResult{kind: "complete", err: err}
	}()
	successes := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err == nil {
			successes++
			continue
		}
		if result.kind == "capture" && !errors.Is(result.err, shootplanning.ErrInvalidPlanTransition) {
			t.Fatalf("capture loser error=%v", result.err)
		}
		if result.kind == "complete" && !errors.Is(result.err, shootplanning.ErrExecutionRevisionConflict) {
			t.Fatalf("complete loser error=%v", result.err)
		}
	}
	if successes != 1 {
		t.Fatalf("capture/complete race successes=%d want 1", successes)
	}
	detail, err := app.GetPlan(ctx, scope, plan.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status == shootplanning.PlanStatusCompleted {
		if len(detail.ExecutionFacts) != 1 || len(detail.Finalizations) != 1 || detail.Finalizations[0].OutcomeEventRefs[0] != first.Event.ID {
			t.Fatalf("completion winner snapshot gap: %+v", detail)
		}
	} else if len(detail.ExecutionFacts) != 2 || len(detail.Finalizations) != 0 {
		t.Fatalf("capture winner left partial completion: %+v", detail)
	}
}

func promoteArchiveCapability(
	t *testing.T,
	ctx context.Context,
	db *store.Store,
	expectedRevision int64,
	target planningcapability.ArchiveCapability,
) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ArchiveCapabilityPromoter().Promote(ctx, expectedRevision, target, planningcapability.ReleaseReadinessEvidence{
		Digest: "shootplanning-test-digest", Environment: "test", Deployment: "shootplanning-test",
		Target: target, CurrentRevision: expectedRevision,
		GeneratedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute),
		LiveBuildsCompatible: true, TargetWiringReady: true,
	}); err != nil {
		t.Fatalf("promote archive capability to %s: %v", target, err)
	}
}

func stringPointer(value string) *string { return &value }

func TestPlanListEnrichmentProjection(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-list-enrich-acct")
	app, err := shootplanning.NewApplication(shootplanning.NewPostgresRepository(), idempotency.NewExecutor())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := app.CreatePlan(ctx, scope, "list-enrich-create", shootplanning.CreatePlanInput{Title: "列表增强", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}

	findItem := func() shootplanning.PlanListItem {
		result, listErr := app.ListPlans(ctx, scope, shootplanning.ListPlansFilter{Page: 1, PageSize: 20})
		if listErr != nil {
			t.Fatal(listErr)
		}
		for _, item := range result.Items {
			if item.ID == plan.ID {
				return item
			}
		}
		t.Fatalf("plan %s missing from list", plan.ID)
		return shootplanning.PlanListItem{}
	}

	independent := findItem()
	if independent.CRM.CustomerID != nil || independent.CRM.CustomerName != nil || independent.CRM.OrderID != nil ||
		independent.CRM.OrderTitle != nil || independent.CRM.OrderStatusAtLink != nil {
		t.Fatalf("independent crm summary=%+v", independent.CRM)
	}
	if independent.ExecutionWindow != nil {
		t.Fatalf("independent window=%+v", independent.ExecutionWindow)
	}
	if independent.ExecutionStats.Captured != 0 || independent.ExecutionStats.Skipped != 0 {
		t.Fatalf("independent stats=%+v", independent.ExecutionStats)
	}
	if independent.ReadinessSummary.RequiredTotal != 0 || independent.ReadinessSummary.RequiredUnchecked != 0 {
		t.Fatalf("independent readiness=%+v", independent.ReadinessSummary)
	}

	engine := crm.NewEngine()
	linkedCustomer := createCRMCustomer(t, customer.NewService(customer.NewPostgresRepository()), scope, "阿晚")
	if outcome := applyCRM(t, scope, engine, plan.ID, plan.Revision, crm.Command{
		Kind: crm.KindLinkCustomer, CustomerID: &linkedCustomer.ID,
	}); outcome.Noop {
		t.Fatalf("link customer was a noop: %+v", outcome)
	}
	linked := findItem()
	if linked.CRM.CustomerID == nil || *linked.CRM.CustomerID != linkedCustomer.ID || linked.CRM.CustomerName == nil || *linked.CRM.CustomerName != "阿晚" {
		t.Fatalf("linked crm summary=%+v", linked.CRM)
	}
	if linked.CRM.OrderID != nil {
		t.Fatalf("linked order should stay empty: %+v", linked.CRM)
	}

	now := time.Now().UTC().Truncate(time.Second)
	detail, err := app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := app.ApplyPlanCommand(ctx, scope, "list-enrich-window", plan.ID, detail.Revision,
		shootplanning.SetExecutionWindowCommand{
			StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Timezone: "Asia/Shanghai",
			LiveWindowStartsAt: now.Add(-2 * time.Hour), LiveWindowEndsAt: now.Add(2 * time.Hour),
		})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "list-enrich-shot-a", plan.ID, mutation.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: stringPointer("镜头一")}})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err = app.ApplyPlanCommand(ctx, scope, "list-enrich-shot-b", plan.ID, mutation.Revision,
		shootplanning.UpsertShotCommand{Shot: shootplanning.ShotWrite{Title: stringPointer("镜头二")}})
	if err != nil {
		t.Fatal(err)
	}
	for index, title := range []string{"必需甲", "必需乙"} {
		mutation, err = app.ApplyPlanCommand(ctx, scope, fmt.Sprintf("list-enrich-ready-%d", index+1), plan.ID, mutation.Revision,
			shootplanning.UpsertReadinessCommand{Item: shootplanning.ReadinessWrite{
				Category: stringPointer("styling"), Title: stringPointer(title), Requirement: stringPointer("required"),
				PreflightStatus: stringPointer("unchecked"), ResponsibilityHint: stringPointer("photographer"),
			}})
		if err != nil {
			t.Fatal(err)
		}
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range detail.ReadinessItems {
		mutation, err = app.ApplyPlanCommand(ctx, scope, fmt.Sprintf("list-enrich-check-%d", index+1), plan.ID, mutation.Revision,
			shootplanning.SetPreflightCommand{ReadinessID: item.ID, PreflightStatus: "checked"})
		if err != nil {
			t.Fatal(err)
		}
	}
	firstRequired := detail.ReadinessItems[0].ID
	ready, err := app.TransitionPlan(ctx, scope, "list-enrich-mark-ready", plan.ID, shootplanning.PlanTransition{
		ExpectedRevision: mutation.Revision, Kind: shootplanning.TransitionMarkReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := app.OpenRunSession(ctx, scope, "list-enrich-session", plan.ID, ready.Revision)
	if err != nil {
		t.Fatal(err)
	}
	shotA := session.Input.Shots[0].ID
	shotB := session.Input.Shots[1].ID
	if _, err := app.AppendShotResult(ctx, scope, "list-enrich-capture", plan.ID, shotA, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 0, SessionID: &session.Session.ID, Result: shootplanning.ShotResultCaptured,
	}); err != nil {
		t.Fatal(err)
	}
	skipReason := "preparation_missing"
	if _, err := app.AppendShotResult(ctx, scope, "list-enrich-skip", plan.ID, shotB, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: 0, SessionID: &session.Session.ID,
		Result: shootplanning.ShotResultSkipped, SkipReason: &skipReason,
	}); err != nil {
		t.Fatal(err)
	}
	detail, err = app.GetPlan(ctx, scope, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApplyPlanCommand(ctx, scope, "list-enrich-uncheck", plan.ID, detail.Revision,
		shootplanning.SetPreflightCommand{ReadinessID: firstRequired, PreflightStatus: "unchecked"}); err != nil {
		t.Fatal(err)
	}

	final := findItem()
	if final.ExecutionWindow == nil || final.ExecutionWindow.Source != "manual" ||
		!final.ExecutionWindow.StartsAt.Equal(now.Add(-time.Hour)) || !final.ExecutionWindow.EndsAt.Equal(now.Add(time.Hour)) ||
		final.ExecutionWindow.Timezone != "Asia/Shanghai" {
		t.Fatalf("final window=%+v", final.ExecutionWindow)
	}
	if final.ExecutionStats.Captured != 1 || final.ExecutionStats.Skipped != 1 {
		t.Fatalf("final stats=%+v", final.ExecutionStats)
	}
	if final.ReadinessSummary.RequiredTotal != 2 || final.ReadinessSummary.RequiredUnchecked != 1 {
		t.Fatalf("final readiness=%+v", final.ReadinessSummary)
	}
	if final.PublicScale.PlannedShotCount != 2 {
		t.Fatalf("final shot count=%d", final.PublicScale.PlannedShotCount)
	}
}

func TestPostgresRepositoryPlanRevisionCAS(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "shoot-plan-cas-acct")
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "CAS", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := repo.LockPlan(ctx, tx, plan.ID)
		if err != nil {
			return err
		}
		_, err = repo.AdvancePlanRevision(ctx, tx, plan.ID, locked.Revision)
		return err
	}); err != nil {
		t.Fatalf("first CAS: %v", err)
	}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := repo.AdvancePlanRevision(ctx, tx, plan.ID, plan.Revision)
		return err
	}); !errors.Is(err, shootplanning.ErrPlanRevisionConflict) {
		t.Fatalf("stale CAS error = %v", err)
	}
}

func TestPlanningArchiveCapabilityFreshInstallAndTransactionReader(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "archive-capability-acct")

	startup, err := db.ArchiveCapabilityStartupReader().Current(ctx)
	if err != nil {
		t.Fatalf("startup capability: %v", err)
	}
	if startup.SingletonKey != planningcapability.SingletonKey ||
		startup.Capability != planningcapability.ArchiveCapabilityCore ||
		startup.Revision != 1 || startup.PromotedAt.IsZero() {
		t.Fatalf("unexpected bootstrap capability: %+v", startup)
	}

	var transactionState planningcapability.ArchiveCapabilityState
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		transactionState, err = tx.ArchiveCapability().Current(ctx)
		return err
	}); err != nil {
		t.Fatalf("transaction capability: %v", err)
	}
	if transactionState != startup {
		t.Fatalf("startup and transaction readers differ: startup=%+v tx=%+v", startup, transactionState)
	}
}

func TestPlanningReminderFenceReservesAndAdvancesContiguously(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "planning-fence-acct")
	repo := shootplanning.NewPostgresRepository()
	planA, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "A", Subject: "主体 A"})
	if err != nil {
		t.Fatalf("create plan A: %v", err)
	}
	planB, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "B", Subject: "主体 B"})
	if err != nil {
		t.Fatalf("create plan B: %v", err)
	}
	if planA.ID > planB.ID {
		planA, planB = planB, planA
	}

	var generationA, generationB int64
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		generationA, err = locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: planA.ID, MutationKind: planningreminder.MutationPlanArchived})
		if err != nil {
			return err
		}
		generationB, err = locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: planB.ID, MutationKind: planningreminder.MutationPlanArchived})
		if err != nil {
			return err
		}
		if err := tx.Insert(ctx, "planning_reminder_generation_resolutions",
			[]string{"generation", "plan_id", "mutation_kind", "resolution_kind", "source_event_id", "resolved_at"},
			generationA, planA.ID, string(planningreminder.MutationPlanArchived), "lifecycle_applied", nil, time.Now().UTC()); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "planning_reminder_generation_resolutions",
			[]string{"generation", "plan_id", "mutation_kind", "resolution_kind", "source_event_id", "resolved_at"},
			generationB, planB.ID, string(planningreminder.MutationPlanArchived), "lifecycle_applied", nil, time.Now().UTC()); err != nil {
			return err
		}
		if err := locked.MarkApplied(ctx, generationB); err != nil {
			return err
		}
		return locked.MarkApplied(ctx, generationA)
	})
	if err != nil {
		t.Fatalf("fence transaction: %v", err)
	}
	if generationA != 1 || generationB != 2 {
		t.Fatalf("generations = %d,%d want 1,2", generationA, generationB)
	}
	var target, applied int64
	if err := scope.QueryRow(ctx, "planning_reminder_account_generations", "target_generation, applied_generation", "TRUE").Scan(&target, &applied); err != nil {
		t.Fatalf("load generation state: %v", err)
	}
	if target != 2 || applied != 2 {
		t.Fatalf("generation state = %d/%d want 2/2", target, applied)
	}
}

func TestPlanningReminderFenceRollbackLeavesNoGenerationGap(t *testing.T) {
	ctx := context.Background()
	db := openPlanningStore(t)
	scope := createPlanningAccount(t, db, "planning-fence-rollback-acct")
	plan, err := shootplanning.NewPostgresRepository().Create(ctx, scope, shootplanning.CreatePlanInput{Title: "回滚", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected rollback")
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		if _, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: plan.ID, MutationKind: planningreminder.MutationPlanArchived}); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("rollback error = %v", err)
	}
	var generation int64
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		generation, err = locked.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: plan.ID, MutationKind: planningreminder.MutationPlanArchived})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if generation != 1 {
		t.Fatalf("generation after rollback = %d want 1", generation)
	}
}

func openPlanningStore(t *testing.T) *store.Store {
	t.Helper()
	db, _ := openPlanningStoreWithURL(t)
	return db
}

func openPlanningStoreWithURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	ctx := context.Background()
	url := storetest.NewURL(t)
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(db.Close)
	return db, url
}

func createPlanningAccount(t *testing.T, db *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := db.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return db.ScopeFor(auth.AccountContext{AccountID: id})
}
