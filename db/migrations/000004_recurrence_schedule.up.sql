-- The scheduler looks for scheduled repeating tasks whose due date has passed; the index holds only those tasks.
CREATE INDEX idx_tasks_recurrence_schedule ON tasks (due_date)
    WHERE deleted_at IS NULL AND recurrence ->> 'trigger' = 'schedule';
