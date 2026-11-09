package service

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

type ownerOnlyAdminManagement struct{ ownerID uint }

func (p ownerOnlyAdminManagement) HasWorkspacePermission(_ context.Context, userID, _ uint, code string) (bool, error) {
	if code == domain.PermAdminManage {
		return userID == p.ownerID, nil
	}
	return true, nil
}
func (ownerOnlyAdminManagement) HasSpacePermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (ownerOnlyAdminManagement) HasFolderPermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (ownerOnlyAdminManagement) HasListPermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}
func (ownerOnlyAdminManagement) HasTaskPermission(context.Context, uint, uint, string) (bool, error) {
	return false, nil
}

type memberRepoStub struct {
	workspaceRepositoryMock
	ownerID    uint
	targetRole string
	noMember   bool
	contact    bool
	added      *domain.Member
	updatedTo  uint
	removed    uint
	addErr     error
}

func (m *memberRepoStub) GetByID(_ context.Context, id uint) (*domain.Workspace, error) {
	w := &domain.Workspace{OwnerID: m.ownerID}
	w.ID = id
	return w, nil
}
func (m *memberRepoStub) GetMember(_ context.Context, workspaceID, userID uint) (*domain.Member, error) {
	if m.noMember {
		return nil, domain.ErrNotFound
	}
	return &domain.Member{WorkspaceID: workspaceID, UserID: userID, Role: domain.Role{Name: m.targetRole}}, nil
}
func (m *memberRepoStub) GetRoleByName(_ context.Context, workspaceID uint, name string) (*domain.Role, error) {
	r := &domain.Role{Name: name}
	r.ID = 42
	return r, nil
}
func (m *memberRepoStub) IsContact(_ context.Context, userID, otherID uint) (bool, error) {
	return m.contact, nil
}
func (m *memberRepoStub) AddMember(_ context.Context, member *domain.Member) error {
	m.added = member
	return m.addErr
}
func (m *memberRepoStub) UpdateMemberRole(_ context.Context, workspaceID, userID, roleID uint) error {
	m.updatedTo = roleID
	return nil
}
func (m *memberRepoStub) RemoveMember(_ context.Context, workspaceID, userID uint) error {
	m.removed = userID
	return nil
}

func TestAddMember_RejectsReservedAndUnknownRoles(t *testing.T) {
	for _, role := range []string{"Owner", "Root", ""} {
		repo := &memberRepoStub{contact: true}
		err := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: repo.ownerID}).AddMember(context.Background(), 1, 2, 3, role)
		if !errors.Is(err, ErrInvalidMemberRole) || repo.added != nil {
			t.Fatalf("role %q: err=%v added=%v", role, err, repo.added)
		}
	}
}

func TestAddMember_RequiresContact(t *testing.T) {
	repo := &memberRepoStub{contact: false}
	err := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: repo.ownerID}).AddMember(context.Background(), 1, 2, 3, "Member")
	if !errors.Is(err, ErrMemberNotContact) || repo.added != nil {
		t.Fatalf("err=%v added=%v", err, repo.added)
	}
}

func TestAddMember_AddsWithResolvedRole(t *testing.T) {
	repo := &memberRepoStub{contact: true, ownerID: 2}
	if err := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: repo.ownerID}).AddMember(context.Background(), 1, 2, 3, "Admin"); err != nil {
		t.Fatal(err)
	}
	if repo.added == nil || repo.added.UserID != 3 || repo.added.WorkspaceID != 1 || repo.added.RoleID != 42 {
		t.Fatalf("added=%+v", repo.added)
	}
}

func TestAddMember_PropagatesDuplicate(t *testing.T) {
	repo := &memberRepoStub{contact: true, addErr: domain.ErrAlreadyMember}
	if err := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: repo.ownerID}).AddMember(context.Background(), 1, 2, 3, "Member"); !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("err=%v", err)
	}
}

func TestOwnerCannotBeReRoledOrRemoved(t *testing.T) {
	repo := &memberRepoStub{ownerID: 9}
	svc := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: repo.ownerID})

	if err := svc.ChangeMemberRole(context.Background(), 1, 2, 9, "Guest"); !errors.Is(err, ErrOwnerImmutable) || repo.updatedTo != 0 {
		t.Fatalf("change owner role: err=%v updatedTo=%d", err, repo.updatedTo)
	}
	if err := svc.RemoveMember(context.Background(), 1, 2, 9); !errors.Is(err, ErrOwnerImmutable) || repo.removed != 0 {
		t.Fatalf("remove owner: err=%v removed=%d", err, repo.removed)
	}
}

func TestMissingMemberMapsToNotFound(t *testing.T) {
	if !errors.Is(ErrMemberTargetNotFound, domain.ErrNotFound) {
		t.Fatal("ErrMemberTargetNotFound must wrap domain.ErrNotFound so the API answers 404")
	}
}

func TestAdminRoleIsManagedByTheOwnerOnly(t *testing.T) {
	const owner, admin = uint(1), uint(2)
	tests := []struct {
		name       string
		actor      uint
		targetRole string
		act        func(svc WorkspaceService, actor uint) error
		wantErr    error
	}{
		{"admin cannot add an admin", admin, "", func(s WorkspaceService, a uint) error {
			return s.AddMember(context.Background(), 1, a, 5, "Admin")
		}, ErrAdminOwnerOnly},
		{"owner can add an admin", owner, "", func(s WorkspaceService, a uint) error {
			return s.AddMember(context.Background(), 1, a, 5, "Admin")
		}, nil},
		{"admin can add a guest", admin, "", func(s WorkspaceService, a uint) error {
			return s.AddMember(context.Background(), 1, a, 5, "Guest")
		}, nil},
		{"admin cannot promote to admin", admin, "Member", func(s WorkspaceService, a uint) error {
			return s.ChangeMemberRole(context.Background(), 1, a, 5, "Admin")
		}, ErrAdminOwnerOnly},
		{"admin cannot demote an admin", admin, "Admin", func(s WorkspaceService, a uint) error {
			return s.ChangeMemberRole(context.Background(), 1, a, 5, "Member")
		}, ErrAdminOwnerOnly},
		{"owner can demote an admin", owner, "Admin", func(s WorkspaceService, a uint) error {
			return s.ChangeMemberRole(context.Background(), 1, a, 5, "Member")
		}, nil},
		{"admin can re-role a member", admin, "Member", func(s WorkspaceService, a uint) error {
			return s.ChangeMemberRole(context.Background(), 1, a, 5, "Guest")
		}, nil},
		{"admin cannot remove an admin", admin, "Admin", func(s WorkspaceService, a uint) error {
			return s.RemoveMember(context.Background(), 1, a, 5)
		}, ErrAdminOwnerOnly},
		{"admin cannot remove themself through the member route", admin, "Admin", func(s WorkspaceService, a uint) error {
			return s.RemoveMember(context.Background(), 1, a, admin)
		}, ErrAdminOwnerOnly},
		{"owner can remove an admin", owner, "Admin", func(s WorkspaceService, a uint) error {
			return s.RemoveMember(context.Background(), 1, a, 5)
		}, nil},
		{"admin can remove a guest", admin, "Guest", func(s WorkspaceService, a uint) error {
			return s.RemoveMember(context.Background(), 1, a, 5)
		}, nil},
		{"admin can leave", admin, "Admin", func(s WorkspaceService, a uint) error {
			return s.Leave(context.Background(), 1, a)
		}, nil},
		{"owner cannot leave", owner, "Owner", func(s WorkspaceService, a uint) error {
			return s.Leave(context.Background(), 1, a)
		}, ErrOwnerImmutable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &memberRepoStub{contact: true, ownerID: owner, targetRole: tt.targetRole}
			svc := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: owner})
			err := tt.act(svc, tt.actor)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err=%v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && (repo.added != nil || repo.updatedTo != 0 || repo.removed != 0) {
				t.Fatalf("a rejected call changed data: added=%v updatedTo=%d removed=%d", repo.added, repo.updatedTo, repo.removed)
			}
		})
	}
}

func TestChangeAndRemoveReportMissingTargetAsNotFound(t *testing.T) {
	repo := &memberRepoStub{ownerID: 1, noMember: true}
	svc := NewWorkspaceService(repo, ownerOnlyAdminManagement{ownerID: 1})
	if err := svc.ChangeMemberRole(context.Background(), 1, 1, 5, "Guest"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("change: err=%v", err)
	}
	if err := svc.RemoveMember(context.Background(), 1, 1, 5); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("remove: err=%v", err)
	}
}
