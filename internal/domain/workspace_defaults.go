package domain

const (
	permissionWorkspaceRead = "workspace.read"
	permissionSpaceRead     = "space.read"
)

type DefaultRole struct {
	Name            string
	PermissionCodes []string
}

func DefaultRoles() []DefaultRole {
	return []DefaultRole{
		{
			Name: "Owner",
			PermissionCodes: []string{
				permissionWorkspaceRead,
				"workspace.update",
				"workspace.delete",
				permissionSpaceRead,
				"space.create",
				"space.update",
				"space.delete",
			},
		},
		{
			Name: "Admin",
			PermissionCodes: []string{
				permissionWorkspaceRead,
				permissionSpaceRead,
				"space.create",
				"space.update",
				"space.delete",
			},
		},
		{
			Name: "Member",
			PermissionCodes: []string{
				permissionWorkspaceRead,
				permissionSpaceRead,
				"task.read",
				"task.create",
				"task.update",
			},
		},
		{
			Name: "Guest",
			PermissionCodes: []string{
				permissionWorkspaceRead,
				permissionSpaceRead,
				"task.read",
			},
		},
	}
}
