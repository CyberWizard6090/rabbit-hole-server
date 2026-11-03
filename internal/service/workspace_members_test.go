package service

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type memberRepoStub struct {
	workspaceRepositoryMock
	ownerID   uint
	contact   bool
	added     *domain.Member
	updatedTo uint
	removed   uint
	addErr    error
}

func (m *memberRepoStub) GetByID(id uint) (*domain.Workspace, error) {
	w := &domain.Workspace{OwnerID: m.ownerID}
	w.ID = id
	return w, nil
}
func (m *memberRepoStub) GetRoleByName(workspaceID uint, name string) (*domain.Role, error) {
	r := &domain.Role{Name: name}
	r.ID = 42
	return r, nil
}
func (m *memberRepoStub) IsContact(userID, otherID uint) (bool, error) { return m.contact, nil }
func (m *memberRepoStub) AddMember(member *domain.Member) error {
	m.added = member
	return m.addErr
}
func (m *memberRepoStub) UpdateMemberRole(workspaceID, userID, roleID uint) error {
	m.updatedTo = roleID
	return nil
}
func (m *memberRepoStub) RemoveMember(workspaceID, userID uint) error {
	m.removed = userID
	return nil
}

func TestAddMember_RejectsReservedAndUnknownRoles(t *testing.T) {
	for _, role := range []string{"Owner", "Root", ""} {
		repo := &memberRepoStub{contact: true}
		err := NewWorkspaceService(repo).AddMember(1, 2, 3, role)
		if !errors.Is(err, ErrInvalidMemberRole) || repo.added != nil {
			t.Fatalf("role %q: err=%v added=%v", role, err, repo.added)
		}
	}
}

func TestAddMember_RequiresContact(t *testing.T) {
	repo := &memberRepoStub{contact: false}
	err := NewWorkspaceService(repo).AddMember(1, 2, 3, "Member")
	if !errors.Is(err, ErrMemberNotContact) || repo.added != nil {
		t.Fatalf("err=%v added=%v", err, repo.added)
	}
}

func TestAddMember_AddsWithResolvedRole(t *testing.T) {
	repo := &memberRepoStub{contact: true}
	if err := NewWorkspaceService(repo).AddMember(1, 2, 3, "Admin"); err != nil {
		t.Fatal(err)
	}
	if repo.added == nil || repo.added.UserID != 3 || repo.added.WorkspaceID != 1 || repo.added.RoleID != 42 {
		t.Fatalf("added=%+v", repo.added)
	}
}

func TestAddMember_PropagatesDuplicate(t *testing.T) {
	repo := &memberRepoStub{contact: true, addErr: domain.ErrAlreadyMember}
	if err := NewWorkspaceService(repo).AddMember(1, 2, 3, "Member"); !errors.Is(err, domain.ErrAlreadyMember) {
		t.Fatalf("err=%v", err)
	}
}

func TestOwnerCannotBeReRoledOrRemoved(t *testing.T) {
	repo := &memberRepoStub{ownerID: 9}
	svc := NewWorkspaceService(repo)

	if err := svc.ChangeMemberRole(1, 9, "Guest"); !errors.Is(err, ErrOwnerImmutable) || repo.updatedTo != 0 {
		t.Fatalf("change owner role: err=%v updatedTo=%d", err, repo.updatedTo)
	}
	if err := svc.RemoveMember(1, 9); !errors.Is(err, ErrOwnerImmutable) || repo.removed != 0 {
		t.Fatalf("remove owner: err=%v removed=%d", err, repo.removed)
	}
}

func TestMissingMemberMapsToNotFound(t *testing.T) {
	if !errors.Is(ErrMemberTargetNotFound, gorm.ErrRecordNotFound) {
		t.Fatal("ErrMemberTargetNotFound must wrap gorm.ErrRecordNotFound so the API answers 404")
	}
}
