package repository

import (
	"errors"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

func mapErr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return err
}

func modelToDomain(m gorm.Model) domain.Model {
	out := domain.Model{ID: m.ID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	if m.DeletedAt.Valid {
		deleted := m.DeletedAt.Time
		out.DeletedAt = &deleted
	}
	return out
}

func modelFromDomain(m domain.Model) gorm.Model {
	out := gorm.Model{ID: m.ID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	if m.DeletedAt != nil {
		out.DeletedAt = gorm.DeletedAt{Time: *m.DeletedAt, Valid: true}
	}
	return out
}

func mapSlice[S, D any](in []S, convert func(S) D) []D {
	if in == nil {
		return nil
	}
	out := make([]D, len(in))
	for i, item := range in {
		out[i] = convert(item)
	}
	return out
}

func userToDomain(u persistence.User) domain.User {
	return domain.User{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, Username: u.Username,
		FirstName: u.FirstName, LastName: u.LastName, AvatarURL: u.AvatarURL, Bio: u.Bio,
		TimeZone: u.TimeZone, TokenVersion: u.TokenVersion, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func userFromDomain(u domain.User) persistence.User {
	return persistence.User{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, Username: u.Username,
		FirstName: u.FirstName, LastName: u.LastName, AvatarURL: u.AvatarURL, Bio: u.Bio,
		TimeZone: u.TimeZone, TokenVersion: u.TokenVersion, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func sessionToDomain(s persistence.UserSession) domain.UserSession {
	return domain.UserSession{ID: s.ID, UserID: s.UserID, TokenHash: s.TokenHash, ExpiresAt: s.ExpiresAt, CreatedAt: s.CreatedAt}
}

func sessionFromDomain(s domain.UserSession) persistence.UserSession {
	return persistence.UserSession{ID: s.ID, UserID: s.UserID, TokenHash: s.TokenHash, ExpiresAt: s.ExpiresAt, CreatedAt: s.CreatedAt}
}

func workspaceToDomain(w persistence.Workspace) domain.Workspace {
	return domain.Workspace{Model: modelToDomain(w.Model), Name: w.Name, OwnerID: w.OwnerID, Description: w.Description}
}

func workspaceFromDomain(w domain.Workspace) persistence.Workspace {
	return persistence.Workspace{Model: modelFromDomain(w.Model), Name: w.Name, OwnerID: w.OwnerID, Description: w.Description}
}

func spaceToDomain(s persistence.Space) domain.Space {
	return domain.Space{
		Model: modelToDomain(s.Model), Name: s.Name, WorkspaceID: s.WorkspaceID, Currency: s.Currency, OwnerID: s.OwnerID,
		Lists: mapSlice(s.Lists, listToDomain),
		Tasks: mapSlice(s.Tasks, taskToDomain),
		Tags:  mapSlice(s.Tags, tagToDomain),
	}
}

func spaceFromDomain(s domain.Space) persistence.Space {
	return persistence.Space{
		Model: modelFromDomain(s.Model), Name: s.Name, WorkspaceID: s.WorkspaceID, Currency: s.Currency, OwnerID: s.OwnerID,
		Lists: mapSlice(s.Lists, listFromDomain),
		Tasks: mapSlice(s.Tasks, taskFromDomain),
		Tags:  mapSlice(s.Tags, tagFromDomain),
	}
}

func folderToDomain(f persistence.Folder) domain.Folder {
	return domain.Folder{Model: modelToDomain(f.Model), Name: f.Name, SpaceID: f.SpaceID, ParentID: f.ParentID}
}

func folderFromDomain(f domain.Folder) persistence.Folder {
	return persistence.Folder{Model: modelFromDomain(f.Model), Name: f.Name, SpaceID: f.SpaceID, ParentID: f.ParentID}
}

func listToDomain(l persistence.List) domain.List {
	return domain.List{
		Model: modelToDomain(l.Model), Name: l.Name, SpaceID: l.SpaceID, FolderID: l.FolderID,
		ParentStatusID: l.ParentStatusID,
		Statuses:       mapSlice(l.Statuses, statusToDomain),
		Tasks:          mapSlice(l.Tasks, taskToDomain),
	}
}

func listFromDomain(l domain.List) persistence.List {
	return persistence.List{
		Model: modelFromDomain(l.Model), Name: l.Name, SpaceID: l.SpaceID, FolderID: l.FolderID,
		ParentStatusID: l.ParentStatusID,
		Statuses:       mapSlice(l.Statuses, statusFromDomain),
		Tasks:          mapSlice(l.Tasks, taskFromDomain),
	}
}

func statusToDomain(s persistence.TaskStatus) domain.TaskStatus {
	return domain.TaskStatus{
		Model: modelToDomain(s.Model), SpaceID: s.SpaceID, ListID: s.ListID, Name: s.Name,
		Color: s.Color, Position: s.Position, Type: domain.StatusType(s.Type),
	}
}

func statusFromDomain(s domain.TaskStatus) persistence.TaskStatus {
	return persistence.TaskStatus{
		Model: modelFromDomain(s.Model), SpaceID: s.SpaceID, ListID: s.ListID, Name: s.Name,
		Color: s.Color, Position: s.Position, Type: persistence.StatusType(s.Type),
	}
}

func tagToDomain(t persistence.Tag) domain.Tag {
	return domain.Tag{ID: t.ID, SpaceID: t.SpaceID, Name: t.Name, Color: t.Color}
}

func tagFromDomain(t domain.Tag) persistence.Tag {
	return persistence.Tag{ID: t.ID, SpaceID: t.SpaceID, Name: t.Name, Color: t.Color}
}

func taskToDomain(t persistence.Task) domain.Task {
	return domain.Task{
		Model: modelToDomain(t.Model), SpaceID: t.SpaceID, ListID: t.ListID, StatusID: t.StatusID,
		ParentID: t.ParentID, Title: t.Title, Description: t.Description, Priority: t.Priority,
		UserID: t.UserID, StartDate: t.StartDate, DueDate: t.DueDate,
		TimeEstimate: t.TimeEstimate, TimeSpent: t.TimeSpent,
		SortStatusPosition: t.SortStatusPosition, SortAssignee: t.SortAssignee,
		Assignees: mapSlice(t.Assignees, userToDomain),
		Tags:      mapSlice(t.Tags, tagToDomain),
	}
}

func taskFromDomain(t domain.Task) persistence.Task {
	return persistence.Task{
		Model: modelFromDomain(t.Model), SpaceID: t.SpaceID, ListID: t.ListID, StatusID: t.StatusID,
		ParentID: t.ParentID, Title: t.Title, Description: t.Description, Priority: t.Priority,
		UserID: t.UserID, StartDate: t.StartDate, DueDate: t.DueDate,
		TimeEstimate: t.TimeEstimate, TimeSpent: t.TimeSpent,
		Assignees: mapSlice(t.Assignees, userFromDomain),
		Tags:      mapSlice(t.Tags, tagFromDomain),
	}
}

func roleToDomain(r persistence.Role) domain.Role {
	return domain.Role{Model: modelToDomain(r.Model), WorkspaceID: r.WorkspaceID, Name: r.Name}
}

func roleFromDomain(r domain.Role) persistence.Role {
	return persistence.Role{Model: modelFromDomain(r.Model), WorkspaceID: r.WorkspaceID, Name: r.Name}
}

func memberToDomain(m persistence.Member) domain.Member {
	return domain.Member{
		Model: modelToDomain(m.Model), WorkspaceID: m.WorkspaceID, UserID: m.UserID,
		RoleID: m.RoleID, Role: roleToDomain(m.Role),
	}
}

func memberFromDomain(m domain.Member) persistence.Member {
	return persistence.Member{
		Model: modelFromDomain(m.Model), WorkspaceID: m.WorkspaceID, UserID: m.UserID,
		RoleID: m.RoleID, Role: roleFromDomain(m.Role),
	}
}
