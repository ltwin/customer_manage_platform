package creativemedia

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrGenerationFetch = errors.New("generated media fetch refused or failed")

// GenerationFetcher 由组合根按部署绑定，不能接受账号自选的白名单或HTTP客户端。
type GenerationFetcher interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

type GenerationHTTPFetcher struct {
	hosts  map[string]bool
	client *http.Client
}

// NewGenerationHTTPFetcher 只允许明确列出的HTTPS域名；域名写死加证书校验已足够，
// 不再检查解析出的地址（2026-10-09 拍板）。
// 不用系统代理，不跟随重定向，不将含签名的URL或底层网络错误暴露给调用方。
func NewGenerationHTTPFetcher(hosts []string) (*GenerationHTTPFetcher, error) {
	f := &GenerationHTTPFetcher{hosts: map[string]bool{}}
	for _, h := range hosts {
		h = strings.ToLower(h)
		if h == "" || strings.ContainsAny(h, "/:@*\\ \t\r\n") || strings.HasSuffix(h, ".") || net.ParseIP(h) != nil {
			return nil, ErrGenerationFetch
		}
		f.hosts[h] = true
	}
	if len(f.hosts) == 0 {
		return nil, ErrGenerationFetch
	}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, DisableCompression: true, MaxIdleConnsPerHost: 2}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !f.hosts[strings.ToLower(host)] {
			return nil, ErrGenerationFetch
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, network, address)
	}
	f.client = &http.Client{Transport: transport, Timeout: 4 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f, nil
}
func (f *GenerationHTTPFetcher) Open(ctx context.Context, location string) (io.ReadCloser, error) {
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || !f.hosts[strings.ToLower(u.Hostname())] || u.Port() != "" && u.Port() != "443" {
		return nil, ErrGenerationFetch
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrGenerationFetch
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, ErrGenerationFetch
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "" {
		_ = resp.Body.Close()
		return nil, ErrGenerationFetch
	}
	return resp.Body, nil
}

var _ GenerationFetcher = (*GenerationHTTPFetcher)(nil)
