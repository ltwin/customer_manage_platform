package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// ConsentRequest is an authorization request the server has checked, carried
// to the consent page and back. It is signed, not secret: the page decodes it
// to show who is asking and where the answer goes.
type ConsentRequest struct {
	ClientID      string `json:"client_id"`
	ClientName    string `json:"client_name"`
	Registration  string `json:"registration"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
	State         string `json:"state"`
	HostKind      string `json:"host_kind"`
	DisplayName   string `json:"display_name"`
	ExpiresAt     int64  `json:"exp"`
}

// AuthorizeResult is one of three answers to GET /oauth/authorize.
type AuthorizeResult struct {
	// Request is the signed package for the consent page.
	Request string
	// Redirect sends a parameter error back to the client's verified callback.
	Redirect string
	// LocalError is shown on this site only: the client or its callback could
	// not be verified, so redirecting anywhere would be an open redirect.
	LocalError string
}

const requestVersion = "v1"

// Authorize checks an authorization request in the order the spec requires:
// the client and its callback first, then everything else.
func (s *Service) Authorize(ctx context.Context, q url.Values) (AuthorizeResult, error) {
	client, err := s.resolveClient(ctx, q.Get("client_id"))
	if errors.Is(err, ErrNotFound) {
		return AuthorizeResult{LocalError: "invalid_client"}, nil
	}
	if err != nil {
		return AuthorizeResult{}, err
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" && len(client.RedirectURIs) == 1 {
		redirect = client.RedirectURIs[0]
	}
	if !validRedirectURI(redirect) || !matchRedirectURI(client.RedirectURIs, redirect) {
		s.logger.WarnContext(ctx, "oauth authorize refused", "reason", "redirect_uri", "client_id", client.ID)
		return AuthorizeResult{LocalError: "invalid_redirect_uri"}, nil
	}
	state := q.Get("state")
	fail := func(code, description string) (AuthorizeResult, error) {
		s.logger.WarnContext(ctx, "oauth authorize refused", "reason", code, "client_id", client.ID)
		return AuthorizeResult{Redirect: s.callback(redirect, url.Values{"error": {code}, "error_description": {description}, "state": {state}})}, nil
	}
	challenge := q.Get("code_challenge")
	switch {
	case q.Get("response_type") != "code":
		return fail("unsupported_response_type", "只支持 response_type=code")
	case q.Get("code_challenge_method") != "S256" || len(challenge) < 43 || len(challenge) > 128 || !pkceAlphabet(challenge):
		return fail("invalid_request", "需要 S256 的 PKCE")
	// One resource only: a client that leaves it out can only mean this one.
	case q.Get("resource") != "" && q.Get("resource") != s.resource:
		return fail("invalid_target", "resource 必须是 "+s.resource)
	case q.Get("scope") != "" && q.Get("scope") != Scope:
		return fail("invalid_scope", "只支持 scope=creative")
	}
	kind, display := identifyHost(client, redirect)
	s.logger.InfoContext(ctx, "oauth authorize request", "client_id", client.ID, "client_name", client.Name,
		"registration", client.Registration, "redirect_host", redirectHosts([]string{redirect}), "host_kind", kind)
	request, err := s.signRequest(ConsentRequest{
		ClientID: client.ID, ClientName: client.Name, Registration: client.Registration, RedirectURI: redirect,
		CodeChallenge: challenge, State: state, HostKind: kind, DisplayName: display,
		ExpiresAt: s.now().Add(requestTTL).Unix(),
	})
	if err != nil {
		return AuthorizeResult{}, err
	}
	return AuthorizeResult{Request: request}, nil
}

func pkceAlphabet(s string) bool {
	for _, r := range s {
		unreserved := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_' || r == '~'
		if !unreserved {
			return false
		}
	}
	return true
}

// callback appends the response to the client's callback, always naming the
// issuer (RFC 9207) so a client talking to several servers can tell answers apart.
func (s *Service) callback(redirect string, params url.Values) string {
	u, _ := url.Parse(redirect)
	q := u.Query()
	for k, v := range params {
		if len(v) > 0 && v[0] != "" {
			q.Set(k, v[0])
		}
	}
	q.Set("iss", s.base)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Service) signRequest(r ConsentRequest) (string, error) {
	payload, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	body := requestVersion + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.requestKey)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) parseRequest(raw string) (ConsentRequest, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != requestVersion {
		return ConsentRequest{}, ErrRequestInvalid
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return ConsentRequest{}, ErrRequestInvalid
	}
	mac := hmac.New(sha256.New, s.requestKey)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ConsentRequest{}, ErrRequestInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ConsentRequest{}, ErrRequestInvalid
	}
	var r ConsentRequest
	if json.Unmarshal(payload, &r) != nil || s.now().Unix() > r.ExpiresAt {
		return ConsentRequest{}, ErrRequestInvalid
	}
	return r, nil
}

// Decide records the photographer's answer on the consent page. "Allow"
// creates the connection and its one-time code in one transaction. Signing in
// again to the same client always makes a new connection; old ones stay until
// the photographer disconnects them.
func (s *Service) Decide(ctx context.Context, accountID, request string, allow bool) (string, error) {
	r, err := s.parseRequest(request)
	if err != nil {
		return "", err
	}
	if !allow {
		s.logger.InfoContext(ctx, "oauth authorize denied", "client_id", r.ClientID)
		return s.callback(r.RedirectURI, url.Values{"error": {"access_denied"}, "state": {r.State}}), nil
	}
	active, err := s.store.AccountActive(ctx, accountID)
	if err != nil {
		return "", err
	}
	if !active {
		return "", ErrAccountInactive
	}
	code, err := s.randomToken()
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	connection := Connection{ID: "cxcon_" + uuid.NewString(), AccountID: accountID, ClientID: r.ClientID,
		ClientName: r.ClientName, HostKind: r.HostKind, DisplayName: r.DisplayName, CreatedAt: now}
	err = s.store.CreateGrant(ctx, connection, digest(code), AuthorizationCode{
		ClientID: r.ClientID, AccountID: accountID, ConnectionID: connection.ID, RedirectURI: r.RedirectURI,
		CodeChallenge: r.CodeChallenge, Resource: s.resource, ExpiresAt: now.Add(codeTTL),
	})
	if err != nil {
		return "", err
	}
	s.logger.InfoContext(ctx, "oauth connection granted", "connection_id", connection.ID, "client_id", r.ClientID, "host_kind", r.HostKind)
	return s.callback(r.RedirectURI, url.Values{"code": {code}, "state": {r.State}}), nil
}
