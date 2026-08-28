package immutablefs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// ErrOSSConfigInvalid 表示 OSS 对象存储配置缺失或非法。
var ErrOSSConfigInvalid = errors.New("oss object store 配置非法：region 与 bucket 必填")

// OSSConfig 描述 OSS 不可变对象存储的连接配置；凭证不经此处传递，
// 由 SDK 凭证链从环境变量（OSS_ACCESS_KEY_ID/SECRET）或 ECS RAM Role 解析。
type OSSConfig struct {
	Region   string
	Endpoint string // 可选；为空时 SDK 按 Region 构造默认域名。ECS 同地域建议内网 endpoint。
	Bucket   string
	UseCName bool // Endpoint 是 bucket 自定义 CNAME 时必须为 true；普通 OSS endpoint 保持 false。
}

// ossAPI 隔离 OSS store 依赖的 SDK 面；生产由 *oss.Client 满足，测试注入 fake。
type ossAPI interface {
	PutObject(context.Context, *oss.PutObjectRequest, ...func(*oss.Options)) (*oss.PutObjectResult, error)
	HeadObject(context.Context, *oss.HeadObjectRequest, ...func(*oss.Options)) (*oss.HeadObjectResult, error)
	GetObject(context.Context, *oss.GetObjectRequest, ...func(*oss.Options)) (*oss.GetObjectResult, error)
	DeleteObject(context.Context, *oss.DeleteObjectRequest, ...func(*oss.Options)) (*oss.DeleteObjectResult, error)
	ListObjectsV2(context.Context, *oss.ListObjectsV2Request, ...func(*oss.Options)) (*oss.ListObjectsV2Result, error)
	GetBucketInfo(context.Context, *oss.GetBucketInfoRequest, ...func(*oss.Options)) (*oss.GetBucketInfoResult, error)
}

// OSS 是 immutablefs.ObjectStore 的阿里云 OSS 实现：单对象携带 x-oss-meta-*
// 用户元数据（无 metadata.json sidecar），List 采用与 Local 相同的 keyset 游标。
type OSS struct {
	api    ossAPI
	bucket string
}

var (
	_ ObjectStore = (*OSS)(nil)
)

// NewOSS 创建阿里云 OSS 不可变对象存储；调用方应在启动时继续调用 Probe。
func NewOSS(cfg OSSConfig) (*OSS, error) {
	if cfg.Region == "" || cfg.Bucket == "" {
		return nil, ErrOSSConfigInvalid
	}
	if cfg.UseCName && cfg.Endpoint == "" {
		return nil, ErrOSSConfigInvalid
	}
	loader := newOSSClientConfig(cfg)
	return &OSS{api: oss.NewClient(loader), bucket: cfg.Bucket}, nil
}

func newOSSClientConfig(cfg OSSConfig) *oss.Config {
	loader := oss.LoadDefaultConfig().
		WithRegion(cfg.Region).
		WithCredentialsProvider(newCredentialsChain())
	if cfg.Endpoint != "" {
		loader = loader.WithEndpoint(cfg.Endpoint)
	}
	if cfg.UseCName {
		loader = loader.WithUseCName(true)
	}
	return loader
}

// Probe 验证 bucket 可达且凭据有效；启动装配时调用，失败应阻止服务起跑。
func (s *OSS) Probe(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.api.GetBucketInfo(ctx, &oss.GetBucketInfoRequest{Bucket: oss.Ptr(s.bucket)})
	if err != nil {
		return classifyOSSError(err)
	}
	return nil
}

// newCredentialsChain：环境变量 AK/SK 优先，ECS RAM Role 兜底；
// 两者都不可用时在首个请求处报错。
func newCredentialsChain() credentials.CredentialsProvider {
	env := credentials.NewEnvironmentVariableCredentialsProvider()
	ecsRole := credentials.NewEcsRoleCredentialsProvider()
	return credentials.CredentialsProviderFunc(func(ctx context.Context) (credentials.Credentials, error) {
		if hasEnvironmentOSSCredentials() {
			cred, err := env.GetCredentials(ctx)
			if err != nil {
				return credentials.Credentials{}, fmt.Errorf("load OSS environment credentials: %w", err)
			}
			return cred, nil
		}
		return ecsRole.GetCredentials(ctx)
	})
}

func hasEnvironmentOSSCredentials() bool {
	return os.Getenv("OSS_ACCESS_KEY_ID") != "" || os.Getenv("OSS_ACCESS_KEY_SECRET") != ""
}

// PutImmutable 创建不可变对象；同 key 已存在时只接受完全相同的元数据。
func (s *OSS) PutImmutable(ctx context.Context, key string, body []byte, expected Metadata) (Metadata, bool, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, false, err
	}
	if expected.Size != int64(len(body)) || expected.Size <= 0 || expected.Checksum != checksum(body) {
		return Metadata{}, false, ErrIntegrity
	}
	if err := validateObjectKey(key); err != nil {
		return Metadata{}, false, err
	}
	if err := validateDomainKeyChecksum(key, expected.Checksum); err != nil {
		return Metadata{}, false, err
	}
	if meta, err := s.Stat(ctx, key); err == nil {
		if sameMeta(meta, expected) {
			return meta, false, nil
		}
		return Metadata{}, false, ErrIntegrity
	} else if !errors.Is(err, ErrNotFound) {
		return Metadata{}, false, err
	}
	_, err := s.api.PutObject(ctx, &oss.PutObjectRequest{
		Bucket:          oss.Ptr(s.bucket),
		Key:             oss.Ptr(key),
		ContentType:     oss.Ptr(expected.MediaType),
		Metadata:        metadataToUserMeta(expected),
		ForbidOverwrite: oss.Ptr("true"),
		Body:            bytes.NewReader(body),
	})
	if err != nil {
		if isOSSConflict(err) {
			meta, statErr := s.Stat(ctx, key)
			if statErr == nil && sameMeta(meta, expected) {
				return meta, false, nil
			}
			if statErr != nil && !errors.Is(statErr, ErrNotFound) {
				return Metadata{}, false, statErr
			}
			return Metadata{}, false, ErrIntegrity
		}
		return Metadata{}, false, classifyOSSError(err)
	}
	persisted, err := s.Stat(ctx, key)
	if err != nil {
		return Metadata{}, false, err
	}
	if !sameMeta(persisted, expected) {
		return Metadata{}, false, ErrIntegrity
	}
	return persisted, true, nil
}

// Open 打开对象正文流；调用方负责关闭返回值。
func (s *OSS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	result, err := s.api.GetObject(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(s.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return nil, classifyOSSError(err)
	}
	if result == nil || result.Body == nil {
		return nil, fmt.Errorf("%w: oss get object 无响应体", ErrTemporary)
	}
	return result.Body, nil
}

// Stat 通过 HEAD 读取并严格校验对象用户元数据。
func (s *OSS) Stat(ctx context.Context, key string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if err := validateObjectKey(key); err != nil {
		return Metadata{}, err
	}
	result, err := s.api.HeadObject(ctx, &oss.HeadObjectRequest{
		Bucket: oss.Ptr(s.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return Metadata{}, classifyOSSError(err)
	}
	return userMetaToMetadata(result)
}

// List 按 key 升序返回 exclusive keyset 页面；扫描期间消失的对象会被跳过。
func (s *OSS) List(ctx context.Context, prefix, cursor string, limit int) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if limit < 1 || limit > 1000 {
		return Page{}, ErrInvalidKey
	}
	if err := validateObjectPrefix(prefix); err != nil {
		return Page{}, err
	}
	if err := validateObjectPrefix(cursor); err != nil {
		return Page{}, err
	}
	items := make([]Item, 0, limit)
	scanCursor := cursor
	done := false
	for len(items) < limit && !done {
		request := &oss.ListObjectsV2Request{
			Bucket:     oss.Ptr(s.bucket),
			MaxKeys:    int32(limit - len(items)),
			StartAfter: oss.Ptr(scanCursor),
		}
		if prefix != "" {
			request.Prefix = oss.Ptr(prefix)
		}
		result, err := s.api.ListObjectsV2(ctx, request)
		if err != nil {
			return Page{}, classifyOSSError(err)
		}
		if result == nil {
			return Page{}, fmt.Errorf("%w: oss list objects 无响应", ErrTemporary)
		}
		if len(result.Contents) == 0 {
			if result.IsTruncated {
				return Page{}, fmt.Errorf("%w: oss list truncated without progress", ErrTemporary)
			}
			done = true
			continue
		}
		lastScanned := stringFromPtr(result.Contents[len(result.Contents)-1].Key)
		if lastScanned == "" || lastScanned <= scanCursor {
			return Page{}, fmt.Errorf("%w: oss list cursor made no progress", ErrTemporary)
		}
		pageItems, err := s.statListedObjects(ctx, prefix, cursor, result.Contents)
		if err != nil {
			return Page{}, err
		}
		items = append(items, pageItems...)
		scanCursor = lastScanned
		done = !result.IsTruncated
	}
	page := Page{Items: items}
	if len(items) == 0 {
		page.Done = done
		return page, nil
	}
	page.NextCursor = items[len(items)-1].Key
	page.Done = done
	return page, nil
}

const listHeadConcurrency = 8

type listedStat struct {
	item Item
	keep bool
}

func (s *OSS) statListedObjects(
	ctx context.Context,
	prefix string,
	cursor string,
	objects []oss.ObjectProperties,
) ([]Item, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]listedStat, len(objects))
	jobs := make(chan int)
	var (
		workers  sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	workerCount := min(listHeadConcurrency, len(objects))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				key := stringFromPtr(objects[index].Key)
				if key == "" || key <= cursor || prefix != "" && !strings.HasPrefix(key, prefix) {
					continue
				}
				meta, err := s.Stat(ctx, key)
				if errors.Is(err, ErrNotFound) {
					continue
				}
				if err != nil {
					errOnce.Do(func() {
						firstErr = err
						cancel()
					})
					continue
				}
				results[index] = listedStat{item: Item{Key: key, Meta: meta}, keep: true}
			}
		}()
	}
	for index := range objects {
		if ctx.Err() != nil {
			break
		}
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	items := make([]Item, 0, len(results))
	for _, result := range results {
		if result.keep {
			items = append(items, result.item)
		}
	}
	return items, nil
}

// Delete 幂等删除 live 对象，并以随后的 HEAD not-found 作为成功 barrier。
func (s *OSS) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateObjectKey(key); err != nil {
		return err
	}
	_, err := s.api.DeleteObject(ctx, &oss.DeleteObjectRequest{
		Bucket: oss.Ptr(s.bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		classified := classifyOSSError(err)
		if errors.Is(classified, ErrNotFound) {
			return nil
		}
		return classified
	}
	_, statErr := s.Stat(ctx, key)
	if errors.Is(statErr, ErrNotFound) {
		return nil
	}
	if statErr == nil {
		return fmt.Errorf("%w: oss delete 后对象仍可见", ErrTemporary)
	}
	return statErr
}

// OpenVerified 仅在持久化元数据与 expected 完全一致时打开对象。
func (s *OSS) OpenVerified(ctx context.Context, key string, expected Metadata) (io.ReadCloser, error) {
	meta, err := s.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	if !sameMeta(meta, expected) {
		return nil, ErrIntegrity
	}
	return s.Open(ctx, key)
}

// RemoveExact 仅删除与 expected 完全一致的 live 对象；对象已不存在时成功。
func (s *OSS) RemoveExact(ctx context.Context, key string, expected Metadata) error {
	meta, err := s.Stat(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !sameMeta(meta, expected) {
		return ErrIntegrity
	}
	return s.Delete(ctx, key)
}

const (
	ossMetaMediaType = "media-type"
	ossMetaChecksum  = "checksum"
	ossMetaSize      = "size"
	ossMetaWidth     = "width"
	ossMetaHeight    = "height"
)

func metadataToUserMeta(meta Metadata) map[string]string {
	user := map[string]string{
		ossMetaMediaType: meta.MediaType,
		ossMetaChecksum:  meta.Checksum,
		ossMetaSize:      strconv.FormatInt(meta.Size, 10),
	}
	if meta.Width > 0 {
		user[ossMetaWidth] = strconv.Itoa(meta.Width)
	}
	if meta.Height > 0 {
		user[ossMetaHeight] = strconv.Itoa(meta.Height)
	}
	return user
}

func userMetaToMetadata(result *oss.HeadObjectResult) (Metadata, error) {
	if result == nil {
		return Metadata{}, ErrIntegrity
	}
	user := result.Metadata
	meta := Metadata{MediaType: user[ossMetaMediaType], Checksum: user[ossMetaChecksum]}
	if meta.MediaType == "" || meta.Checksum == "" || !validSHA256Checksum(meta.Checksum) {
		return Metadata{}, ErrIntegrity
	}
	if result.ContentType == nil || stringFromPtr(result.ContentType) != meta.MediaType {
		return Metadata{}, ErrIntegrity
	}
	size, err := parsePositiveInt64(user[ossMetaSize])
	if err != nil || size != result.ContentLength {
		return Metadata{}, ErrIntegrity
	}
	meta.Size = size
	if meta.Width, err = parseOptionalPositiveInt(user[ossMetaWidth]); err != nil {
		return Metadata{}, ErrIntegrity
	}
	if meta.Height, err = parseOptionalPositiveInt(user[ossMetaHeight]); err != nil {
		return Metadata{}, ErrIntegrity
	}
	if (meta.Width == 0) != (meta.Height == 0) {
		return Metadata{}, ErrIntegrity
	}
	if result.LastModified != nil {
		meta.ModifiedAt = result.LastModified.UTC()
	}
	return meta, nil
}

func parsePositiveInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, ErrIntegrity
	}
	return value, nil
}

func parseOptionalPositiveInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, ErrIntegrity
	}
	return value, nil
}

func validSHA256Checksum(value string) bool {
	if len(value) != len("sha256-")+64 || !strings.HasPrefix(value, "sha256-") {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256-") {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}

// classifyOSSError 把 SDK 错误映射为 immutablefs 哨兵错误：
// 对象不存在→ErrNotFound；408/429/5xx 与网络类故障→ErrTemporary；
// NoSuchBucket 是 bucket 配置/可用性故障，不得伪装成对象不存在。
// 其余（凭证、参数等 4xx）原样上抛，视为永久错误。
func classifyOSSError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *oss.ServiceError
	if errors.As(err, &serviceErr) {
		switch {
		case serviceErr.Code == "NoSuchBucket":
			return err
		case serviceErr.StatusCode == http.StatusNotFound || serviceErr.Code == "NoSuchKey":
			return ErrNotFound
		case serviceErr.StatusCode == http.StatusRequestTimeout ||
			serviceErr.StatusCode == http.StatusTooManyRequests ||
			serviceErr.StatusCode >= http.StatusInternalServerError:
			return fmt.Errorf("%w: oss %s (request %s): %w",
				ErrTemporary, serviceErr.Code, serviceErr.RequestID, err)
		default:
			return err
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %w", ErrTemporary, netErr)
	}
	// SDK 客户端侧错误（参数、签名、序列化）是永久错误，原样上抛。
	var clientErr *oss.ClientError
	if errors.As(err, &clientErr) {
		return err
	}
	// 其余未知错误按可重试处理，与 Local 把文件系统异常归为临时的方向一致。
	return fmt.Errorf("%w: %w", ErrTemporary, err)
}

func isOSSConflict(err error) bool {
	var serviceErr *oss.ServiceError
	return errors.As(err, &serviceErr) && serviceErr.StatusCode == http.StatusConflict
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// validateObjectKey 校验非空对象键：相对、clean、无穿越段。
func validateObjectKey(key string) error {
	if key == "" {
		return ErrInvalidKey
	}
	return validateObjectPrefix(key)
}

func validateDomainKeyChecksum(key, expectedChecksum string) error {
	if !strings.HasPrefix(key, "planning/") && !strings.HasPrefix(key, "avatars/") {
		return nil
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == expectedChecksum {
			return nil
		}
	}
	return ErrIntegrity
}

// validateObjectPrefix 与 validateObjectKey 同规则，但允许空串（全根列举/游标起点）。
func validateObjectPrefix(value string) error {
	if value == "" {
		return nil
	}
	if strings.Contains(value, "\\") || path.IsAbs(value) {
		return ErrInvalidKey
	}
	clean := path.Clean(strings.TrimSuffix(value, "/"))
	if clean != strings.TrimSuffix(value, "/") {
		return ErrInvalidKey
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ErrInvalidKey
	}
	return nil
}
