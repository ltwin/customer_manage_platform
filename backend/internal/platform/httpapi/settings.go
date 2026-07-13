package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func (h *handlers) GetSettings(c *gin.Context) {
	scope, ok := h.settingsScope(c)
	if !ok {
		return
	}
	if h.settings == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	s, err := h.settings.Get(c.Request.Context(), scope)
	if h.abortSettingsError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPISettings(s))
}

func (h *handlers) UpdateSettings(c *gin.Context) {
	scope, ok := h.settingsScope(c)
	if !ok {
		return
	}
	if h.settings == nil {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return
	}
	var body UpdateSettingsJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := settings.PatchInput{
		Timezone:          body.Timezone,
		BirthdayLeadDays:  body.BirthdayLeadDays,
		FollowUpAfterDays: body.FollowUpAfterDays,
		DigestHour:        body.DigestHour,
	}
	if body.ChurnThresholds != nil {
		entries := make([]settings.ChurnThreshold, 0, len(*body.ChurnThresholds))
		for _, e := range *body.ChurnThresholds {
			entries = append(entries, settings.ChurnThreshold{
				ShootType: string(e.ShootType),
				Days:      e.Days,
			})
		}
		input.ChurnThresholds = &entries
	}
	s, err := h.settings.Patch(c.Request.Context(), scope, input)
	if h.abortSettingsError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPISettings(s))
}

func (h *handlers) settingsScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil {
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortSettingsError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var ve settings.ValidationError
	if errors.As(err, &ve) || errors.Is(err, settings.ErrValidation) {
		msg := err.Error()
		if errors.As(err, &ve) {
			msg = ve.Message
		}
		abortError(c, http.StatusBadRequest, CodeValidationFailed, msg)
		return true
	}
	_ = c.Error(err)
	return true
}

func toAPISettings(s settings.Settings) Settings {
	thresholds := make([]ChurnThreshold, 0, len(s.ChurnThresholds))
	for _, e := range s.ChurnThresholds {
		thresholds = append(thresholds, ChurnThreshold{
			ShootType: ShootType(e.ShootType),
			Days:      e.Days,
		})
	}
	return Settings{
		Timezone:          s.Timezone,
		BirthdayLeadDays:  s.BirthdayLeadDays,
		FollowUpAfterDays: s.FollowUpAfterDays,
		ChurnThresholds:   thresholds,
		DigestHour:        s.DigestHour,
		TelegramChatId:    s.TelegramChatID,
	}
}
