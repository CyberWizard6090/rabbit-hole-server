package pagination

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func testCodec(t *testing.T) *Codec {
	t.Helper()
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func TestTaskCursorContext(t *testing.T) {
	if got, want := TaskCursorContext(42), (CursorContext{
		Resource: "tasks",
		Scope:    "42",
		Sort:     "created_at_desc_id_desc",
	}); got != want {
		t.Fatalf("TaskCursorContext(42) = %+v, want %+v", got, want)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	codec := testCodec(t)
	context := TaskCursorContext(7)
	position := TaskCursorPosition{
		CreatedAt: time.Date(2026, 10, 9, 12, 30, 0, 123456000, time.FixedZone("test", 2*60*60)),
		ID:        42,
	}
	token, err := EncodePosition(codec, context, DirectionNext, position)
	if err != nil {
		t.Fatal(err)
	}
	got, err := codec.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resource != context.Resource || got.Scope != context.Scope || got.Sort != context.Sort || got.Direction != DirectionNext {
		t.Fatalf("cursor context mismatch: %#v", got)
	}
	gotPosition, err := DecodePosition[TaskCursorPosition](got, context)
	if err != nil {
		t.Fatal(err)
	}
	if gotPosition.ID != position.ID || !gotPosition.CreatedAt.Equal(position.CreatedAt) {
		t.Fatalf("position round trip mismatch: got=%#v want=%#v", gotPosition, position)
	}
}

func TestCursorSupportsDifferentResourcePositionShapes(t *testing.T) {
	type namePosition struct {
		Name string `json:"name"`
		ID   uint   `json:"id"`
	}
	codec := testCodec(t)
	ctx := CursorContext{Resource: "tags", Scope: "space-55", Sort: "name_asc_id_asc"}
	want := namePosition{Name: "Bug", ID: 12}
	token, err := EncodePosition(codec, ctx, DirectionNext, want)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Decode(token)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePosition[namePosition](cursor, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

func TestDecodeRejectsTampering(t *testing.T) {
	codec := testCodec(t)
	token, err := EncodePosition(codec, TaskCursorContext(7), DirectionNext, TaskCursorPosition{CreatedAt: time.Now(), ID: 42})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	mutated := parts[0] + "A." + parts[1]
	if _, err := codec.Decode(mutated); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestDecodeEmptyCursor(t *testing.T) {
	got, err := testCodec(t).Decode("")
	if err != nil || got != nil {
		t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestCursorValidation(t *testing.T) {
	codec := testCodec(t)
	for name, cursor := range map[string]Cursor{
		"missing resource":  {Sort: "created_at_desc_id_desc", Position: []byte(`{"id":"1"}`), Direction: DirectionNext},
		"missing sort":      {Resource: "tasks", Position: []byte(`{"id":"1"}`), Direction: DirectionNext},
		"missing position":  {Resource: "tasks", Sort: "created_at_desc_id_desc", Direction: DirectionNext},
		"invalid direction": {Resource: "tasks", Sort: "created_at_desc_id_desc", Position: []byte(`{"id":"1"}`), Direction: "sideways"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Encode(cursor); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("expected invalid cursor, got %v", err)
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	if got, err := ParseLimit(""); err != nil || got != DefaultLimit {
		t.Fatalf("default: got %d, err %v", got, err)
	}
	for _, input := range []string{"abc", "0", "-1", "101", "1.5"} {
		if _, err := ParseLimit(input); !errors.Is(err, ErrInvalidLimit) {
			t.Errorf("ParseLimit(%q): got %v", input, err)
		}
	}
	if got, err := ParseLimit("100"); err != nil || got != 100 {
		t.Fatalf("max: got %d, err %v", got, err)
	}
}

func TestParseParams(t *testing.T) {
	codec := testCodec(t)
	token, err := EncodePosition(codec, TaskCursorContext(7), DirectionNext, TaskCursorPosition{CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), ID: 42})
	if err != nil {
		t.Fatal(err)
	}
	params, err := ParseParams("25", token, codec, TaskCursorContext(7))
	if err != nil || params.Limit != 25 || params.Cursor == nil {
		t.Fatalf("ParseParams() = (%+v, %v)", params, err)
	}
	if _, err := ParseParams("invalid", token, codec, TaskCursorContext(7)); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("expected invalid limit, got %v", err)
	}
	if _, err := ParseParams("25", "invalid", codec, TaskCursorContext(7)); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestParseParamsRejectsCursorFromDifferentContext(t *testing.T) {
	codec := testCodec(t)
	token, err := EncodePosition(codec, TaskCursorContext(7), DirectionNext, TaskCursorPosition{CreatedAt: time.Now(), ID: 42})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []CursorContext{
		TaskCursorContext(8),
		{Resource: "spaces", Scope: "7", Sort: "created_at_desc_id_desc"},
		{Resource: "tasks", Scope: "7", Sort: "title_asc"},
	} {
		if _, err := ParseParams("10", token, codec, expected); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("expected context mismatch for %+v, got %v", expected, err)
		}
	}
}

func TestCodecRequiresStrongSecret(t *testing.T) {
	if _, err := NewCodec([]byte("short")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
}

func TestTrimExtra(t *testing.T) {
	got, extra, err := TrimExtra([]int{1, 2, 3}, 2)
	if err != nil || !extra || len(got) != 2 {
		t.Fatalf("got %v, extra %v, err %v", got, extra, err)
	}
}

func TestDecodeRejectsOversizedCursor(t *testing.T) {
	if _, err := testCodec(t).Decode(strings.Repeat("a", MaxCursorLength+1)); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestEncodeRejectsOversizedPosition(t *testing.T) {
	_, err := EncodePosition(testCodec(t), CursorContext{Resource: "test", Sort: "value_asc"}, DirectionNext, struct {
		Value string `json:"value"`
	}{Value: strings.Repeat("x", MaxCursorLength*2)})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected oversized cursor error, got %v", err)
	}
}
