ALTER TABLE tasks
    DROP CONSTRAINT chk_tasks_recurrence_object,
    DROP COLUMN recurrence;
