package service

import (
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func sliceRunes(text string, m domain.SearchMatch) string {
	r := []rune(text)
	return string(r[m.Start:m.End])
}

func TestPlainTextStripsMarkup(t *testing.T) {
	cases := map[string]string{
		"<p>Hello <b>wor</b>ld</p><p>next</p>":      "Hello world next",
		"a &lt;b&gt; &amp; c":                       "a <b> & c",
		"1 < 2 and 3 > 2":                           "1 < 2 and 3 > 2",
		"<img src=\"x\" onerror=\"alert(1)\">after": "after",
		"line\n\n  break":                           "line break",
	}
	for in, want := range cases {
		if got := plainText(in); got != want {
			t.Errorf("plainText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := plainText("<p>x</p>"); strings.ContainsAny(got, "<>") {
		t.Errorf("markup left in %q", got)
	}
}

func TestBuildSnippetRuneOffsetsForCyrillic(t *testing.T) {
	snippet, ok := buildSnippet("Привет, Мир! Это ПРИВЕТ из Москвы", "привет")
	if !ok || len(snippet.Matches) != 2 {
		t.Fatalf("snippet = %#v ok=%v", snippet, ok)
	}
	for _, m := range snippet.Matches {
		if got := strings.ToLower(sliceRunes(snippet.Text, m)); got != "привет" {
			t.Fatalf("match %v selects %q", m, got)
		}
	}
	if snippet.Matches[0].Start != 0 || snippet.Matches[0].End != 6 {
		t.Fatalf("first match = %v", snippet.Matches[0])
	}
}

func TestBuildSnippetWindowAndEllipsisShiftOffsets(t *testing.T) {
	text := strings.Repeat("слово ", 40) + "искомое" + strings.Repeat(" хвост", 40)
	snippet, ok := buildSnippet(text, "ИСКОМОЕ")
	if !ok || len(snippet.Matches) != 1 {
		t.Fatalf("snippet = %#v ok=%v", snippet, ok)
	}
	runes := []rune(snippet.Text)
	if runes[0] != '…' || runes[len(runes)-1] != '…' {
		t.Fatalf("missing ellipsis: %q", snippet.Text)
	}
	if len(runes) > snippetMaxRunes+2 {
		t.Fatalf("snippet has %d runes", len(runes))
	}
	if got := sliceRunes(snippet.Text, snippet.Matches[0]); got != "искомое" {
		t.Fatalf("match selects %q", got)
	}
}

func TestBuildSnippetFuzzyMatchHighlightsTheTypoWord(t *testing.T) {
	snippet, ok := buildSnippet("Please fix the deployment script", "deploymnt")
	if !ok || len(snippet.Matches) != 1 {
		t.Fatalf("snippet = %#v ok=%v", snippet, ok)
	}
	if got := sliceRunes(snippet.Text, snippet.Matches[0]); got != "deployment" {
		t.Fatalf("match selects %q", got)
	}
}

func TestBuildSnippetWithoutMatchStartsAtBeginning(t *testing.T) {
	snippet, ok := buildSnippet("<p>nothing here</p>", "zzzz")
	if ok || snippet.Text != "nothing here" || len(snippet.Matches) != 0 || snippet.Matches == nil {
		t.Fatalf("snippet = %#v ok=%v", snippet, ok)
	}
}

func TestBuildSearchResultPicksDescriptionWhenTitleDoesNotMatch(t *testing.T) {
	description := "<p>needs the <b>quarterly</b> report</p>"
	result := buildSearchResult(domain.SearchHit{
		Type: domain.SearchTypeTask, ID: 1, Text: "Plan", Description: &description,
	}, "quarterly")
	if result.Field != "description" || strings.Contains(result.Snippet.Text, "<") {
		t.Fatalf("result = %#v", result)
	}
	if got := sliceRunes(result.Snippet.Text, result.Snippet.Matches[0]); got != "quarterly" {
		t.Fatalf("match selects %q", got)
	}
	title := buildSearchResult(domain.SearchHit{Type: domain.SearchTypeTask, Text: "Quarterly plan", Description: &description}, "quarterly")
	if title.Field != "title" {
		t.Fatalf("field = %s", title.Field)
	}
	if c := buildSearchResult(domain.SearchHit{Type: domain.SearchTypeComment, Text: "x quarterly"}, "quarterly"); c.Field != "body" {
		t.Fatalf("comment field = %s", c.Field)
	}
	if c := buildSearchResult(domain.SearchHit{Type: domain.SearchTypeChecklistItem, Text: "x quarterly"}, "quarterly"); c.Field != "text" {
		t.Fatalf("item field = %s", c.Field)
	}
}
