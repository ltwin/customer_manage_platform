package creativemedia

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
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

// NewGenerationHTTPFetcher 只允许明确列出的HTTPS域名，每次连接解析并固定公网地址。
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
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, ErrGenerationFetch
		}
		for _, ip := range ips {
			if !publicGenerationAddress(ip) {
				return nil, ErrGenerationFetch
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	f.client = &http.Client{Transport: transport, Timeout: 4 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f, nil
}
func publicGenerationAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Contains(ip) {
			return false
		}
	}
	return true
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
