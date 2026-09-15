package creativemedia

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// ticketSigner issues short-lived, opaque, stateless capabilities bound to
// account, revision, role and purpose. Tickets never carry storage keys and
// every GET still re-checks the current usage grant.
type ticketSigner struct {
	key  []byte
	base string
}
type ticketClaims struct {
	Account   string `json:"a"`
	Revision  string `json:"r"`
	Role      string `json:"o"`
	Purpose   string `json:"p"`
	FileName  string `json:"n,omitempty"`
	ExpiresAt int64  `json:"e"`
}

func (t ticketSigner) sign(c ticketClaims) string {
	raw, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, t.key)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (t ticketSigner) verify(token string, now time.Time) (ticketClaims, error) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || len(token) > 2048 {
		return ticketClaims{}, ErrTicket
	}
	mac := hmac.New(sha256.New, t.key)
	_, _ = mac.Write([]byte(payload))
	want, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return ticketClaims{}, ErrTicket
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ticketClaims{}, ErrTicket
	}
	var c ticketClaims
	if err := json.Unmarshal(raw, &c); err != nil || c.Account == "" || c.Revision == "" || now.Unix() >= c.ExpiresAt {
		return ticketClaims{}, ErrTicket
	}
	return c, nil
}

// PartSigner signs local part uploads the same way; the URL is only usable
// by the API's part endpoint and only for one key/session/part.
func (s *Service) PartSigner(base string) func(key, session string, part int, expires time.Time) versionedfs.PartAuthorization {
	return func(key, session string, part int, expires time.Time) versionedfs.PartAuthorization {
		token := s.tickets.sign(ticketClaims{Account: "part", Revision: key, Role: session, Purpose: strconv.Itoa(part), ExpiresAt: expires.Unix()})
		return versionedfs.PartAuthorization{Number: part, Method: "PUT", URL: base + "?token=" + url.QueryEscape(token), Headers: map[string]string{"Content-Type": "application/octet-stream"}, ExpiresAt: expires}
	}
}

// WriteLocalPart validates a signed local part upload and forwards the bytes.
func (s *Service) WriteLocalPart(ctx context.Context, token string, body io.Reader) error {
	c, err := s.tickets.verify(token, time.Now())
	if err != nil || c.Account != "part" {
		return ErrTicket
	}
	writer, ok := s.adapter.(versionedfs.LocalPartWriter)
	if !ok {
		return ErrTicket
	}
	part, err := strconv.Atoi(c.Purpose)
	if err != nil {
		return ErrTicket
	}
	_, err = writer.WritePart(ctx, c.Revision, c.Role, part, body, s.cfg.PartSize)
	return err
}

// IssueTicket authorizes browser media tags for one revision/role/purpose.
func (s *Service) IssueTicket(ctx context.Context, scope store.AccountScope, in TicketInput) (Ticket, error) {
	if in.ContentRevisionID == "" || in.Role != "original" && in.Role != "display" || in.Purpose != "display" && in.Purpose != "download" || len(in.FileName) > 255 {
		return Ticket{}, creativeops.ErrValidation
	}
	var media creativecontent.MediaObject
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		r, err := creativecontent.RequireUsable(ctx, tx, in.ContentRevisionID, "display")
		if err != nil {
			return err
		}
		for _, m := range r.Media {
			if m.Role == in.Role {
				media = m
				return nil
			}
		}
		return creativecontent.ErrNotFound
	})
	if errors.Is(err, creativecontent.ErrMissingRoot) {
		err = creativecontent.ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	expires := time.Now().Add(s.cfg.TicketTTL)
	token := s.tickets.sign(ticketClaims{Account: scope.AccountID(), Revision: in.ContentRevisionID, Role: in.Role, Purpose: in.Purpose, FileName: in.FileName, ExpiresAt: expires.Unix()})
	return Ticket{URL: s.tickets.base + "/" + url.PathEscape(in.ContentRevisionID) + "/" + in.Role + "?ticket=" + url.QueryEscape(token), ExpiresAt: expires, Mime: media.Mime, ByteSize: media.ByteSize}, nil
}

// Stream is an authorized, version-fixed reader plus the headers a handler
// needs. Close releases the storage reader; the read pin expires on its own.
type Stream struct {
	Body        io.ReadCloser
	Mime        string
	Size        int64
	ETag        string
	Range       *versionedfs.ByteRange
	FileName    string
	Download    bool
	PinID       string
	Unavailable bool
}

// Open validates the ticket, re-checks usage inside a transaction, records a
// read pin and opens the exact blob version. The account comes from the ticket
// (it was issued to an authenticated session), never from the request. The
// range is chosen after the size is known so authorization happens once and
// the storage object is opened once.
func (s *Service) Open(ctx context.Context, scopeFor func(account string) store.AccountScope, token, revisionID, role string, chooseRange func(size int64) (*versionedfs.ByteRange, error)) (*Stream, error) {
	c, err := s.tickets.verify(token, time.Now())
	if err != nil || c.Account == "part" || c.Revision != revisionID || c.Role != role {
		return nil, ErrTicket
	}
	scope := scopeFor(c.Account)
	var media creativecontent.MediaObject
	var key, version string
	pin := "ccpn_" + uuid.NewString()
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		r, err := creativecontent.RequireUsable(ctx, tx, revisionID, "display")
		if err != nil {
			return err
		}
		found := false
		for _, m := range r.Media {
			if m.Role == role {
				media, found = m, true
			}
		}
		if !found {
			return creativecontent.ErrNotFound
		}
		var state string
		var objectVersion *string
		if err := tx.QueryRowForUpdate(ctx, "creative_blobs", "state,object_key,object_version", "id=$2", media.BlobID).Scan(&state, &key, &objectVersion); err != nil {
			return err
		}
		if state != "ready" || objectVersion == nil {
			return creativecontent.ErrNotFound
		}
		version = *objectVersion
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		owner := "display"
		if c.Purpose == "download" {
			owner = "download"
		}
		return tx.Insert(ctx, "creative_blob_read_pins", []string{"id", "blob_id", "owner_kind", "expires_at"}, pin, media.BlobID, owner, now.Add(s.cfg.ReadPinTTL))
	})
	if errors.Is(err, creativecontent.ErrMissingRoot) || errors.Is(err, store.ErrNoRows) {
		err = creativecontent.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var rng *versionedfs.ByteRange
	if chooseRange != nil {
		rng, err = chooseRange(media.ByteSize)
		if err != nil {
			return &Stream{Mime: media.Mime, Size: media.ByteSize, PinID: pin}, err
		}
	}
	if rng != nil && (rng.Start < 0 || rng.End < rng.Start || rng.End >= media.ByteSize) {
		return &Stream{Mime: media.Mime, Size: media.ByteSize, PinID: pin}, ErrRange
	}
	body, stat, err := s.adapter.OpenVersion(ctx, key, version, rng)
	if err != nil {
		return nil, err
	}
	if rng == nil && stat.Size != media.ByteSize {
		_ = body.Close()
		return nil, creativecontent.ErrNotFound
	}
	name := c.FileName
	if name == "" {
		name = revisionID
	}
	return &Stream{Body: body, Mime: media.Mime, Size: media.ByteSize, ETag: media.BlobID + ":" + version, Range: rng, FileName: name, Download: c.Purpose == "download", PinID: pin}, nil
}

// RenewPin extends an active read; failures let the pin lapse naturally.
func (s *Service) RenewPin(ctx context.Context, scope store.AccountScope, pin string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		n, err := tx.Update(ctx, "creative_blob_read_pins", "expires_at=$2", "id=$3 AND expires_at>clock_timestamp()", now.Add(s.cfg.ReadPinTTL), pin)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrTicket
		}
		return nil
	})
}
