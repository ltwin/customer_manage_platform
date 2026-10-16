package llmgateway

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

// generationHTTPDo只在标准Transport提供建连前失败的正面证据时声明未派发。
// GotConn之后的任何失败（包括部分写入、响应丢失）都保留受理未知。
// 不用WroteRequest回调缺席作判断：该回调可能晚于Do返回。
func generationHTTPDo(client *http.Client, req *http.Request) (*http.Response, error) {
	if req.Context().Err() != nil {
		return nil, ErrNotDispatched
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	_, standard := transport.(*http.Transport)
	var connected, handshakeFailed atomic.Bool
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				handshakeFailed.Store(true)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	response, err := client.Do(req)
	if err == nil {
		return response, nil
	}
	if standard && !connected.Load() {
		var dns *net.DNSError
		var op *net.OpError
		if handshakeFailed.Load() || errors.As(err, &dns) || (errors.As(err, &op) && op.Op == "dial") {
			return response, ErrNotDispatched
		}
	}
	// 底层错误可能带临时地址，不穿过适配器边界。
	return response, ErrUnavailable
}
