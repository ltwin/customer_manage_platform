package httpapi

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
)

const maxAvatarMultipartBytes = customerdomain.MaxAvatarContentBytes + 1024*1024

var (
	quotedAvatarRevisionPattern = regexp.MustCompile(`^"ar-(0|[1-9][0-9]*)"$`)
	avatarVersionQueryPattern   = regexp.MustCompile(`^sha256-[0-9a-f]{64}$`)
)

type AvatarProcessor interface {
	Process([]byte, string) (customerdomain.AvatarContent, error)
}

func (h *handlers) putCustomerAvatarRoute(c *gin.Context) {
	ifMatch, ok := bindAvatarIfMatch(c)
	if !ok {
		return
	}
	h.PutCustomerAvatar(c, c.Param("id"), PutCustomerAvatarParams{IfMatch: AvatarIfMatch(ifMatch)})
}

func (h *handlers) deleteCustomerAvatarRoute(c *gin.Context) {
	ifMatch, ok := bindAvatarIfMatch(c)
	if !ok {
		return
	}
	h.DeleteCustomerAvatar(c, c.Param("id"), DeleteCustomerAvatarParams{IfMatch: AvatarIfMatch(ifMatch)})
}

func (h *handlers) getCustomerAvatarContentRoute(c *gin.Context) {
	version := strings.TrimSpace(c.Query("v"))
	if !avatarVersionQueryPattern.MatchString(version) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "v 必填且必须是合法 avatar_version")
		return
	}
	params := GetCustomerAvatarContentParams{V: version}
	if value := strings.TrimSpace(c.GetHeader("If-None-Match")); value != "" {
		params.IfNoneMatch = &value
	}
	h.GetCustomerAvatarContent(c, c.Param("id"), params)
}

func (h *handlers) PutCustomerAvatar(c *gin.Context, id Id, params PutCustomerAvatarParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	revision, ok := parseAvatarIfMatch(c, string(params.IfMatch))
	if !ok {
		return
	}
	if h.avatar == nil || h.avatarProcessor == nil {
		_ = c.Error(errors.New("customer avatar dependencies missing"))
		return
	}
	if h.abortCustomerError(c, h.avatar.ValidateSetTarget(c.Request.Context(), scope, id)) {
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
	raw, readErr := io.ReadAll(io.LimitReader(file, customerdomain.MaxAvatarContentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > customerdomain.MaxAvatarContentBytes {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "file 无法完整读取或超过 5 MiB")
		return
	}
	content, err := h.avatarProcessor.Process(raw, fileHeader.Header.Get("Content-Type"))
	if h.abortCustomerError(c, err) {
		return
	}
	updated, err := h.avatar.Set(c.Request.Context(), scope, id, revision, content)
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPICustomer(updated))
}

func (h *handlers) DeleteCustomerAvatar(c *gin.Context, id Id, params DeleteCustomerAvatarParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	revision, ok := parseAvatarIfMatch(c, string(params.IfMatch))
	if !ok {
		return
	}
	if h.avatar == nil {
		_ = c.Error(errors.New("customer avatar dependencies missing"))
		return
	}
	updated, err := h.avatar.Remove(c.Request.Context(), scope, id, revision)
	if h.abortCustomerError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPICustomer(updated))
}

func (h *handlers) GetCustomerAvatarContent(c *gin.Context, id Id, params GetCustomerAvatarContentParams) {
	scope, ok := h.customerScope(c)
	if !ok {
		return
	}
	if h.avatar == nil {
		_ = c.Error(errors.New("customer avatar dependencies missing"))
		return
	}
	content, err := h.avatar.ReadContent(c.Request.Context(), scope, id, params.V)
	if h.abortCustomerError(c, err) {
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

func bindAvatarIfMatch(c *gin.Context) (string, bool) {
	value := strings.TrimSpace(c.GetHeader("If-Match"))
	if !quotedAvatarRevisionPattern.MatchString(value) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 必填且必须是 quoted avatar_revision")
		return "", false
	}
	return value, true
}

func parseAvatarIfMatch(c *gin.Context, value string) (int64, bool) {
	if !quotedAvatarRevisionPattern.MatchString(value) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 非法")
		return 0, false
	}
	revision, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, `"ar-`), `"`), 10, 64)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "If-Match 超出范围")
		return 0, false
	}
	return revision, true
}
