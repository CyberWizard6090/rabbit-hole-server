ALTER TABLE IF EXISTS lists DROP CONSTRAINT IF EXISTS fk_lists_parent_status;

DROP TABLE IF EXISTS
    task_assignees,
    task_tags,
    tasks,
    tags,
    task_statuses,
    lists,
    folders,
    spaces,
    workspace_members,
    role_permissions,
    permissions,
    roles,
    workspaces,
    user_contacts,
    user_sessions,
    users;

DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TYPE IF EXISTS task_status_type;