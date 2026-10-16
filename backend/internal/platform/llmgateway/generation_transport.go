package llmgateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"syscall"
)

// generationHTTPDo只在标准Transport提供建连前失败的正面证据时声明未派发。
// GotConn之后的任何失败（包括部分写入、响应丢失）都保留受理未知。
// 不用WroteRequest回调缺席作判断：该回调可能晚于Do返回。
func generationHTTPDo(client *http.Client, req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotDispatched, transportFailureKind(err))
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
			return response, fmt.Errorf("%w: %s", ErrNotDispatched, transportFailureKind(err))
		}
	}
	// 底层错误可能带临时地址，不穿过适配器边界；只保留排查用的分类。
	return response, fmt.Errorf("%w: %s connected=%t", ErrUnavailable, transportFailureKind(err), connected.Load())
}

// transportFailureKind 给传输失败一个不含地址的分类，供日志区分超时、断连与建连失败。
func transportFailureKind(err error) string {
	var dns *net.DNSError
	var op *net.OpError
	var tlsErr *tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &tlsErr) || errors.As(err, &certErr):
		return "tls"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "eof"
	case errors.As(err, &op):
		return "net_" + op.Op
	default:
		return "transport"
	}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
