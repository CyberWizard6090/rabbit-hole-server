package pagination

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	cursorVersion   = 2
	MaxCursorLength = 2048
	minSecretLength = 32
)

type Direction string

const (
	DirectionNext Direction = "next"
	DirectionPrev Direction = "prev"
)

// CursorContext identifies the resource and the collection/query scope to which a
// cursor belongs. Scope is commonly a parent ID (for example, a list ID) or a
// stable filter fingerprint. Both values are signed as part of the cursor.
type CursorContext struct {
	Resource string
	Scope    string
	Sort     string
}

// Cursor carries opaque, resource-specific keyset position data. Position is
// intentionally JSON so different resources can use different key fields.
type Cursor struct {
	Resource  string          `json:"r"`
	Scope     string          `json:"s,omitempty"`
	Sort      string          `json:"o"`
	Position  json.RawMessage `json:"p"`
	Direction Direction       `json:"d"`
}

func (c Cursor) Validate() error {
	if strings.TrimSpace(c.Resource) == "" {
		return fmt.Errorf("%w: resource is required", ErrInvalidCursor)
	}
	if strings.TrimSpace(c.Sort) == "" {
		return fmt.Errorf("%w: sort definition is required", ErrInvalidCursor)
	}
	position := strings.TrimSpace(string(c.Position))
	if position == "" || position == "null" || !json.Valid(c.Position) {
		return fmt.Errorf("%w: valid position is required", ErrInvalidCursor)
	}
	if c.Direction != DirectionNext && c.Direction != DirectionPrev {
		return fmt.Errorf("%w: unsupported direction", ErrInvalidCursor)
	}
	return nil
}

// ValidateContext rejects a validly signed cursor when it is used for a
// different resource or collection scope. The context must come from the
// current request, never from the cursor itself.
func (c Cursor) ValidateContext(expected CursorContext) error {
	if strings.TrimSpace(expected.Resource) == "" || strings.TrimSpace(expected.Sort) == "" {
		return fmt.Errorf("%w: expected resource and sort are required", ErrInvalidConfig)
	}
	if c.Resource != expected.Resource || c.Scope != expected.Scope || c.Sort != expected.Sort {
		return fmt.Errorf("%w: cursor context mismatch", ErrInvalidCursor)
	}
	return nil
}

type cursorEnvelope struct {
	Version int    `json:"v"`
	Payload Cursor `json:"p"`
}

// EncodePosition serializes an entity-specific keyset position into a signed
// cursor. Position types may differ between resources and sort strategies.
func EncodePosition[T any](codec *Codec, context CursorContext, direction Direction, position T) (string, error) {
	if strings.TrimSpace(context.Resource) == "" || strings.TrimSpace(context.Sort) == "" {
		return "", ErrInvalidConfig
	}
	rawPosition, err := json.Marshal(position)
	if err != nil {
		return "", fmt.Errorf("pagination: marshal cursor position: %w", err)
	}
	return codec.Encode(Cursor{
		Resource:  context.Resource,
		Scope:     context.Scope,
		Sort:      context.Sort,
		Position:  rawPosition,
		Direction: direction,
	})
}

// DecodePosition validates cursor context and decodes its resource-specific
// position into the type expected by the caller.
func DecodePosition[T any](cursor *Cursor, expected CursorContext) (T, error) {
	var position T
	if cursor == nil {
		return position, fmt.Errorf("%w: cursor is required", ErrInvalidCursor)
	}
	if err := cursor.Validate(); err != nil {
		return position, err
	}
	if err := cursor.ValidateContext(expected); err != nil {
		return position, err
	}
	if err := json.Unmarshal(cursor.Position, &position); err != nil {
		return position, fmt.Errorf("%w: invalid position for %s", ErrInvalidCursor, expected.Resource)
	}
	return position, nil
}

type Codec struct {
	secret []byte
}

func NewCodec(secret []byte) (*Codec, error) {
	if len(secret) < minSecretLength {
		return nil, fmt.Errorf("%w: cursor signing secret must be at least %d bytes", ErrInvalidConfig, minSecretLength)
	}
	key := append([]byte(nil), secret...)
	return &Codec{secret: key}, nil
}

func (c *Codec) Encode(cursor Cursor) (string, error) {
	if c == nil || len(c.secret) < minSecretLength {
		return "", ErrInvalidConfig
	}
	if err := cursor.Validate(); err != nil {
		return "", err
	}

	payload, err := json.Marshal(cursorEnvelope{Version: cursorVersion, Payload: cursor})
	if err != nil {
		return "", fmt.Errorf("pagination: marshal cursor: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := c.sign(body)
	token := body + "." + base64.RawURLEncoding.EncodeToString(sig)
	if len(token) > MaxCursorLength {
		return "", fmt.Errorf("%w: token too long", ErrInvalidCursor)
	}
	return token, nil
}

func (c *Codec) Decode(token string) (*Cursor, error) {
	if token == "" {
		return nil, nil
	}
	if c == nil || len(c.secret) < minSecretLength {
		return nil, ErrInvalidConfig
	}
	if len(token) > MaxCursorLength {
		return nil, fmt.Errorf("%w: token too long", ErrInvalidCursor)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("%w: malformed token", ErrInvalidCursor)
	}
	providedSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed signature", ErrInvalidCursor)
	}
	expectedSig := c.sign(parts[0])
	if !hmac.Equal(providedSig, expectedSig) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrInvalidCursor)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: malformed payload encoding", ErrInvalidCursor)
	}
	var envelope cursorEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("%w: malformed payload", ErrInvalidCursor)
	}
	if envelope.Version != cursorVersion {
		return nil, fmt.Errorf("%w: unsupported version", ErrInvalidCursor)
	}
	if err := envelope.Payload.Validate(); err != nil {
		return nil, err
	}
	return &envelope.Payload, nil
}

func (c *Codec) sign(body string) []byte {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(body))
	return mac.Sum(nil)
}
