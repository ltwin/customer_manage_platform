package httpapi

import (
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

const (
	maxForwardedSourceBytes = 1024
	maxForwardedSourceHops  = 16
)

func clientMeta(c *gin.Context, trustedProxyCIDRs []netip.Prefix) auth.ClientMeta {
	return auth.ClientMeta{
		SourceIP: resolveClientSource(
			c.Request.RemoteAddr,
			strings.Join(c.Request.Header.Values("X-Forwarded-For"), ","),
			trustedProxyCIDRs,
		),
		UserAgent: c.Request.UserAgent(),
	}
}

func resolveClientSource(remoteAddr, forwardedFor string, trustedProxyCIDRs []netip.Prefix) netip.Addr {
	direct := parseRemoteSource(remoteAddr)
	if !direct.IsValid() || !sourceIsTrusted(direct, trustedProxyCIDRs) || forwardedFor == "" {
		return direct
	}
	if len(forwardedFor) > maxForwardedSourceBytes {
		return direct
	}
	parts := strings.Split(forwardedFor, ",")
	if len(parts) > maxForwardedSourceHops {
		return direct
	}
	chain := make([]netip.Addr, len(parts))
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return direct
		}
		address, err := netip.ParseAddr(part)
		if err != nil {
			return direct
		}
		chain[index] = address.Unmap()
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !sourceIsTrusted(chain[index], trustedProxyCIDRs) {
			return chain[index]
		}
	}
	return chain[0]
}

func parseRemoteSource(value string) netip.Addr {
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr().Unmap()
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap()
}

func sourceIsTrusted(source netip.Addr, trustedProxyCIDRs []netip.Prefix) bool {
	for _, prefix := range trustedProxyCIDRs {
		if prefix.Contains(source) {
			return true
		}
	}
	return false
}
