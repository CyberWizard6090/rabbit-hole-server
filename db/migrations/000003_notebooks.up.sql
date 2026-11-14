-- Notebooks hold collaborative documents. A notebook belongs to a workspace; a document belongs to a notebook.
-- Document content is a Yjs document stored as opaque bytes: one compacted snapshot plus the updates after it.

INSERT INTO permissions (code) VALUES
    ('doc.read'),
    ('doc.create'),
    ('doc.update'),
    ('doc.delete')
ON CONFLICT (code) WHERE deleted_at IS NULL DO NOTHING;

CREATE TABLE notebooks (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    workspace_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    user_id BIGINT NOT NULL,
    position BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_notebooks_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT fk_workspaces_notebooks FOREIGN KEY (workspace_id) REFERENCES workspaces (id) ON DELETE CASCADE,
    CONSTRAINT fk_notebooks_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT uq_notebooks_workspace_id_id UNIQUE (workspace_id, id)
);
CREATE INDEX idx_notebooks_workspace_position ON notebooks (workspace_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_notebooks_deleted_at ON notebooks (deleted_at);
CREATE TRIGGER trg_notebooks_updated_at BEFORE UPDATE ON notebooks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TABLE documents (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    workspace_id BIGINT NOT NULL,
    notebook_id BIGINT NOT NULL,
    title TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    position BIGINT NOT NULL DEFAULT 0,
    update_seq BIGINT NOT NULL DEFAULT 0,
    last_edited_by BIGINT,
    last_edited_at TIMESTAMPTZ,
    CONSTRAINT chk_documents_title_not_blank CHECK (btrim(title) <> ''),
    CONSTRAINT chk_documents_update_seq CHECK (update_seq >= 0),
    CONSTRAINT fk_notebooks_documents FOREIGN KEY (workspace_id, notebook_id) REFERENCES notebooks (workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT fk_documents_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT fk_documents_last_editor FOREIGN KEY (last_edited_by) REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX idx_documents_notebook_position ON documents (notebook_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX idx_documents_workspace_id ON documents (workspace_id);
CREATE INDEX idx_documents_deleted_at ON documents (deleted_at);
CREATE INDEX idx_documents_title_trgm ON documents USING GIN (title gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_documents_updated_at BEFORE UPDATE ON documents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
COMMENT ON COLUMN documents.update_seq IS 'Last allocated update number; it is incremented under the row lock, so updates of one document are totally ordered.';

-- One compacted Yjs state per document; updates with seq <= upto_seq are already merged into it.
CREATE TABLE document_snapshots (
    document_id BIGINT PRIMARY KEY,
    upto_seq BIGINT NOT NULL DEFAULT 0,
    state BYTEA NOT NULL,
    format INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_document_snapshots_upto_seq CHECK (upto_seq >= 0),
    CONSTRAINT fk_documents_snapshot FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE
);

CREATE TABLE document_updates (
    document_id BIGINT NOT NULL,
    seq BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, seq),
    CONSTRAINT fk_documents_updates FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE,
    CONSTRAINT fk_document_updates_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- Who created and who last edited a block. The server fills it from the authenticated user and the block ids the
-- client reports with each update, never from user ids sent by the client.
CREATE TABLE document_block_activity (
    document_id BIGINT NOT NULL,
    block_id TEXT NOT NULL,
    created_by BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_edited_by BIGINT NOT NULL,
    last_edited_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, block_id),
    CONSTRAINT fk_documents_block_activity FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE,
    CONSTRAINT fk_block_activity_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT fk_block_activity_edited_by FOREIGN KEY (last_edited_by) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT chk_block_activity_block_id CHECK (block_id <> '' AND length(block_id) <= 100)
);
CREATE INDEX idx_block_activity_editor ON document_block_activity (document_id, last_edited_by);
