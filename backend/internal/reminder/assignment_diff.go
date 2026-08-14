package reminder

import (
	"time"
)

// DiffDesiredAgainstCurrent 实现 §1.5 真值表（pure）：比较 fingerprint+valid_until+timezone_snapshot。
func DiffDesiredAgainstCurrent(
	current []CurrentReminderGroup,
	desired []DesiredReminderGroup,
	withdrawReasonForRemoved func(CurrentReminderGroup) string,
) ProjectionWritePlan {
	if withdrawReasonForRemoved == nil {
		withdrawReasonForRemoved = func(CurrentReminderGroup) string {
			return WithdrawReasonSourceChanged
		}
	}

	currentByBucket := map[string]CurrentReminderGroup{}
	for _, g := range current {
		if g.State != GroupStateCurrent {
			continue
		}
		currentByBucket[groupBucketKey(g.SlotID, g.DueDate, g.LeadRuleVersion)] = g
	}

	desiredBuckets := map[string]struct{}{}
	plan := ProjectionWritePlan{}
	for _, d := range desired {
		bucket := groupBucketKey(d.SlotID, d.DueDate, d.LeadRuleVersion)
		desiredBuckets[bucket] = struct{}{}
		cur, ok := currentByBucket[bucket]
		if !ok {
			plan.Creates = append(plan.Creates, d)
			continue
		}
		sameFP := cur.GroupFingerprint == d.GroupFingerprint
		sameValid := cur.ValidUntil.Equal(d.ValidUntil)
		sameTZ := cur.TimezoneSnapshot == d.TimezoneSnapshot
		if sameFP && sameValid && sameTZ {
			plan.NoOpGroupIDs = append(plan.NoOpGroupIDs, cur.GroupID)
			continue
		}
		if sameFP && (!sameValid || !sameTZ) {
			plan.TemporalUpdates = append(plan.TemporalUpdates, TemporalGroupUpdate{
				GroupID:          cur.GroupID,
				ReminderID:       cur.ReminderID,
				ValidUntil:       d.ValidUntil,
				TimezoneSnapshot: d.TimezoneSnapshot,
				NextValidityRev:  cur.ValidityRevision + 1,
			})
			continue
		}
		// fingerprint changed within bucket → withdraw old + create new
		plan.Withdrawals = append(plan.Withdrawals, GroupWithdrawal{
			GroupID:    cur.GroupID,
			ReminderID: cur.ReminderID,
			Reason:     WithdrawReasonSourceChanged,
			Dismiss:    cur.ReminderStatus == StatusPending,
		})
		plan.Creates = append(plan.Creates, d)
	}

	for bucket, cur := range currentByBucket {
		if _, ok := desiredBuckets[bucket]; ok {
			continue
		}
		plan.Withdrawals = append(plan.Withdrawals, GroupWithdrawal{
			GroupID:    cur.GroupID,
			ReminderID: cur.ReminderID,
			Reason:     withdrawReasonForRemoved(cur),
			Dismiss:    cur.ReminderStatus == StatusPending,
		})
	}
	return plan
}

// InferWithdrawReason 根据 sidecar / source 聚合态推断撤回原因。
func InferWithdrawReason(sidecar PlanOrderSlotSidecar, sources []DesiredSourceState, now time.Time) string {
	if sidecar.PlanArchived {
		return WithdrawReasonPlanArchived
	}
	if sidecar.OrderID != nil && !sidecar.OrderActive {
		return WithdrawReasonOrderInactive
	}
	allRevoked := len(sources) > 0
	for _, s := range sources {
		if s.SourceState != SourceStateRevoked {
			allRevoked = false
			break
		}
	}
	if allRevoked {
		return WithdrawReasonAssignmentRevoked
	}
	if sidecar.SlotStartAt != nil && !sidecar.SlotStartAt.After(now) {
		return WithdrawReasonShootStarted
	}
	if !futureShootSlot(sidecar, now) {
		return WithdrawReasonSlotUnavailable
	}
	return WithdrawReasonSourceChanged
}

func groupBucketKey(slotID string, dueDate time.Time, leadRuleVersion string) string {
	return slotID + "\x00" + FormatDate(dueDate) + "\x00" + leadRuleVersion
}
