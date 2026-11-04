package repository

import (
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func countPlaceholders(sql string) int { return strings.Count(sql, "?") }

func TestBuildSearchQueryPlaceholdersMatchArguments(t *testing.T) {
	space := uint(7)
	for _, query := range []string{"ab", "abc", "100% _x\\"} {
		for _, types := range [][]domain.SearchType{nil, {domain.SearchTypeTask}, {domain.SearchTypeComment, domain.SearchTypeChecklistItem}} {
			for _, sp := range []*uint{nil, &space} {
				params := domain.SearchParams{WorkspaceID: 3, Query: query, Types: types, SpaceID: sp, Limit: 10, Offset: 20}
				sql, args := buildSearchQuery(params)
				if countPlaceholders(sql) != len(args) {
					t.Fatalf("select %q: %d placeholders, %d args", query, countPlaceholders(sql), len(args))
				}
				sql, args = buildSearchCountQuery(params)
				if countPlaceholders(sql) != len(args) {
					t.Fatalf("count %q: %d placeholders, %d args", query, countPlaceholders(sql), len(args))
				}
			}
		}
	}
}

func TestBuildSearchQueryEscapesWildcardsAndNeverInlinesInput(t *testing.T) {
	sql, args := buildSearchQuery(domain.SearchParams{WorkspaceID: 1, Query: `50%_\x`, Limit: 5})
	if strings.Contains(sql, "50%") {
		t.Fatalf("query text is inlined: %s", sql)
	}
	found := false
	for _, a := range args {
		if s, ok := a.(string); ok && s == `%50\%\_\\x%` {
			found = true
		}
	}
	if !found {
		t.Fatalf("escaped pattern missing from %v", args)
	}
}

func TestBuildSearchQueryFuzzyOnlyFromThreeRunes(t *testing.T) {
	short, _ := buildSearchQuery(domain.SearchParams{WorkspaceID: 1, Query: "аб", Limit: 5})
	long, _ := buildSearchQuery(domain.SearchParams{WorkspaceID: 1, Query: "абв", Limit: 5})
	if strings.Contains(short, "<%") || strings.Contains(short, "word_similarity") {
		t.Fatal("two-character query must not use trigram similarity")
	}
	if !strings.Contains(long, "<%") || !strings.Contains(long, "word_similarity") {
		t.Fatal("three-character query must use trigram similarity")
	}
}

func TestBuildSearchQueryTypesAndScope(t *testing.T) {
	space := uint(9)
	sql, _ := buildSearchQuery(domain.SearchParams{WorkspaceID: 1, Query: "abc", Types: []domain.SearchType{domain.SearchTypeComment}, SpaceID: &space, Limit: 5})
	if strings.Contains(sql, "task_checklist_items") || !strings.Contains(sql, "task_comments") || strings.Contains(sql, "UNION ALL") {
		t.Fatalf("unexpected branches: %s", sql)
	}
	if !strings.Contains(sql, "s.id = ?") || !strings.Contains(sql, "deleted_at IS NULL") || !strings.Contains(sql, "dead_folders") {
		t.Fatalf("scope conditions missing: %s", sql)
	}
	all, _ := buildSearchQuery(domain.SearchParams{WorkspaceID: 1, Query: "abc", Limit: 5})
	if strings.Count(all, "UNION ALL") != 2 || !strings.Contains(all, "ORDER BY score DESC, updated_at DESC, type ASC, id ASC") {
		t.Fatalf("unexpected union or order: %s", all)
	}
	if strings.Contains(all, "email") {
		t.Fatal("e-mail must never be searched")
	}
}
