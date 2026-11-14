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
	PermMemberManage    = "member.manage"
	PermAdminManage     = "member.manage_admins"
	PermFormRead        = "form.read"
	PermFormManage      = "form.manage"
	PermTemplateRead    = "template.read"
	PermTemplateManage  = "template.manage"
	PermFieldManage     = "customfield.manage"
	PermDocRead         = "doc.read"
	PermDocCreate       = "doc.create"
	PermDocUpdate       = "doc.update"
	PermDocDelete       = "doc.delete"
)

func AllPermissions() []string {
	return []string{
		PermWorkspaceRead, PermWorkspaceUpdate, PermWorkspaceDelete, PermMemberManage,
		PermSpaceRead, PermSpaceCreate, PermSpaceUpdate, PermSpaceDelete,
		PermTaskRead, PermTaskCreate, PermTaskUpdate, PermTaskDelete,
		PermAdminManage, PermFormRead, PermFormManage, PermTemplateRead, PermTemplateManage, PermFieldManage,
		PermDocRead, PermDocCreate, PermDocUpdate, PermDocDelete,
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
				PermMemberManage,
				PermSpaceRead,
				PermSpaceCreate,
				PermSpaceUpdate,
				PermSpaceDelete,
				PermTaskRead,
				PermTaskCreate,
				PermTaskUpdate,
				PermTaskDelete,
				PermAdminManage,
				PermFormRead,
				PermFormManage,
				PermTemplateRead,
				PermTemplateManage,
				PermFieldManage,
				PermDocRead,
				PermDocCreate,
				PermDocUpdate,
				PermDocDelete,
			},
		},
		{
			Name: "Admin",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermWorkspaceUpdate,
				PermMemberManage,
				PermSpaceRead,
				PermSpaceCreate,
				PermSpaceUpdate,
				PermSpaceDelete,
				PermTaskRead,
				PermTaskCreate,
				PermTaskUpdate,
				PermTaskDelete,
				PermFormRead,
				PermFormManage,
				PermTemplateRead,
				PermTemplateManage,
				PermFieldManage,
				PermDocRead,
				PermDocCreate,
				PermDocUpdate,
				PermDocDelete,
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
				PermFormRead,
				PermTemplateRead,
				PermDocRead,
				PermDocCreate,
				PermDocUpdate,
				PermDocDelete,
			},
		},
		{
			Name: "Guest",
			PermissionCodes: []string{
				PermWorkspaceRead,
				PermSpaceRead,
				PermTaskRead,
				PermDocRead,
			},
		},
	}
}

func AssignableRoles() []string { return []string{"Admin", "Member", "Guest"} }
