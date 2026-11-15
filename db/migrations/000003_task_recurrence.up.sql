-- A repeating task: the rule is stored on the live task. Completing the task creates the next one and moves the rule to it.
ALTER TABLE tasks
    ADD COLUMN recurrence JSONB,
    ADD CONSTRAINT chk_tasks_recurrence_object CHECK (recurrence IS NULL OR jsonb_typeof(recurrence) = 'object');
