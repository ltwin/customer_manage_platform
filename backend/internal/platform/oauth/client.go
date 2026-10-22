package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	metadataMaxBytes = 5 << 10
	metadataTimeout  = 3 * time.Second
	metadataTTL      = 24 * time.Hour
	maxRedirectURIs  = 5
	maxClientName    = 100
)

var authMethods = []string{"none", "client_secret_post", "client_secret_basic"}

// validRedirectURI accepts a loopback http callback (command-line hosts) or an
// https one (hosts whose servers finish the flow). Nothing else: no custom
// schemes, fragments or credentials.
func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" || u.Opaque != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return loopbackHost(u.Hostname())
	}
	return false
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// matchRedirectURI compares a requested callback with a registered one. A
// loopback callback may use any port (RFC 8252 §7.3): native clients pick one
// at run time. Everything else must be identical.
func matchRedirectURI(registered []string, requested string) bool {
	if slices.Contains(registered, requested) {
		return true
	}
	r, err := url.Parse(requested)
	if err != nil || r.Scheme != "http" || !loopbackHost(r.Hostname()) {
		return false
	}
	for _, candidate := range registered {
		c, err := url.Parse(candidate)
		if err != nil || c.Scheme != "http" || !loopbackHost(c.Hostname()) {
			continue
		}
		if c.Hostname() == r.Hostname() && c.EscapedPath() == r.EscapedPath() && c.RawQuery == r.RawQuery {
			return true
		}
	}
	return false
}

func validRedirectURIs(uris []string) bool {
	if len(uris) < 1 || len(uris) > maxRedirectURIs {
		return false
	}
	for _, u := range uris {
		if !validRedirectURI(u) {
			return false
		}
	}
	return true
}

func validClientName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= maxClientName
}

// RegistrationRequest is the RFC 7591 body. Unknown fields are kept verbatim
// in the stored metadata and change nothing.
type RegistrationRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	AuthMethod   string   `json:"token_endpoint_auth_method"`
}

// RegistrationResponse answers a registration; the secret is shown only here.
type RegistrationResponse struct {
	ClientID              string   `json:"client_id"`
	ClientIDIssuedAt      int64    `json:"client_id_issued_at"`
	ClientName            string   `json:"client_name"`
	RedirectURIs          []string `json:"redirect_uris"`
	GrantTypes            []string `json:"grant_types"`
	AuthMethod            string   `json:"token_endpoint_auth_method"`
	ClientSecret          string   `json:"client_secret,omitempty"`
	ClientSecretExpiresAt *int64   `json:"client_secret_expires_at,omitempty"`
}

// Register is dynamic client registration, kept for hosts that do not use a
// metadata document. ip is the caller as resolved by trusted proxies.
func (s *Service) Register(ctx context.Context, ip string, raw []byte) (RegistrationResponse, error) {
	now := s.now().UTC()
	if wait, ok := s.allowRegistration(ip, now); !ok {
		return RegistrationResponse{}, &Error{Code: "too_many_requests", Description: "注册过于频繁，请稍后再试", RetryAfter: wait}
	}
	var req RegistrationRequest
	var fields map[string]json.RawMessage
	if len(raw) > metadataMaxBytes || json.Unmarshal(raw, &req) != nil || json.Unmarshal(raw, &fields) != nil {
		return RegistrationResponse{}, oauthError("invalid_client_metadata", "注册信息不是合法的 JSON 对象或超过 5 KB")
	}
	if !validClientName(req.ClientName) {
		return RegistrationResponse{}, oauthError("invalid_client_metadata", "client_name 必须是 1–100 个字符")
	}
	if !validRedirectURIs(req.RedirectURIs) {
		return RegistrationResponse{}, oauthError("invalid_redirect_uri", "redirect_uris 须为 1–5 个回环地址或 https 地址")
	}
	if req.AuthMethod == "" {
		req.AuthMethod = "none"
	}
	if !slices.Contains(authMethods, req.AuthMethod) {
		return RegistrationResponse{}, oauthError("invalid_client_metadata", "不支持的 token_endpoint_auth_method")
	}
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return RegistrationResponse{}, oauthError("invalid_client_metadata", "只支持 authorization_code 与 refresh_token")
		}
	}
	client := Client{ID: "oclient_" + uuid.NewString(), Registration: "dcr", Name: strings.TrimSpace(req.ClientName),
		RedirectURIs: req.RedirectURIs, AuthMethod: req.AuthMethod, Metadata: raw}
	resp := RegistrationResponse{ClientID: client.ID, ClientIDIssuedAt: now.Unix(), ClientName: client.Name,
		RedirectURIs: client.RedirectURIs, GrantTypes: req.GrantTypes, AuthMethod: client.AuthMethod}
	if client.AuthMethod != "none" {
		secret, err := s.randomToken()
		if err != nil {
			return RegistrationResponse{}, err
		}
		client.SecretHash = digest(secret)
		never := int64(0)
		resp.ClientSecret, resp.ClientSecretExpiresAt = secret, &never
	}
	if err := s.store.SaveClient(ctx, client); err != nil {
		return RegistrationResponse{}, err
	}
	s.logger.InfoContext(ctx, "oauth client registered", "client_id", client.ID, "client_name", client.Name,
		"auth_method", client.AuthMethod, "redirect_hosts", redirectHosts(client.RedirectURIs))
	return resp, nil
}

// resolveClient finds the client named by an authorization request: a URL is
// a metadata document (fetched again once a day), anything else a registration.
func (s *Service) resolveClient(ctx context.Context, id string) (Client, error) {
	if !strings.HasPrefix(id, "https://") {
		c, err := s.store.Client(ctx, id)
		if err == nil && c.Registration != "dcr" {
			return Client{}, ErrNotFound
		}
		return c, err
	}
	cached, err := s.store.Client(ctx, id)
	if err == nil && cached.FetchedAt != nil && s.now().Sub(*cached.FetchedAt) < metadataTTL {
		return cached, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Client{}, err
	}
	fetched, err := s.fetchMetadata(ctx, id)
	if err != nil {
		s.logger.WarnContext(ctx, "oauth client metadata refused", "client_id", id, "reason", err.Error())
		return Client{}, ErrNotFound
	}
	if err := s.store.SaveClient(ctx, fetched); err != nil {
		return Client{}, err
	}
	return fetched, nil
}

var errMetadata = errors.New("client metadata document refused")

// fetchMetadata reads a Client ID Metadata Document. The URL comes from an
// anonymous request, so the fetch is bounded in every way and the dialer
// refuses anything but public addresses.
func (s *Service) fetchMetadata(ctx context.Context, id string) (Client, error) {
	u, err := url.Parse(id)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Path == "" || u.Path == "/" {
		return Client{}, fmt.Errorf("%w: client_id must be an https URL with a path", errMetadata)
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return Client{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return Client{}, fmt.Errorf("%w: %v", errMetadata, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Client{}, fmt.Errorf("%w: status %d", errMetadata, resp.StatusCode)
	}
	if media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); media != "application/json" {
		return Client{}, fmt.Errorf("%w: content type %q", errMetadata, media)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, metadataMaxBytes+1))
	if err != nil || len(raw) > metadataMaxBytes {
		return Client{}, fmt.Errorf("%w: body over 5 KB", errMetadata)
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Client{}, fmt.Errorf("%w: not json", errMetadata)
	}
	if doc.ClientID != id {
		return Client{}, fmt.Errorf("%w: client_id does not match its URL", errMetadata)
	}
	if !validClientName(doc.ClientName) || !validRedirectURIs(doc.RedirectURIs) {
		return Client{}, fmt.Errorf("%w: client_name or redirect_uris", errMetadata)
	}
	if doc.AuthMethod != "" && doc.AuthMethod != "none" {
		return Client{}, fmt.Errorf("%w: a metadata document cannot hold a secret", errMetadata)
	}
	now := s.now().UTC()
	return Client{ID: id, Registration: "cimd", Name: strings.TrimSpace(doc.ClientName), RedirectURIs: doc.RedirectURIs,
		AuthMethod: "none", Metadata: raw, FetchedAt: &now}, nil
}

// guardedClient never follows redirects and dials only public unicast
// addresses, checked on the connection actually made so a DNS answer cannot
// swap in an internal address after validation.
func guardedClient() *http.Client {
	dialer := &net.Dialer{Timeout: metadataTimeout, Control: publicAddressOnly}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSHandshakeTimeout:    metadataTimeout,
		ResponseHeaderTimeout:  metadataTimeout,
		MaxResponseHeaderBytes: 16 << 10,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       metadataTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func publicAddressOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !publicAddress(addr) {
		return fmt.Errorf("address %s is not public", addr)
	}
	return nil
}

func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(addr) && !netip.MustParsePrefix("fc00::/7").Contains(addr)
}

func redirectHosts(uris []string) []string {
	hosts := []string{}
	for _, raw := range uris {
		if u, err := url.Parse(raw); err == nil && !slices.Contains(hosts, u.Hostname()) {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts
}
