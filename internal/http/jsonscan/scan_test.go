package jsonscan

import (
	"math"
	"reflect"
	"testing"
)

func TestScan(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Issue
	}{
		{"clean", `{"name":"ok","n":[1,2,{"a":"b"}],"f":1.5e300,"t":true,"z":null}`, nil},
		{"escaped nul", `{"name":"a\u0000b"}`, []Issue{{Field: "name", Code: CodeInvalidValue}}},
		{"escaped backslash then text is not nul", `{"name":"a\\u0000b"}`, nil},
		{"nul after escaped quote", `{"name":"a\"\u0000"}`, []Issue{{Field: "name", Code: CodeInvalidValue}}},
		{"nested path", `{"items":[{"text":"ok"},{"text":"x\u0000"}]}`, []Issue{{Field: "items[1].text", Code: CodeInvalidValue}}},
		{"array of strings", `{"tags":["a","b\u0000"]}`, []Issue{{Field: "tags[1]", Code: CodeInvalidValue}}},
		{"top level array", `["a\u0000"]`, []Issue{{Field: "[0]", Code: CodeInvalidValue}}},
		{"top level string", `"a\u0000"`, []Issue{{Field: "request", Code: CodeInvalidValue}}},
		{"key with nul", `{"a\u0000b":"x"}`, []Issue{{Field: "a\x00b", Code: CodeInvalidValue}}},
		{"unknown field value", `{"known":"ok","other":{"deep":"\u0000"}}`, []Issue{{Field: "other.deep", Code: CodeInvalidValue}}},
		{"valid surrogate pair", `{"e":"😀"}`, nil},
		{"lone high surrogate", `{"e":"x\ud800y"}`, []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"lone low surrogate", `{"e":"x\udc00y"}`, []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"high followed by non surrogate", `{"e":"\ud800A"}`, []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"high at end", `{"e":"\ud800"}`, []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"invalid utf8", "{\"e\":\"a\xffb\"}", []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"truncated utf8", "{\"e\":\"\xe2\x82\"}", []Issue{{Field: "e", Code: CodeInvalidEncoding}}},
		{"valid multibyte", "{\"e\":\"héllo €\"}", nil},
		{"big positive integer", `{"id":18446744073709551615}`, []Issue{{Field: "id", Code: CodeTooLarge, Params: map[string]any{"max": int64(math.MaxInt64)}}}},
		{"just above int64", `{"id":9223372036854775808}`, []Issue{{Field: "id", Code: CodeTooLarge, Params: map[string]any{"max": int64(math.MaxInt64)}}}},
		{"max int64 ok", `{"id":9223372036854775807}`, nil},
		{"min int64 ok", `{"id":-9223372036854775808}`, nil},
		{"big negative integer", `{"id":-9223372036854775809}`, []Issue{{Field: "id", Code: CodeTooSmall, Params: map[string]any{"min": int64(math.MinInt64)}}}},
		{"big float is not an integer", `{"id":1e30,"g":123456789012345678901234567890.5}`, nil},
		{"big integer in array", `{"ids":[1,99999999999999999999]}`, []Issue{{Field: "ids[1]", Code: CodeTooLarge, Params: map[string]any{"max": int64(math.MaxInt64)}}}},
		{"number text inside string is ignored", `{"s":"99999999999999999999"}`, nil},
		{"truncated body does not panic", `{"a":"b\u00`, nil},
		{"truncated escape", `{"a":"b\`, nil},
		{"unterminated string", `{"a":"b`, nil},
		{"garbage", `}}]]::,,"`, nil},
		{"empty", ``, nil},
		{"multiple issues", `{"a":"\u0000","b":"\ud800","c":99999999999999999999}`, []Issue{
			{Field: "a", Code: CodeInvalidValue},
			{Field: "b", Code: CodeInvalidEncoding},
			{Field: "c", Code: CodeTooLarge, Params: map[string]any{"max": int64(math.MaxInt64)}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Scan([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Scan(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestScanCapsNumberOfIssues(t *testing.T) {
	in := `["\u0000"`
	for i := 0; i < 100; i++ {
		in += `,"\u0000"`
	}
	in += `]`
	if got := Scan([]byte(in)); len(got) != maxIssues {
		t.Fatalf("issues = %d, want %d", len(got), maxIssues)
	}
}

func FuzzScan(f *testing.F) {
	for _, seed := range []string{`{"a":"\u0000"}`, `["\ud800"]`, `{"n":1e5}`, `"`, `{"a":[`, "\xff"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = Scan(data)
	})
}
