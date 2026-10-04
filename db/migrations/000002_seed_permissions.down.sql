DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM role_permissions rp
        JOIN permissions p ON p.id = rp.permission_id
        WHERE p.code IN (
            'workspace.read',
            'workspace.update',
            'workspace.delete',
            'space.read',
            'space.create',
            'space.update',
            'space.delete',
            'task.read',
            'task.create',
            'task.update',
            'task.delete'
        )
    ) THEN
        RAISE EXCEPTION
            'cannot rollback permissions migration: permissions are still referenced by role_permissions';
    END IF;

    DELETE FROM permissions
    WHERE code IN (
        'workspace.read',
        'workspace.update',
        'workspace.delete',
        'space.read',
        'space.create',
        'space.update',
        'space.delete',
        'task.read',
        'task.create',
        'task.update',
        'task.delete'
    );
END $$;