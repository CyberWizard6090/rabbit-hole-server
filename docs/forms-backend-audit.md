# Forms Backend: audit of the existing backend

## Existing architecture

- The backend is Go with Gin, GORM, and PostgreSQL. Database changes are ordered SQL
  migrations embedded from `db/migrations` and run by the existing migration command.
- Application code is organized under `internal/domain`, `dto`, `repository`, `service`,
  `handler`, and `app`; it does not currently use one directory per domain module.
- Existing IDs are PostgreSQL `BIGSERIAL` / Go `uint`. The Forms schema should use this
  convention and must not require converting existing tables to UUID.
- A Workspace contains Spaces; Spaces contain Lists and Tasks. The current product has no
  Projects module, so a form's destination should use the existing List/Space hierarchy.

## Relevant models and contracts

- `domain.Workspace` and `domain.Space` carry workspace ownership. Lists belong to a Space;
  Tasks carry both `space_id` and `list_id`.
- Task creation is already separated from HTTP: `TaskService.CreateTask` delegates to
  `TaskRepository.Create`. The repository uses a transaction and validates the List's status,
  parent task, tags, and assignee membership before inserting the task.
- Task APIs are currently rooted at `POST /api/v1/lists/{list_id}/tasks` and the existing
  task list/get/update/delete routes. The create DTO does not allow callers to set an origin.
- Authorization is workspace-role based. Middleware currently resolves Workspace, Space,
  Folder, List, and Task resources and checks the corresponding permission code.
- Existing migrations define foreign keys, checks, indexes, and soft-delete behavior in SQL.
  New tables and cross-module constraints should follow that approach rather than relying on
  GORM auto-migration.

## Gaps against the Forms specification at audit time

- There are no Custom Fields, Task Templates, Forms, Form Versions, Questions, Submissions,
  answer storage, mapping, or idempotency tables or APIs.
- Tasks do not record a creation source or a source reference.
- There is no Projects abstraction; the existing List is the task destination.
- The permission middleware has no Form or Submission resource. New management routes need
  workspace-scoped authorization; public submission routes need separate rate and body limits.
- Each repository currently manages its own transaction. An atomic Submission + Task workflow
  must introduce an explicit shared transaction boundary before it can promise all-or-nothing
  persistence across modules.
- The current task and workspace IDs are numeric. Form snapshots and mappings must refer to
  these existing IDs, with database constraints preventing cross-workspace references.

## Implementation decisions

1. Keep the existing IDs, Workspace/Space/List model, HTTP envelope, authentication, and
   migration conventions.
2. Treat a List as the destination for a form-created Task; validate its Workspace via its
   parent Space.
3. Keep Forms, Submissions, and task mapping in separate domain/service/repository packages.
   Integrate through a task-creation contract rather than sharing persistence structs.
4. Publish immutable version snapshots; bind each Submission and its mapping configuration to
   the version used when it was submitted.
5. Add source metadata to Tasks as additive fields. Only internal service contracts may mark a
   task as form-created; the public Task API must not accept caller-controlled source metadata.
6. Implement the initial release in the order specified by the project: task foundation,
   Custom Fields and Templates, Forms and versioning, then transactional submission processing
   and hardening. Automation, outbound webhooks, and analytics remain later-stage work.

## Implemented since the audit

- Tasks now persist an internal creation source and form submission reference, and the task
  service exposes a workspace-checked form creation contract.
- Custom Field definitions and options have SQL migrations, workspace-scoped management routes,
  typed task values, and task/workspace/field ownership checks.
- Forms provide Workspace-scoped management, immutable publication snapshots, public
  submissions, answer validation, and idempotent request handling.
- Task Templates have Workspace scoping, immutable configuration versions, archive behavior,
  and task application.
- Form task configurations are validated before publication and copied to the immutable form
  version. The Submission processor applies mappings and template values in one PostgreSQL
  transaction, records the Submission-to-Task relation and outbox event, and persists failures
  for authorized retry.
- Automation rules, outbound webhook delivery, advanced uploads, and analytics remain later-stage
  work.
