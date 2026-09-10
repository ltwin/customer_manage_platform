package creativeops

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"
)

// PageCursor binds a position to the authenticated account and query.
// Repositories still scope every row; the cursor is not an authorization credential.
type PageCursor struct {
	Query string    `json:"query"`
	Time  time.Time `json:"time"`
	ID    string    `json:"id"`
}

func pageQuery(account, query string) string {
	sum := sha256.Sum256([]byte(account + "\x00" + query))
	return hex.EncodeToString(sum[:])
}
func DecodePageCursor(raw, account, query string) (PageCursor, error) {
	if raw == "" {
		return PageCursor{}, nil
	}
	if len(raw) > 1024 {
		return PageCursor{}, ErrValidation
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return PageCursor{}, ErrValidation
	}
	var c PageCursor
	if err := Decode(data, &c); err != nil || c.Query != pageQuery(account, query) || c.Time.IsZero() || c.ID == "" {
		return PageCursor{}, ErrValidation
	}
	return c, nil
}
func EncodePageCursor(account, query string, at time.Time, id string) (string, error) {
	data, err := json.Marshal(PageCursor{Query: pageQuery(account, query), Time: at, ID: id})
	return base64.RawURLEncoding.EncodeToString(data), err
}
