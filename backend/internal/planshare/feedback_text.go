package planshare

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	anonymousDisplayName    = "匿名"
	nicknameMaxRunes        = 40
	feedbackContentMinRunes = 1
	feedbackContentMaxRunes = 2000
)

func normalizeAuthorDisplayName(raw string) (string, error) {
	if strings.ContainsAny(raw, "<>") || looksLikeHTML(raw) {
		return "", validationError("author_display_name must be plain text")
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return anonymousDisplayName, nil
	}
	if utf8.RuneCountInString(trimmed) > nicknameMaxRunes {
		return "", validationError("author_display_name exceeds 40 runes")
	}
	return trimmed, nil
}

func normalizeFeedbackContent(raw string) (string, error) {
	if strings.ContainsAny(raw, "<>") || looksLikeHTML(raw) {
		return "", validationError("content must be plain text")
	}
	if strings.TrimSpace(raw) == "" {
		return "", validationError("content required")
	}
	n := utf8.RuneCountInString(raw)
	if n < feedbackContentMinRunes || n > feedbackContentMaxRunes {
		return "", validationError("content length out of range")
	}
	return raw, nil
}

func looksLikeHTML(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "<script") ||
		strings.Contains(lower, "</") ||
		strings.Contains(lower, "javascript:")
}

func rejectUnknownControlRunes(s string) error {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return validationError("text contains control characters")
		}
	}
	return nil
}
