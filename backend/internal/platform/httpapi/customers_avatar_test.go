package httpapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

func TestCustomerAvatarHTTPVerticalSlice(t *testing.T) {
	router, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	created := createCustomerAPI(t, router, token, "avatar-http")
	imageA := avatarPNG(t, color.NRGBA{R: 220, G: 10, B: 30, A: 255})
	imageB := avatarPNG(t, color.NRGBA{R: 10, G: 120, B: 230, A: 255})

	put := avatarMultipartRequest(t, router, http.MethodPut, "/api/v1/customers/"+*created.Id+"/avatar", token, `"ar-0"`, "image/png", imageA)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT avatar: want 200, got %d %s", put.Code, put.Body.String())
	}
	var withAvatar struct {
		AvatarRevision string  `json:"avatar_revision"`
		AvatarVersion  *string `json:"avatar_version"`
		AvatarURL      *string `json:"avatar_url"`
	}
	if err := json.Unmarshal(put.Body.Bytes(), &withAvatar); err != nil {
		t.Fatalf("decode PUT: %v", err)
	}
	if withAvatar.AvatarRevision != "ar-1" || withAvatar.AvatarVersion == nil || withAvatar.AvatarURL == nil {
		t.Fatalf("PUT projection mismatch: %+v", withAvatar)
	}

	get := avatarRequest(router, http.MethodGet, *withAvatar.AvatarURL, token, "", "")
	if get.Code != http.StatusOK || get.Header().Get("ETag") != `"`+*withAvatar.AvatarVersion+`"` ||
		get.Header().Get("Cache-Control") != "private, no-cache" || get.Header().Get("Vary") != "Authorization" ||
		get.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET avatar response mismatch: status=%d headers=%v body=%s", get.Code, get.Header(), get.Body.String())
	}
	if _, err := png.Decode(bytes.NewReader(get.Body.Bytes())); err != nil {
		t.Fatalf("GET body is not normalized PNG: %v", err)
	}
	notModified := avatarRequest(router, http.MethodGet, *withAvatar.AvatarURL, token, "If-None-Match", get.Header().Get("ETag"))
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional GET: status=%d body=%q", notModified.Code, notModified.Body.String())
	}
	missingVersion := avatarRequest(router, http.MethodGet, "/api/v1/customers/"+*created.Id+"/avatar/content", token, "", "")
	if missingVersion.Code != http.StatusBadRequest {
		t.Fatalf("missing v: want 400, got %d", missingVersion.Code)
	}

	noOp := avatarMultipartRequest(t, router, http.MethodPut, "/api/v1/customers/"+*created.Id+"/avatar", token, `"ar-0"`, "image/png", imageA)
	if noOp.Code != http.StatusOK {
		t.Fatalf("same-content stale revision no-op: %d %s", noOp.Code, noOp.Body.String())
	}
	conflict := avatarMultipartRequest(t, router, http.MethodPut, "/api/v1/customers/"+*created.Id+"/avatar", token, `"ar-0"`, "image/png", imageB)
	if conflict.Code != http.StatusConflict || errorCode(t, conflict.Body.Bytes()) != "avatar_revision_conflict" {
		t.Fatalf("stale different-content PUT: %d %s", conflict.Code, conflict.Body.String())
	}
	stale := avatarRequest(router, http.MethodGet, "/api/v1/customers/"+*created.Id+"/avatar/content?v=sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", token, "", "")
	if stale.Code != http.StatusConflict || errorCode(t, stale.Body.Bytes()) != "avatar_version_stale" {
		t.Fatalf("stale v: %d %s", stale.Code, stale.Body.String())
	}

	remove := avatarRequest(router, http.MethodDelete, "/api/v1/customers/"+*created.Id+"/avatar", token, "If-Match", `"ar-1"`)
	if remove.Code != http.StatusOK {
		t.Fatalf("DELETE avatar: %d %s", remove.Code, remove.Body.String())
	}
	var removed struct {
		AvatarRevision string  `json:"avatar_revision"`
		AvatarVersion  *string `json:"avatar_version"`
	}
	_ = json.Unmarshal(remove.Body.Bytes(), &removed)
	if removed.AvatarRevision != "ar-2" || removed.AvatarVersion != nil {
		t.Fatalf("DELETE projection mismatch: %+v", removed)
	}
	if rec := avatarRequest(router, http.MethodGet, *withAvatar.AvatarURL, token, "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after remove: want 404, got %d", rec.Code)
	}
}

func TestCustomerAvatarHTTPAuthAndInputGuards(t *testing.T) {
	router, database, issuer := newCustomerAPIRouter(t)
	tokenA := issueToken(t, issuer, testAcctID)
	created := createCustomerAPI(t, router, tokenA, "avatar-guard")
	if err := database.CreateAccount(t.Context(), "acct-avatar-other", "hash"); err != nil {
		t.Fatalf("create other account: %v", err)
	}
	tokenB := issueToken(t, issuer, "acct-avatar-other")
	path := "/api/v1/customers/" + *created.Id + "/avatar"
	content := avatarPNG(t, color.NRGBA{R: 1, G: 2, B: 3, A: 255})

	if rec := avatarMultipartRequest(t, router, http.MethodPut, path, "", `"ar-0"`, "image/png", content); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT: %d", rec.Code)
	}
	if rec := avatarMultipartRequest(t, router, http.MethodPut, path, tokenB, `"ar-0"`, "image/png", content); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-account PUT: %d %s", rec.Code, rec.Body.String())
	}
	if rec := avatarMultipartRequest(t, router, http.MethodPut, path, tokenA, "", "image/png", content); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing If-Match: %d", rec.Code)
	}
	if rec := avatarMultipartRequest(t, router, http.MethodPut, path, tokenA, `"ar-0"`, "image/jpeg", content); rec.Code != http.StatusBadRequest {
		t.Fatalf("fake MIME: %d %s", rec.Code, rec.Body.String())
	}
}

func avatarMultipartRequest(
	t *testing.T,
	h http.Handler,
	method, path, token, ifMatch, mediaType string,
	content []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="ignored"`)
	header.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	_, _ = part.Write(content)
	_ = writer.Close()
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func avatarRequest(h http.Handler, method, path, token, header, value string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if header != "" {
		req.Header.Set(header, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func avatarPNG(t *testing.T, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.SetNRGBA(x, y, fill)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return output.Bytes()
}
