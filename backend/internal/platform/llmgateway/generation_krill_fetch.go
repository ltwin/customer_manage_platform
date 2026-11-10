package llmgateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// 2026-11-09 通过真实异步任务的 content 302 核实；不放开 R2 通配符。
const krillImageFilesHost = "e7ccb6761104cf83c278ad6f60ce6b17.r2.cloudflarestorage.com"

// Open 满足媒体层的 GenerationFetcher 接口。只对固定任务 content 路径附加凭证；
// 仅接受一次到已核实图片源的 HTTPS 重定向，第二跳不携带 API key。
func (a *KrillImages) Open(ctx context.Context, location string) (io.ReadCloser, error) {
	u, err := url.Parse(location)
	if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || u.RawPath != "" || u.Scheme+"://"+u.Host != a.baseURL {
		return nil, fmt.Errorf("%w: krill content location", ErrProtocol)
	}
	const prefix = "/v1/images/"
	if !strings.HasPrefix(u.Path, prefix) || !strings.HasSuffix(u.Path, "/content") || !krillTaskID(strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), "/content")) {
		return nil, fmt.Errorf("%w: krill content identity", ErrProtocol)
	}
	response, err := a.fetch(ctx, location, true)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusFound || response.StatusCode == http.StatusTemporaryRedirect || response.StatusCode == http.StatusSeeOther {
		redirect := response.Header.Get("Location")
		_ = response.Body.Close()
		target, err := url.Parse(redirect)
		if err != nil || len(redirect) > 8192 || target.Scheme != "https" || target.Host != krillImageFilesHost || target.User != nil || target.Fragment != "" || target.Opaque != "" || target.RawPath != "" || !strings.HasPrefix(target.Path, "/krill/relay-image-inputs/") {
			return nil, fmt.Errorf("%w: krill result location", ErrProtocol)
		}
		response, err = a.fetch(ctx, redirect, false)
		if err != nil {
			return nil, err
		}
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: krill content response", ErrProtocol)
	}
	return response.Body, nil
}

func (a *KrillImages) fetch(ctx context.Context, location string, authenticated bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: krill content request", ErrProtocol)
	}
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: krill content transport", ErrUnavailable)
	}
	return resp, nil
}
