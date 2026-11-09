DROP TABLE workspace_audit_log;
DROP TABLE login_lockouts;
DROP TABLE rate_limit_windows;
DROP TRIGGER trg_validate_form_task_config_workspace ON form_task_configs;
DROP FUNCTION validate_form_task_config_workspace();
DROP TABLE outbox_events;
DROP TABLE form_uploads;
DROP TABLE form_submission_tasks;
DROP TABLE form_task_configs;
ALTER TABLE form_submissions DROP CONSTRAINT chk_form_submission_task_status;
ALTER TABLE form_submissions DROP COLUMN task_creation_error;
ALTER TABLE form_submissions DROP COLUMN task_creation_status;
ALTER TABLE forms DROP COLUMN task_config;
ALTER TABLE forms DROP COLUMN settings;

DROP TABLE task_template_versions;
DROP TABLE task_templates;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS fk_tasks_source_submission;
ALTER TABLE forms DROP CONSTRAINT IF EXISTS fk_forms_current_version;
DROP TABLE IF EXISTS form_idempotency_keys;
DROP TABLE IF EXISTS submission_answers;
DROP TABLE IF EXISTS form_submissions;
DROP TABLE IF EXISTS form_question_options;
DROP TABLE IF EXISTS form_questions;
DROP TABLE IF EXISTS form_versions;
DROP TABLE IF EXISTS form_slug_aliases;
DROP TABLE IF EXISTS forms;
DROP FUNCTION IF EXISTS validate_form_slug_alias_available();
DROP FUNCTION IF EXISTS validate_form_slug_available();

DROP TABLE IF EXISTS task_custom_field_values;
DROP TABLE IF EXISTS custom_field_options;
DROP TABLE IF EXISTS custom_fields;
DROP FUNCTION IF EXISTS validate_task_custom_field_workspace();

DROP INDEX IF EXISTS idx_tasks_source_reference_id;

ALTER TABLE tasks
    DROP CONSTRAINT IF EXISTS chk_tasks_source_reference,
    DROP CONSTRAINT IF EXISTS chk_tasks_source,
    DROP COLUMN IF EXISTS source_reference_id,
    DROP COLUMN IF EXISTS source;

ALTER TABLE IF EXISTS lists DROP CONSTRAINT IF EXISTS fk_lists_parent_status;

DROP TABLE IF EXISTS
    notifications,
    channel_confirmations,
    notification_channels,
    notification_settings,
    notification_preferences,
    task_watchers,
    task_checklist_items,
    task_checklists,
    task_activities,
    files,
    task_comment_mentions,
    task_comments,
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
    email_tokens,
    users;

DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TYPE IF EXISTS task_status_type;