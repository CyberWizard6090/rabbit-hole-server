package integration

import "testing"

func TestSchema_ForeignKeysToTasksAreServedByUsableIndexes(t *testing.T) {
	tc := newTestContext(t)
	var missing []string
	err := tc.DB.Raw(`
		SELECT c.conrelid::regclass::text || '.' || c.conname
		FROM pg_constraint c
		WHERE c.contype = 'f' AND c.confrelid = 'tasks'::regclass AND array_length(c.conkey, 1) = 1
		  AND NOT EXISTS (
		      SELECT 1 FROM pg_index i
		      WHERE i.indrelid = c.conrelid AND i.indkey[0] = c.conkey[1]
		        AND (i.indpred IS NULL OR pg_get_expr(i.indpred, i.indrelid) ILIKE '%IS NOT NULL%'))
		ORDER BY 1`).Scan(&missing).Error
	if err != nil {
		t.Fatalf("inspect foreign keys: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("these foreign keys to tasks have no index the foreign key checks can use (a partial index with another condition does not count), so deleting a task scans the child table: %v", missing)
	}
}
