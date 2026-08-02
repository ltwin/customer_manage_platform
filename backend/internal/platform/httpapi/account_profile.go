package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/accountprofile"
	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
)

var quotedProfileRevisionPattern = regexp.MustCompile(`^"pr-(0|[1-9][0-9]*)"$`)

func (h *handlers) patchAccountProfileRoute(c *gin.Context) {
	value := strings.TrimSpace(c.GetHeader("If-Match"))
	if !quotedProfileRevisionPattern.MatchString(value) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 必填且必须是 quoted profile_revision")
		return
	}
	h.PatchAccountProfile(c, PatchAccountProfileParams{IfMatch: ProfileIfMatch(value)})
}

func (h *handlers) putAccountProfileAvatarRoute(c *gin.Context) {
	ifMatch, ok := bindAvatarIfMatch(c)
	if !ok {
		return
	}
	h.PutAccountProfileAvatar(c, PutAccountProfileAvatarParams{IfMatch: AvatarIfMatch(ifMatch)})
}

func (h *handlers) deleteAccountProfileAvatarRoute(c *gin.Context) {
	ifMatch, ok := bindAvatarIfMatch(c)
	if !ok {
		return
	}
	h.DeleteAccountProfileAvatar(c, DeleteAccountProfileAvatarParams{IfMatch: AvatarIfMatch(ifMatch)})
}

func (h *handlers) getAccountProfileAvatarContentRoute(c *gin.Context) {
	version := strings.TrimSpace(c.Query("v"))
	if !avatarVersionQueryPattern.MatchString(version) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "v 必填且必须是合法 avatar_version")
		return
	}
	params := GetAccountProfileAvatarContentParams{V: version}
	if value := strings.TrimSpace(c.GetHeader("If-None-Match")); value != "" {
		params.IfNoneMatch = &value
	}
	h.GetAccountProfileAvatarContent(c, params)
}

func (h *handlers) GetAccountProfile(c *gin.Context) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	if h.accountProfile == nil {
		_ = c.Error(errors.New("account profile dependencies missing"))
		return
	}
	profile, err := h.accountProfile.Get(c.Request.Context(), scope)
	if h.abortAccountProfileError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIAccountProfile(profile))
}

func (h *handlers) PatchAccountProfile(c *gin.Context, params PatchAccountProfileParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	revision, ok := parseProfileIfMatch(c, string(params.IfMatch))
	if !ok {
		return
	}
	if h.accountProfile == nil {
		_ = c.Error(errors.New("account profile dependencies missing"))
		return
	}
	var body PatchAccountProfileJSONRequestBody
	if err := bindStrictJSON(c, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
		return
	}
	if !body.DisplayName.IsSpecified() {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "display_name 必填（可设为 null 清除）")
		return
	}
	var displayName *string
	if !body.DisplayName.IsNull() {
		value, err := body.DisplayName.Get()
		if err != nil {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, "display_name 非法")
			return
		}
		displayName = &value
	}
	updated, err := h.accountProfile.Patch(c.Request.Context(), scope, revision, accountprofile.PatchInput{
		DisplayName: accountprofile.OptionalString{Set: true, Value: displayName},
	})
	if h.abortAccountProfileError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIAccountProfile(updated))
}

func (h *handlers) PutAccountProfileAvatar(c *gin.Context, params PutAccountProfileAvatarParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	revision, ok := parseAvatarIfMatch(c, string(params.IfMatch))
	if !ok {
		return
	}
	if h.accountProfile == nil {
		_ = c.Error(errors.New("account profile dependencies missing"))
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAvatarMultipartBytes)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "file 必填且 multipart 格式必须合法")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "file 无法读取")
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, avatarmedia.MaxContentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > avatarmedia.MaxContentBytes {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "file 无法完整读取或超过 5 MiB")
		return
	}
	content, err := avatarmedia.DecodeConfirm(raw, fileHeader.Header.Get("Content-Type"))
	if h.abortAccountProfileError(c, err) {
		return
	}
	updated, err := h.accountProfile.SetAvatar(c.Request.Context(), scope, revision, content)
	if h.abortAccountProfileError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIAccountProfile(updated))
}

func (h *handlers) DeleteAccountProfileAvatar(c *gin.Context, params DeleteAccountProfileAvatarParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	revision, ok := parseAvatarIfMatch(c, string(params.IfMatch))
	if !ok {
		return
	}
	if h.accountProfile == nil {
		_ = c.Error(errors.New("account profile dependencies missing"))
		return
	}
	updated, err := h.accountProfile.RemoveAvatar(c.Request.Context(), scope, revision)
	if h.abortAccountProfileError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIAccountProfile(updated))
}

func (h *handlers) GetAccountProfileAvatarContent(c *gin.Context, params GetAccountProfileAvatarContentParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	if h.accountProfile == nil {
		_ = c.Error(errors.New("account profile dependencies missing"))
		return
	}
	content, err := h.accountProfile.ReadAvatar(c.Request.Context(), scope, params.V)
	if h.abortAccountProfileError(c, err) {
		return
	}
	etag := `"` + content.Checksum() + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "private, no-cache")
	c.Header("Vary", "Authorization")
	c.Header("X-Content-Type-Options", "nosniff")
	if params.IfNoneMatch != nil && *params.IfNoneMatch == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, content.MediaType(), content.Bytes())
}

func (h *handlers) abortAccountProfileError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, accountprofile.ErrValidationFailed):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, profileMessage(err))
	case errors.Is(err, accountprofile.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, profileMessage(err))
	case errors.Is(err, accountprofile.ErrProfileRevisionConflict):
		abortError(c, http.StatusConflict, "profile_revision_conflict", profileMessage(err))
	case errors.Is(err, accountprofile.ErrAvatarRevisionConflict):
		abortError(c, http.StatusConflict, "avatar_revision_conflict", profileMessage(err))
	case errors.Is(err, accountprofile.ErrAvatarVersionStale):
		abortError(c, http.StatusConflict, "avatar_version_stale", profileMessage(err))
	case errors.Is(err, avatarmedia.ErrObjectIntegrity):
		abortError(c, http.StatusConflict, "avatar_object_integrity", profileMessage(err))
	default:
		var validation avatarmedia.ValidationError
		if errors.As(err, &validation) {
			abortError(c, http.StatusBadRequest, CodeValidationFailed, validation.Message)
			return true
		}
		_ = c.Error(err)
	}
	return true
}

func parseProfileIfMatch(c *gin.Context, value string) (int64, bool) {
	if !quotedProfileRevisionPattern.MatchString(value) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 必填且必须是 quoted profile_revision")
		return 0, false
	}
	revision, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, `"pr-`), `"`), 10, 64)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 超出范围")
		return 0, false
	}
	return revision, true
}

func toAPIAccountProfile(profile accountprofile.Profile) AccountProfile {
	out := AccountProfile{
		ProfileRevision: fmt.Sprintf("pr-%d", profile.ProfileRevision),
		AvatarRevision:  fmt.Sprintf("ar-%d", profile.AvatarRevision),
	}
	out.DisplayName.SetNull()
	if profile.DisplayName != nil {
		out.DisplayName.Set(*profile.DisplayName)
	}
	if profile.AvatarVersion != nil {
		version := AvatarVersion(*profile.AvatarVersion)
		out.AvatarVersion = &version
		url := fmt.Sprintf("/api/v1/account/profile/avatar/content?v=%s", *profile.AvatarVersion)
		out.AvatarUrl = &url
	}
	if profile.AvatarUpdatedAt != nil {
		t := profile.AvatarUpdatedAt.UTC()
		out.AvatarUpdatedAt = &t
	}
	if profile.UpdatedAt != nil {
		t := profile.UpdatedAt.UTC()
		out.UpdatedAt = &t
	}
	return out
}

func profileMessage(err error) string {
	var validation accountprofile.ValidationError
	if errors.As(err, &validation) && validation.Message != "" {
		return validation.Message
	}
	return err.Error()
}
