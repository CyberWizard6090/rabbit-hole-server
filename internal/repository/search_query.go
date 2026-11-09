package repository

import (
	"strings"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

const (
	minFuzzyRunes       = 3
	fuzzyScoreCeiling   = "0.79"
	descriptionWeight   = "0.6"
	wordSimilarityLimit = "0.5"
)

type searchSQL struct {
	sb   strings.Builder
	args []any
}

func (b *searchSQL) write(sql string, args ...any) {
	b.sb.WriteString(sql)
	b.args = append(b.args, args...)
}

type searchColumn struct {
	name string
}

func (c searchColumn) arms(fuzzy bool, contains, query string) []searchArm {
	arms := []searchArm{{sql: c.name + ` ILIKE ? ESCAPE '\'`, args: []any{contains}}}
	if fuzzy {
		arms = append(arms, searchArm{sql: "? <% " + c.name, args: []any{query}})
	}
	return arms
}

type searchArm struct {
	sql  string
	args []any
}

type candidateSource struct {
	cte     string
	from    string
	idExpr  string
	live    string
	columns []searchColumn
}

var (
	taskCandidates = candidateSource{
		cte: "task_candidates", from: "tasks t", idExpr: "t.id", live: "t.deleted_at IS NULL",
		columns: []searchColumn{{"t.title"}, {"t.description"}},
	}
	commentCandidates = candidateSource{
		cte: "comment_candidates", from: "task_comments c", idExpr: "c.id", live: "c.deleted_at IS NULL",
		columns: []searchColumn{{"c.body"}},
	}
	checklistCandidates = candidateSource{
		cte: "checklist_candidates", from: "task_checklist_items i", idExpr: "i.id", live: "TRUE",
		columns: []searchColumn{{"i.text"}},
	}
)

func candidatesFor(kind domain.SearchType) candidateSource {
	switch kind {
	case domain.SearchTypeComment:
		return commentCandidates
	case domain.SearchTypeChecklistItem:
		return checklistCandidates
	default:
		return taskCandidates
	}
}

func writeCandidates(b *searchSQL, src candidateSource, terms searchTerms) {
	b.write(", " + src.cte + " AS MATERIALIZED (")
	first := true
	for _, col := range src.columns {
		for _, arm := range col.arms(terms.fuzzy, terms.contains, terms.query) {
			if !first {
				b.write(" UNION ")
			}
			first = false
			b.write("SELECT "+src.idExpr+" AS id FROM "+src.from+" WHERE "+src.live+" AND "+arm.sql, arm.args...)
		}
	}
	b.write(")")
}

func (c searchColumn) score(fuzzy bool, b *searchSQL, prefix, contains, query string) {
	b.write("(CASE WHEN lower("+c.name+") = lower(?) THEN 1.0::float8 "+
		"WHEN "+c.name+` ILIKE ? ESCAPE '\' THEN 0.9::float8 `+
		"WHEN "+c.name+` ILIKE ? ESCAPE '\' THEN 0.8::float8 `,
		query, prefix, contains)
	if fuzzy {
		b.write("ELSE LEAST(word_similarity(?, " + c.name + ")::float8, " + fuzzyScoreCeiling + "::float8) END)")
		b.args = append(b.args, query)
		return
	}
	b.write("ELSE 0.0::float8 END)")
}

type searchTerms struct {
	query    string
	prefix   string
	contains string
	fuzzy    bool
}

func newSearchTerms(query string) searchTerms {
	escaped := escapeLike(query)
	return searchTerms{
		query:    query,
		prefix:   escaped + "%",
		contains: "%" + escaped + "%",
		fuzzy:    utf8.RuneCountInString(query) >= minFuzzyRunes,
	}
}

const liveTaskJoins = " JOIN lists l ON l.id = t.list_id AND l.deleted_at IS NULL" +
	" JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL AND s.workspace_id = ?" +
	" LEFT JOIN dead_folders df ON df.id = l.folder_id"

func scopeWhere(b *searchSQL, params domain.SearchParams) {
	b.write(" AND t.deleted_at IS NULL AND df.id IS NULL")
	if params.SpaceID != nil {
		b.write(" AND s.id = ?", *params.SpaceID)
	}
}

func taskContext(kind domain.SearchType) string {
	return "'" + string(kind) + "' AS type, t.id AS task_id, t.list_id AS list_id, t.space_id AS space_id, t.title AS task_title, "
}

func writeBranch(b *searchSQL, kind domain.SearchType, params domain.SearchParams, terms searchTerms, withScore bool) {
	b.write("SELECT ")
	if withScore {
		b.write(taskContext(kind))
	} else {
		b.write("1 AS one ")
	}
	switch kind {
	case domain.SearchTypeTask:
		title, description := searchColumn{"t.title"}, searchColumn{"t.description"}
		if withScore {
			b.write("t.id AS id, t.title AS text, t.description AS description, GREATEST(")
			title.score(terms.fuzzy, b, terms.prefix, terms.contains, terms.query)
			b.write(", " + descriptionWeight + "::float8 * ")
			description.score(terms.fuzzy, b, terms.prefix, terms.contains, terms.query)
			b.write(") AS score, t.updated_at AS updated_at")
		}
		b.write(" FROM task_candidates cd JOIN tasks t ON t.id = cd.id"+liveTaskJoins, params.WorkspaceID)
		b.write(" WHERE TRUE")
		scopeWhere(b, params)
	case domain.SearchTypeComment:
		body := searchColumn{"c.body"}
		if withScore {
			b.write("c.id AS id, c.body AS text, NULL::text AS description, ")
			body.score(terms.fuzzy, b, terms.prefix, terms.contains, terms.query)
			b.write(" AS score, c.updated_at AS updated_at")
		}
		b.write(" FROM comment_candidates cd JOIN task_comments c ON c.id = cd.id JOIN tasks t ON t.id = c.task_id" + liveTaskJoins)
		b.args = append(b.args, params.WorkspaceID)
		b.write(" WHERE c.deleted_at IS NULL")
		scopeWhere(b, params)
	case domain.SearchTypeChecklistItem:
		text := searchColumn{"i.text"}
		if withScore {
			b.write("i.id AS id, i.text AS text, NULL::text AS description, ")
			text.score(terms.fuzzy, b, terms.prefix, terms.contains, terms.query)
			b.write(" AS score, i.updated_at AS updated_at")
		}
		b.write(" FROM checklist_candidates cd JOIN task_checklist_items i ON i.id = cd.id JOIN task_checklists k ON k.id = i.checklist_id JOIN tasks t ON t.id = k.task_id" + liveTaskJoins)
		b.args = append(b.args, params.WorkspaceID)
		b.write(" WHERE TRUE")
		scopeWhere(b, params)
	}
}

func writeDeadFolders(b *searchSQL, workspaceID uint) {
	b.write("WITH RECURSIVE dead_folders AS ("+
		"SELECT f.id FROM folders f JOIN spaces fs ON fs.id = f.space_id "+
		"WHERE fs.workspace_id = ? AND f.deleted_at IS NOT NULL "+
		"UNION "+
		"SELECT c.id FROM folders c JOIN dead_folders d ON c.parent_id = d.id)", workspaceID)
}

func writeCandidateCTEs(b *searchSQL, types []domain.SearchType, terms searchTerms) {
	for _, kind := range searchTypesOrDefault(types) {
		writeCandidates(b, candidatesFor(kind), terms)
	}
	b.write(" ")
}

func searchTypesOrDefault(types []domain.SearchType) []domain.SearchType {
	if len(types) == 0 {
		return domain.AllSearchTypes()
	}
	return types
}

func buildSearchQuery(params domain.SearchParams) (string, []any) {
	terms := newSearchTerms(params.Query)
	b := &searchSQL{}
	writeDeadFolders(b, params.WorkspaceID)
	writeCandidateCTEs(b, params.Types, terms)
	b.write("SELECT type, id, task_id, list_id, space_id, task_title, text, description, score, updated_at FROM (")
	for i, kind := range searchTypesOrDefault(params.Types) {
		if i > 0 {
			b.write(" UNION ALL ")
		}
		writeBranch(b, kind, params, terms, true)
	}
	b.write(") hits ORDER BY score DESC, updated_at DESC, type ASC, id ASC LIMIT ? OFFSET ?", params.Limit, params.Offset)
	return b.sb.String(), b.args
}

func buildSearchCountQuery(params domain.SearchParams) (string, []any) {
	terms := newSearchTerms(params.Query)
	b := &searchSQL{}
	writeDeadFolders(b, params.WorkspaceID)
	writeCandidateCTEs(b, params.Types, terms)
	b.write("SELECT count(*) FROM (")
	for i, kind := range searchTypesOrDefault(params.Types) {
		if i > 0 {
			b.write(" UNION ALL ")
		}
		writeBranch(b, kind, params, terms, false)
	}
	b.write(") hits")
	return b.sb.String(), b.args
}

func SearchQuerySQL(params domain.SearchParams) (string, []any) {
	return buildSearchQuery(params)
}
