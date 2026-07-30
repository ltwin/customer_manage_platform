package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const maxPackageBodyBytes = 1 << 20

func (h *handlers) listPackagesRoute(c *gin.Context) {
	params, ok := bindListPackagesParams(c)
	if !ok {
		return
	}
	h.ListPackages(c, params)
}

func (h *handlers) CreatePackage(c *gin.Context) {
	scope, ok := h.packageScope(c)
	if !ok {
		return
	}
	body, ok := bindCreatePackageBody(c)
	if !ok {
		return
	}
	created, err := h.packages.Create(c.Request.Context(), scope, pkgcatalog.CreateInput{
		Name:             body.Name,
		ShootType:        string(body.ShootType),
		PricingMode:      string(body.PricingMode),
		BasePrice:        body.BasePrice,
		DurationMinutes:  body.DurationMinutes,
		ShotCountMin:     body.ShotCountMin,
		ShotCountMax:     body.ShotCountMax,
		RawDeliveryCount: body.RawDeliveryCount,
		RetouchCount:     body.RetouchCount,
		Note:             body.Note,
	})
	if h.abortPackageError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, toAPIPackage(created))
}

func (h *handlers) ListPackages(c *gin.Context, params ListPackagesParams) {
	scope, ok := h.packageScope(c)
	if !ok {
		return
	}
	filter := pkgcatalog.ListFilter{}
	if params.Status != nil {
		filter.Status = string(*params.Status)
	}
	if params.Page != nil {
		filter.Page = int(*params.Page)
	}
	if params.PageSize != nil {
		filter.PageSize = int(*params.PageSize)
	}
	result, err := h.packages.List(c.Request.Context(), scope, filter)
	if h.abortPackageError(c, err) {
		return
	}
	items := make([]PackageListItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toAPIPackageListItem(item))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": result.Total})
}

func (h *handlers) UpdatePackage(c *gin.Context, id Id) {
	scope, ok := h.packageScope(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackageBodyBytes)
	var body UpdatePackageJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return
	}
	input := pkgcatalog.UpdateInput{
		Name:             body.Name,
		BasePrice:        body.BasePrice,
		DurationMinutes:  body.DurationMinutes,
		ShotCountMin:     body.ShotCountMin,
		ShotCountMax:     body.ShotCountMax,
		RawDeliveryCount: body.RawDeliveryCount,
		RetouchCount:     body.RetouchCount,
		Note:             body.Note,
	}
	if body.ShootType != nil {
		shootType := string(*body.ShootType)
		input.ShootType = &shootType
	}
	if body.PricingMode != nil {
		pricingMode := string(*body.PricingMode)
		input.PricingMode = &pricingMode
	}
	if body.Status != nil {
		status := string(*body.Status)
		input.Status = &status
	}
	updated, err := h.packages.Update(c.Request.Context(), scope, id, input)
	if h.abortPackageError(c, err) {
		return
	}
	c.JSON(http.StatusOK, toAPIPackage(updated))
}

func (h *handlers) DeletePackage(c *gin.Context, id Id) {
	scope, ok := h.packageScope(c)
	if !ok {
		return
	}
	err := h.packages.Delete(c.Request.Context(), scope, id)
	if h.abortPackageError(c, err) {
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) packageScope(c *gin.Context) (store.AccountScope, bool) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.scopeFactory == nil || h.packages == nil {
		_ = c.Error(errors.New("package route dependencies missing"))
		return store.AccountScope{}, false
	}
	return h.scopeFactory.ScopeFor(ac), true
}

func (h *handlers) abortPackageError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, pkgcatalog.ErrValidation):
		abortError(c, http.StatusBadRequest, CodeValidationFailed, packageMessage(err))
	case errors.Is(err, pkgcatalog.ErrNotFound):
		abortError(c, http.StatusNotFound, CodeNotFound, packageMessage(err))
	case errors.Is(err, pkgcatalog.ErrPackageInUse):
		abortError(c, http.StatusConflict, CodePackageInUse, packageMessage(err))
	default:
		_ = c.Error(err)
	}
	return true
}

func bindListPackagesParams(c *gin.Context) (ListPackagesParams, bool) {
	var params ListPackagesParams
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		value := ListPackagesParamsStatus(status)
		params.Status = &value
	}
	page, ok := bindOptionalPageParam(c, "page")
	if !ok {
		return ListPackagesParams{}, false
	}
	if page != nil {
		value := Page(*page)
		params.Page = &value
	}
	pageSize, ok := bindOptionalPageParam(c, "page_size")
	if !ok {
		return ListPackagesParams{}, false
	}
	if pageSize != nil {
		value := PageSize(*pageSize)
		params.PageSize = &value
	}
	return params, true
}

func bindCreatePackageBody(c *gin.Context) (CreatePackageJSONRequestBody, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackageBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return CreatePackageJSONRequestBody{}, false
	}
	var body CreatePackageJSONRequestBody
	if err := json.Unmarshal(raw, &body); err != nil {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求体格式错误")
		return CreatePackageJSONRequestBody{}, false
	}
	if !jsonFieldPresent(raw, "base_price") {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "base_price 必填")
		return CreatePackageJSONRequestBody{}, false
	}
	return body, true
}

func jsonFieldPresent(raw []byte, name string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	value, ok := fields[name]
	return ok && strings.TrimSpace(string(value)) != "null"
}

func toAPIPackage(pkg pkgcatalog.Package) Package {
	return Package{
		AccountId:        stringPointer(pkg.AccountID),
		BasePrice:        pkg.BasePrice,
		CreatedAt:        timePointer(pkg.CreatedAt),
		DurationMinutes:  pkg.DurationMinutes,
		Id:               stringPointer(pkg.ID),
		Name:             pkg.Name,
		Note:             pkg.Note,
		PricingMode:      PricingMode(pkg.PricingMode),
		RawDeliveryCount: pkg.RawDeliveryCount,
		RetouchCount:     pkg.RetouchCount,
		ShootType:        ShootType(pkg.ShootType),
		ShotCountMax:     pkg.ShotCountMax,
		ShotCountMin:     pkg.ShotCountMin,
		Status:           PackageStatus(pkg.Status),
	}
}

func toAPIPackageListItem(item pkgcatalog.ListItem) PackageListItem {
	return PackageListItem{
		AccountId:        stringPointer(item.AccountID),
		BasePrice:        item.BasePrice,
		CreatedAt:        timePointer(item.CreatedAt),
		DurationMinutes:  item.DurationMinutes,
		Id:               stringPointer(item.ID),
		Name:             item.Name,
		Note:             item.Note,
		OrdersCount:      item.OrdersCount,
		PricingMode:      PricingMode(item.PricingMode),
		RawDeliveryCount: item.RawDeliveryCount,
		RetouchCount:     item.RetouchCount,
		ShootType:        ShootType(item.ShootType),
		ShotCountMax:     item.ShotCountMax,
		ShotCountMin:     item.ShotCountMin,
		Status:           PackageStatus(item.Status),
	}
}

func packageMessage(err error) string {
	msg := err.Error()
	if strings.Contains(msg, ": ") {
		parts := strings.SplitN(msg, ": ", 2)
		return parts[1]
	}
	return msg
}
