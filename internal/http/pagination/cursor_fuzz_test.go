package pagination

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzDecode(f *testing.F) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		f.Fatal(err)
	}
	valid, err := EncodePosition(codec, TaskCursorContext(7), DirectionNext, TaskCursorPosition{ID: 1})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{
		"", ".", "..", "a.b", "a.b.c", valid, valid + "x", valid[:len(valid)/2],
		strings.Repeat("A", MaxCursorLength+1), "%00.%00", "e30.e30", "bnVsbA.bnVsbA",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, token string) {
		cursor, err := codec.Decode(token)
		if err != nil {
			return
		}
		if token == "" {
			if cursor != nil {
				t.Fatal("empty token must decode to nil")
			}
			return
		}
		if err := cursor.Validate(); err != nil {
			t.Fatalf("Decode accepted an invalid cursor %+v: %v", cursor, err)
		}
	})
}

func FuzzDecodeSignedPayload(f *testing.F) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{
		``, `{}`, `null`, `[]`, `"x"`, `{"v":1}`,
		`{"v":1,"c":{"r":"tasks","o":"created_at_desc_id_desc","p":{"id":1},"d":"next"}}`,
		`{"v":1,"c":{"r":"tasks","o":"o","p":null,"d":"next"}}`,
		`{"v":1,"c":{"r":"","o":"","p":{},"d":"sideways"}}`,
		`{"v":1,"c":{"r":"tasks","s":"7","o":"created_at_desc_id_desc","p":{"created_at":"not-a-time","id":-1},"d":"prev"}}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		body := base64.RawURLEncoding.EncodeToString(payload)
		token := body + "." + base64.RawURLEncoding.EncodeToString(codec.sign(body))

		cursor, err := codec.Decode(token)
		if err != nil {
			return
		}
		if err := cursor.Validate(); err != nil {
			t.Fatalf("accepted invalid cursor %+v: %v", cursor, err)
		}
		_, _ = DecodePosition[TaskCursorPosition](cursor, TaskCursorContext(7))
	})
}

func FuzzDecodePosition(f *testing.F) {
	for _, seed := range []string{
		`{"created_at":"2026-01-02T03:04:05Z","id":42}`, `{}`, `null`, `[]`, `{"id":"1"}`,
		`{"id":18446744073709551615}`, `{"id":-1}`, `{"created_at":"0000-00-00T00:00:00Z"}`, `1e999`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, position []byte) {
		cursor := &Cursor{
			Resource: taskResource, Scope: "7", Sort: taskSort,
			Position: json.RawMessage(position), Direction: DirectionNext,
		}
		_, _ = DecodePosition[TaskCursorPosition](cursor, TaskCursorContext(7))
	})
}

func FuzzCursorRoundTrip(f *testing.F) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add("tasks", "7", "created_at_desc_id_desc", []byte(`{"id":1}`), true)
	f.Add("r", "", "o", []byte(`"x"`), false)
	f.Add("ключ", "scope with spaces", "sort\n", []byte(`[1,2,3]`), true)

	f.Fuzz(func(t *testing.T, resource, scope, sort string, position []byte, next bool) {
		if !utf8.ValidString(resource) || !utf8.ValidString(scope) || !utf8.ValidString(sort) {
			t.Skip("invalid UTF-8 does not round-trip through JSON")
		}
		direction := DirectionPrev
		if next {
			direction = DirectionNext
		}
		in := Cursor{Resource: resource, Scope: scope, Sort: sort, Position: json.RawMessage(position), Direction: direction}
		token, err := codec.Encode(in)
		if err != nil {
			return
		}
		out, err := codec.Decode(token)
		if err != nil {
			t.Fatalf("encoded cursor %+v failed to decode: %v", in, err)
		}
		if out.Resource != in.Resource || out.Scope != in.Scope || out.Sort != in.Sort || out.Direction != in.Direction {
			t.Fatalf("round trip changed cursor: in=%+v out=%+v", in, out)
		}
		var a, b bytes.Buffer
		if json.Compact(&a, in.Position) == nil && json.Compact(&b, out.Position) == nil && !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Fatalf("position changed: %s -> %s", a.String(), b.String())
		}
	})
}
