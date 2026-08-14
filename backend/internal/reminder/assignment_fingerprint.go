package reminder

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
)

// CanonicalGroupMemberV1 是 fingerprint 成员字段。
type CanonicalGroupMemberV1 struct {
	AssignmentID       string
	AssignmentRevision int64
	ContentFingerprint string
}

// CanonicalPlanAssignmentReminderGroupV1Input 是唯一 fingerprint 输入。
type CanonicalPlanAssignmentReminderGroupV1Input struct {
	AccountID       string
	PlanID          string
	OrderID         string
	SlotID          string
	DueDate         string // YYYY-MM-DD
	LeadRuleVersion string
	Members         []CanonicalGroupMemberV1 // 调用方可任意顺序；编码前按 assignment_id 排序
}

// CanonicalPlanAssignmentReminderGroupV1 生成 length-prefixed canonical bytes。
// 禁止歧义字符串拼接；分段边界、控制字符与非 ASCII 均进入字段字节原样。
func CanonicalPlanAssignmentReminderGroupV1(in CanonicalPlanAssignmentReminderGroupV1Input) []byte {
	members := append([]CanonicalGroupMemberV1(nil), in.Members...)
	sort.Slice(members, func(i, j int) bool {
		return members[i].AssignmentID < members[j].AssignmentID
	})

	var buf []byte
	buf = appendLengthPrefixed(buf, []byte(canonicalGroupVersion))
	buf = appendLengthPrefixed(buf, []byte(in.AccountID))
	buf = appendLengthPrefixed(buf, []byte(in.PlanID))
	buf = appendLengthPrefixed(buf, []byte(in.OrderID))
	buf = appendLengthPrefixed(buf, []byte(in.SlotID))
	buf = appendLengthPrefixed(buf, []byte(in.DueDate))
	buf = appendLengthPrefixed(buf, []byte(in.LeadRuleVersion))
	buf = appendLengthPrefixed(buf, u64Bytes(uint64(len(members))))
	for _, m := range members {
		buf = appendLengthPrefixed(buf, []byte(m.AssignmentID))
		buf = appendLengthPrefixed(buf, []byte(strconv.FormatInt(m.AssignmentRevision, 10)))
		buf = appendLengthPrefixed(buf, []byte(m.ContentFingerprint))
	}
	return buf
}

// GroupFingerprintSHA256 返回 lowercase hex SHA-256(canonical bytes)。
func GroupFingerprintSHA256(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// PlanAssignmentChecklistDedupKey 是 Reminder occurrence identity。
func PlanAssignmentChecklistDedupKey(groupID string) string {
	return "plan_assignment_checklist:v1:group:" + groupID
}

func appendLengthPrefixed(dst, field []byte) []byte {
	if len(field) > int(^uint32(0)) {
		panic(fmt.Sprintf("canonical field too large: %d", len(field)))
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(field)))
	dst = append(dst, length[:]...)
	dst = append(dst, field...)
	return dst
}

func u64Bytes(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}
