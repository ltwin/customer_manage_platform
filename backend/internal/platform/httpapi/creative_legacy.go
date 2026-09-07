package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/samson/customer-manage-platform/backend/internal/creativeworkspace"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const creativeLegacyGuardKey = "creative_legacy_write"

// All registered legacy mutations are covered, including future operations under
// the same routes. The database scope rechecks after acquiring the account lock.
func creativeLegacyMiddleware(pilot *creativeworkspace.Pilot, factory ScopeFactory) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pilot == nil || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		route := c.FullPath()
		legacy := strings.HasPrefix(route, "/api/v1/shoot-plans")
		if route == "/api/v1/settings" || (strings.Contains(route, "/business-drafts/") && strings.HasSuffix(route, "/apply")) {
			var body map[string]json.RawMessage
			raw, err := readRequestBody(c)
			if err != nil {
				abortError(c, 400, CodeValidationFailed, "请求体格式错误")
				return
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				abortError(c, 400, CodeValidationFailed, "请求体格式错误")
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			if route == "/api/v1/settings" {
				_, legacy = body["planning_business_rules"]
			} else {
				var decision string
				if err := json.Unmarshal(body["decision"], &decision); err == nil && decision == "dismiss" {
					legacy = false
				}
			}
		}
		if !legacy {
			c.Next()
			return
		}
		account, ok := auth.AccountContextFrom(c.Request.Context())
		if !ok {
			abortError(c, 401, CodeUnauthorized, "未认证")
			return
		}
		blocked, err := pilot.LegacyWriteBlocked(c.Request.Context(), factory.ScopeFor(account))
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if blocked {
			abortError(c, 409, "legacy_read_only", "旧策划记录已只读，请前往创意空间")
			return
		}
		c.Set(creativeLegacyGuardKey, true)
		c.Next()
	}
}

func creativeLegacyScope(c *gin.Context, sc store.AccountScope) store.AccountScope {
	if c.GetBool(creativeLegacyGuardKey) {
		return sc.WithLegacyPlanningWrite()
	}
	return sc
}
func abortLegacyWriteError(c *gin.Context, err error) bool {
	if !errors.Is(err, store.ErrLegacyReadOnly) {
		return false
	}
	abortError(c, 409, "legacy_read_only", "旧策划记录已只读，请前往创意空间")
	return true
}
