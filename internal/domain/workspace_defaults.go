package domain

const (
	PermWorkspaceRead   = "workspace.read"
	PermWorkspaceUpdate = "workspace.update"
	PermWorkspaceDelete = "workspace.delete"
	PermSpaceRead       = "space.read"
	PermSpaceCreate     = "space.create"
	PermSpaceUpdate     = "space.update"
	PermSpaceDelete     = "space.delete"
	PermTaskRead        = "task.read"
	PermTaskCreate      = "task.create"
	PermTaskUpdate      = "task.update"
	PermTaskDelete      = "task.delete"
)

func AllPermissions() []string {
	return []string{
		PermWorkspaceRead, PermWorkspaceUpdate, PermWorkspaceDelete,
		PermSpaceRead, PermSpaceCreate, PermSpaceUpdate, PermSpaceDelete,
		PermTaskRead, PermTaskCreate, PermTaskUpdate, PermTaskDelete,
	}
}

type DefaultRole struct {
	Name            string
	PermissionCodes []string
}

func DefaultRoles() []DefaultRole {
	return []DefaultRole{
		{
			Name: "Owner",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermWorkspaceUpdate,
				PermWorkspaceDelete,
				PermSpaceRead,
				PermSpaceCreate,
				PermSpaceUpdate,
				PermSpaceDelete,
				PermTaskRead,
				PermTaskCreate,
				PermTaskUpdate,
				PermTaskDelete,
			},
		},
		{
			Name: "Admin",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermSpaceRead,
				PermSpaceCreate,
				PermSpaceUpdate,
				PermSpaceDelete,
				PermTaskRead,
				PermTaskCreate,
				PermTaskUpdate,
				PermTaskDelete,
			},
		},
		{
			Name: "Member",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermSpaceRead,
				PermTaskRead,
				PermTaskCreate,
				PermTaskUpdate,
				PermTaskDelete,
			},
		},
		{
			Name: "Guest",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermSpaceRead,
				PermTaskRead,
			},
		},
	}
}
