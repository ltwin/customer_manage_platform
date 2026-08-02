package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/dataexport"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fakeDataExportService struct {
	document dataexport.Document
	err      error
	build    func(context.Context, store.AccountScope) (dataexport.Document, error)
}

func (s fakeDataExportService) Build(ctx context.Context, scope store.AccountScope) (dataexport.Document, error) {
	if s.build != nil {
		return s.build(ctx, scope)
	}
	return s.document, s.err
}

type fakeDataExportScopeFactory struct{}

func (fakeDataExportScopeFactory) ScopeFor(auth.AccountContext) store.AccountScope {
	return store.AccountScope{}
}

type failingGinWriter struct {
	gin.ResponseWriter
	writes int
}

func (w *failingGinWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("synthetic transport failure")
}

func TestExportAllProjectsEmptyVersionedDocument(t *testing.T) {
	doc := emptyExportDocumentFixture()

	h := handlers{
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport:   fakeDataExportService{document: doc},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	req = req.WithContext(auth.WithAccountContext(req.Context(), auth.AccountContext{AccountID: "acct-fixture"}))
	c.Request = req

	h.ExportAll(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="photographer-crm-export-20260721T083015Z.json"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != strconv.Itoa(recorder.Body.Len()) {
		t.Fatalf("Content-Length = %q, body length = %d", got, recorder.Body.Len())
	}
	var got ExportDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode export document: %v", err)
	}
	if got.SchemaVersion != 3 || got.Counts != (ExportCounts{}) {
		t.Fatalf("document version/counts = %d/%+v", got.SchemaVersion, got.Counts)
	}
	if got.Customers == nil || got.SocialIdentities == nil || got.CustomerNotes == nil ||
		got.Packages == nil || got.Orders == nil || got.ScheduleSlots == nil || got.Reminders == nil {
		t.Fatal("all exported arrays must encode as []")
	}
}

func TestExportAllDoesNotSendAttachmentHeadersBeforeEveryPreSendStageSucceeds(t *testing.T) {
	boom := errors.New("synthetic pre-send failure")
	tests := []struct {
		name       string
		configure  func(*handlers)
		cancel     bool
		wantErrors int
	}{
		{
			name:       "build",
			wantErrors: 1,
			configure: func(h *handlers) {
				h.dataExport = fakeDataExportService{err: boom}
			},
		},
		{
			name:       "mapping",
			wantErrors: 1,
			configure: func(h *handlers) {
				h.dataExportMap = func(dataexport.Document) (ExportDocument, error) {
					return ExportDocument{}, boom
				}
			},
		},
		{
			name:       "serialization",
			wantErrors: 1,
			configure: func(h *handlers) {
				h.dataExportEncode = func(ExportDocument) ([]byte, error) { return nil, boom }
			},
		},
		{name: "context canceled", cancel: true, wantErrors: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handlers{
				scopeFactory: fakeDataExportScopeFactory{},
				dataExport:   fakeDataExportService{document: emptyExportDocumentFixture()},
			}
			if tt.configure != nil {
				tt.configure(&h)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx := auth.WithAccountContext(context.Background(), auth.AccountContext{AccountID: "acct-fixture"})
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/export", nil).WithContext(ctx)

			h.ExportAll(c)

			if got := recorder.Header().Get("Content-Disposition"); got != "" {
				t.Fatalf("pre-send failure emitted attachment header %q", got)
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("pre-send failure emitted body %q", recorder.Body.String())
			}
			if len(c.Errors) != tt.wantErrors {
				t.Fatalf("pre-send failure errors = %d, want %d", len(c.Errors), tt.wantErrors)
			}
		})
	}
}

func TestExportAllDoesNotRenderErrorEnvelopeWhenCanceledRequestReturnsWrappedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handlers{
		logger:       logger,
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport: fakeDataExportService{
			build: func(context.Context, store.AccountScope) (dataexport.Document, error) {
				cancel()
				return dataexport.Document{}, fmt.Errorf("PII-SENTINEL: %w", context.Canceled)
			},
		},
	}
	result := executeDataExportThroughProductionMiddleware(t, logger, &h, ctx)

	assertCanceledExportResponse(t, result, logs.String(), "canceled")
}

func TestExportAllRendersErrorEnvelopeWhenActiveRequestReturnsWrappedCancellation(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handlers{
		logger:       logger,
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport: fakeDataExportService{
			err: fmt.Errorf("synthetic wrapper: %w", context.Canceled),
		},
	}
	result := executeDataExportThroughProductionMiddleware(t, logger, &h, context.Background())

	assertInternalExportErrorEnvelope(t, result, logs.String())
}

func TestExportAllDoesNotRenderErrorEnvelopeWhenExpiredRequestReturnsWrappedDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handlers{
		logger:       logger,
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport: fakeDataExportService{
			err: fmt.Errorf("PII-SENTINEL: %w", context.DeadlineExceeded),
		},
	}
	result := executeDataExportThroughProductionMiddleware(t, logger, &h, ctx)

	assertCanceledExportResponse(t, result, logs.String(), "deadline_exceeded")
}

func TestExportAllRendersErrorEnvelopeWhenActiveRequestReturnsWrappedDeadline(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handlers{
		logger:       logger,
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport: fakeDataExportService{
			err: fmt.Errorf("synthetic wrapper: %w", context.DeadlineExceeded),
		},
	}
	result := executeDataExportThroughProductionMiddleware(t, logger, &h, context.Background())

	assertInternalExportErrorEnvelope(t, result, logs.String())
}

func TestExportAllDoesNotRenderErrorEnvelopeWhenContextIsCanceledBeforeSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := handlers{
		logger:       logger,
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport: fakeDataExportService{
			build: func(context.Context, store.AccountScope) (dataexport.Document, error) {
				cancel()
				return emptyExportDocumentFixture(), nil
			},
		},
	}
	result := executeDataExportThroughProductionMiddleware(t, logger, &h, ctx)

	assertCanceledExportResponse(t, result, logs.String(), "canceled")
}

type dataExportMiddlewareResult struct {
	recorder      *httptest.ResponseRecorder
	handlerErrors int
}

func executeDataExportThroughProductionMiddleware(
	t *testing.T,
	logger *slog.Logger,
	h *handlers,
	ctx context.Context,
) dataExportMiddlewareResult {
	t.Helper()
	result := dataExportMiddlewareResult{recorder: httptest.NewRecorder()}
	engine := gin.New()
	engine.Use(
		recoveryMiddleware(logger),
		requestLogMiddleware(logger),
		errorEnvelopeMiddleware(logger),
	)
	engine.GET("/api/v1/export", func(c *gin.Context) {
		accountContext := auth.WithAccountContext(ctx, auth.AccountContext{AccountID: "acct-fixture"})
		c.Request = c.Request.WithContext(accountContext)
		h.ExportAll(c)
		result.handlerErrors = len(c.Errors)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	engine.ServeHTTP(result.recorder, request)
	return result
}

func assertCanceledExportResponse(t *testing.T, result dataExportMiddlewareResult, logs string, reason string) {
	t.Helper()
	recorder := result.recorder
	if got := recorder.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("canceled export emitted attachment header %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "" {
		t.Fatalf("canceled export emitted content type %q", got)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("canceled export emitted error envelope %q", recorder.Body.String())
	}
	if result.handlerErrors != 0 {
		t.Fatalf("canceled export enqueued %d handler errors, want 0", result.handlerErrors)
	}
	if !strings.Contains(logs, "data export canceled") {
		t.Fatalf("canceled export log missing cancellation metadata: %s", logs)
	}
	if !strings.Contains(logs, `"reason":"`+reason+`"`) {
		t.Fatalf("canceled export log missing reason %q: %s", reason, logs)
	}
	if strings.Contains(logs, "PII-SENTINEL") || strings.Contains(logs, "synthetic wrapper") {
		t.Fatalf("canceled export log leaked wrapped error or payload: %s", logs)
	}
}

func assertInternalExportErrorEnvelope(t *testing.T, result dataExportMiddlewareResult, logs string) {
	t.Helper()
	if result.recorder.Code != http.StatusInternalServerError {
		t.Fatalf("active request status = %d, want 500; body=%s", result.recorder.Code, result.recorder.Body.String())
	}
	if result.recorder.Header().Get("Content-Disposition") != "" {
		t.Fatal("active request failure emitted attachment headers")
	}
	if result.handlerErrors != 1 {
		t.Fatalf("active request handler errors = %d, want 1", result.handlerErrors)
	}
	var envelope ErrorEnvelope
	if err := json.Unmarshal(result.recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode active request error envelope: %v; body=%s", err, result.recorder.Body.String())
	}
	if envelope.Error.Code != CodeInternal {
		t.Fatalf("active request error code = %q, want %q", envelope.Error.Code, CodeInternal)
	}
	if strings.Contains(logs, "data export canceled") {
		t.Fatalf("active request was misclassified as cancellation: %s", logs)
	}
}

func TestExportAllFailsClosedWhenHTTPDependenciesAreMissing(t *testing.T) {
	tests := []struct {
		name string
		h    handlers
	}{
		{
			name: "scope factory",
			h:    handlers{dataExport: fakeDataExportService{document: emptyExportDocumentFixture()}},
		},
		{
			name: "service",
			h:    handlers{scopeFactory: fakeDataExportScopeFactory{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
			c.Request = req.WithContext(auth.WithAccountContext(req.Context(), auth.AccountContext{AccountID: "acct-fixture"}))

			tt.h.ExportAll(c)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", recorder.Code)
			}
			if recorder.Header().Get("Content-Disposition") != "" {
				t.Fatal("missing dependency must not emit attachment headers")
			}
		})
	}
}

func TestExportAllTransportFailureDoesNotAttemptSecondEnvelope(t *testing.T) {
	doc := emptyExportDocumentFixture()
	doc.Customers = append(doc.Customers, customerdomain.Customer{DisplayName: "PII-SENTINEL"})
	doc.Counts.Customers = 1
	var logs bytes.Buffer
	h := handlers{
		logger:       slog.New(slog.NewJSONHandler(&logs, nil)),
		scopeFactory: fakeDataExportScopeFactory{},
		dataExport:   fakeDataExportService{document: doc},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := &failingGinWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	c.Request = req.WithContext(auth.WithAccountContext(req.Context(), auth.AccountContext{AccountID: "acct-fixture"}))

	h.ExportAll(c)

	if writer.writes != 1 {
		t.Fatalf("transport write attempts = %d, want exactly 1", writer.writes)
	}
	if len(c.Errors) != 0 {
		t.Fatalf("post-header transport failure must not enqueue a second envelope: %+v", c.Errors)
	}
	if recorder.Header().Get("Content-Disposition") == "" {
		t.Fatal("transport failure must occur after success headers are committed")
	}
	if strings.Contains(logs.String(), "PII-SENTINEL") || strings.Contains(logs.String(), "customers") {
		t.Fatalf("transport log leaked payload data: %s", logs.String())
	}
}

func emptyExportDocumentFixture() dataexport.Document {
	doc := dataexport.Document{
		ExportedAt:    time.Date(2026, time.July, 21, 8, 30, 15, 0, time.UTC),
		SchemaVersion: 3,
	}
	empty := dataexport.EmptySnapshot()
	doc.Customers = empty.Customers
	doc.SocialIdentities = empty.SocialIdentities
	doc.CustomerNotes = empty.CustomerNotes
	doc.Packages = empty.Packages
	doc.Orders = empty.Orders
	doc.ScheduleSlots = empty.ScheduleSlots
	doc.Reminders = empty.Reminders
	doc.Settings = empty.Settings
	doc.AccountProfile = empty.AccountProfile
	return doc
}
