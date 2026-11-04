package domain

import "context"

type PermissionChecker interface {
	HasWorkspacePermission(ctx context.Context, userID, workspaceID uint, code string) (bool, error)
	HasSpacePermission(ctx context.Context, userID, spaceID uint, code string) (bool, error)
	HasFolderPermission(ctx context.Context, userID, folderID uint, code string) (bool, error)
	HasListPermission(ctx context.Context, userID, listID uint, code string) (bool, error)
	HasTaskPermission(ctx context.Context, userID, taskID uint, code string) (bool, error)
}
