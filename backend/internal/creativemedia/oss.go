package creativemedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// OSSAPI is the SDK surface the adapter depends on; tests inject a fake.
type OSSAPI interface {
	InitiateMultipartUpload(context.Context, *oss.InitiateMultipartUploadRequest, ...func(*oss.Options)) (*oss.InitiateMultipartUploadResult, error)
	ListParts(context.Context, *oss.ListPartsRequest, ...func(*oss.Options)) (*oss.ListPartsResult, error)
	CompleteMultipartUpload(context.Context, *oss.CompleteMultipartUploadRequest, ...func(*oss.Options)) (*oss.CompleteMultipartUploadResult, error)
	AbortMultipartUpload(context.Context, *oss.AbortMultipartUploadRequest, ...func(*oss.Options)) (*oss.AbortMultipartUploadResult, error)
	HeadObject(context.Context, *oss.HeadObjectRequest, ...func(*oss.Options)) (*oss.HeadObjectResult, error)
	GetObject(context.Context, *oss.GetObjectRequest, ...func(*oss.Options)) (*oss.GetObjectResult, error)
	PutObject(context.Context, *oss.PutObjectRequest, ...func(*oss.Options)) (*oss.PutObjectResult, error)
	DeleteObject(context.Context, *oss.DeleteObjectRequest, ...func(*oss.Options)) (*oss.DeleteObjectResult, error)
	Presign(context.Context, any, ...func(*oss.PresignOptions)) (*oss.PresignResult, error)
}

// OSS stores staging and blobs in one private bucket under creative-v2/.
// Part writes use server-signed URLs bound to key/uploadId/partNumber.
type OSS struct {
	api    OSSAPI
	bucket string
}

func NewOSS(api OSSAPI, bucket string) (*OSS, error) {
	if api == nil || bucket == "" {
		return nil, errors.New("creative media oss api and bucket are required")
	}
	return &OSS{api: api, bucket: bucket}, nil
}
func (s *OSS) Driver() string { return "oss" }
func (s *OSS) Bucket() string { return s.bucket }

func (s *OSS) InitMultipart(ctx context.Context, key, mime string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	result, err := s.api.InitiateMultipartUpload(ctx, &oss.InitiateMultipartUploadRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), ContentType: oss.Ptr(mime)})
	if err != nil {
		return "", classify(err)
	}
	if result == nil || result.UploadId == nil || *result.UploadId == "" {
		return "", ErrUnknownResult
	}
	return *result.UploadId, nil
}
func (s *OSS) AuthorizePart(ctx context.Context, key, session string, part int, expires time.Time) (PartAuthorization, error) {
	if err := validKey(key); err != nil {
		return PartAuthorization{}, err
	}
	if session == "" || part < 1 || part > 10000 {
		return PartAuthorization{}, fmt.Errorf("%w: part authorization", ErrState)
	}
	result, err := s.api.Presign(ctx, &oss.UploadPartRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), UploadId: oss.Ptr(session), PartNumber: int32(part)}, oss.PresignExpiration(expires))
	if err != nil {
		return PartAuthorization{}, classify(err)
	}
	headers := map[string]string{}
	for k, v := range result.SignedHeaders {
		headers[k] = v
	}
	return PartAuthorization{Number: part, Method: result.Method, URL: result.URL, Headers: headers, ExpiresAt: result.Expiration}, nil
}
func (s *OSS) ListParts(ctx context.Context, key, session string) ([]Part, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	parts := []Part{}
	marker := int32(0)
	for {
		result, err := s.api.ListParts(ctx, &oss.ListPartsRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), UploadId: oss.Ptr(session), MaxParts: 1000, PartNumberMarker: marker})
		if err != nil {
			return nil, classify(err)
		}
		if result == nil {
			return nil, ErrUnknownResult
		}
		for _, p := range result.Parts {
			parts = append(parts, Part{Number: int(p.PartNumber), ETag: strings.Trim(deref(p.ETag), `"`), Size: p.Size})
		}
		if !result.IsTruncated || result.NextPartNumberMarker <= marker {
			return parts, nil
		}
		marker = result.NextPartNumberMarker
	}
}
func (s *OSS) CompleteMultipart(ctx context.Context, key, session string, parts []Part) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	body := &oss.CompleteMultipartUpload{}
	for _, p := range parts {
		body.Parts = append(body.Parts, oss.UploadPart{PartNumber: int32(p.Number), ETag: oss.Ptr(`"` + p.ETag + `"`)})
	}
	result, err := s.api.CompleteMultipartUpload(ctx, &oss.CompleteMultipartUploadRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), UploadId: oss.Ptr(session), CompleteMultipartUpload: body})
	if err != nil {
		return "", classify(err)
	}
	if result == nil || deref(result.VersionId) == "" {
		return "", fmt.Errorf("%w: bucket versioning is required for fixed staging versions", ErrUnknownResult)
	}
	return *result.VersionId, nil
}
func (s *OSS) AbortMultipart(ctx context.Context, key, session string) error {
	if err := validKey(key); err != nil {
		return err
	}
	_, err := s.api.AbortMultipartUpload(ctx, &oss.AbortMultipartUploadRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), UploadId: oss.Ptr(session)})
	if err != nil && !errors.Is(classify(err), ErrNotFound) {
		return classify(err)
	}
	return nil
}
func (s *OSS) StatVersion(ctx context.Context, key, version string) (ObjectStat, error) {
	if err := validKey(key); err != nil {
		return ObjectStat{}, err
	}
	if version == "" {
		return ObjectStat{}, ErrNotFound
	}
	result, err := s.api.HeadObject(ctx, &oss.HeadObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), VersionId: oss.Ptr(version)})
	if err != nil {
		return ObjectStat{}, classify(err)
	}
	return ObjectStat{Size: result.ContentLength, Version: version}, nil
}
func (s *OSS) OpenVersion(ctx context.Context, key, version string, rng *ByteRange) (io.ReadCloser, ObjectStat, error) {
	stat, err := s.StatVersion(ctx, key, version)
	if err != nil {
		return nil, ObjectStat{}, err
	}
	request := &oss.GetObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), VersionId: oss.Ptr(version)}
	if rng != nil {
		if rng.Start < 0 || rng.End < rng.Start || rng.End >= stat.Size {
			return nil, ObjectStat{}, ErrRange
		}
		request.Range = oss.Ptr("bytes=" + strconv.FormatInt(rng.Start, 10) + "-" + strconv.FormatInt(rng.End, 10))
		request.RangeBehavior = oss.Ptr("standard")
	}
	result, err := s.api.GetObject(ctx, request)
	if err != nil {
		return nil, ObjectStat{}, classify(err)
	}
	if result == nil || result.Body == nil {
		return nil, ObjectStat{}, ErrUnknownResult
	}
	return result.Body, stat, nil
}
func (s *OSS) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	result, err := s.api.PutObject(ctx, &oss.PutObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), Body: body, ContentLength: oss.Ptr(size), ContentType: oss.Ptr(mime), ForbidOverwrite: oss.Ptr("true")})
	if err != nil {
		return "", classify(err)
	}
	if result == nil || deref(result.VersionId) == "" {
		return "", fmt.Errorf("%w: bucket versioning is required for fixed blob versions", ErrUnknownResult)
	}
	return *result.VersionId, nil
}
func (s *OSS) DeleteExact(ctx context.Context, key, version string) error {
	if err := validKey(key); err != nil {
		return err
	}
	if version == "" {
		return ErrNotFound
	}
	_, err := s.api.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), VersionId: oss.Ptr(version)})
	if err != nil && !errors.Is(classify(err), ErrNotFound) {
		return classify(err)
	}
	return nil
}
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func classify(err error) error {
	var serviceErr *oss.ServiceError
	if errors.As(err, &serviceErr) {
		switch {
		case serviceErr.StatusCode == http.StatusNotFound || serviceErr.Code == "NoSuchKey" || serviceErr.Code == "NoSuchUpload":
			return ErrNotFound
		case serviceErr.StatusCode == http.StatusRequestedRangeNotSatisfiable:
			return ErrRange
		}
	}
	return err
}
