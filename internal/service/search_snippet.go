package service

import (
	"html"
	"strings"
	"unicode"

	"rabbit-hole-server/internal/domain"
)

const (
	snippetMaxRunes     = 160
	snippetLeadRunes    = 40
	snippetMaxMatches   = 20
	snippetEllipsis     = '…'
	fuzzyWordSimilarity = 0.5
)

var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "ul": true, "ol": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true, "hr": true,
	"table": true, "img": true,
}

func tagName(tag []rune) string {
	i := 0
	if i < len(tag) && tag[i] == '/' {
		i++
	}
	start := i
	for i < len(tag) && (unicode.IsLetter(tag[i]) || unicode.IsDigit(tag[i])) {
		i++
	}
	return strings.ToLower(string(tag[start:i]))
}

func plainText(raw string) string {
	runes := []rune(raw)
	var out strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '<' && i+1 < len(runes) && (unicode.IsLetter(runes[i+1]) || runes[i+1] == '/' || runes[i+1] == '!') {
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '>' {
					end = j
					break
				}
			}
			if end > 0 {
				if blockTags[tagName(runes[i+1:end])] {
					out.WriteRune(' ')
				}
				i = end
				continue
			}
		}
		out.WriteRune(r)
	}
	return strings.Join(strings.Fields(html.UnescapeString(out.String())), " ")
}

func foldRune(r rune) rune {
	return unicode.ToLower(r)
}

func literalMatches(text, query []rune) []domain.SearchMatch {
	var matches []domain.SearchMatch
	if len(query) == 0 {
		return matches
	}
	for i := 0; i+len(query) <= len(text) && len(matches) < snippetMaxMatches; {
		ok := true
		for j, q := range query {
			if foldRune(text[i+j]) != foldRune(q) {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, domain.SearchMatch{Start: i, End: i + len(query)})
			i += len(query)
			continue
		}
		i++
	}
	return matches
}

func trigramSet(word []rune) map[string]struct{} {
	padded := make([]rune, 0, len(word)+3)
	padded = append(padded, ' ', ' ')
	for _, r := range word {
		padded = append(padded, foldRune(r))
	}
	padded = append(padded, ' ')
	set := make(map[string]struct{}, len(padded))
	for i := 0; i+3 <= len(padded); i++ {
		set[string(padded[i:i+3])] = struct{}{}
	}
	return set
}

func trigramSimilarity(a, b []rune) float64 {
	sa, sb := trigramSet(a), trigramSet(b)
	common := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			common++
		}
	}
	union := len(sa) + len(sb) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func fuzzyMatches(text, query []rune) []domain.SearchMatch {
	var matches []domain.SearchMatch
	queryWords := strings.FieldsFunc(string(query), func(r rune) bool { return !isWordRune(r) })
	for i := 0; i < len(text) && len(matches) < snippetMaxMatches; {
		if !isWordRune(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && isWordRune(text[j]) {
			j++
		}
		for _, qw := range queryWords {
			if trigramSimilarity(text[i:j], []rune(qw)) >= fuzzyWordSimilarity {
				matches = append(matches, domain.SearchMatch{Start: i, End: j})
				break
			}
		}
		i = j
	}
	return matches
}

func findMatches(text []rune, query string) []domain.SearchMatch {
	q := []rune(strings.TrimSpace(query))
	if matches := literalMatches(text, q); len(matches) > 0 {
		return matches
	}
	return fuzzyMatches(text, q)
}

func buildSnippet(raw, query string) (domain.SearchSnippet, bool) {
	text := []rune(plainText(raw))
	matches := findMatches(text, query)

	start := 0
	if len(matches) > 0 && matches[0].Start > snippetLeadRunes {
		start = matches[0].Start - snippetLeadRunes
	}
	end := start + snippetMaxRunes
	if end > len(text) {
		end = len(text)
		if end-snippetMaxRunes > 0 && start > end-snippetMaxRunes {
			start = end - snippetMaxRunes
		}
	}
	for start < end && unicode.IsSpace(text[start]) {
		start++
	}

	window := make([]rune, 0, end-start+2)
	shift := -start
	if start > 0 {
		window = append(window, snippetEllipsis)
		shift++
	}
	window = append(window, text[start:end]...)
	if end < len(text) {
		window = append(window, snippetEllipsis)
	}

	out := make([]domain.SearchMatch, 0, len(matches))
	for _, m := range matches {
		if m.End <= start || m.Start >= end {
			continue
		}
		s, e := m.Start, m.End
		if s < start {
			s = start
		}
		if e > end {
			e = end
		}
		out = append(out, domain.SearchMatch{Start: s + shift, End: e + shift})
	}
	return domain.SearchSnippet{Text: string(window), Matches: out}, len(matches) > 0
}

func buildSearchResult(hit domain.SearchHit, query string) domain.SearchResult {
	result := domain.SearchResult{
		Type: hit.Type, ID: hit.ID, TaskID: hit.TaskID, ListID: hit.ListID, SpaceID: hit.SpaceID,
		TaskTitle: hit.TaskTitle, Score: hit.Score, UpdatedAt: hit.UpdatedAt,
	}
	switch hit.Type {
	case domain.SearchTypeComment:
		result.Field = "body"
		result.Snippet, _ = buildSnippet(hit.Text, query)
	case domain.SearchTypeChecklistItem:
		result.Field = "text"
		result.Snippet, _ = buildSnippet(hit.Text, query)
	default:
		result.Field = "title"
		snippet, matched := buildSnippet(hit.Text, query)
		if !matched && hit.Description != nil {
			if described, ok := buildSnippet(*hit.Description, query); ok {
				result.Field = "description"
				snippet = described
			}
		}
		result.Snippet = snippet
	}
	return result
}
