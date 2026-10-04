INSERT INTO permissions (code) VALUES
    ('workspace.read'),
    ('workspace.update'),
    ('workspace.delete'),
    ('space.read'),
    ('space.create'),
    ('space.update'),
    ('space.delete'),
    ('task.read'),
    ('task.create'),
    ('task.update'),
    ('task.delete')
ON CONFLICT (code) WHERE deleted_at IS NULL DO NOTHING;