package reminder

import (
	"fmt"
	"sort"
	"time"
)

// ReduceDesiredReminderGroups 是 pure reducer：eligibility / date-only due / grouping。
// repository 负责锁与落盘；HTTP 不得复制 due/fingerprint。
func ReduceDesiredReminderGroups(in ReduceDesiredReminderGroupsInput) (ReduceDesiredReminderGroupsOutput, error) {
	if in.AccountID == "" || in.PlanID == "" {
		return ReduceDesiredReminderGroupsOutput{}, fmt.Errorf("account_id and plan_id required")
	}
	if in.Timezone == "" {
		return ReduceDesiredReminderGroupsOutput{}, fmt.Errorf("timezone required")
	}
	if in.ActivationGeneration < 1 {
		return ReduceDesiredReminderGroupsOutput{}, fmt.Errorf("activation_generation must be >= 1")
	}

	sources := make([]DesiredSourceState, 0, len(in.Assignments))
	type groupAccum struct {
		orderID         string
		slotID          string
		dueDate         string
		leadRuleVersion string
		validUntil      DesiredReminderGroup
		members         []DesiredMember
	}
	groups := map[string]*groupAccum{}

	schedulable := futureShootSlot(in.Sidecar, in.Now) && !in.Sidecar.PlanArchived &&
		in.Sidecar.OrderID != nil && in.Sidecar.OrderActive

	for _, a := range in.Assignments {
		src, err := projectSource(a)
		if err != nil {
			return ReduceDesiredReminderGroupsOutput{}, err
		}
		if src.SourceState == SourceStateRevoked {
			src.ProjectionState = ProjectionStateRevoked
			sources = append(sources, src)
			continue
		}
		if src.AssignmentKind == AssignmentKindOnSiteSupport {
			src.ProjectionState = ProjectionStateIneligibleOnSite
			sources = append(sources, src)
			continue
		}
		// readiness active
		if !schedulable {
			src.ProjectionState = ProjectionStateUnscheduled
			sources = append(sources, src)
			continue
		}
		leadDays := *src.PreparationLeadDaysSnapshot
		leadRule := *src.LeadRuleVersion
		due, validUntil, err := ComputeAssignmentDueDate(*in.Sidecar.SlotStartAt, in.Timezone, leadDays, in.Now)
		if err != nil {
			src.ProjectionState = ProjectionStateUnscheduled
			sources = append(sources, src)
			continue
		}
		src.ProjectionState = ProjectionStateGrouped
		sources = append(sources, src)

		dueStr := FormatDate(due)
		slotID := *in.Sidecar.SlotID
		orderID := *in.Sidecar.OrderID
		bucket := slotID + "\x00" + dueStr + "\x00" + leadRule
		acc, ok := groups[bucket]
		if !ok {
			acc = &groupAccum{
				orderID:         orderID,
				slotID:          slotID,
				dueDate:         dueStr,
				leadRuleVersion: leadRule,
				validUntil: DesiredReminderGroup{
					OrderID:              orderID,
					SlotID:               slotID,
					DueDate:              due,
					TimezoneSnapshot:     in.Timezone,
					ValidUntil:           validUntil,
					LeadRuleVersion:      leadRule,
					ActivationGeneration: in.ActivationGeneration,
				},
			}
			groups[bucket] = acc
		}
		acc.members = append(acc.members, DesiredMember{
			AssignmentID:       src.AssignmentID,
			AssignmentRevision: src.AssignmentRevision,
			ContentFingerprint: src.ContentFingerprint,
			ContentSnapshot:    src.ContentSnapshot,
		})
	}

	outGroups := make([]DesiredReminderGroup, 0, len(groups))
	for _, acc := range groups {
		sort.Slice(acc.members, func(i, j int) bool {
			return acc.members[i].AssignmentID < acc.members[j].AssignmentID
		})
		canonMembers := make([]CanonicalGroupMemberV1, 0, len(acc.members))
		for i := range acc.members {
			acc.members[i].Position = i
			canonMembers = append(canonMembers, CanonicalGroupMemberV1{
				AssignmentID:       acc.members[i].AssignmentID,
				AssignmentRevision: acc.members[i].AssignmentRevision,
				ContentFingerprint: acc.members[i].ContentFingerprint,
			})
		}
		fp := GroupFingerprintSHA256(CanonicalPlanAssignmentReminderGroupV1(CanonicalPlanAssignmentReminderGroupV1Input{
			AccountID:       in.AccountID,
			PlanID:          in.PlanID,
			OrderID:         acc.orderID,
			SlotID:          acc.slotID,
			DueDate:         acc.dueDate,
			LeadRuleVersion: acc.leadRuleVersion,
			Members:         canonMembers,
		}))
		g := acc.validUntil
		g.GroupFingerprint = fp
		g.Members = acc.members
		outGroups = append(outGroups, g)
	}
	sort.Slice(outGroups, func(i, j int) bool {
		if outGroups[i].LeadRuleVersion != outGroups[j].LeadRuleVersion {
			return outGroups[i].LeadRuleVersion < outGroups[j].LeadRuleVersion
		}
		if outGroups[i].SlotID != outGroups[j].SlotID {
			return outGroups[i].SlotID < outGroups[j].SlotID
		}
		return FormatDate(outGroups[i].DueDate) < FormatDate(outGroups[j].DueDate)
	})
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].AssignmentID < sources[j].AssignmentID
	})
	return ReduceDesiredReminderGroupsOutput{Sources: sources, Groups: outGroups}, nil
}

func projectSource(a SafeAssignmentInput) (DesiredSourceState, error) {
	if a.AssignmentID == "" || a.AssignmentRevision < 1 || a.ContentFingerprint == "" || a.ContentSnapshot == "" {
		return DesiredSourceState{}, fmt.Errorf("assignment source incomplete")
	}
	switch a.AssignmentKind {
	case AssignmentKindReadiness:
		if a.ReadinessItemID == nil || *a.ReadinessItemID == "" {
			return DesiredSourceState{}, fmt.Errorf("readiness assignment missing readiness_item_id")
		}
		if a.PreparationLeadDaysSnapshot == nil || a.LeadRuleVersion == nil || *a.LeadRuleVersion == "" {
			return DesiredSourceState{}, fmt.Errorf("readiness assignment missing lead snapshot")
		}
	case AssignmentKindOnSiteSupport:
		if a.PreparationLeadDaysSnapshot != nil || a.LeadRuleVersion != nil || a.ReadinessItemID != nil {
			return DesiredSourceState{}, fmt.Errorf("on-site assignment must not carry lead/readiness fields")
		}
	default:
		return DesiredSourceState{}, fmt.Errorf("unknown assignment_kind %q", a.AssignmentKind)
	}
	state := a.SourceState
	if state == "" {
		state = SourceStateActive
	}
	if state != SourceStateActive && state != SourceStateRevoked {
		return DesiredSourceState{}, fmt.Errorf("invalid source_state %q", state)
	}
	return DesiredSourceState{
		AssignmentID:                 a.AssignmentID,
		AssignmentRevision:           a.AssignmentRevision,
		AssignmentKind:               a.AssignmentKind,
		ReadinessItemID:              a.ReadinessItemID,
		ContentSnapshot:              a.ContentSnapshot,
		ClaimedByDisplayNameSnapshot: a.ClaimedByDisplayNameSnapshot,
		ContentFingerprint:           a.ContentFingerprint,
		PreparationLeadDaysSnapshot:  a.PreparationLeadDaysSnapshot,
		LeadRuleVersion:              a.LeadRuleVersion,
		SourceState:                  state,
		SourceOccurredAt:             a.SourceOccurredAt,
	}, nil
}

func futureShootSlot(sidecar PlanOrderSlotSidecar, now time.Time) bool {
	if sidecar.SlotID == nil || *sidecar.SlotID == "" || sidecar.SlotStartAt == nil {
		return false
	}
	if sidecar.SlotType != slotTypeShoot {
		return false
	}
	return sidecar.SlotStartAt.After(now)
}
