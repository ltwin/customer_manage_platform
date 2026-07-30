package customer_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
)

// REV-001：merge 与 PATCH 并发时 merged 只读守护不被 TOCTOU 绕过——
// 并发 Update 要么先于 merge 落在 active 客户上（合法），要么被行锁串行化后
// 读到 merged 状态收 ErrCustomerMerged；不允许出现「写入已 merged 客户」。
func TestMergeConcurrentWriteGuard(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	for round := range 8 {
		target := createActiveCustomer(t, svc, scope, fmt.Sprintf("target-%d", round))
		source := createActiveCustomer(t, svc, scope, fmt.Sprintf("source-%d", round))

		var wg sync.WaitGroup
		var updated customer.Customer
		var updateErr, mergeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, mergeErr = svc.Merge(ctx, scope, target.ID, source.ID)
		}()
		go func() {
			defer wg.Done()
			updated, updateErr = svc.Update(ctx, scope, source.ID, customer.UpdateInput{RealName: valueString("并发写")})
		}()
		wg.Wait()

		if mergeErr != nil {
			t.Fatalf("round %d: merge should succeed, got %v", round, mergeErr)
		}
		switch {
		case updateErr == nil:
			// 合法时序：Update 先拿到锁、写在 active 客户上，随后才被 merge。
			if updated.Status != customer.StatusActive {
				t.Fatalf("round %d: update succeeded but landed on %s customer (TOCTOU)", round, updated.Status)
			}
		case errors.Is(updateErr, customer.ErrCustomerMerged):
			// 串行化后读到 merged：source 上不得有本次写入。
			detail, err := svc.Detail(ctx, scope, source.ID)
			if err != nil {
				t.Fatalf("round %d: source detail: %v", round, err)
			}
			if detail.RealName != nil {
				t.Fatalf("round %d: rejected update must not write, got %+v", round, detail.Customer)
			}
		default:
			t.Fatalf("round %d: unexpected update error: %v", round, updateErr)
		}
	}
}
