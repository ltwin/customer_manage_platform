package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/dataexport"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// DataExportService is the HTTP adapter's narrow view of dataexport.Service.
type DataExportService interface {
	Build(context.Context, store.AccountScope) (dataexport.Document, error)
}

type dataExportProjector func(dataexport.Document) (ExportDocument, error)
type dataExportEncoder func(ExportDocument) ([]byte, error)

func (h *handlers) ExportAll(c *gin.Context) {
	ac, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return
	}
	if h.scopeFactory == nil || h.dataExport == nil {
		abortError(c, http.StatusInternalServerError, CodeInternal, "内部错误")
		return
	}
	doc, err := h.dataExport.Build(c.Request.Context(), h.scopeFactory.ScopeFor(ac))
	if err != nil {
		if h.dataExportRequestCanceled(c, "build") {
			return
		}
		_ = c.Error(err)
		return
	}
	project := h.dataExportMap
	if project == nil {
		project = func(document dataexport.Document) (ExportDocument, error) {
			return toAPIExportDocument(document), nil
		}
	}
	response, err := project(doc)
	if err != nil {
		_ = c.Error(fmt.Errorf("project data export response: %w", err))
		return
	}
	encode := h.dataExportEncode
	if encode == nil {
		encode = func(document ExportDocument) ([]byte, error) {
			return json.Marshal(document)
		}
	}
	body, err := encode(response)
	if err != nil {
		_ = c.Error(fmt.Errorf("serialize data export response: %w", err))
		return
	}
	if err := c.Request.Context().Err(); err != nil {
		h.dataExportRequestCanceled(c, "pre_send")
		return
	}
	filename := "photographer-crm-export-" + doc.ExportedAt.UTC().Format("20060102T150405Z") + ".json"
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Content-Length", strconv.Itoa(len(body)))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(body); err != nil && h.logger != nil {
		h.logger.Error("data export transport failed",
			slog.String("stage", "write"),
			slog.Int("serialized_bytes", len(body)),
			slog.Any("error", err),
		)
	}
}

func (h *handlers) dataExportRequestCanceled(c *gin.Context, stage string) bool {
	requestErr := c.Request.Context().Err()
	if requestErr == nil {
		return false
	}
	reason := "canceled"
	if errors.Is(requestErr, context.DeadlineExceeded) {
		reason = "deadline_exceeded"
	}
	if h.logger != nil {
		h.logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "data export canceled",
			slog.String("stage", stage),
			slog.String("reason", reason),
		)
	}
	return true
}

func toAPIExportDocument(doc dataexport.Document) ExportDocument {
	customers := make([]Customer, 0, len(doc.Customers))
	for _, item := range doc.Customers {
		customers = append(customers, toAPICustomer(item))
	}
	identities := make([]SocialIdentity, 0, len(doc.SocialIdentities))
	for _, item := range doc.SocialIdentities {
		identities = append(identities, SocialIdentity{
			AccountId:  stringPointer(item.AccountID),
			CreatedAt:  timePointer(item.CreatedAt),
			CustomerId: item.CustomerID,
			Handle:     item.Handle,
			Id:         stringPointer(item.ID),
			Platform:   SocialPlatform(item.Platform),
			Remark:     item.Remark,
		})
	}
	notes := make([]CustomerNote, 0, len(doc.CustomerNotes))
	for _, item := range doc.CustomerNotes {
		notes = append(notes, toAPICustomerNote(item))
	}
	packages := make([]Package, 0, len(doc.Packages))
	for _, item := range doc.Packages {
		packages = append(packages, toAPIPackage(item))
	}
	orders := make([]Order, 0, len(doc.Orders))
	for _, item := range doc.Orders {
		orders = append(orders, toAPIOrder(item))
	}
	slots := make([]ScheduleSlot, 0, len(doc.ScheduleSlots))
	for _, item := range doc.ScheduleSlots {
		slots = append(slots, toAPIScheduleSlot(item))
	}
	reminders := make([]Reminder, 0, len(doc.Reminders))
	for _, item := range doc.Reminders {
		reminders = append(reminders, toAPIReminder(item))
	}
	return ExportDocument{
		Counts: ExportCounts{
			CustomerNotes:    doc.Counts.CustomerNotes,
			Customers:        doc.Counts.Customers,
			Orders:           doc.Counts.Orders,
			Packages:         doc.Counts.Packages,
			Reminders:        doc.Counts.Reminders,
			ScheduleSlots:    doc.Counts.ScheduleSlots,
			SocialIdentities: doc.Counts.SocialIdentities,
		},
		CustomerNotes:    notes,
		Customers:        customers,
		ExportedAt:       doc.ExportedAt,
		Orders:           orders,
		Packages:         packages,
		Reminders:        reminders,
		ScheduleSlots:    slots,
		SchemaVersion:    ExportDocumentSchemaVersion(doc.SchemaVersion),
		Settings:         toAPISettings(doc.Settings),
		SocialIdentities: identities,
	}
}
