// Package creativeops owns the new creative namespace's operation identities and
// durable command receipts. Domain commands supply typed validation and effects.
package creativeops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	ErrValidation = errors.New("invalid creative operation")
	ErrConflict   = errors.New("creative operation identity conflict")
	ErrExpired    = errors.New("creative operation recovery window expired")
	ErrNotFound   = errors.New("creative operation not found")
)

// Revision is an optimistic concurrency token, never a floating point JSON number.
type Revision int64

func (r Revision) MarshalJSON() ([]byte, error) {
	if r < 1 {
		return nil, ErrValidation
	}
	return json.Marshal(strconv.FormatInt(int64(r), 10))
}
func (r *Revision) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil || s == "" || s[0] == '0' {
		return ErrValidation
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != s {
		return ErrValidation
	}
	*r = Revision(n)
	return nil
}

func ValidOperationID(id string) bool {
	v, err := uuid.Parse(id)
	return err == nil && v != uuid.Nil && v.String() == id
}

// ValidateOperationKey checks the HTTP envelope without inventing a new identity.
func ValidateOperationKey(header, operationID string) error {
	if !ValidOperationID(operationID) || header != operationID {
		return ErrValidation
	}
	return nil
}

func NewResourceID(prefix string) (string, error) {
	switch prefix {
	case "ccnt", "ccrv", "ccrd", "ccug", "ccup", "ccuc", "ccbl", "ccpn", "cchd", "ccas", "ccag", "cctg", "cctc", "ccpj", "cccv", "cwnode", "cwedge", "cwinp", "ccch":
		return prefix + "_" + uuid.NewString(), nil
	default:
		return "", ErrValidation
	}
}

// Decode rejects extra fields and trailing JSON values at typed command boundaries.
func Decode(data []byte, target any) error {
	if len(data) == 0 || len(data) > 1<<20 || !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return ErrValidation
	}
	if err := validateJSONKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrValidation
	}
	return nil
}

// JSON objects have unique, case-unambiguous keys throughout the tree. Go's
// struct decoder folds field names and ignores null for scalar destinations;
// accepting duplicates could otherwise give hashing and execution different meanings.
func validateJSONKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := validateJSONValue(dec, 0); err != nil {
		return fmt.Errorf("%w: ambiguous or malformed JSON", ErrValidation)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrValidation
	}
	return nil
}
func validateJSONValue(dec *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrValidation
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]bool)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrValidation
			}
			folded := strings.Map(func(r rune) rune {
				min := r
				for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
					if next < min {
						min = next
					}
				}
				return min
			}, key)
			if keys[folded] {
				return ErrValidation
			}
			keys[folded] = true
			if err := validateJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := validateJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrValidation
	}
	_, err = dec.Token()
	return err
}

type Command struct {
	OperationID string
	CreatedAt   time.Time
	Payload     json.RawMessage
}

type Outcome struct {
	HTTPStatus     int
	Response       json.RawMessage
	ResultKind     string
	ResultID       *string
	ResultRevision *Revision
}

type Receipt struct {
	OperationID   string
	OperationType string
	CreatedAt     time.Time
	RetainedUntil time.Time
	Outcome       Outcome
}
