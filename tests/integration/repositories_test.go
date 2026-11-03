package integration

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
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
	if err := repository.NewWorkspaceRepository(tc.DB).Create(ws, ownerID); err != nil {
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

	var member domain.Member
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
	repoWorkspace(t, tc, alice, "Alice A")
	repoWorkspace(t, tc, alice, "Alice B")
	repoWorkspace(t, tc, bob, "Bob")

	repo := repository.NewWorkspaceRepository(tc.DB)
	got, err := repo.GetAllForUser(alice)
	if err != nil || len(got) != 2 {
		t.Fatalf("alice workspaces = %d err=%v, want 2", len(got), err)
	}
	got, err = repo.GetAllForUser(bob)
	if err != nil || len(got) != 1 || got[0].Name != "Bob" {
		t.Fatalf("bob workspaces = %+v err=%v", got, err)
	}
	if _, err := repo.GetByID(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetByID(missing) err = %v, want not found", err)
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
		if err := repo.Create(s); err != nil {
			t.Fatalf("create space: %v", err)
		}
	}
	if err := repo.Create(&domain.Space{Name: "elsewhere", WorkspaceID: other.ID, OwnerID: owner, Currency: "USD"}); err != nil {
		t.Fatal(err)
	}

	page1, total, err := repo.GetAll(ws.ID, 2, 0)
	if err != nil || total != 5 || len(page1) != 2 {
		t.Fatalf("page1 len=%d total=%d err=%v", len(page1), total, err)
	}
	if page1[0].Name != "S5" || page1[1].Name != "S4" {
		t.Fatalf("page1 = %s,%s, want newest first S5,S4", page1[0].Name, page1[1].Name)
	}
	page3, _, _ := repo.GetAll(ws.ID, 2, 4)
	if len(page3) != 1 || page3[0].Name != "S1" {
		t.Fatalf("last page = %+v", page3)
	}
	beyond, total, _ := repo.GetAll(ws.ID, 2, 100)
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
	if err := spaces.Create(space); err != nil {
		t.Fatal(err)
	}
	list := &domain.List{SpaceID: space.ID, Name: "L"}
	if err := spaces.CreateList(list); err != nil {
		t.Fatal(err)
	}
	for _, st := range []domain.TaskStatus{
		{SpaceID: space.ID, ListID: list.ID, Name: "third", Color: "#000003", Position: 3, Type: domain.StatusDone},
		{SpaceID: space.ID, ListID: list.ID, Name: "first", Color: "#000001", Position: 1, Type: domain.StatusTodo},
		{SpaceID: space.ID, ListID: list.ID, Name: "second", Color: "#000002", Position: 2, Type: domain.StatusInProgress},
	} {
		st := st
		if err := spaces.CreateStatus(&st); err != nil {
			t.Fatal(err)
		}
	}

	dash, err := spaces.GetDashboard(space.ID)
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
	if _, err := spaces.GetDashboard(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
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
		if err := spaces.Create(s); err != nil {
			t.Fatal(err)
		}
	}

	f := &domain.Folder{SpaceID: a.ID, Name: "Docs"}
	if err := folders.Create(f); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := folders.Create(&domain.Folder{SpaceID: b.ID, Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}

	inA, err := folders.GetAllBySpace(a.ID)
	if err != nil || len(inA) != 1 || inA[0].Name != "Docs" {
		t.Fatalf("space A folders = %+v err=%v", inA, err)
	}

	got, err := folders.GetByID(f.ID)
	if err != nil || got.Name != "Docs" {
		t.Fatalf("GetByID = %+v err=%v", got, err)
	}
	got.Name = "Renamed"
	if err := folders.Update(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := folders.GetByID(f.ID); again.Name != "Renamed" {
		t.Fatalf("name after update = %q", again.Name)
	}

	if err := folders.Delete(f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := folders.GetByID(f.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("GetByID after delete err = %v", err)
	}
	if err := folders.Delete(f.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
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
	if err := spaces.Create(space); err != nil {
		t.Fatal(err)
	}
	list := &domain.List{SpaceID: space.ID, Name: "L"}
	if err := spaces.CreateList(list); err != nil {
		t.Fatal(err)
	}

	mk := func(name string, pos int) *domain.TaskStatus {
		st := &domain.TaskStatus{SpaceID: space.ID, ListID: list.ID, Name: name, Color: "#112233", Position: pos, Type: domain.StatusTodo}
		if err := statuses.Create(st); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return st
	}
	order := func() string {
		t.Helper()
		all, err := statuses.GetAllByList(list.ID)
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

	if err := statuses.UpdatePosition(c, 1); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x a b]" {
		t.Fatalf("after move c->1 = %s", got)
	}

	if err := statuses.UpdatePosition(a, 99); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x b a]" {
		t.Fatalf("after move a->end = %s", got)
	}

	if err := statuses.Delete(list.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "[c x a]" {
		t.Fatalf("after delete b = %s", got)
	}
	if err := statuses.Delete(list.ID, b.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleting twice err = %v, want not found", err)
	}
}
