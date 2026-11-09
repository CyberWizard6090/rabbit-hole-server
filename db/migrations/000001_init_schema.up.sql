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
    token_version INTEGER NOT NULL DEFAULT 0,
    email_verified_at TIMESTAMPTZ,
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
    family_id TEXT NOT NULL DEFAULT '',
    rotated_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_user_sessions_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_user_sessions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_user_sessions_user_id ON user_sessions (user_id);
CREATE INDEX idx_user_sessions_family_id ON user_sessions (family_id);

CREATE TABLE email_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    payload TEXT,
    CONSTRAINT chk_email_tokens_kind CHECK (kind IN ('verify_email', 'reset_password', 'change_email')),
    CONSTRAINT chk_email_tokens_payload CHECK ((kind = 'change_email' AND (payload IS NOT NULL OR used_at IS NOT NULL)) OR (kind <> 'change_email' AND payload IS NULL)),
    CONSTRAINT uq_email_tokens_token_hash UNIQUE (token_hash),
    CONSTRAINT fk_email_tokens_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_email_tokens_user_kind ON email_tokens (user_id, kind, created_at DESC);

CREATE TABLE user_contacts (
    user_id BIGINT NOT NULL,
    contact_id BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    responded_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, contact_id),
    CONSTRAINT chk_user_contacts_status CHECK (status IN ('pending', 'accepted', 'declined')),
    CONSTRAINT fk_user_contacts_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_user_contacts_contacts FOREIGN KEY (contact_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT chk_user_contacts_not_self CHECK (user_id <> contact_id)
);
CREATE UNIQUE INDEX uq_user_contacts_pair ON user_contacts (LEAST(user_id, contact_id), GREATEST(user_id, contact_id));
CREATE INDEX idx_user_contacts_contact_status ON user_contacts (contact_id, status);

CREATE TABLE workspaces (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    name VARCHAR(255) NOT NULL,
    owner_id BIGINT NOT NULL,
    description TEXT,
    avatar_url TEXT NOT NULL DEFAULT '',
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
    CONSTRAINT fk_folders_lists FOREIGN KEY (folder_id) REFERENCES folders (id) ON DELETE CASCADE,
    CONSTRAINT uq_lists_space_id_id UNIQUE (space_id, id)
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
    CONSTRAINT fk_lists_statuses FOREIGN KEY (space_id, list_id) REFERENCES lists (space_id, id) ON DELETE CASCADE,
    CONSTRAINT uq_task_statuses_list_id_id UNIQUE (list_id, id),
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
    position BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_tasks_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT fk_spaces_tasks FOREIGN KEY (space_id) REFERENCES spaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_lists_tasks FOREIGN KEY (space_id, list_id) REFERENCES lists (space_id, id) ON DELETE NO ACTION,
    CONSTRAINT fk_tasks_status FOREIGN KEY (list_id, status_id) REFERENCES task_statuses (list_id, id) ON DELETE NO ACTION,
    CONSTRAINT fk_tasks_parent FOREIGN KEY (parent_id) REFERENCES tasks (id) ON DELETE SET NULL,
    CONSTRAINT fk_tasks_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_tasks_priority CHECK (priority BETWEEN 1 AND 5),
    CONSTRAINT chk_tasks_time_estimate CHECK (time_estimate >= 0),
    CONSTRAINT chk_tasks_time_spent CHECK (time_spent >= 0),
    CONSTRAINT chk_tasks_date_order CHECK (start_date IS NULL OR due_date IS NULL OR due_date >= start_date),
    CONSTRAINT chk_tasks_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_tasks_deleted_at ON tasks (deleted_at);
CREATE INDEX idx_tasks_list_keyset ON tasks (list_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_list_id_id ON tasks (list_id, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_list_priority_id ON tasks (list_id, priority, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_list_due_date_id ON tasks (list_id, due_date, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_list_position_id ON tasks (list_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_title_trgm ON tasks USING GIN (title gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_description_trgm ON tasks USING GIN (description gin_trgm_ops) WHERE deleted_at IS NULL;
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

CREATE TABLE task_comments (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    body TEXT NOT NULL,
    CONSTRAINT chk_task_comments_body_not_blank CHECK (btrim(body) <> ''),
    CONSTRAINT fk_task_comments_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_comments_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
);
CREATE INDEX idx_task_comments_deleted_at ON task_comments (deleted_at);
CREATE INDEX idx_task_comments_task_id_id ON task_comments (task_id, id);
CREATE INDEX idx_task_comments_body_trgm ON task_comments USING GIN (body gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_task_comments_updated_at BEFORE UPDATE ON task_comments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE task_comment_mentions (
    comment_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    PRIMARY KEY (comment_id, user_id),
    CONSTRAINT fk_task_comment_mentions_comment FOREIGN KEY (comment_id) REFERENCES task_comments (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_comment_mentions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_task_comment_mentions_user_id ON task_comment_mentions (user_id);

CREATE TABLE task_activities (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id BIGINT NOT NULL,
    user_id BIGINT,
    actor_type TEXT NOT NULL DEFAULT 'user',
    actor_id BIGINT,
    actor_label TEXT,
    action TEXT NOT NULL,
    field TEXT,
    old_value JSONB,
    new_value JSONB,
    CONSTRAINT chk_task_activities_actor_type CHECK (actor_type IN ('user', 'form', 'automation', 'integration', 'system')),
    CONSTRAINT chk_task_activities_actor CHECK (
        (actor_type = 'user' AND user_id IS NOT NULL AND (actor_id IS NULL OR actor_id = user_id))
        OR (actor_type <> 'user' AND user_id IS NULL)
    ),
    CONSTRAINT fk_task_activities_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_activities_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
);
CREATE INDEX idx_task_activities_task_id_id ON task_activities (task_id, id DESC);

CREATE TABLE files (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind TEXT NOT NULL,
    public_id TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    workspace_id BIGINT,
    task_id BIGINT,
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL,
    CONSTRAINT chk_files_kind CHECK (kind IN ('attachment', 'image', 'avatar', 'workspace_avatar')),
    CONSTRAINT chk_files_size CHECK (size > 0),
    CONSTRAINT chk_files_attachment_task CHECK (kind <> 'attachment' OR task_id IS NOT NULL),
    CONSTRAINT chk_files_workspace_avatar CHECK (kind <> 'workspace_avatar' OR (workspace_id IS NOT NULL AND task_id IS NULL)),
    CONSTRAINT fk_files_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT fk_files_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_files_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_files_public_id ON files (public_id);
CREATE UNIQUE INDEX uq_files_storage_key ON files (storage_key);
CREATE INDEX idx_files_task_id_id ON files (task_id, id) WHERE kind = 'attachment';
CREATE INDEX idx_files_task_id ON files (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_files_user_avatar ON files (user_id) WHERE kind = 'avatar';
CREATE UNIQUE INDEX uq_files_workspace_avatar ON files (workspace_id) WHERE kind = 'workspace_avatar';

CREATE TABLE task_checklists (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    position BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_task_checklists_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_task_checklists_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE
);
CREATE INDEX idx_task_checklists_task_position ON task_checklists (task_id, position, id);
CREATE TRIGGER trg_task_checklists_updated_at BEFORE UPDATE ON task_checklists
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE task_checklist_items (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    checklist_id BIGINT NOT NULL,
    text TEXT NOT NULL,
    resolved BOOLEAN NOT NULL DEFAULT false,
    resolved_at TIMESTAMPTZ,
    resolved_by BIGINT,
    assignee_id BIGINT,
    position BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_task_checklist_items_text_not_blank CHECK (btrim(text) <> ''),
    CONSTRAINT chk_task_checklist_items_resolution CHECK (resolved OR (resolved_at IS NULL AND resolved_by IS NULL)),
    CONSTRAINT fk_task_checklist_items_checklist FOREIGN KEY (checklist_id) REFERENCES task_checklists (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_checklist_items_resolved_by FOREIGN KEY (resolved_by) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT fk_task_checklist_items_assignee FOREIGN KEY (assignee_id) REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX idx_task_checklist_items_checklist_position ON task_checklist_items (checklist_id, position, id);
CREATE INDEX idx_task_checklist_items_assignee ON task_checklist_items (assignee_id);
CREATE INDEX idx_task_checklist_items_text_trgm ON task_checklist_items USING GIN (text gin_trgm_ops);
CREATE TRIGGER trg_task_checklist_items_updated_at BEFORE UPDATE ON task_checklist_items
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE task_watchers (
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    muted BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, user_id),
    CONSTRAINT fk_task_watchers_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_watchers_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_task_watchers_user_id ON task_watchers (user_id);

CREATE TABLE notification_preferences (
    user_id BIGINT NOT NULL,
    type TEXT NOT NULL,
    channel TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    PRIMARY KEY (user_id, type, channel),
    CONSTRAINT fk_notification_preferences_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE TABLE notification_settings (
    user_id BIGINT PRIMARY KEY,
    locale TEXT NOT NULL DEFAULT 'en',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_notification_settings_locale CHECK (locale IN ('en', 'ru')),
    CONSTRAINT fk_notification_settings_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    destination TEXT NOT NULL,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    secret_enc TEXT,
    failure_count INTEGER NOT NULL DEFAULT 0,
    disabled_at TIMESTAMPTZ,
    last_error TEXT,
    CONSTRAINT chk_notification_channels_destination CHECK (btrim(destination) <> ''),
    CONSTRAINT fk_notification_channels_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_notification_channels_destination ON notification_channels (user_id, kind, destination);

CREATE TABLE channel_confirmations (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_channel_confirmations_channel FOREIGN KEY (channel_id) REFERENCES notification_channels (id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uq_channel_confirmations_token ON channel_confirmations (token_hash);
CREATE INDEX idx_channel_confirmations_channel ON channel_confirmations (channel_id, created_at DESC);

CREATE TABLE notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    type TEXT NOT NULL,
    task_id BIGINT,
    actor_id BIGINT,
    workspace_id BIGINT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ,
    CONSTRAINT fk_notifications_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_actor FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT fk_notifications_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE
);
CREATE INDEX idx_notifications_user_id_id ON notifications (user_id, id DESC);
CREATE INDEX idx_notifications_user_unread ON notifications (user_id, type) WHERE read_at IS NULL;
CREATE INDEX idx_notifications_task_id ON notifications (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_notifications_actor_id ON notifications (actor_id) WHERE actor_id IS NOT NULL;

ALTER TABLE tasks
    ADD COLUMN source VARCHAR(30) NOT NULL DEFAULT 'manual',
    ADD COLUMN source_reference_id BIGINT,
    ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN actor_id BIGINT,
    ADD COLUMN actor_label TEXT,
    ADD COLUMN requester JSONB;

ALTER TABLE tasks
    ADD CONSTRAINT chk_tasks_source
        CHECK (source IN ('manual', 'form', 'api', 'automation')),
    ADD CONSTRAINT chk_tasks_source_reference
        CHECK (source <> 'form' OR source_reference_id IS NOT NULL),
    ADD CONSTRAINT chk_tasks_actor_type
        CHECK (actor_type IN ('user', 'form', 'automation', 'integration', 'system')),
    ADD CONSTRAINT chk_tasks_actor_id
        CHECK (actor_type <> 'user' OR actor_id IS NULL OR actor_id = user_id);

CREATE INDEX idx_tasks_source_reference_id
    ON tasks (source_reference_id)
    WHERE source_reference_id IS NOT NULL;

CREATE TABLE custom_fields (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    workspace_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    type VARCHAR(50) NOT NULL,
    required BOOLEAN NOT NULL DEFAULT false,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT fk_custom_fields_workspace
        FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT chk_custom_fields_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT chk_custom_fields_type CHECK (type IN (
        'text', 'number', 'integer', 'boolean', 'datetime', 'date', 'url',
        'email', 'phone', 'dropdown', 'multi_select', 'checkbox', 'people', 'files'
    ))
);
CREATE INDEX idx_custom_fields_deleted_at ON custom_fields (deleted_at);
CREATE INDEX idx_custom_fields_workspace_id ON custom_fields (workspace_id);
CREATE UNIQUE INDEX idx_custom_fields_workspace_name
    ON custom_fields (workspace_id, lower(name)) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_custom_fields_updated_at BEFORE UPDATE ON custom_fields
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE custom_field_options (
    id BIGSERIAL PRIMARY KEY,
    custom_field_id BIGINT NOT NULL,
    label VARCHAR(255) NOT NULL,
    value VARCHAR(255) NOT NULL,
    position INTEGER NOT NULL,
    CONSTRAINT fk_custom_field_options_field
        FOREIGN KEY (custom_field_id) REFERENCES custom_fields (id) ON DELETE CASCADE,
    CONSTRAINT chk_custom_field_options_label_not_blank CHECK (btrim(label) <> ''),
    CONSTRAINT chk_custom_field_options_value_not_blank CHECK (btrim(value) <> ''),
    CONSTRAINT uq_custom_field_options_value UNIQUE (custom_field_id, value)
);
CREATE INDEX idx_custom_field_options_field_position
    ON custom_field_options (custom_field_id, position, id);

CREATE TABLE task_custom_field_values (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL,
    custom_field_id BIGINT NOT NULL,
    workspace_id BIGINT NOT NULL,
    value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_task_custom_field_values_task
        FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_custom_field_values_field
        FOREIGN KEY (custom_field_id) REFERENCES custom_fields (id) ON DELETE RESTRICT,
    CONSTRAINT fk_task_custom_field_values_workspace
        FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT uq_task_custom_field_value UNIQUE (task_id, custom_field_id)
);
CREATE INDEX idx_task_custom_field_values_workspace ON task_custom_field_values (workspace_id);
CREATE TRIGGER trg_task_custom_field_values_updated_at BEFORE UPDATE ON task_custom_field_values
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE FUNCTION validate_task_custom_field_workspace()
RETURNS TRIGGER AS $$
DECLARE
    task_workspace_id BIGINT;
    field_workspace_id BIGINT;
BEGIN
    SELECT spaces.workspace_id INTO task_workspace_id
    FROM tasks
    JOIN spaces ON spaces.id = tasks.space_id
    WHERE tasks.id = NEW.task_id;

    SELECT workspace_id INTO field_workspace_id
    FROM custom_fields
    WHERE id = NEW.custom_field_id;

    IF task_workspace_id IS NULL OR field_workspace_id IS NULL
       OR task_workspace_id <> field_workspace_id
       OR NEW.workspace_id <> task_workspace_id THEN
        RAISE EXCEPTION 'task and custom field must belong to the same workspace'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_validate_task_custom_field_workspace
    BEFORE INSERT OR UPDATE ON task_custom_field_values
    FOR EACH ROW EXECUTE FUNCTION validate_task_custom_field_workspace();

CREATE TABLE forms (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    workspace_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    slug VARCHAR(255) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    current_version_id BIGINT,
    created_by BIGINT NOT NULL,
    CONSTRAINT fk_forms_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_forms_creator FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_forms_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT chk_forms_slug_not_blank CHECK (btrim(slug) <> ''),
    CONSTRAINT chk_forms_status CHECK (status IN ('draft', 'published', 'archived'))
);
CREATE INDEX idx_forms_workspace_status ON forms (workspace_id, status, id);
CREATE UNIQUE INDEX uq_forms_slug ON forms (lower(slug));
CREATE TRIGGER trg_forms_updated_at BEFORE UPDATE ON forms
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE form_slug_aliases (
    id BIGSERIAL PRIMARY KEY,
    form_id BIGINT NOT NULL,
    slug VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_form_slug_aliases_form FOREIGN KEY (form_id) REFERENCES forms (id) ON DELETE CASCADE,
    CONSTRAINT chk_form_slug_aliases_slug_not_blank CHECK (btrim(slug) <> '')
);
CREATE UNIQUE INDEX uq_form_slug_aliases_slug ON form_slug_aliases (lower(slug));
CREATE INDEX idx_form_slug_aliases_form ON form_slug_aliases (form_id, id);

CREATE FUNCTION validate_form_slug_available()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('form_slug'), hashtext(lower(NEW.slug)));
    IF EXISTS (
        SELECT 1 FROM form_slug_aliases WHERE lower(slug) = lower(NEW.slug) AND form_id <> NEW.id
    ) THEN
        RAISE EXCEPTION 'form slug is an alias of another form' USING ERRCODE = '23505';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_validate_form_slug_available
    BEFORE INSERT OR UPDATE OF slug ON forms
    FOR EACH ROW EXECUTE FUNCTION validate_form_slug_available();

CREATE FUNCTION validate_form_slug_alias_available()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('form_slug'), hashtext(lower(NEW.slug)));
    IF EXISTS (
        SELECT 1 FROM forms WHERE lower(slug) = lower(NEW.slug) AND id <> NEW.form_id
    ) THEN
        RAISE EXCEPTION 'form slug is used by another form' USING ERRCODE = '23505';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_validate_form_slug_alias_available
    BEFORE INSERT OR UPDATE OF slug ON form_slug_aliases
    FOR EACH ROW EXECUTE FUNCTION validate_form_slug_alias_available();

CREATE TABLE form_versions (
    id BIGSERIAL PRIMARY KEY,
    form_id BIGINT NOT NULL,
    version INTEGER NOT NULL,
    schema_snapshot JSONB NOT NULL,
    status VARCHAR(30) NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_form_versions_form FOREIGN KEY (form_id) REFERENCES forms (id) ON DELETE CASCADE,
    CONSTRAINT uq_form_version_number UNIQUE (form_id, version),
    CONSTRAINT uq_form_versions_form_id UNIQUE (form_id, id),
    CONSTRAINT chk_form_versions_status CHECK (status = 'published'),
    CONSTRAINT chk_form_versions_number CHECK (version > 0)
);

CREATE TABLE form_questions (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    form_id BIGINT NOT NULL,
    type VARCHAR(50) NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    required BOOLEAN NOT NULL DEFAULT false,
    position INTEGER NOT NULL,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT fk_form_questions_form FOREIGN KEY (form_id) REFERENCES forms (id) ON DELETE CASCADE,
    CONSTRAINT chk_form_questions_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT chk_form_questions_type CHECK (type IN (
        'short_text', 'long_text', 'number', 'email', 'phone', 'url', 'date', 'datetime',
        'single_choice', 'multiple_choice', 'dropdown', 'checkbox', 'rating', 'scale',
        'file', 'section', 'description', 'divider'
    ))
);
CREATE INDEX idx_form_questions_form_position ON form_questions (form_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_form_questions_deleted_at ON form_questions (deleted_at);
CREATE TRIGGER trg_form_questions_updated_at BEFORE UPDATE ON form_questions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE form_question_options (
    id BIGSERIAL PRIMARY KEY,
    question_id BIGINT NOT NULL,
    label VARCHAR(255) NOT NULL,
    value VARCHAR(255) NOT NULL,
    position INTEGER NOT NULL,
    CONSTRAINT fk_form_question_options_question
        FOREIGN KEY (question_id) REFERENCES form_questions (id) ON DELETE CASCADE,
    CONSTRAINT chk_form_question_options_label_not_blank CHECK (btrim(label) <> ''),
    CONSTRAINT chk_form_question_options_value_not_blank CHECK (btrim(value) <> ''),
    CONSTRAINT uq_form_question_option_value UNIQUE (question_id, value)
);
CREATE INDEX idx_form_question_options_question_position ON form_question_options (question_id, position, id);

CREATE TABLE form_submissions (
    id BIGSERIAL PRIMARY KEY,
    form_id BIGINT NOT NULL,
    form_version_id BIGINT NOT NULL,
    workspace_id BIGINT NOT NULL,
    respondent_id BIGINT,
    status VARCHAR(30) NOT NULL DEFAULT 'submitted',
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_form_submissions_form FOREIGN KEY (form_id) REFERENCES forms (id) ON DELETE RESTRICT,
    CONSTRAINT fk_form_submissions_version FOREIGN KEY (form_id, form_version_id)
        REFERENCES form_versions (form_id, id) ON DELETE RESTRICT,
    CONSTRAINT fk_form_submissions_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_form_submissions_respondent FOREIGN KEY (respondent_id) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT chk_form_submissions_status CHECK (status IN ('submitted', 'processing', 'processed', 'failed'))
);
CREATE INDEX idx_form_submissions_form_submitted ON form_submissions (form_id, submitted_at DESC, id DESC);
CREATE INDEX idx_form_submissions_workspace ON form_submissions (workspace_id);

CREATE TABLE submission_answers (
    id BIGSERIAL PRIMARY KEY,
    submission_id BIGINT NOT NULL,
    question_id BIGINT NOT NULL,
    value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_submission_answers_submission
        FOREIGN KEY (submission_id) REFERENCES form_submissions (id) ON DELETE CASCADE,
    CONSTRAINT fk_submission_answers_question
        FOREIGN KEY (question_id) REFERENCES form_questions (id) ON DELETE RESTRICT,
    CONSTRAINT uq_submission_answer UNIQUE (submission_id, question_id)
);
CREATE INDEX idx_submission_answers_question ON submission_answers (question_id);

CREATE TABLE form_idempotency_keys (
    id BIGSERIAL PRIMARY KEY,
    form_id BIGINT NOT NULL,
    key VARCHAR(255) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    submission_id BIGINT,
    status VARCHAR(30) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_form_idempotency_form FOREIGN KEY (form_id) REFERENCES forms (id) ON DELETE CASCADE,
    CONSTRAINT fk_form_idempotency_submission FOREIGN KEY (submission_id) REFERENCES form_submissions (id) ON DELETE CASCADE,
    CONSTRAINT uq_form_idempotency_key UNIQUE (form_id, key),
    CONSTRAINT chk_form_idempotency_status CHECK (status IN ('processing', 'completed', 'failed'))
);
CREATE INDEX idx_form_idempotency_expiry ON form_idempotency_keys (expires_at);

ALTER TABLE forms
    ADD CONSTRAINT fk_forms_current_version
        FOREIGN KEY (id, current_version_id) REFERENCES form_versions (form_id, id) ON DELETE RESTRICT;

ALTER TABLE tasks
    ADD CONSTRAINT fk_tasks_source_submission
        FOREIGN KEY (source_reference_id) REFERENCES form_submissions (id) ON DELETE RESTRICT;

CREATE TABLE task_templates (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'active',
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_task_templates_name CHECK (btrim(name) <> ''),
    CONSTRAINT chk_task_templates_status CHECK (status IN ('active', 'archived'))
);
CREATE INDEX idx_task_templates_workspace ON task_templates(workspace_id, status, id);
CREATE TRIGGER trg_task_templates_updated_at BEFORE UPDATE ON task_templates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE task_template_versions (
    id BIGSERIAL PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES task_templates(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL CHECK (version > 0),
    configuration JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_template_version UNIQUE (template_id, version),
    CONSTRAINT uq_task_template_versions_pair UNIQUE (template_id, id)
);

ALTER TABLE forms ADD COLUMN task_config JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE form_submissions
    ADD COLUMN task_creation_status VARCHAR(30) NOT NULL DEFAULT 'pending',
    ADD COLUMN task_creation_error TEXT NOT NULL DEFAULT '';
ALTER TABLE form_submissions
    ADD CONSTRAINT chk_form_submission_task_status CHECK (
        task_creation_status IN ('pending', 'processing', 'succeeded', 'failed', 'skipped')
    );

CREATE TABLE form_task_configs (
    id BIGSERIAL PRIMARY KEY,
    form_id BIGINT NOT NULL,
    form_version_id BIGINT NOT NULL,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    target_list_id BIGINT NOT NULL REFERENCES lists(id) ON DELETE RESTRICT,
    template_version_id BIGINT,
    enabled BOOLEAN NOT NULL DEFAULT false,
    configuration JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_form_task_config_version UNIQUE (form_version_id),
    CONSTRAINT uq_form_task_config_id_form UNIQUE (id, form_id),
    CONSTRAINT fk_form_task_configs_version FOREIGN KEY (form_id, form_version_id)
        REFERENCES form_versions(form_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_form_task_configs_template_version FOREIGN KEY (template_version_id)
        REFERENCES task_template_versions(id) ON DELETE RESTRICT
);
CREATE TRIGGER trg_form_task_configs_updated_at BEFORE UPDATE ON form_task_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE form_submission_tasks (
    id BIGSERIAL PRIMARY KEY,
    submission_id BIGINT NOT NULL UNIQUE REFERENCES form_submissions(id) ON DELETE RESTRICT,
    task_id BIGINT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE RESTRICT,
    template_version_id BIGINT REFERENCES task_template_versions(id) ON DELETE RESTRICT,
    mapping_snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbox_events (
    id BIGSERIAL PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    aggregate_type VARCHAR(100) NOT NULL,
    aggregate_id BIGINT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until TIMESTAMPTZ,
    last_error TEXT,
    dead_at TIMESTAMPTZ
);
CREATE INDEX idx_outbox_events_pending ON outbox_events(next_attempt_at, id) WHERE published_at IS NULL AND dead_at IS NULL;

ALTER TABLE forms ADD COLUMN settings JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE form_uploads (
    id BIGSERIAL PRIMARY KEY,
    public_id TEXT NOT NULL,
    form_id BIGINT NOT NULL REFERENCES forms(id) ON DELETE CASCADE,
    question_id BIGINT REFERENCES form_questions(id) ON DELETE SET NULL,
    submission_id BIGINT REFERENCES form_submissions(id) ON DELETE CASCADE,
    file_id BIGINT REFERENCES files(id) ON DELETE SET NULL,
    storage_key TEXT NOT NULL,
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    CONSTRAINT chk_form_uploads_size CHECK (size > 0),
    CONSTRAINT chk_form_uploads_claim CHECK ((claimed_at IS NULL) = (submission_id IS NULL))
);
CREATE UNIQUE INDEX uq_form_uploads_public_id ON form_uploads (public_id);
CREATE UNIQUE INDEX uq_form_uploads_storage_key ON form_uploads (storage_key);
CREATE INDEX idx_form_uploads_pending ON form_uploads (form_id) WHERE claimed_at IS NULL;
CREATE INDEX idx_form_uploads_expiry ON form_uploads (expires_at) WHERE claimed_at IS NULL;
CREATE INDEX idx_form_uploads_submission ON form_uploads (submission_id) WHERE submission_id IS NOT NULL;

CREATE FUNCTION validate_form_task_config_workspace()
RETURNS TRIGGER AS $$
DECLARE
    form_workspace BIGINT;
    list_workspace BIGINT;
BEGIN
    SELECT workspace_id INTO form_workspace FROM forms WHERE id = NEW.form_id;
    SELECT spaces.workspace_id INTO list_workspace
    FROM lists JOIN spaces ON spaces.id = lists.space_id
    WHERE lists.id = NEW.target_list_id AND lists.deleted_at IS NULL AND spaces.deleted_at IS NULL;
    IF form_workspace IS NULL OR form_workspace <> NEW.workspace_id
       OR list_workspace IS NULL OR list_workspace <> NEW.workspace_id THEN
        RAISE EXCEPTION 'form task target must belong to its workspace' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER trg_validate_form_task_config_workspace
    BEFORE INSERT OR UPDATE ON form_task_configs
    FOR EACH ROW EXECUTE FUNCTION validate_form_task_config_workspace();

CREATE TABLE rate_limit_windows (
    bucket TEXT NOT NULL,
    key TEXT NOT NULL,
    count INTEGER NOT NULL,
    reset_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (bucket, key)
);
CREATE INDEX idx_rate_limit_windows_reset ON rate_limit_windows(reset_at);

CREATE TABLE login_lockouts (
    subject TEXT PRIMARY KEY,
    failures INTEGER NOT NULL,
    last_fail_at TIMESTAMPTZ NOT NULL,
    locked_until TIMESTAMPTZ
);
CREATE INDEX idx_login_lockouts_last_fail ON login_lockouts(last_fail_at);

CREATE TABLE workspace_audit_log (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL,
    actor_id BIGINT,
    target_user_id BIGINT,
    action TEXT NOT NULL,
    changes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT fk_workspace_audit_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_workspace_audit_actor FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT fk_workspace_audit_target FOREIGN KEY (target_user_id) REFERENCES users (id) ON DELETE SET NULL,
    CONSTRAINT chk_workspace_audit_action CHECK (action IN (
        'workspace.updated', 'workspace.avatar_changed', 'workspace.avatar_removed',
        'member.added', 'member.role_changed', 'member.removed', 'member.left'))
);
CREATE INDEX idx_workspace_audit_log_workspace ON workspace_audit_log (workspace_id, id DESC);

CREATE TABLE saved_views (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    workspace_id BIGINT NOT NULL,
    list_id BIGINT,
    owner_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    shared BOOLEAN NOT NULL DEFAULT false,
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    sort TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_saved_views_name CHECK (btrim(name) <> ''),
    CONSTRAINT fk_saved_views_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_saved_views_list FOREIGN KEY (list_id) REFERENCES lists (id) ON DELETE CASCADE,
    CONSTRAINT fk_saved_views_owner FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_saved_views_workspace_list ON saved_views (workspace_id, list_id);
CREATE INDEX idx_saved_views_owner ON saved_views (owner_id, workspace_id);
CREATE INDEX idx_saved_views_list ON saved_views (list_id) WHERE list_id IS NOT NULL;

CREATE TABLE task_time_entries (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    seconds INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    CONSTRAINT chk_time_entries_seconds CHECK (seconds >= 0 AND seconds <= 86400),
    CONSTRAINT chk_time_entries_running CHECK (ended_at IS NOT NULL OR seconds = 0),
    CONSTRAINT chk_time_entries_order CHECK (ended_at IS NULL OR ended_at >= started_at),
    CONSTRAINT fk_time_entries_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_time_entries_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
CREATE INDEX idx_time_entries_task ON task_time_entries (task_id, id DESC);
CREATE INDEX idx_time_entries_user_started ON task_time_entries (user_id, started_at);
CREATE UNIQUE INDEX uq_time_entries_one_running_timer ON task_time_entries (user_id) WHERE ended_at IS NULL;

CREATE TABLE task_relations (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    from_task_id BIGINT NOT NULL,
    to_task_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    created_by BIGINT,
    CONSTRAINT chk_task_relations_kind CHECK (kind IN ('blocks', 'related')),
    CONSTRAINT chk_task_relations_distinct CHECK (from_task_id <> to_task_id),
    CONSTRAINT chk_task_relations_related_order CHECK (kind <> 'related' OR from_task_id < to_task_id),
    CONSTRAINT uq_task_relations UNIQUE (from_task_id, to_task_id, kind),
    CONSTRAINT fk_task_relations_from FOREIGN KEY (from_task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_relations_to FOREIGN KEY (to_task_id) REFERENCES tasks (id) ON DELETE CASCADE,
    CONSTRAINT fk_task_relations_creator FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX idx_task_relations_to ON task_relations (to_task_id);
CREATE INDEX idx_task_relations_creator ON task_relations (created_by) WHERE created_by IS NOT NULL;

