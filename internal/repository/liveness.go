package repository

import "fmt"

const liveListSQL = "EXISTS (SELECT 1 FROM lists lv " +
	"JOIN spaces sv ON sv.id = lv.space_id AND sv.deleted_at IS NULL " +
	"JOIN workspaces wv ON wv.id = sv.workspace_id AND wv.deleted_at IS NULL " +
	"LEFT JOIN folders fv ON fv.id = lv.folder_id " +
	"WHERE lv.id = %s AND lv.deleted_at IS NULL AND (lv.folder_id IS NULL OR fv.deleted_at IS NULL))"

const liveFolderSQL = "EXISTS (SELECT 1 FROM folders fl " +
	"JOIN spaces sf ON sf.id = fl.space_id AND sf.deleted_at IS NULL " +
	"JOIN workspaces wf ON wf.id = sf.workspace_id AND wf.deleted_at IS NULL " +
	"WHERE fl.id = %s AND fl.deleted_at IS NULL " +
	"AND (fl.parent_id IS NULL OR NOT EXISTS (SELECT 1 FROM folders pf WHERE pf.id = fl.parent_id AND pf.deleted_at IS NOT NULL)))"

func liveListPredicate(listID string) string {
	return fmt.Sprintf(liveListSQL, listID)
}

func liveFolderPredicate(folderID string) string {
	return fmt.Sprintf(liveFolderSQL, folderID)
}

func liveTaskPredicate(task string) string {
	return "(" + task + ".deleted_at IS NULL AND " + liveListPredicate(task+".list_id") +
		" AND (" + task + ".parent_id IS NULL OR NOT EXISTS (SELECT 1 FROM tasks pt WHERE pt.id = " +
		task + ".parent_id AND pt.deleted_at IS NOT NULL)))"
}

func liveWorkspacePredicate(workspaceID string) string {
	return "EXISTS (SELECT 1 FROM workspaces lw WHERE lw.id = " + workspaceID + " AND lw.deleted_at IS NULL)"
}
