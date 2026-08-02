package accountprofile

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

func normalizeDisplayName(input OptionalString) (value *string, clear bool, err error) {
	if !input.Set {
		return nil, false, ValidationError{Message: "display_name 必填（可设为 null 清除）"}
	}
	if input.Value == nil {
		return nil, true, nil
	}
	trimmed := strings.TrimSpace(*input.Value)
	if trimmed == "" {
		return nil, false, ValidationError{Message: "display_name 不能为空字符串"}
	}
	if containsControlRune(trimmed) {
		return nil, false, ValidationError{Message: "display_name 禁止控制字符"}
	}
	if graphemeCount(trimmed) > MaxDisplayNameGraphemes {
		return nil, false, ValidationError{Message: "display_name 最多 40 个 Unicode grapheme"}
	}
	return &trimmed, false, nil
}

func graphemeCount(value string) int {
	count := 0
	gr := uniseg.NewGraphemes(value)
	for gr.Next() {
		count++
	}
	return count
}

func containsControlRune(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func sameOptionalDisplayName(current *string, next *string) bool {
	if current == nil && next == nil {
		return true
	}
	if current == nil || next == nil {
		return false
	}
	return *current == *next
}
