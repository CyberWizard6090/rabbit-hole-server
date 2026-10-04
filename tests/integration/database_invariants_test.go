package integration

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/db/migrations"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/repository"
	"rabbit-hole-server/internal/service"

	"github.com/jackc/pgx/v5/pgconn"

	"gorm.io/gorm"
)

func TestMigrationLifecycleLeavesCurrentSchema(t *testing.T) {
	tc := newTestContext(t)

	var version int
	var dirty bool
	if err := tc.DB.Raw("SELECT version, dirty FROM schema_migrations").Row().Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration state: %v", err)
	}
	if version != 2 || dirty {
		t.Fatalf("migration state = (%d, dirty=%t), want (2, dirty=false)", version, dirty)
	}
}

func TestQueryIndexesAndTrigramExtensionExist(t *testing.T) {
	tc := newTestContext(t)

	var indexCount int64
	if err := tc.DB.Raw(`
		SELECT count(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND indexname IN (
			'idx_task_statuses_list_position',
			'idx_tasks_list_created_at',
			'idx_user_sessions_user_id',
			'uq_user_sessions_token_hash',
			'uq_users_email',
			'uq_users_username',
			'idx_users_username_trgm',
			'idx_users_email_trgm'
		  )
	`).Scan(&indexCount).Error; err != nil {
		t.Fatalf("check query indexes: %v", err)
	}
	if indexCount != 8 {
		t.Fatalf("query indexes found = %d, want 8", indexCount)
	}

	var extensionCount int64
	if err := tc.DB.Raw("SELECT count(*) FROM pg_extension WHERE extname = 'pg_trgm'").Scan(&extensionCount).Error; err != nil {
		t.Fatalf("check pg_trgm extension: %v", err)
	}
	if extensionCount != 1 {
		t.Fatal("pg_trgm extension is not installed")
	}
}

func TestDatabaseRejectsBlankNames(t *testing.T) {
	tc := newTestContext(t)
	user := domain.User{Email: "blank-name-owner@example.test", PasswordHash: "x"}
	if err := tc.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace := domain.Workspace{Name: "Workspace", OwnerID: user.ID}
	if err := tc.DB.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	space := domain.Space{Name: "Space", WorkspaceID: workspace.ID, OwnerID: user.ID}
	if err := tc.DB.Create(&space).Error; err != nil {
		t.Fatalf("create space: %v", err)
	}
	list := domain.List{Name: "List", SpaceID: space.ID}
	if err := tc.DB.Create(&list).Error; err != nil {
		t.Fatalf("create list: %v", err)
	}
	status := domain.TaskStatus{
		SpaceID: space.ID, ListID: list.ID, Name: "Status", Color: "#000000", Position: 1, Type: domain.StatusTodo,
	}
	if err := tc.DB.Create(&status).Error; err != nil {
		t.Fatalf("create status: %v", err)
	}

	checks := []struct {
		table string
		sql   string
		args  func(name string, suffix int) []any
	}{
		{"workspaces", "INSERT INTO workspaces (name, owner_id) VALUES (?, ?)", func(name string, _ int) []any {
			return []any{name, user.ID}
		}},
		{"roles", "INSERT INTO roles (workspace_id, name) VALUES (?, ?)", func(name string, _ int) []any {
			return []any{workspace.ID, name}
		}},
		{"spaces", "INSERT INTO spaces (name, workspace_id, owner_id) VALUES (?, ?, ?)", func(name string, _ int) []any {
			return []any{name, workspace.ID, user.ID}
		}},
		{"folders", "INSERT INTO folders (name, space_id) VALUES (?, ?)", func(name string, _ int) []any {
			return []any{name, space.ID}
		}},
		{"lists", "INSERT INTO lists (name, space_id) VALUES (?, ?)", func(name string, _ int) []any {
			return []any{name, space.ID}
		}},
		{"task_statuses", "INSERT INTO task_statuses (space_id, list_id, name, color, position, type) VALUES (?, ?, ?, '#000000', 1, 'todo')", func(name string, _ int) []any {
			return []any{space.ID, list.ID, name}
		}},
		{"tags", "INSERT INTO tags (space_id, name, color) VALUES (?, ?, '#000000')", func(name string, _ int) []any {
			return []any{space.ID, name}
		}},
		{"tasks", "INSERT INTO tasks (space_id, list_id, status_id, title, user_id) VALUES (?, ?, ?, ?, ?)", func(name string, _ int) []any {
			return []any{space.ID, list.ID, status.ID, name, user.ID}
		}},
	}

	suffix := 0
	for _, check := range checks {
		for _, invalidName := range []string{"", "   "} {
			suffix++
			tx := tc.DB.Begin()
			if tx.Error != nil {
				t.Fatalf("begin %s check: %v", check.table, tx.Error)
			}
			err := tx.Exec(check.sql, check.args(invalidName, suffix)...).Error
			_ = tx.Rollback().Error
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("insert blank %s name %q error = %v, want check violation 23514", check.table, invalidName, err)
			}
		}
	}
}

func TestIntegrationDatabaseURLMustBeDistinct(t *testing.T) {
	if !integrationDBRequiredInCI("true", "") {
		t.Fatal("CI with no TEST_DB_URL was not rejected")
	}
	if integrationDBRequiredInCI("false", "") {
		t.Fatal("local tests should remain skippable without TEST_DB_URL")
	}
	if integrationDBRequiredInCI("true", "postgres://localhost/rabbit_hole_test") {
		t.Fatal("CI with TEST_DB_URL was rejected")
	}

	applicationURL := "host=127.0.0.1 port=5432 user=postgres dbname=rabbit_hole"
	if err := ensureSeparateDatabaseURLs(applicationURL, applicationURL); err == nil {
		t.Fatal("same PostgreSQL database was accepted for integration tests")
	}
	if err := ensureSeparateDatabaseURLs(applicationURL, "host=localhost port=5432 user=postgres dbname=rabbit_hole"); err == nil {
		t.Fatal("same database name on a different host was accepted for integration tests")
	}
	if err := ensureSeparateDatabaseURLs(applicationURL, "host=127.0.0.1 port=5432 user=postgres dbname=rabbit_hole_test"); err != nil {
		t.Fatalf("distinct PostgreSQL database was rejected: %v", err)
	}
}

func TestIntegrationResetPreservesPermissions(t *testing.T) {
	tc := newTestContext(t)
	user := domain.User{Email: "reset-check@example.test", PasswordHash: "x"}
	if err := tc.DB.Create(&user).Error; err != nil {
		t.Fatalf("create reset-check user: %v", err)
	}

	if err := resetIntegrationDatabase(); err != nil {
		t.Fatalf("reset integration database: %v", err)
	}

	var userCount, permissionCount int64
	if err := tc.DB.Model(&domain.User{}).Count(&userCount).Error; err != nil {
		t.Fatalf("count users after reset: %v", err)
	}
	if err := tc.DB.Model(&domain.Permission{}).Count(&permissionCount).Error; err != nil {
		t.Fatalf("count permissions after reset: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("users after reset = %d, want 0", userCount)
	}
	if permissionCount != 11 {
		t.Fatalf("permissions after reset = %d, want 11", permissionCount)
	}
}

func TestSoftDeletedUniqueRowsCanBeRecreated(t *testing.T) {
	tc := newTestContext(t)
	tx := tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	user := domain.User{Email: "partial-index@example.test", PasswordHash: "x"}
	if err := tx.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace := domain.Workspace{Name: "Partial index", OwnerID: user.ID}
	if err := tx.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	role := domain.Role{WorkspaceID: workspace.ID, Name: "Member"}
	if err := tx.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := tx.Delete(&role).Error; err != nil {
		t.Fatalf("soft-delete role: %v", err)
	}
	role = domain.Role{WorkspaceID: workspace.ID, Name: "Member"}
	if err := tx.Create(&role).Error; err != nil {
		t.Fatalf("recreate soft-deleted role: %v", err)
	}

	member := domain.Member{WorkspaceID: workspace.ID, UserID: user.ID, RoleID: role.ID}
	if err := tx.Create(&member).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := tx.Delete(&member).Error; err != nil {
		t.Fatalf("soft-delete member: %v", err)
	}
	member = domain.Member{WorkspaceID: workspace.ID, UserID: user.ID, RoleID: role.ID}
	if err := tx.Create(&member).Error; err != nil {
		t.Fatalf("recreate soft-deleted member: %v", err)
	}

	permission := domain.Permission{Code: "integration.partial-index"}
	if err := tx.Create(&permission).Error; err != nil {
		t.Fatalf("create permission: %v", err)
	}
	if err := tx.Delete(&permission).Error; err != nil {
		t.Fatalf("soft-delete permission: %v", err)
	}
	permission = domain.Permission{Code: "integration.partial-index"}
	if err := tx.Create(&permission).Error; err != nil {
		t.Fatalf("recreate soft-deleted permission: %v", err)
	}

	grant := domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	if err := tx.Create(&grant).Error; err != nil {
		t.Fatalf("create role permission: %v", err)
	}
	if err := tx.Delete(&grant).Error; err != nil {
		t.Fatalf("soft-delete role permission: %v", err)
	}
	grant = domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	if err := tx.Create(&grant).Error; err != nil {
		t.Fatalf("recreate soft-deleted role permission: %v", err)
	}
}

func TestWorkspaceDeleteCascadesRolesAndMembers(t *testing.T) {
	tc := newTestContext(t)
	tx := tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	workspace, space, list := createWorkspaceTaskGraph(t, tx, "workspace-cascade@example.test", "Cascade")

	requireDBSuccess(t, tx.Unscoped().Delete(&workspace).Error, "hard-delete workspace")
	var remaining int64
	requireDBSuccess(t, tx.Raw(`
		SELECT
			(SELECT count(*) FROM roles WHERE workspace_id = ?) +
			(SELECT count(*) FROM workspace_members WHERE workspace_id = ?) +
			(SELECT count(*) FROM spaces WHERE workspace_id = ?) +
			(SELECT count(*) FROM lists WHERE space_id = ?) +
			(SELECT count(*) FROM task_statuses WHERE list_id = ?) +
			(SELECT count(*) FROM tasks WHERE list_id = ?)
	`, workspace.ID, workspace.ID, workspace.ID, space.ID, list.ID, list.ID).Scan(&remaining).Error, "count workspace dependents")
	if remaining != 0 {
		t.Fatalf("remaining workspace dependents = %d, want 0", remaining)
	}
}

func TestConcurrentStatusCreatesSerializePositions(t *testing.T) {
	tc := newTestContext(t)
	owner := domain.User{Email: "status-concurrency@example.test", PasswordHash: "x"}
	if err := tc.DB.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace := domain.Workspace{Name: "Status concurrency", OwnerID: owner.ID}
	if err := tc.DB.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	space := domain.Space{WorkspaceID: workspace.ID, OwnerID: owner.ID, Name: "Status concurrency"}
	if err := tc.DB.Create(&space).Error; err != nil {
		t.Fatalf("create space: %v", err)
	}
	list := domain.List{SpaceID: space.ID, Name: "Status concurrency"}
	if err := tc.DB.Create(&list).Error; err != nil {
		t.Fatalf("create list: %v", err)
	}

	statusRepo := repository.NewStatusRepository(tc.DB)
	errCh := make(chan error, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			errCh <- statusRepo.Create(&domain.TaskStatus{
				SpaceID:  space.ID,
				ListID:   list.ID,
				Name:     fmt.Sprintf("Status %d", index),
				Color:    "#123456",
				Position: 1,
				Type:     domain.StatusTodo,
			})
		}(index)
	}
	workers.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("create concurrent status: %v", err)
		}
	}

	statuses, err := statusRepo.GetAllByList(list.ID)
	if err != nil {
		t.Fatalf("read statuses: %v", err)
	}
	if len(statuses) != 2 || statuses[0].Position != 1 || statuses[1].Position != 2 {
		t.Fatalf("status positions = %v, want [1 2]", []int{statuses[0].Position, statuses[1].Position})
	}
}

func TestStatusDeleteCompactsPositions(t *testing.T) {
	tc := newTestContext(t)
	_, space, list := createWorkspaceTaskGraph(t, tc.DB, "status-delete@example.test", "Status delete")

	statusRepo := repository.NewStatusRepository(tc.DB)
	middle := domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Doing", Color: "#123456", Position: 2, Type: domain.StatusTodo}
	last := domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Done", Color: "#123456", Position: 3, Type: domain.StatusTodo}
	requireDBSuccess(t, statusRepo.Create(&middle), "create middle status")
	requireDBSuccess(t, statusRepo.Create(&last), "create last status")
	requireDBSuccess(t, statusRepo.Delete(list.ID, middle.ID), "delete middle status")

	statuses, err := statusRepo.GetAllByList(list.ID)
	requireDBSuccess(t, err, "read statuses")
	if len(statuses) != 2 || statuses[0].Position != 1 || statuses[1].Position != 2 || statuses[1].ID != last.ID {
		t.Fatalf("statuses after delete = %+v, want positions [1 2] ending with the last status", statuses)
	}
	if err := statusRepo.Delete(list.ID, middle.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete error = %v, want record not found", err)
	}
}

func TestTaskIntegerColumnsAreNotNullable(t *testing.T) {
	tc := newTestContext(t)

	var columns []struct {
		ColumnName string
		DataType   string
		IsNullable string
	}
	if err := tc.DB.Raw(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND (table_name, column_name) IN (
			('task_statuses', 'position'),
			('tasks', 'priority'),
			('tasks', 'time_estimate'),
			('tasks', 'time_spent')
		  )
	`).Scan(&columns).Error; err != nil {
		t.Fatalf("read integer column definitions: %v", err)
	}
	if len(columns) != 4 {
		t.Fatalf("integer columns found = %d, want 4", len(columns))
	}
	for _, column := range columns {
		if column.DataType != "integer" || column.IsNullable != "NO" {
			t.Errorf("%s: data_type=%q is_nullable=%q, want integer/NO", column.ColumnName, column.DataType, column.IsNullable)
		}
	}
}

func TestTimezoneAndCurrencyAreNotNullable(t *testing.T) {
	tc := newTestContext(t)

	var columns []struct {
		TableName  string
		ColumnName string
		DataType   string
		IsNullable string
	}
	if err := tc.DB.Raw(`
		SELECT table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND (table_name, column_name) IN (
			('users', 'time_zone'),
			('spaces', 'currency')
		  )
	`).Scan(&columns).Error; err != nil {
		t.Fatalf("read required string column definitions: %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("required string columns found = %d, want 2", len(columns))
	}
	for _, column := range columns {
		if column.DataType != "text" || column.IsNullable != "NO" {
			t.Errorf("%s.%s: data_type=%q is_nullable=%q, want text/NO", column.TableName, column.ColumnName, column.DataType, column.IsNullable)
		}
	}
}

func TestModelTimestampsAreNotNullable(t *testing.T) {
	tc := newTestContext(t)

	var nullableCount int64
	if err := tc.DB.Raw(`
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name IN (
			'users', 'user_sessions', 'workspaces', 'roles', 'permissions',
			'role_permissions', 'workspace_members', 'spaces', 'folders',
			'lists', 'task_statuses', 'tasks'
		  )
		  AND column_name IN ('created_at', 'updated_at')
		  AND is_nullable <> 'NO'
	`).Scan(&nullableCount).Error; err != nil {
		t.Fatalf("check timestamp nullability: %v", err)
	}
	if nullableCount != 0 {
		t.Fatalf("nullable model timestamp columns = %d, want 0", nullableCount)
	}

	var withoutTimeZoneCount int64
	if err := tc.DB.Raw(`
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name IN (
			'users', 'user_sessions', 'workspaces', 'roles', 'permissions',
			'role_permissions', 'workspace_members', 'spaces', 'folders',
			'lists', 'task_statuses', 'tasks'
		  )
		  AND column_name IN ('created_at', 'updated_at', 'deleted_at', 'start_date', 'due_date')
		  AND data_type = 'timestamp without time zone'
	`).Scan(&withoutTimeZoneCount).Error; err != nil {
		t.Fatalf("check timestamp time zones: %v", err)
	}
	if withoutTimeZoneCount != 0 {
		t.Fatalf("timestamp columns without time zone = %d, want 0", withoutTimeZoneCount)
	}
}

func TestUpdatedAtTriggerHandlesDirectTaskUpdates(t *testing.T) {
	tc := newTestContext(t)
	_, _, list := createWorkspaceTaskGraph(t, tc.DB, "updated-at-trigger@example.test", "Updated at trigger")

	var task domain.Task
	if err := tc.DB.Where("list_id = ?", list.ID).First(&task).Error; err != nil {
		t.Fatalf("find task: %v", err)
	}
	staleTimestamp := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	if err := tc.DB.Exec(
		"UPDATE tasks SET title = ?, updated_at = ? WHERE id = ?",
		"Changed directly", staleTimestamp, task.ID,
	).Error; err != nil {
		t.Fatalf("update task directly: %v", err)
	}

	var updatedAt time.Time
	if err := tc.DB.Raw("SELECT updated_at FROM tasks WHERE id = ?", task.ID).Scan(&updatedAt).Error; err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !updatedAt.After(staleTimestamp) {
		t.Fatalf("updated_at = %s, want later than %s", updatedAt, staleTimestamp)
	}
}

func TestPermissionsRollbackRefusesReferencedPermissions(t *testing.T) {
	tc := newTestContext(t)
	user := domain.User{Email: "permission-rollback@example.test", PasswordHash: "x"}
	if err := tc.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	workspace := domain.Workspace{Name: "Permission rollback", OwnerID: user.ID}
	if err := tc.DB.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	role := domain.Role{WorkspaceID: workspace.ID, Name: "Member"}
	if err := tc.DB.Create(&role).Error; err != nil {
		t.Fatalf("create role: %v", err)
	}
	var permission domain.Permission
	if err := tc.DB.Where("code = ?", "workspace.read").First(&permission).Error; err != nil {
		t.Fatalf("find seeded permission: %v", err)
	}
	grant := domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	if err := tc.DB.Create(&grant).Error; err != nil {
		t.Fatalf("create permission reference: %v", err)
	}
	if err := tc.DB.Unscoped().Delete(&permission).Error; err == nil {
		t.Fatal("physically deleting a referenced permission unexpectedly succeeded")
	}

	rollbackSQL, err := migrations.FS.ReadFile("000002_seed_permissions.down.sql")
	if err != nil {
		t.Fatalf("read embedded permissions rollback: %v", err)
	}
	err = tc.DB.Exec(string(rollbackSQL)).Error
	if err == nil || !strings.Contains(err.Error(), "cannot rollback permissions migration") {
		t.Fatalf("rollback error = %v, want referenced-permissions error", err)
	}

	var permissionCount, grantCount int64
	if err := tc.DB.Model(&domain.Permission{}).Where("id = ?", permission.ID).Count(&permissionCount).Error; err != nil {
		t.Fatalf("check permission after rejected rollback: %v", err)
	}
	if err := tc.DB.Model(&domain.RolePermission{}).Where("id = ?", grant.ID).Count(&grantCount).Error; err != nil {
		t.Fatalf("check grant after rejected rollback: %v", err)
	}
	if permissionCount != 1 || grantCount != 1 {
		t.Fatalf("rollback removed referenced data: permissions=%d grants=%d", permissionCount, grantCount)
	}
}

func TestDatabaseCheckConstraintsRejectInvalidValues(t *testing.T) {
	tc := newTestContext(t)
	owner := domain.User{Email: "check-constraints@example.test", PasswordHash: "x"}
	if err := tc.DB.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace := domain.Workspace{Name: "Check constraints", OwnerID: owner.ID}
	if err := tc.DB.Create(&workspace).Error; err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	space := domain.Space{WorkspaceID: workspace.ID, OwnerID: owner.ID, Name: "Checks"}
	if err := tc.DB.Create(&space).Error; err != nil {
		t.Fatalf("create space: %v", err)
	}
	list := domain.List{SpaceID: space.ID, Name: "Checks"}
	if err := tc.DB.Create(&list).Error; err != nil {
		t.Fatalf("create list: %v", err)
	}
	status := domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Todo", Color: "#123456", Position: 1, Type: domain.StatusTodo}
	if err := tc.DB.Create(&status).Error; err != nil {
		t.Fatalf("create status: %v", err)
	}
	assertRejected := func(name string, create func(*gorm.DB) error) {
		t.Run(name, func(t *testing.T) {
			tx := tc.DB.Begin()
			if tx.Error != nil {
				t.Fatalf("begin transaction: %v", tx.Error)
			}
			err := create(tx)
			_ = tx.Rollback().Error
			if err == nil {
				t.Fatal("invalid value was accepted")
			}
		})
	}

	assertRejected("status type", func(tx *gorm.DB) error {
		return tx.Create(&domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Invalid type", Color: "#123456", Position: 2, Type: domain.StatusTypeUndefined}).Error
	})
	assertRejected("status color", func(tx *gorm.DB) error {
		return tx.Create(&domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Invalid color", Color: "#fff", Position: 2, Type: domain.StatusTodo}).Error
	})
	assertRejected("tag color", func(tx *gorm.DB) error {
		return tx.Create(&domain.Tag{SpaceID: space.ID, Name: "Invalid color", Color: "red"}).Error
	})
	assertRejected("priority range", func(tx *gorm.DB) error {
		return tx.Create(&domain.Task{SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, UserID: owner.ID, Title: "Invalid priority", Priority: 6}).Error
	})
	assertRejected("negative estimate", func(tx *gorm.DB) error {
		return tx.Create(&domain.Task{SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, UserID: owner.ID, Title: "Invalid estimate", TimeEstimate: -1}).Error
	})
	assertRejected("negative time spent", func(tx *gorm.DB) error {
		return tx.Create(&domain.Task{SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, UserID: owner.ID, Title: "Invalid time spent", TimeSpent: -1}).Error
	})
	assertRejected("date order", func(tx *gorm.DB) error {
		startDate := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
		dueDate := startDate.Add(-24 * time.Hour)
		return tx.Create(&domain.Task{SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, UserID: owner.ID, Title: "Invalid dates", StartDate: &startDate, DueDate: &dueDate}).Error
	})
	assertRejected("self parent task", func(tx *gorm.DB) error {
		const id uint = 900001
		parentID := id
		return tx.Create(&domain.Task{Model: gorm.Model{ID: id}, SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, ParentID: &parentID, UserID: owner.ID, Title: "Self parent"}).Error
	})
	assertRejected("self parent folder", func(tx *gorm.DB) error {
		const id uint = 900002
		parentID := id
		return tx.Create(&domain.Folder{Model: gorm.Model{ID: id}, SpaceID: space.ID, Name: "Self parent", ParentID: &parentID}).Error
	})
	assertRejected("self contact", func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO user_contacts (user_id, contact_id) VALUES (?, ?)", owner.ID, owner.ID).Error
	})
}

func TestPermissionCheckerIgnoresSoftDeletedRows(t *testing.T) {
	tc := newTestContext(t)
	user, workspace, role, member, permission := createPermissionFixture(t, tc.DB)
	grant := domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	requireDBSuccess(t, tc.DB.Create(&grant).Error, "create initial grant")
	checker := service.NewPermissionChecker(tc.DB)
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, true)
	requireDBSuccess(t, tc.DB.Delete(&member).Error, "soft-delete member")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, false)
	member = domain.Member{WorkspaceID: workspace.ID, UserID: user.ID, RoleID: role.ID}
	requireDBSuccess(t, tc.DB.Create(&member).Error, "recreate member")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, true)
	requireDBSuccess(t, tc.DB.Delete(&grant).Error, "soft-delete grant")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, false)
	grant = domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	requireDBSuccess(t, tc.DB.Create(&grant).Error, "recreate grant")
	requireDBSuccess(t, tc.DB.Delete(&role).Error, "soft-delete role")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, false)
	role = domain.Role{WorkspaceID: workspace.ID, Name: "Member"}
	requireDBSuccess(t, tc.DB.Create(&role).Error, "recreate role")
	member.RoleID = role.ID
	requireDBSuccess(t, tc.DB.Save(&member).Error, "move member to recreated role")
	grant = domain.RolePermission{RoleID: role.ID, PermissionID: permission.ID}
	requireDBSuccess(t, tc.DB.Create(&grant).Error, "grant permission to recreated role")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, true)
	requireDBSuccess(t, tc.DB.Delete(&permission).Error, "soft-delete permission")
	assertWorkspacePermission(t, checker, user.ID, workspace.ID, permission.Code, false)
}

func createWorkspaceTaskGraph(t *testing.T, db *gorm.DB, email, name string) (domain.Workspace, domain.Space, domain.List) {
	t.Helper()
	owner := domain.User{Email: email, PasswordHash: "x"}
	requireDBSuccess(t, db.Create(&owner).Error, "create owner")
	workspace := domain.Workspace{Name: name, OwnerID: owner.ID}
	requireDBSuccess(t, db.Create(&workspace).Error, "create workspace")
	role := domain.Role{WorkspaceID: workspace.ID, Name: "Owner"}
	requireDBSuccess(t, db.Create(&role).Error, "create role")
	member := domain.Member{WorkspaceID: workspace.ID, UserID: owner.ID, RoleID: role.ID}
	requireDBSuccess(t, db.Create(&member).Error, "create member")
	space := domain.Space{WorkspaceID: workspace.ID, OwnerID: owner.ID, Name: name + " space"}
	requireDBSuccess(t, db.Create(&space).Error, "create space")
	list := domain.List{SpaceID: space.ID, Name: name + " list"}
	requireDBSuccess(t, db.Create(&list).Error, "create list")
	status := domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: "Todo", Color: "#123456", Position: 1, Type: domain.StatusTodo}
	requireDBSuccess(t, db.Create(&status).Error, "create status")
	task := domain.Task{SpaceID: space.ID, ListID: list.ID, StatusID: status.ID, UserID: owner.ID, Title: name + " task"}
	requireDBSuccess(t, db.Create(&task).Error, "create task")
	return workspace, space, list
}

func createPermissionFixture(t *testing.T, db *gorm.DB) (domain.User, domain.Workspace, domain.Role, domain.Member, domain.Permission) {
	t.Helper()
	user := domain.User{Email: "permission-checker@example.test", PasswordHash: "x"}
	requireDBSuccess(t, db.Create(&user).Error, "create user")
	workspace := domain.Workspace{Name: "Permission checker", OwnerID: user.ID}
	requireDBSuccess(t, db.Create(&workspace).Error, "create workspace")
	role := domain.Role{WorkspaceID: workspace.ID, Name: "Member"}
	requireDBSuccess(t, db.Create(&role).Error, "create role")
	member := domain.Member{WorkspaceID: workspace.ID, UserID: user.ID, RoleID: role.ID}
	requireDBSuccess(t, db.Create(&member).Error, "create member")
	permission := domain.Permission{Code: "integration.permission.checker"}
	requireDBSuccess(t, db.Create(&permission).Error, "create permission")
	return user, workspace, role, member, permission
}

func assertWorkspacePermission(t *testing.T, checker service.PermissionChecker, userID, workspaceID uint, code string, want bool) {
	t.Helper()
	got, err := checker.HasWorkspacePermission(userID, workspaceID, code)
	requireDBSuccess(t, err, "check workspace permission")
	if got != want {
		t.Fatalf("permission result = %t, want %t", got, want)
	}
}

func requireDBSuccess(t *testing.T, err error, operation string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", operation, err)
	}
}
