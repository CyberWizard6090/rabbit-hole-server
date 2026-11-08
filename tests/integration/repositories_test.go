package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
	"rabbit-hole-server/internal/repository"
)

func repoUser(t *testing.T, tc *TestContext, label string) uint {
	t.Helper()
	id, _ := newUser(t, tc, fmt.Sprintf("%s-%d@example.test", label, time.Now().UnixNano()))
	return id
}

func repoWorkspace(t *testing.T, tc *TestContext, ownerID uint, name string) *domain.Workspace {
	t.Helper()
	ws := &domain.Workspace{Name: name, OwnerID: ownerID}
	if err := repository.NewWorkspaceRepository(tc.DB).Create(context.Background(), ws, ownerID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return ws
}

func TestWorkspaceRepository_CreateSeedsRolesPermissionsAndOwner(t *testing.T) {
	tc := newTestContext(t)
	owner := repoUser(t, tc, "ws-owner")
	ws := repoWorkspace(t, tc, owner, "Seeded")

	var roles []domain.Role
	if err := tc.DB.Where("workspace_id = ?", ws.ID).Find(&roles).Error; err != nil {
		t.Fatal(err)
	}
	if len(roles) != len(domain.DefaultRoles()) {
		t.Fatalf("roles = %d, want %d", len(roles), len(domain.DefaultRoles()))
	}

	for _, def := range domain.DefaultRoles() {
		var count int64
		tc.DB.Table("role_permissions").
			Joins("JOIN roles ON roles.id = role_permissions.role_id").
			Where("roles.workspace_id = ? AND roles.name = ? AND role_permissions.deleted_at IS NULL", ws.ID, def.Name).
			Count(&count)
		if int(count) != len(def.PermissionCodes) {
			t.Errorf("role %s has %d permissions, want %d", def.Name, count, len(def.PermissionCodes))
		}
	}

	var member persistence.Member
	if err := tc.DB.Preload("Role").Where("workspace_id = ? AND user_id = ?", ws.ID, owner).First(&member).Error; err != nil {
		t.Fatalf("owner membership: %v", err)
	}
	if member.Role.Name != "Owner" {
		t.Fatalf("owner role = %q", member.Role.Name)
	}
}

func TestWorkspaceRepository_GetAllForUserOnlyReturnsMemberships(t *testing.T) {
	tc := newTestContext(t)
	alice, bob := repoUser(t, tc, "alice"), repoUser(t, tc, "bob")
	aliceA := repoWorkspace(t, tc, alice, "Alice A")
	repoWorkspace(t, tc, alice, "Alice B")
	repoWorkspace(t, tc, bob, "Bob")

	repo := repository.NewWorkspaceRepository(tc.DB)
	if err := repo.RemoveMember(context.Background(), aliceA.ID, alice); err != nil {
		t.Fatalf("remove alice membership: %v", err)
	}
	got, err := repo.GetAllForUser(context.Background(), alice)
	if err != nil || len(got) != 1 || got[0].Name != "Alice B" {
		t.Fatalf("alice workspaces = %+v err=%v, want only Alice B", got, err)
	}
	got, err = repo.GetAllForUser(context.Background(), bob)
	if err != nil || len(got) != 1 || got[0].Name != "Bob" {
		t.Fatalf("bob workspaces = %+v err=%v", got, err)
	}
	if _, err := repo.GetByID(context.Background(), 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID(missing) err = %v, want not found", err)
	}
}

func TestWorkspaceCreationAndAccountDeletionSerialize(t *testing.T) {
	tc := newTestContext(t)
	userRepo := repository.NewUserRepository(tc.DB)
	workspaceRepo := repository.NewWorkspaceRepository(tc.DB)

	for i := 0; i < 4; i++ {
		userID, _ := newUser(t, tc, fmt.Sprintf("delete-workspace-race-%d-%d@example.test", time.Now().UnixNano(), i))
		start := make(chan struct{})
		type operationResult struct {
			operation string
			err       error
		}
		results := make(chan operationResult, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			workspace := &domain.Workspace{Name: "Concurrent workspace", OwnerID: userID}
			results <- operationResult{operation: "create", err: workspaceRepo.Create(context.Background(), workspace, userID)}
		}()
		go func() {
			defer wg.Done()
			<-start
			results <- operationResult{operation: "delete", err: userRepo.AnonymizeIfNoOwnedWorkspaces(context.Background(), userID)}
		}()
		close(start)
		wg.Wait()
		first, second := <-results, <-results
		resultsByOperation := map[string]error{first.operation: first.err, second.operation: second.err}
		createErr, deleteErr := resultsByOperation["create"], resultsByOperation["delete"]

		switch {
		case createErr == nil && errors.Is(deleteErr, domain.ErrOwnsWorkspaces):
		case errors.Is(createErr, domain.ErrNotFound) && deleteErr == nil:
		default:
			t.Fatalf("create error = %v, delete error = %v; want exactly one serialized operation to succeed", createErr, deleteErr)
		}

		var user persistence.User
		if err := tc.DB.First(&user, userID).Error; err != nil {
			t.Fatal(err)
		}
		var workspaces int64
		if err := tc.DB.Model(&persistence.Workspace{}).Where("owner_id = ?", userID).Count(&workspaces).Error; err != nil {
			t.Fatal(err)
		}
		if createErr == nil && (workspaces != 1 || user.PasswordHash == "!") {
			t.Fatalf("created workspace state: count=%d password_hash=%q", workspaces, user.PasswordHash)
		}
		if deleteErr == nil && (workspaces != 0 || user.PasswordHash != "!") {
			t.Fatalf("deleted account state: count=%d password_hash=%q", workspaces, user.PasswordHash)
		}
	}

	deletedUserID, _ := newUser(t, tc, fmt.Sprintf("deleted-owner-%d@example.test", time.Now().UnixNano()))
	if err := userRepo.AnonymizeIfNoOwnedWorkspaces(context.Background(), deletedUserID); err != nil {
		t.Fatalf("anonymize account: %v", err)
	}
	workspace := &domain.Workspace{Name: "Deleted account workspace", OwnerID: deletedUserID}
	if err := workspaceRepo.Create(context.Background(), workspace, deletedUserID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create workspace for deleted account: %v, want not found", err)
	}
}

func TestFileRepositoryAttachmentLimitIsAtomic(t *testing.T) {
	tc := newTestContext(t)
	user, space, list := createWorkspaceTaskGraph(
		t, tc.DB, fmt.Sprintf("attachment-race-%d@example.test", time.Now().UnixNano()), "Attachment race",
	)
	var task persistence.Task
	if err := tc.DB.Where("list_id = ?", list.ID).First(&task).Error; err != nil {
		t.Fatal(err)
	}

	if err := tc.DB.Exec(
		"INSERT INTO files (kind, public_id, storage_key, user_id, task_id, name, content_type, size) "+
			"SELECT 'attachment', lpad(to_hex(n), 32, '0'), 'attachment/' || lpad(to_hex(n), 32, '0'), ?, ?, 'seed.txt', 'text/plain', 1 "+
			"FROM generate_series(1, ?) AS n",
		user.ID, task.ID, domain.MaxAttachmentsPerTask-1,
	).Error; err != nil {
		t.Fatal(err)
	}

	repo := repository.NewFileRepository(tc.DB)
	const attempts = 20
	start := make(chan struct{})
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			id := fmt.Sprintf("%032x", index+1000)
			taskID := task.ID
			_, err := repo.CreateAttachment(context.Background(), domain.NewFile{
				Kind: domain.FileKindAttachment, PublicID: id, StorageKey: "attachment/" + id,
				UserID: user.ID, TaskID: &taskID, Name: "concurrent.txt", ContentType: "text/plain", Size: 1,
			})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	created, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrTooManyAttachments):
			rejected++
		default:
			t.Fatalf("create attachment: %v", err)
		}
	}
	if created != 1 || rejected != attempts-1 {
		t.Fatalf("created=%d rejected=%d, want 1 and %d", created, rejected, attempts-1)
	}
	var count int64
	if err := tc.DB.Table("files").Where("task_id = ? AND kind = 'attachment'", task.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != domain.MaxAttachmentsPerTask {
		t.Fatalf("attachments = %d, want limit %d (space %d)", count, domain.MaxAttachmentsPerTask, space.ID)
	}
}

func TestSpaceRepository_GetAllPaginatesNewestFirstPerWorkspace(t *testing.T) {
	tc := newTestContext(t)
	owner := repoUser(t, tc, "space-owner")
	ws := repoWorkspace(t, tc, owner, "Spaces")
	other := repoWorkspace(t, tc, owner, "Other")
	repo := repository.NewSpaceRepository(tc.DB)

	for i := 1; i <= 5; i++ {
		s := &domain.Space{Name: fmt.Sprintf("S%d", i), WorkspaceID: ws.ID, OwnerID: owner, Currency: "USD"}
		if err := repo.Create(context.Background(), s); err != nil {
			t.Fatalf("create space: %v", err)
		}
	}
	if err := repo.Create(context.Background(), &domain.Space{Name: "elsewhere", WorkspaceID: other.ID, OwnerID: owner, Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	page1, total, err := repo.GetAll(context.Background(), ws.ID, 2, 0)
	if err != nil || total != 5 || len(page1) != 2 {
		t.Fatalf("page1 len=%d total=%d err=%v", len(page1), total, err)
	}
	if page1[0].Name != "S5" || page1[1].Name != "S4" {
		t.Fatalf("page1 = %s,%s, want newest first S5,S4", page1[0].Name, page1[1].Name)
	}
	page3, _, _ := repo.GetAll(context.Background(), ws.ID, 2, 4)
	if len(page3) != 1 || page3[0].Name != "S1" {
		t.Fatalf("last page = %+v", page3)
	}
	beyond, total, _ := repo.GetAll(context.Background(), ws.ID, 2, 100)
	if len(beyond) != 0 || total != 5 {
		t.Fatalf("beyond last page: len=%d total=%d", len(beyond), total)
	}
}

func TestSpaceRepository_DashboardLoadsListsStatusesInOrder(t *testing.T) {
	tc := newTestContext(t)
	owner := repoUser(t, tc, "dash-owner")
	ws := repoWorkspace(t, tc, owner, "Dash")
	spaces := repository.NewSpaceRepository(tc.DB)

	space := &domain.Space{Name: "D", WorkspaceID: ws.ID, OwnerID: owner, Currency: "USD"}
	if err := spaces.Create(context.Background(), space); err != nil {
		t.Fatal(err)
	}
	list := &domain.List{SpaceID: space.ID, Name: "L"}
	if err := spaces.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if err := tc.DB.Exec("DELETE FROM task_statuses WHERE list_id = ?", list.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, st := range []domain.TaskStatus{
		{SpaceID: space.ID, ListID: list.ID, Name: "third", Color: "#000003", Position: 3, Type: domain.StatusDone},
		{SpaceID: space.ID, ListID: list.ID, Name: "first", Color: "#000001", Position: 1, Type: domain.StatusTodo},
		{SpaceID: space.ID, ListID: list.ID, Name: "second", Color: "#000002", Position: 2, Type: domain.StatusInProgress},
	} {
		st := st
		if err := spaces.CreateStatus(context.Background(), &st); err != nil {
			t.Fatal(err)
		}
	}

	dash, err := spaces.GetDashboard(context.Background(), space.ID, 50)
	if err != nil || len(dash.Lists) != 1 {
		t.Fatalf("dashboard lists=%d err=%v", len(dash.Lists), err)
	}
	var names []string
	for _, st := range dash.Lists[0].Statuses {
		names = append(names, st.Name)
	}
	if fmt.Sprint(names) != "[first second third]" {
		t.Fatalf("statuses = %v, want ordered by position", names)
	}
	if _, err := spaces.GetDashboard(context.Background(), 999999, 50); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing dashboard err = %v", err)
	}
}

func TestFolderRepository_CRUDAndScoping(t *testing.T) {
	tc := newTestContext(t)
	owner := repoUser(t, tc, "folder-owner")
	ws := repoWorkspace(t, tc, owner, "Folders")
	spaces := repository.NewSpaceRepository(tc.DB)
	folders := repository.NewFolderRepository(tc.DB)

	a := &domain.Space{Name: "A", WorkspaceID: ws.ID, OwnerID: owner, Currency: "USD"}
	b := &domain.Space{Name: "B", WorkspaceID: ws.ID, OwnerID: owner, Currency: "USD"}
	for _, s := range []*domain.Space{a, b} {
		if err := spaces.Create(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}

	f := &domain.Folder{SpaceID: a.ID, Name: "Docs"}
	if err := folders.Create(context.Background(), f); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := folders.Create(context.Background(), &domain.Folder{SpaceID: b.ID, Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}

	inA, err := folders.GetAllBySpace(context.Background(), a.ID)
	if err != nil || len(inA) != 1 || inA[0].Name != "Docs" {
		t.Fatalf("space A folders = %+v err=%v", inA, err)
	}

	got, err := folders.GetByID(context.Background(), f.ID)
	if err != nil || got.Name != "Docs" {
		t.Fatalf("GetByID = %+v err=%v", got, err)
	}
	got.Name = "Renamed"
	if err := folders.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if again, _ := folders.GetByID(context.Background(), f.ID); again.Name != "Renamed" {
		t.Fatalf("name after update = %q", again.Name)
	}

	if err := folders.Delete(context.Background(), f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := folders.GetByID(context.Background(), f.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByID after delete err = %v", err)
	}
	if err := folders.Delete(context.Background(), f.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete err = %v, want not found", err)
	}
}

func TestStatusRepository_PositionsStayContiguous(t *testing.T) {
	tc := newTestContext(t)
	owner := repoUser(t, tc, "status-owner")
	ws := repoWorkspace(t, tc, owner, "Statuses")
	spaces := repository.NewSpaceRepository(tc.DB)
	statuses := repository.NewStatusRepository(tc.DB)

	space := &domain.Space{Name: "S", WorkspaceID: ws.ID, OwnerID: owner, Currency: "USD"}
	if err := spaces.Create(context.Background(), space); err != nil {
		t.Fatal(err)
	}
	list := &domain.List{SpaceID: space.ID, Name: "L"}
	if err := spaces.CreateList(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if err := tc.DB.Exec("DELETE FROM task_statuses WHERE list_id = ?", list.ID).Error; err != nil {
		t.Fatal(err)
	}

	mk := func(name string, pos int) *domain.TaskStatus {
		st := &domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: name, Color: "#112233", Position: pos, Type: domain.StatusTodo}
		if err := statuses.Create(context.Background(), st); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return st
	}
	order := func() string {
		t.Helper()
		all, err := statuses.GetAllByList(context.Background(), list.ID)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i, st := range all {
			if st.Position != i+1 {
				t.Fatalf("positions not contiguous: %s has position %d at index %d", st.Name, st.Position, i)
			}
			out = append(out, st.Name)
		}
		return fmt.Sprint(out)
	}

	a, b, c := mk("a", 1), mk("b", 2), mk("c", 3)
	if got := order(); got != "[a b c]" {
		t.Fatalf("initial = %s", got)
	}

	mk("x", 1)
	if got := order(); got != "[x a b c]" {
		t.Fatalf("after insert at 1 = %s", got)
	}

	if err := statuses.UpdatePosition(context.Background(), c, 1); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x a b]" {
		t.Fatalf("after move c->1 = %s", got)
	}

	if err := statuses.UpdatePosition(context.Background(), a, 99); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x b a]" {
		t.Fatalf("after move a->end = %s", got)
	}

	if err := statuses.Delete(context.Background(), list.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x a]" {
		t.Fatalf("after delete b = %s", got)
	}
	if err := statuses.Delete(context.Background(), list.ID, b.ID, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice err = %v, want not found", err)
	}
}
