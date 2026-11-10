package versionedfs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// presignOnly 只实现签名；其余SDK方法在本测试中不会被调用。
type presignOnly struct {
	OSSAPI
	got *oss.GetObjectRequest
	url string
}

func (p *presignOnly) Presign(_ context.Context, request any, _ ...func(*oss.PresignOptions)) (*oss.PresignResult, error) {
	p.got, _ = request.(*oss.GetObjectRequest)
	return &oss.PresignResult{Method: "GET", URL: p.url}, nil
}

func TestSignVersionReadPinsTheVersion(t *testing.T) {
	api := &presignOnly{url: "https://bucket.oss-cn-hangzhou.aliyuncs.com/creative-v2/blobs/b/versions/s?x-oss-signature=1"}
	s, err := NewOSS(api, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	location, err := s.SignVersionRead(t.Context(), "creative-v2/blobs/b/versions/s", "v1", time.Now().Add(time.Minute))
	if err != nil || location != api.url || api.got == nil || *api.got.VersionId != "v1" || *api.got.Bucket != "bucket" {
		t.Fatalf("signed %q %v %+v", location, err, api.got)
	}
	if _, err := s.SignVersionRead(t.Context(), "creative-v2/blobs/b/versions/s", "", time.Now().Add(time.Minute)); !errors.Is(err, ErrState) {
		t.Fatalf("unversioned read signed: %v", err)
	}
	api.url = "https://bucket.oss-cn-hangzhou-internal.aliyuncs.com/x"
	if _, err := s.SignVersionRead(t.Context(), "creative-v2/blobs/b/versions/s", "v1", time.Now().Add(time.Minute)); !errors.Is(err, ErrState) {
		t.Fatalf("an internal endpoint link was handed out: %v", err)
	}
	api.url = "http://bucket.internal/x"
	if _, err := s.SignVersionRead(t.Context(), "creative-v2/blobs/b/versions/s", "v1", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("a non-https link was handed out")
	}
}
