-- SQLBook: Code
CREATE TYPE task_status_type AS ENUM ('todo', 'in_progress', 'done');

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    username TEXT,
    first_name TEXT,
    last_name TEXT,
    avatar_url TEXT,
    bio TEXT,
    time_zone TEXT NOT NULL DEFAULT 'UTC',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_users_email ON users (lower(email));
CREATE UNIQUE INDEX uq_users_username ON users (username);
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_users_username_trgm ON users USING GIN (username gin_trgm_ops);
CREATE INDEX idx_users_email_trgm ON users USING GIN (email gin_trgm_ops);
CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE user_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_user_sessions_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_user_sessions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_user_sessions_user_id ON user_sessions (user_id);

CREATE TABLE user_contacts (
    user_id BIGINT NOT NULL,
    contact_id BIGINT NOT NULL,
    PRIMARY KEY (user_id, contact_id),
    CONSTRAINT fk_user_contacts_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_user_contacts_contacts FOREIGN KEY (contact_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT chk_user_contacts_not_self CHECK (user_id <> contact_id)
);

CREATE TABLE workspaces (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    name VARCHAR(255) NOT NULL,
    owner_id BIGINT NOT NULL,
    description TEXT,
    CONSTRAINT chk_workspaces_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_workspaces_owner FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE RESTRICT
);
CREATE INDEX idx_workspaces_deleted_at ON workspaces (deleted_at);
CREATE TRIGGER trg_workspaces_updated_at BEFORE UPDATE ON workspaces
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE roles (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    workspace_id BIGINT NOT NULL,
    name VARCHAR(100) NOT NULL,
    CONSTRAINT chk_roles_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_workspaces_roles FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX idx_roles_deleted_at ON roles (deleted_at);
CREATE INDEX idx_roles_workspace_id ON roles (workspace_id);
CREATE UNIQUE INDEX idx_roles_workspace_name ON roles (workspace_id, name) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_roles_updated_at BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE permissions (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    code VARCHAR(100) NOT NULL,
    description VARCHAR(255)
);
CREATE UNIQUE INDEX idx_permissions_code ON permissions (code) WHERE deleted_at IS NULL;
CREATE INDEX idx_permissions_deleted_at ON permissions (deleted_at);
CREATE TRIGGER trg_permissions_updated_at BEFORE UPDATE ON permissions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE role_permissions (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    role_id BIGINT NOT NULL,
    permission_id BIGINT NOT NULL,
    CONSTRAINT fk_roles_permissions FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE,
    CONSTRAINT fk_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES permissions (id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX idx_role_permission ON role_permissions (role_id, permission_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_role_permissions_deleted_at ON role_permissions (deleted_at);
CREATE INDEX idx_role_permissions_permission_id ON role_permissions (permission_id);
CREATE INDEX idx_role_permissions_role_id ON role_permissions (role_id);
CREATE TRIGGER trg_role_permissions_updated_at BEFORE UPDATE ON role_permissions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE workspace_members (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    workspace_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role_id BIGINT NOT NULL,
    CONSTRAINT fk_workspaces_members FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_workspace_members_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_workspace_members_role FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE RESTRICT
);
CREATE INDEX idx_workspace_members_deleted_at ON workspace_members (deleted_at);
CREATE INDEX idx_workspace_members_role_id ON workspace_members (role_id);
CREATE INDEX idx_workspace_members_user_id ON workspace_members (user_id);
CREATE INDEX idx_workspace_members_workspace_id ON workspace_members (workspace_id);
CREATE UNIQUE INDEX idx_workspace_user ON workspace_members (workspace_id, user_id) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_workspace_members_updated_at BEFORE UPDATE ON workspace_members
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE spaces (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    name TEXT NOT NULL,
    workspace_id BIGINT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    owner_id BIGINT NOT NULL,
    CONSTRAINT chk_spaces_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_workspaces_spaces FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_spaces_owner FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE RESTRICT
);
CREATE INDEX idx_spaces_deleted_at ON spaces (deleted_at);
CREATE INDEX idx_spaces_workspace_id ON spaces (workspace_id);
CREATE TRIGGER trg_spaces_updated_at BEFORE UPDATE ON spaces
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE folders (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    name VARCHAR(255) NOT NULL,
    space_id BIGINT NOT NULL,
    parent_id BIGINT,
    CONSTRAINT chk_folders_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_spaces_folders FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_folders_children FOREIGN KEY (parent_id) REFERENCES folders (id) ON DELETE CASCADE,
    CONSTRAINT chk_folders_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_folders_deleted_at ON folders (deleted_at);
CREATE INDEX idx_folders_parent_id ON folders (parent_id);
CREATE INDEX idx_folders_space_id ON folders (space_id);
CREATE TRIGGER trg_folders_updated_at BEFORE UPDATE ON folders
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE lists (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    name TEXT NOT NULL,
    space_id BIGINT NOT NULL,
    folder_id BIGINT,
    parent_status_id BIGINT,
    CONSTRAINT chk_lists_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_spaces_lists FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_folders_lists FOREIGN KEY (folder_id) REFERENCES folders (id) ON DELETE CASCADE
);
CREATE INDEX idx_lists_deleted_at ON lists (deleted_at);
CREATE INDEX idx_lists_folder_id ON lists (folder_id);
CREATE INDEX idx_lists_parent_status_id ON lists (parent_status_id);
CREATE INDEX idx_lists_space_id ON lists (space_id);
CREATE TRIGGER trg_lists_updated_at BEFORE UPDATE ON lists
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE task_statuses (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    space_id BIGINT NOT NULL,
    list_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    color VARCHAR(7) NOT NULL,
    position INTEGER NOT NULL,
    type task_status_type NOT NULL,
    CONSTRAINT chk_task_statuses_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_spaces_statuses FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_lists_statuses FOREIGN KEY (list_id) REFERENCES lists (id) ON DELETE CASCADE,
    CONSTRAINT chk_task_statuses_color CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    CONSTRAINT chk_task_statuses_position CHECK (position >= 1)
);
CREATE INDEX idx_task_statuses_deleted_at ON task_statuses (deleted_at);
CREATE INDEX idx_task_statuses_list_position ON task_statuses (list_id, position);
CREATE INDEX idx_task_statuses_space_id ON task_statuses (space_id);
ALTER TABLE lists ADD CONSTRAINT fk_lists_parent_status
    FOREIGN KEY (parent_status_id) REFERENCES task_statuses (id) ON DELETE SET NULL;
CREATE TRIGGER trg_task_statuses_updated_at BEFORE UPDATE ON task_statuses
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE tags (
    id BIGSERIAL PRIMARY KEY,
    space_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    color VARCHAR(7) NOT NULL,
    CONSTRAINT chk_tags_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_spaces_tags FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT chk_tags_color CHECK (color ~ '^#[0-9A-Fa-f]{6}$')
);
CREATE INDEX idx_tags_space_id ON tags (space_id);
CREATE UNIQUE INDEX idx_space_tag_name ON tags (space_id, LOWER(name));

CREATE TABLE tasks (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    space_id BIGINT NOT NULL,
    list_id BIGINT NOT NULL,
    status_id BIGINT NOT NULL,
    parent_id BIGINT,
    title TEXT NOT NULL,
    description TEXT,
    priority INTEGER NOT NULL DEFAULT 2,
    user_id BIGINT NOT NULL,
    start_date TIMESTAMPTZ,
    due_date TIMESTAMPTZ,
    time_estimate INTEGER NOT NULL DEFAULT 0,
    time_spent INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT chk_tasks_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT fk_spaces_tasks FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_lists_tasks FOREIGN KEY (list_id) REFERENCES lists (id) ON DELETE NO ACTION,
    CONSTRAINT fk_tasks_status FOREIGN KEY (status_id) REFERENCES task_statuses (id) ON DELETE NO ACTION,
    CONSTRAINT fk_tasks_parent FOREIGN KEY (parent_id) REFERENCES tasks (id) ON DELETE SET NULL,
    CONSTRAINT fk_tasks_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_tasks_priority CHECK (priority BETWEEN 1 AND 5),
    CONSTRAINT chk_tasks_time_estimate CHECK (time_estimate >= 0),
    CONSTRAINT chk_tasks_time_spent CHECK (time_spent >= 0),
    CONSTRAINT chk_tasks_date_order CHECK (start_date IS NULL OR due_date IS NULL OR due_date >= start_date),
    CONSTRAINT chk_tasks_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_tasks_deleted_at ON tasks (deleted_at);
CREATE INDEX idx_tasks_list_created_at ON tasks (list_id, created_at DESC);
CREATE INDEX idx_tasks_parent_id ON tasks (parent_id);
CREATE INDEX idx_tasks_space_id ON tasks (space_id);
CREATE INDEX idx_tasks_status_id ON tasks (status_id);
CREATE INDEX idx_tasks_user_id ON tasks (user_id);
CREATE TRIGGER trg_tasks_updated_at BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
COMMENT ON COLUMN tasks.priority IS 'Priority scale: integer values 1 through 5.';
COMMENT ON COLUMN tasks.time_estimate IS 'Estimated duration as a nonnegative integer; the unit is not defined in application code.';
COMMENT ON COLUMN tasks.time_spent IS 'Spent duration as a nonnegative integer; the unit is not defined in application code.';

CREATE TABLE task_tags (
    task_id BIGINT NOT NULL,
    tag_id BIGINT NOT NULL,
    PRIMARY KEY (task_id, tag_id),
    CONSTRAINT fk_task_tags_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_tags_tag FOREIGN KEY (tag_id) REFERENCES tags (id) ON DELETE CASCADE
);
CREATE INDEX idx_task_tags_tag_id ON task_tags (tag_id);

CREATE TABLE task_assignees (
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    PRIMARY KEY (task_id, user_id),
    CONSTRAINT fk_task_assignees_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_assignees_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_task_assignees_user_id ON task_assignees (user_id);