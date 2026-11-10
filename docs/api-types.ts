// Generated from docs/openapi.yaml by `make api-types`. Do not edit.
// Every response is checked against the same schemas by the contract test.

export interface AccessToken {
  /** JWT for the Authorization header; the refresh token travels in an HttpOnly cookie. */
  access_token: string;
}

/** One entry of the task history. `old_value` and `new_value` depend on `action`. */
export interface Activity {
  action: string;
  actor: ActorView;
  created_at: string;
  field?: string;
  id: number;
  new_value?: unknown;
  old_value?: unknown;
  task_id: number;
}

/** Public projection of who or what initiated something. `created_by` of a task and `actor` of an activity entry. */
export interface ActorView {
  /** Only for `user`. */
  avatar_url?: string;
  /** Only for `user`. */
  first_name?: string;
  /** The user id, the form id or the automation id depending on `type`; null for `system`. */
  id: number | null;
  /** Only for `user`. */
  last_name?: string;
  /** Display name of the user, or the label recorded at event time (the form name at creation for `form`). */
  name: string;
  type: "user" | "form" | "automation" | "integration" | "system";
  /** Only for `user`. */
  username?: string;
}

export interface AddContactRequest {
  contact_id: number;
}

export interface AddMemberRequest {
  role: "Admin" | "Member" | "Guest";
  user_id: number;
}

export interface ApplyTaskTemplateRequest {
  /** Custom field values keyed by field id; override the template values. */
  custom_fields?: Record<string, unknown>;
  description?: string;
  list_id: number;
  status_id?: number;
  title?: string;
}

export interface Attachment {
  content_type: string;
  created_at: string;
  id: number;
  name: string;
  size: number;
  task_id: number;
  uploader: CommentUser;
}

export interface AuditEntry {
  action: string;
  actor: CommentUser | null;
  changes: unknown;
  created_at: string;
  id: number;
  target: CommentUser | null;
}

export interface AvatarUrl {
  /** Empty when there is no avatar. */
  avatar_url: string;
}

export interface BulkTaskResult {
  failed: number;
  results: ({
    code?: string;
    ok: boolean;
    params?: Record<string, unknown>;
    task_id: number;
  })[];
  succeeded: number;
}

export interface ChangeEmailRequest {
  new_email: string;
  password: string;
}

export interface ChangePasswordRequest {
  current_password: string;
  new_password: string;
}

export interface Checklist {
  created_at: string;
  id: number;
  items: ChecklistItem[];
  name: string;
  position: number;
  task_id: number;
  updated_at: string;
}

export interface ChecklistItem {
  assignee: CommentUser | null;
  checklist_id: number;
  created_at: string;
  id: number;
  position: number;
  resolved: boolean;
  resolved_at: string | null;
  resolved_by: CommentUser | null;
  text: string;
  updated_at: string;
}

export interface ChecklistNameRequest {
  name: string;
}

export interface Comment {
  author: CommentUser;
  body: string;
  created_at: string;
  edited: boolean;
  id: number;
  mentions: CommentUser[];
  task_id: number;
  updated_at: string;
}

/** Public projection of a user: never an e-mail address. */
export interface CommentUser {
  avatar_url: string;
  first_name: string;
  id: number;
  last_name: string;
  username: string;
}

export interface ContactEntry {
  created_at: string;
  user: CommentUser;
}

export interface CreateChecklistItemRequest {
  assignee_id?: number | null;
  text: string;
}

export interface CreateCommentRequest {
  /** Stored as sent (after trimming); the client decides the format. */
  body: string;
  mention_user_ids?: number[];
}

export interface CreateFolderRequest {
  name: string;
}

export interface CreateListRequest {
  name: string;
  /** Must be a status of the same space; otherwise 400 VALIDATION_FAILED with details[].field parent_status_id and code NOT_FOUND. */
  parent_status_id?: number | null;
}

export interface CreateSpaceRequest {
  /** Three-letter ISO 4217 code. */
  currency?: string;
  name: string;
}

export interface CreateStatusRequest {
  /** 7-char hex color */
  color: string;
  /** Must match the list in the path. */
  list_id: number;
  name: string;
  position?: number;
  type: number;
}

export interface CreateTagRequest {
  /** 7-char hex color */
  color: string;
  name: string;
}

export interface CreateTaskRequest {
  /** Must be workspace members. */
  assignee_ids?: number[];
  /** Custom field values keyed by field id, for example `{"7": "ACME"}`; saved with the task in one transaction. Every required custom field of the workspace needs a non-empty value, otherwise 400 `VALIDATION_FAILED` with one `custom_fields.<id>` detail (`REQUIRED`, params `field_id`) per missing field. Unknown fields and invalid values are 400 `INVALID_CUSTOM_FIELD`. Tasks created by a form fail with `task_creation_status` `failed` when a required field has no value. */
  custom_fields?: Record<string, unknown>;
  description?: string;
  /** Must not be before start_date (400 INVALID_DATE_RANGE). */
  due_date?: string | null;
  new_tag_names?: string[];
  /** Must be a task of the same list. */
  parent_id?: number | null;
  /** 1 low, 2 normal, 3 high, 4 urgent, 5 critical; higher is more urgent. 0 or omitted means 2 (normal). See TaskPriority. */
  priority?: number;
  start_date?: string | null;
  /** Must be a status of the list in the path. */
  status_id: number;
  /** Must be tags of the same space. */
  tag_ids?: number[];
  /** Seconds. */
  time_estimate?: number;
  title: string;
}

export interface CreateWorkspaceRequest {
  description?: string;
  name: string;
}

export interface CustomField {
  config: Record<string, unknown>;
  created_at: string;
  description: string;
  id: number;
  name: string;
  options: (Record<string, unknown>)[];
  required: boolean;
  type: string;
  updated_at: string;
  workspace_id: number;
}

export interface CustomFieldRequest {
  /** Numeric fields support min/max; text fields support min_length/max_length. */
  config?: Record<string, unknown>;
  description?: string;
  name: string;
  options?: ({
    label: string;
    value: string;
  })[];
  required?: boolean;
  type: "text" | "number" | "integer" | "boolean" | "datetime" | "date" | "url" | "email" | "phone" | "dropdown" | "multi_select" | "checkbox" | "people" | "files";
}

export interface CustomFieldValueRequest {
  /** JSON value matching the field's type; use the delete endpoint to clear an optional value. */
  value: unknown;
}

/** Values keyed by custom field id. */
export type CustomFieldValues = Record<string, unknown>;

export interface DeleteAccountRequest {
  password: string;
}

export interface EmailRequest {
  email: string;
}

export interface Envelope {
  data?: unknown;
  error?: {
    /** Stable identifier the client translates; see docs/error-codes.md */
    code: string;
    /** One entry per invalid input of the request */
    details?: ({
    code: string;
    /** JSON path of the input, e.g. items[2].text */
    field: string;
    params?: Record<string, unknown>;
  })[];
    /** Values for the translation, e.g. {retry_after: 30} */
    params?: Record<string, unknown>;
    /** Equals the X-Request-ID response header */
    request_id?: string;
  };
  pagination?: {
    limit?: number;
    page?: number;
    pages?: number;
    total?: number;
  };
}

export interface EventTicket {
  expires_in: number;
  /** One-time, valid for `expires_in` seconds. */
  ticket: string;
  topic: string;
}

export interface Folder {
  created_at: string;
  id: number;
  name: string;
  parent_id: number | null;
  space_id: number;
  updated_at: string;
}

/** `questions`, `current_version` and `settings` appear on the endpoints that say so. */
export interface Form {
  created_at: string;
  created_by: number;
  current_version?: Record<string, unknown>;
  current_version_id?: number;
  description: string;
  id: number;
  name: string;
  previous_slugs?: string[];
  questions?: FormQuestion[];
  settings?: Record<string, unknown>;
  slug: string;
  status: "draft" | "published" | "archived";
  task_config?: Record<string, unknown>;
  updated_at: string;
  workspace_id: number;
}

/** FormRequest plus optional questions and task configuration, created atomically. See POST /workspaces/{workspace_id}/forms. */
export type FormCreateRequest = FormRequest & {
    /** At most 100. Array order defines the positions; `position` inside items is ignored. */
    questions?: FormQuestionRequest[];
    task_config?: FormTaskConfigurationRequest;
  };

/** A built-in form preset in the shape of the create request (without `slug` and `task_config`). */
export interface FormPreset {
  description?: string;
  key?: "feedback" | "project_request" | "order" | "application" | "it_request";
  name?: string;
  questions?: FormQuestionRequest[];
}

export interface FormQuestion {
  description?: string;
  id: number;
  options?: (Record<string, unknown>)[];
  position: number;
  required: boolean;
  settings?: Record<string, unknown>;
  title: string;
  type: string;
}

/** A question of a form. `settings` is a JSON object (default `{}`; `null` is treated as `{}`); only the keys listed */
export interface FormQuestionRequest {
  description?: string;
  options?: ({
    label: string;
    value: string;
  })[];
  /** Optional. Omitted: appended as max(position)+1 (0 for the first). Taken: the question is inserted and the following ones are shifted by +1. Ignored by the atomic form creation. */
  position?: number;
  required?: boolean;
  /** Per-type settings; see the type table in the description of this schema. Besides those keys: */
  settings?: Record<string, unknown>;
  title: string;
  type: "short_text" | "long_text" | "number" | "email" | "phone" | "url" | "date" | "datetime" | "single_choice" | "multiple_choice" | "dropdown" | "checkbox" | "rating" | "scale" | "file" | "section" | "description" | "divider";
}

export interface FormRequest {
  description?: string;
  name: string;
  settings?: FormSettings;
  /** Globally unique public URL slug: lower-case letters, digits and hyphens, starting with a letter or digit. */
  slug: string;
}

/** Form settings (request and response; `logo_url` and `cover_url` are output only). Omitted fields mean not set. */
export interface FormSettings {
  accent_color?: string;
  /** After this moment the public form is flagged `closed` and submissions get 410 FORM_CLOSED. */
  closes_at?: string | null;
  /** Same as logo_file_id. */
  cover_file_id?: string;
  cover_url?: string;
  /** Public id of an image uploaded to this workspace with `POST /workspaces/{workspace_id}/images` (the last path segment of the returned `url`; the full url is accepted too). Avatars and images of other workspaces are refused. */
  logo_file_id?: string;
  /** Public URL of the logo, served like editor images. */
  logo_url?: string;
  /** Total submissions accepted; further ones get 409 FORM_FULL. */
  max_submissions?: number | null;
  /** Workspace member ids that get an in-app `form_submission` notification for every new submission (only while they can still read the workspace). Never exposed publicly. */
  notify_on_submit?: number[];
  /** Absolute https URL (http only when APP_ENV=development, no credentials, no javascript: or data: schemes). Where the client sends the visitor after a successful submission. */
  redirect_url?: string;
  submit_button_text?: string;
  success_message?: string;
}

export interface FormSubmission {
  answers?: ({
    question_id?: number;
    /** JSON value in the format of the question type, see FormSubmissionRequest. */
    value?: unknown;
  })[];
  form_id?: number;
  form_version_id?: number;
  id?: number;
  respondent_id?: number | null;
  status?: "submitted" | "processing" | "processed" | "failed";
  submitted_at?: string;
  /** Stable failure code, present only after a failed attempt and only for workspace members; never part of the anonymous public submission response. */
  task_creation_error?: string;
  task_creation_status?: TaskCreationStatus;
  workspace_id?: number;
}

export interface FormSubmissionFile {
  content_type?: string;
  /** Use with GET /workspaces/{workspace_id}/submissions/{submission_id}/files/{file_id}. */
  id?: number;
  name?: string;
  question_id?: number;
  size?: number;
  /** Present once the file was attached to the task created from the submission. */
  task_attachment_id?: number;
}

export interface FormSubmissionRequest {
  /** Maps question IDs (as JSON object keys) to JSON values. Every question is optional unless `required` is true. An answer to a */
  answers?: Record<string, unknown>;
}

export interface FormTaskConfigView {
  enabled: boolean;
  mappings: (Record<string, unknown>)[];
  owner_id: number | null;
  target: {
    list_id: number;
    workspace_id: number;
  };
  task_defaults: {
    assignee_id: number | null;
    /** Empty means not set: the task template or the default priority decides. */
    priority: "" | "low" | "normal" | "high" | "urgent" | "critical";
    status_id: number | null;
  };
  /** `template_id` and `version_id` when the configuration builds tasks from a task template. */
  template?: Record<string, unknown>;
}

export interface FormTaskConfigurationRequest {
  enabled: boolean;
  mappings: ({
    question_id?: number;
    /** Zero-based index into the `questions` array of the same create request. */
    question_index?: number;
    target: {
    /** `task_field`: title, description, priority, status_id, assignee_id, assignee, due_date. `requester`: `name` (a text question) or `email` (an email question); both optional answers, ignored when the respondent was signed in. `task_field` `assignee` takes a single_choice or dropdown question without transform whose option values are workspace member user ids (every option is checked when the configuration is saved). At processing time a value that is not a current member is not applied: the task is created without that assignee (falling back to `task_defaults.assignee_id`) and the mapping entry in the stored mapping snapshot carries `failure` (`INVALID_ID` or `NOT_A_MEMBER`); the submission never fails because of it. */
    field?: "title" | "description" | "priority" | "status_id" | "assignee_id" | "assignee" | "due_date" | "name" | "email";
    field_id?: number;
    type: "task_field" | "custom_field" | "requester";
  };
    transform?: {
    type?: "enum";
    /** Option value -> target value. For `priority` targets the values are priority names (low, normal, high, urgent, critical); for `status_id` and `assignee_id` they are ids as strings. */
    values?: Record<string, string>;
  };
  })[];
  /** Optional technical owner (`tasks.user_id`) of the created tasks. Must be a current workspace member when saved (400 with detail `owner_id`) and is checked again for every submission; if that user has left the workspace the form creator is used instead. Tasks are always attributed to the form (`created_by.type = form`), not to the owner. */
  owner_id?: number | null;
  target: {
    list_id?: number;
    workspace_id: number;
  };
  /** Lowest precedence: mappings > template > task_defaults. See PUT task-config. */
  task_defaults: {
    assignee_id?: number | null;
    priority?: TaskPriorityName;
    status_id?: number | null;
  };
  template?: {
    template_id?: number;
    /** Optional. Omitted: the latest version of the template when the configuration is saved, stored in the configuration and snapshotted when the form is published. */
    version_id?: number;
  };
}

export interface FormUploadResult {
  content_type?: string;
  expires_at?: string;
  /** Opaque upload id to use inside the answer of a `file` question. */
  id?: string;
  name?: string;
  size?: number;
}

export interface ImageUpload {
  content_type: string;
  size: number;
  url: string;
}

/** `statuses` and `tasks` are filled only by the dashboard, otherwise null. */
export interface List {
  created_at: string;
  folder_id: number | null;
  id: number;
  name: string;
  parent_status_id?: number;
  space_id: number;
  statuses?: TaskStatus[] | null;
  task_count: number;
  tasks?: Task[] | null;
  updated_at: string;
}

/** Built-in list template; `name`, `description` and the status names are localized from Accept-Language. */
export interface ListTemplate {
  description: string;
  key: string;
  name: string;
  statuses: ({
    color: string;
    name: string;
    type: 1 | 2 | 3;
  })[];
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface Member {
  avatar_url: string;
  first_name: string;
  last_name: string;
  role: "Owner" | "Admin" | "Member" | "Guest";
  user_id: number;
  username: string;
}

export interface MergeTagsRequest {
  source_tag_id: number;
  target_tag_id: number;
}

/** Confirmation without data. The text is for logs only: the client shows its own wording. */
export interface Message {
  message: string;
}

export interface MoveTaskRequest {
  /** Place the task right after this task. Mutually exclusive with before_id. */
  after_id?: number;
  /** Place the task right before this task. */
  before_id?: number;
  /** Target list; defaults to the current one. */
  list_id?: number;
  /** Target status; required when list_id changes the list. */
  status_id?: number;
}

export interface Notification {
  actor: CommentUser;
  created_at: string;
  id: number;
  payload: Record<string, unknown>;
  read_at: string | null;
  task_id?: number;
  type: "mention" | "assigned" | "comment" | "status_changed" | "due_date_changed" | "form_submission";
  workspace_id: number;
}

/** `id` is null for the implicit in-app channel and the account address. */
export interface NotificationChannel {
  created_at: string | null;
  destination?: string;
  id: number | null;
  implicit: boolean;
  kind: "in_app" | "email" | "webhook";
  /** Webhook signing secret, shown once on creation. */
  secret?: string;
  verified: boolean;
  verified_at: string | null;
}

export interface NotificationPreference {
  channel: "in_app" | "email" | "webhook";
  default: boolean;
  enabled: boolean;
  type: string;
  /** False while the channel has no verified destination. */
  usable: boolean;
}

export interface NotificationSettings {
  locale: "en" | "ru";
}

export interface Pagination {
  limit: number;
  page: number;
  pages: number;
  total: number;
}

export interface PublicForm {
  closed: boolean;
  description: string;
  full: boolean;
  name: string;
  questions: FormQuestion[];
  settings: Record<string, unknown>;
  slug: string;
  version: number;
}

/** Answer of the public submit endpoint; nothing but what the respondent needs. */
export interface PublicFormSubmission {
  created_at?: string;
  id?: number;
  /** `settings.redirect_url` of the published version. */
  redirect_url?: string;
  status?: "submitted" | "processing" | "processed" | "failed";
  /** `settings.success_message` of the published version. */
  success_message?: string;
  task_creation_status?: TaskCreationStatus;
}

export interface ReadResult {
  updated: number;
}

export interface RegisterRequest {
  email: string;
  password: string;
  username?: string;
}

export interface RelatedTask {
  done: boolean;
  id: number;
  list_id: number;
  status_id: number;
  title: string;
}

export interface ReorderFormQuestionsRequest {
  /** Must contain every question ID exactly once. */
  ids: number[];
}

export interface ReorderRequest {
  ids: number[];
}

export interface ResetPasswordRequest {
  new_password: string;
  token: string;
}

export interface RunningTimer {
  timer: {
    entry_id: number;
    seconds: number;
    started_at: string;
    task_id: number;
    task_title: string;
  } | null;
}

export interface SavedView {
  created_at: string;
  filters: Record<string, string>;
  id: number;
  is_owner: boolean;
  list_id: number | null;
  name: string;
  owner: CommentUser | null;
  shared: boolean;
  sort: string;
  updated_at: string;
  workspace_id: number;
}

export interface SearchResult {
  field?: "title" | "description" | "body" | "text";
  id?: number;
  list_id?: number;
  score?: number;
  snippet?: {
    /** Offsets in Unicode code points (runes) into snippet.text; end is exclusive. */
    matches?: ({
    end?: number;
    start?: number;
  })[];
    /** Plain text, never HTML */
    text?: string;
  };
  space_id?: number;
  task_id?: number;
  task_title?: string;
  type?: "task" | "comment" | "checklist_item";
  updated_at?: string;
}

export interface SecretResult {
  secret: string;
}

/** `lists`, `tasks` and `tags` are filled only by the endpoints that say so, otherwise null. */
export interface Space {
  created_at: string;
  currency: string;
  id: number;
  lists?: List[] | null;
  name: string;
  owner_id: number;
  tags?: Tag[] | null;
  tasks?: Task[] | null;
  updated_at: string;
  workspace_id: number;
}

export interface Tag {
  color: string;
  id: number;
  name: string;
  /** Only on the tag endpoints, not inside tasks. */
  usage_count?: number;
}

/** Task as returned by every task endpoint, the dashboard and realtime events. */
export interface Task {
  assignees: CommentUser[];
  /** Number of resolved items; never above checklist_items_total. */
  checklist_items_resolved: number;
  /** Number of items over all checklists of the task; 0 without checklists. Subtasks are not counted. */
  checklist_items_total: number;
  created_at: string;
  created_by: ActorView;
  description: string;
  due_date: string | null;
  id: number;
  list_id: number;
  parent_id: number | null;
  position: number;
  priority: TaskPriority;
  /** Only present on tasks created from a form that identified a requester, and only for callers with `task.update` on the task (members and above). Guests and realtime events never carry it. */
  requester?: TaskRequester;
  source: "manual" | "form" | "template" | "automation" | "api";
  source_reference_id?: number;
  start_date: string | null;
  status_id: number;
  /** Number of live direct subtasks. */
  subtask_count: number;
  tags: Tag[];
  /** Seconds. */
  time_estimate: number;
  /** Seconds, summed from the time entries. */
  time_spent: number;
  title: string;
  updated_at: string;
  /** The technical owner. Permission rules such as 'the author can edit own task' point at it. For a task created from a form it is `owner_id` of the task configuration when that user is still a workspace member, otherwise the form creator; for every other task it is the creating user. */
  user_id: number;
}

export interface TaskCreationState {
  error: string;
  status: TaskCreationStatus;
}

/** Outcome of the automatic task creation for a submission. */
export type TaskCreationStatus = "pending" | "processing" | "succeeded" | "failed" | "skipped";

/** One page of a keyset-paginated task list. */
export interface TaskPage {
  has_next: boolean;
  has_prev: boolean;
  items: Task[];
  next_cursor?: string;
  prev_cursor?: string;
  /** Only with `with_total=true`. */
  total?: number;
}

/** The single priority scale for tasks, task templates and forms. Higher number = more urgent. */
export type TaskPriority = 1 | 2 | 3 | 4 | 5;

/** Name of a TaskPriority, accepted wherever a priority is given as a string (task templates, form `task_defaults.priority` */
export type TaskPriorityName = "low" | "normal" | "high" | "urgent" | "critical";

export interface TaskRelations {
  /** True while at least one blocker is not done. */
  blocked: boolean;
  blocked_by: RelatedTask[];
  blocks: RelatedTask[];
  related: RelatedTask[];
}

/** Who asked for the task. If the respondent of the form was signed in (a valid Bearer token on the public */
export interface TaskRequester {
  email?: string;
  name?: string;
  user?: CommentUser;
}

export interface TaskStatus {
  color: string;
  created_at: string;
  id: number;
  list_id: number;
  name: string;
  position: number;
  /** 1 open, 2 in progress, 3 done. */
  type: 1 | 2 | 3;
  updated_at: string;
}

/** `versions` is filled by the single-template endpoints only. */
export interface TaskTemplate {
  created_at: string;
  created_by: number;
  description: string;
  id: number;
  name: string;
  status: "active" | "archived";
  updated_at: string;
  versions?: TaskTemplateVersion[];
  workspace_id: number;
}

export interface TaskTemplateConfiguration {
  custom_fields?: ({
    field_id: number;
    value: unknown;
  })[];
  /** Reserved for future subtask expansion. */
  subtasks?: (Record<string, unknown>)[];
  tags?: number[];
  task?: {
    assignee_id?: number | null;
    description?: string | null;
    /** Absolute due day (UTC). Mutually exclusive with `due_in_days`. Honored by apply and by form tasks. */
    due_date?: string | null;
    /** Relative due day: that many days after the day of the apply request (UTC) or of the form submission. 0 is the same day. */
    due_in_days?: number | null;
    priority?: TaskPriorityName;
    status_id?: number | null;
    title?: string | null;
  };
}

export interface TaskTemplateRequest {
  configuration: TaskTemplateConfiguration;
  description?: string;
  name: string;
}

export interface TaskTemplateVersion {
  configuration: Record<string, unknown>;
  created_at: string;
  id: number;
  template_id: number;
  version: number;
}

export interface TaskTemplateVersionRequest {
  configuration: TaskTemplateConfiguration;
}

export interface TaskTime {
  entries: TimeEntry[];
  total_seconds: number;
}

export interface TimeEntry {
  created_at: string;
  ended_at: string | null;
  id: number;
  note: string;
  running: boolean;
  seconds: number;
  started_at: string;
  task_id: number;
  user: CommentUser | null;
}

export interface TimeReport {
  from: string;
  to: string;
  total_seconds: number;
  users: ({
    entries: number;
    seconds: number;
    user: CommentUser | null;
  })[];
}

export interface UnreadCount {
  by_type: Record<string, number>;
  total: number;
}

export interface UpdateChecklistItemRequest {
  assignee_id?: number | null;
  resolved?: boolean;
  text?: string;
}

export interface UpdateCommentRequest {
  body?: string;
  mention_user_ids?: number[];
}

export interface UpdateFolderRequest {
  name?: string;
}

export interface UpdateListRequest {
  /** Moves the list into this folder; null moves it to the space root. The folder must be alive and belong to the same space (400 `INVALID_FOLDER_REFERENCE`). Tasks and statuses move along. */
  folder_id?: number | null;
  name?: string;
}

export interface UpdateMemberRoleRequest {
  role: "Admin" | "Member" | "Guest";
}

export interface UpdateProfileRequest {
  bio?: string;
  first_name?: string;
  last_name?: string;
  time_zone?: string;
  username?: string;
}

export interface UpdateSpaceRequest {
  currency?: string;
  name?: string;
}

export interface UpdateStatusRequest {
  color?: string;
  name?: string;
  position?: number;
  type?: number;
}

export interface UpdateTagRequest {
  color?: string;
  /** A name already used by another tag of the space (case-insensitive) is 409 TAG_NAME_TAKEN. */
  name?: string;
}

export interface UpdateTaskRequest {
  add_tag_ids?: number[];
  add_tag_names?: string[];
  /** Replaces the whole set of assignees; [] removes all. Must be workspace members. Responses list assignees as public profiles (id, username, first_name, last_name, avatar_url), never with e-mail, bio or time zone. */
  assignee_ids?: number[];
  description?: string;
  /** null clears the date. Must not be before start_date (400 INVALID_DATE_RANGE). */
  due_date?: string | null;
  /** Another task of the same list; null makes the task top-level. A cycle is rejected. */
  parent_id?: number | null;
  /** 1 low, 2 normal, 3 high, 4 urgent, 5 critical. See TaskPriority. */
  priority?: number;
  /** Tags to detach; tags the task does not have are ignored. Applied atomically with the other changes under the task lock; a tag in both `remove_tag_ids` and `add_tag_ids` is 400 `CONFLICTING_TAG_CHANGE`. The change is recorded as a `tag_ids` activity entry. */
  remove_tag_ids?: number[];
  /** null clears the date. */
  start_date?: string | null;
  status_id?: number;
  /** Seconds. */
  time_estimate?: number;
  title?: string;
}

export interface UpdateTaskTemplateRequest {
  description?: string;
  name?: string;
}

export interface UpdateWorkspaceRequest {
  description?: string;
  name?: string;
}

/** The caller's own account. */
export interface UserProfile {
  avatar_url: string;
  bio: string;
  created_at: string;
  email: string;
  email_verified_at: string | null;
  first_name: string;
  id: number;
  last_name: string;
  /** Present while an e-mail change waits for confirmation. */
  pending_email?: string;
  time_zone: string;
  updated_at: string;
  username: string;
}

export interface VerifyEmailRequest {
  token: string;
}

export interface WatchState {
  watching: boolean;
}

export interface Watchers {
  watchers: CommentUser[];
  watching: boolean;
}

export interface Workspace {
  avatar_url: string;
  created_at: string;
  description: string;
  id: number;
  name: string;
  owner_id: number;
  updated_at: string;
}

export interface WorkspaceSummary {
  avatar_url?: string;
  created_at?: string;
  description?: string;
  id?: number;
  name?: string;
  owner_id?: number;
  /** The caller's role in this workspace. */
  role?: "owner" | "admin" | "member" | "guest";
  updated_at?: string;
}

export interface Endpoints {
  "DELETE /contacts/requests/{user_id}": {
    params: { user_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /contacts/{contact_id}": {
    params: { contact_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /folders/{folder_id}": {
    params: { folder_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /lists/{list_id}": {
    params: { list_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /lists/{list_id}/statuses/{status_id}": {
    params: { list_id: number; status_id: number };
    query?: { replacement_status_id?: number };
    status: 200;
    data: Message;
  };
  "DELETE /notifications/channels/{channel_id}": {
    params: { channel_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /notifications/{notification_id}": {
    params: { notification_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /profile": {
    body: DeleteAccountRequest;
    status: 200;
    data: Message;
  };
  "DELETE /profile/avatar": {
    status: 200;
    data: AvatarUrl;
  };
  "DELETE /profile/email-change": {
    status: 200;
    data: Message;
  };
  "DELETE /spaces/{space_id}": {
    params: { space_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /spaces/{space_id}/tags/{tag_id}": {
    params: { space_id: number; tag_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /task-templates/{template_id}": {
    params: { template_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}": {
    params: { task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/attachments/{attachment_id}": {
    params: { attachment_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/checklists/{checklist_id}": {
    params: { checklist_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/checklists/{checklist_id}/items/{item_id}": {
    params: { checklist_id: number; item_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/comments/{comment_id}": {
    params: { comment_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/custom-fields/{field_id}": {
    params: { field_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/relations/{other_id}": {
    params: { other_id: number; task_id: number };
    query?: { kind: "blocks" | "blocked_by" | "related" };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/time/{entry_id}": {
    params: { entry_id: number; task_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /tasks/{task_id}/watch": {
    params: { task_id: number };
    status: 200;
    data: WatchState;
  };
  "DELETE /workspaces/{workspace_id}": {
    params: { workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/avatar": {
    params: { workspace_id: number };
    status: 200;
    data: AvatarUrl;
  };
  "DELETE /workspaces/{workspace_id}/custom-fields/{field_id}": {
    params: { field_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/forms/{form_id}": {
    params: { form_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/forms/{form_id}/questions/{question_id}": {
    params: { form_id: number; question_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/members/me": {
    params: { workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/members/{user_id}": {
    params: { user_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "DELETE /workspaces/{workspace_id}/views/{view_id}": {
    params: { view_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "GET /contacts": {
    status: 200;
    data: CommentUser[];
    pagination: Pagination;
  };
  "GET /contacts/requests/incoming": {
    status: 200;
    data: ContactEntry[];
    pagination: Pagination;
  };
  "GET /contacts/requests/outgoing": {
    status: 200;
    data: ContactEntry[];
    pagination: Pagination;
  };
  "GET /folders/{folder_id}/lists": {
    params: { folder_id: number };
    status: 200;
    data: List[];
  };
  "GET /form-templates": {
    status: 200;
    data: unknown;
    pagination: Pagination;
  };
  "GET /images/{public_id}": {
    params: { public_id: string };
    status: 200;
  };
  "GET /list-templates": {
    status: 200;
    data: ListTemplate[];
  };
  "GET /lists/{list_id}/events": {
    params: { list_id: number };
    query?: { ticket: string };
  };
  "GET /lists/{list_id}/statuses": {
    params: { list_id: number };
    status: 200;
    data: TaskStatus[];
  };
  "GET /lists/{list_id}/tasks": {
    params: { list_id: number };
    query?: { assignee_id?: string; created_from?: string; created_to?: string; cursor?: string; due_from?: string; due_to?: string; has_due?: boolean; has_open_checklist_items?: boolean; has_parent?: boolean; limit?: number; no_tags?: boolean; parent_id?: string; priority?: string; q?: string; sort?: string; start_from?: string; start_to?: string; status_id?: string; tag_id?: string; unassigned?: boolean; with_total?: boolean };
    status: 200;
    data: TaskPage;
  };
  "GET /notifications": {
    query?: { limit?: number; page?: number; type?: string; unread?: boolean };
    status: 200;
    data: Notification[];
    pagination: Pagination;
  };
  "GET /notifications/channels": {
    status: 200;
    data: NotificationChannel[];
  };
  "GET /notifications/events": {
    query?: { ticket: string };
  };
  "GET /notifications/preferences": {
    status: 200;
    data: NotificationPreference[];
  };
  "GET /notifications/settings": {
    status: 200;
    data: NotificationSettings;
  };
  "GET /notifications/unread-count": {
    status: 200;
    data: UnreadCount;
  };
  "GET /profile": {
    status: 200;
    data: UserProfile;
  };
  "GET /public/forms/{slug}": {
    params: { slug: string };
    status: 200;
    data: PublicForm;
  };
  "GET /spaces/{space_id}": {
    params: { space_id: number };
    status: 200;
    data: Space;
  };
  "GET /spaces/{space_id}/dashboard": {
    params: { space_id: number };
    status: 200;
    data: Space;
  };
  "GET /spaces/{space_id}/events": {
    params: { space_id: number };
    query?: { ticket: string };
  };
  "GET /spaces/{space_id}/folders": {
    params: { space_id: number };
    status: 200;
    data: Folder[];
  };
  "GET /spaces/{space_id}/lists": {
    params: { space_id: number };
    status: 200;
    data: List[];
  };
  "GET /spaces/{space_id}/tags": {
    params: { space_id: number };
    status: 200;
    data: Tag[];
  };
  "GET /spaces/{space_id}/tags/{tag_id}": {
    params: { space_id: number; tag_id: number };
    status: 200;
    data: Tag;
  };
  "GET /submissions/{submission_id}/task-status": {
    params: { submission_id: number };
    status: 200;
    data: TaskCreationState;
  };
  "GET /submissions/{submission_id}/tasks": {
    params: { submission_id: number };
    status: 200;
    data: Task[];
  };
  "GET /task-templates/{template_id}": {
    params: { template_id: number };
    status: 200;
    data: TaskTemplate;
  };
  "GET /task-templates/{template_id}/versions": {
    params: { template_id: number };
    status: 200;
    data: TaskTemplateVersion[];
  };
  "GET /tasks/{task_id}": {
    params: { task_id: number };
    status: 200;
    data: Task;
  };
  "GET /tasks/{task_id}/activity": {
    params: { task_id: number };
    query?: { limit?: number; page?: number };
    status: 200;
    data: Activity[];
  };
  "GET /tasks/{task_id}/attachments": {
    params: { task_id: number };
    status: 200;
    data: Attachment[];
  };
  "GET /tasks/{task_id}/attachments/{attachment_id}/download": {
    params: { attachment_id: number; task_id: number };
    status: 200;
  };
  "GET /tasks/{task_id}/checklists": {
    params: { task_id: number };
    status: 200;
    data: Checklist[];
  };
  "GET /tasks/{task_id}/comments": {
    params: { task_id: number };
    query?: { limit?: number; page?: number };
    status: 200;
    data: Comment[];
    pagination: Pagination;
  };
  "GET /tasks/{task_id}/custom-fields": {
    params: { task_id: number };
    status: 200;
    data: CustomFieldValues;
  };
  "GET /tasks/{task_id}/relations": {
    params: { task_id: number };
    status: 200;
    data: TaskRelations;
  };
  "GET /tasks/{task_id}/time": {
    params: { task_id: number };
    status: 200;
    data: TaskTime;
  };
  "GET /tasks/{task_id}/watchers": {
    params: { task_id: number };
    status: 200;
    data: Watchers;
  };
  "GET /time/running": {
    status: 200;
    data: RunningTimer;
  };
  "GET /users/search": {
    query?: { q?: string };
    status: 200;
    data: CommentUser[];
  };
  "GET /workspaces": {
    query?: { limit?: number; page?: number };
    status: 200;
    data: unknown;
    pagination: Pagination;
  };
  "GET /workspaces/{workspace_id}": {
    params: { workspace_id: number };
    status: 200;
    data: Workspace;
  };
  "GET /workspaces/{workspace_id}/audit-log": {
    params: { workspace_id: number };
    query?: { action?: string; limit?: number; page?: number };
    status: 200;
    data: AuditEntry[];
    pagination: Pagination;
  };
  "GET /workspaces/{workspace_id}/custom-fields": {
    params: { workspace_id: number };
    status: 200;
    data: CustomField[];
  };
  "GET /workspaces/{workspace_id}/custom-fields/{field_id}": {
    params: { field_id: number; workspace_id: number };
    status: 200;
    data: CustomField;
  };
  "GET /workspaces/{workspace_id}/forms": {
    params: { workspace_id: number };
    status: 200;
    data: Form[];
  };
  "GET /workspaces/{workspace_id}/forms/{form_id}": {
    params: { form_id: number; workspace_id: number };
    status: 200;
    data: Form;
  };
  "GET /workspaces/{workspace_id}/forms/{form_id}/submissions": {
    params: { form_id: number; workspace_id: number };
    query?: { limit?: number; page?: number; status?: "submitted" | "processing" | "processed" | "failed"; task_creation_status?: TaskCreationStatus };
    status: 200;
    data: FormSubmission[];
    pagination: Pagination;
  };
  "GET /workspaces/{workspace_id}/forms/{form_id}/task-config": {
    params: { form_id: number; workspace_id: number };
    status: 200;
    data: FormTaskConfigView;
  };
  "GET /workspaces/{workspace_id}/members": {
    params: { workspace_id: number };
    status: 200;
    data: Member[];
  };
  "GET /workspaces/{workspace_id}/search": {
    params: { workspace_id: number };
    query?: { limit?: number; page?: number; q: string; space_id?: number; types?: string };
    status: 200;
    data: unknown;
    pagination: Pagination;
  };
  "GET /workspaces/{workspace_id}/spaces": {
    params: { workspace_id: number };
    query?: { limit?: number; page?: number };
    status: 200;
    data: Space[];
    pagination: Pagination;
  };
  "GET /workspaces/{workspace_id}/submissions/{submission_id}": {
    params: { submission_id: number; workspace_id: number };
    status: 200;
    data: FormSubmission;
  };
  "GET /workspaces/{workspace_id}/submissions/{submission_id}/files/{file_id}": {
    params: { file_id: number; submission_id: number; workspace_id: number };
    status: 200;
  };
  "GET /workspaces/{workspace_id}/task-templates": {
    params: { workspace_id: number };
    status: 200;
    data: TaskTemplate[];
  };
  "GET /workspaces/{workspace_id}/time-report": {
    params: { workspace_id: number };
    query?: { from?: string; to?: string; user_id?: number };
    status: 200;
    data: TimeReport;
  };
  "GET /workspaces/{workspace_id}/views": {
    params: { workspace_id: number };
    query?: { list_id?: number };
    status: 200;
    data: SavedView[];
  };
  "PATCH /folders/{folder_id}": {
    params: { folder_id: number };
    body: UpdateFolderRequest;
    status: 200;
    data: Folder;
  };
  "PATCH /lists/{list_id}": {
    params: { list_id: number };
    body: UpdateListRequest;
    status: 200;
    data: List;
  };
  "PATCH /profile": {
    body: UpdateProfileRequest;
    status: 200;
    data: UserProfile;
  };
  "PATCH /spaces/{space_id}": {
    params: { space_id: number };
    body: UpdateSpaceRequest;
    status: 200;
    data: Space;
  };
  "PATCH /task-templates/{template_id}": {
    params: { template_id: number };
    body: UpdateTaskTemplateRequest;
    status: 200;
    data: TaskTemplate;
  };
  "PATCH /tasks/{task_id}": {
    params: { task_id: number };
    body: UpdateTaskRequest;
    status: 200;
    data: Task;
  };
  "PATCH /tasks/{task_id}/checklists/{checklist_id}": {
    params: { checklist_id: number; task_id: number };
    body: ChecklistNameRequest;
    status: 200;
    data: Checklist;
  };
  "PATCH /tasks/{task_id}/checklists/{checklist_id}/items/{item_id}": {
    params: { checklist_id: number; item_id: number; task_id: number };
    body: UpdateChecklistItemRequest;
    status: 200;
    data: ChecklistItem;
  };
  "PATCH /tasks/{task_id}/comments/{comment_id}": {
    params: { comment_id: number; task_id: number };
    body: UpdateCommentRequest;
    status: 200;
    data: Comment;
  };
  "PATCH /tasks/{task_id}/time/{entry_id}": {
    params: { entry_id: number; task_id: number };
    status: 200;
    data: TimeEntry;
  };
  "PATCH /workspaces/{workspace_id}": {
    params: { workspace_id: number };
    body: UpdateWorkspaceRequest;
    status: 200;
    data: Workspace;
  };
  "PATCH /workspaces/{workspace_id}/custom-fields/{field_id}": {
    params: { field_id: number; workspace_id: number };
    body: CustomFieldRequest;
    status: 200;
    data: CustomField;
  };
  "PATCH /workspaces/{workspace_id}/forms/{form_id}": {
    params: { form_id: number; workspace_id: number };
    body: FormRequest;
    status: 200;
    data: Form;
  };
  "PATCH /workspaces/{workspace_id}/forms/{form_id}/questions/{question_id}": {
    params: { form_id: number; question_id: number; workspace_id: number };
    body: FormQuestionRequest;
    status: 200;
    data: FormQuestion;
  };
  "PATCH /workspaces/{workspace_id}/members/{user_id}": {
    params: { user_id: number; workspace_id: number };
    body: UpdateMemberRoleRequest;
    status: 200;
    data: Message;
  };
  "PATCH /workspaces/{workspace_id}/views/{view_id}": {
    params: { view_id: number; workspace_id: number };
    status: 200;
    data: SavedView;
  };
  "POST /auth/confirm-email-change": {
    body: VerifyEmailRequest;
    status: 200;
    data: Message;
  };
  "POST /auth/forgot-password": {
    body: EmailRequest;
    status: 202;
    data: Message;
  };
  "POST /auth/login": {
    body: LoginRequest;
    status: 200;
    data: AccessToken;
  };
  "POST /auth/logout": {
    status: 200;
    data: Message;
  };
  "POST /auth/logout-all": {
    status: 200;
    data: Message;
  };
  "POST /auth/refresh": {
    status: 200;
    data: AccessToken;
  };
  "POST /auth/register": {
    body: RegisterRequest;
    status: 201;
    data: Message;
  };
  "POST /auth/resend-verification": {
    body: EmailRequest;
    status: 202;
    data: Message;
  };
  "POST /auth/reset-password": {
    body: ResetPasswordRequest;
    status: 200;
    data: Message;
  };
  "POST /auth/verify-email": {
    body: VerifyEmailRequest;
    status: 200;
    data: Message;
  };
  "POST /contacts": {
    body: AddContactRequest;
    status: 202;
    data: Message;
  };
  "POST /contacts/requests/{user_id}/accept": {
    params: { user_id: number };
    status: 200;
    data: Message;
  };
  "POST /contacts/requests/{user_id}/decline": {
    params: { user_id: number };
    status: 200;
    data: Message;
  };
  "POST /folders/{folder_id}/lists": {
    params: { folder_id: number };
    body: CreateListRequest;
    status: 201;
    data: List;
  };
  "POST /lists/{list_id}/events/ticket": {
    params: { list_id: number };
    status: 200;
    data: EventTicket;
  };
  "POST /lists/{list_id}/statuses": {
    params: { list_id: number };
    body: CreateStatusRequest;
    status: 201;
    data: TaskStatus;
  };
  "POST /lists/{list_id}/tasks": {
    params: { list_id: number };
    body: CreateTaskRequest;
    status: 201;
    data: Task;
  };
  "POST /notifications/channels": {
    status: 201;
    data: NotificationChannel;
  };
  "POST /notifications/channels/confirm": {
    status: 200;
    data: Message;
  };
  "POST /notifications/channels/{channel_id}/resend": {
    params: { channel_id: number };
    status: 202;
    data: Message;
  };
  "POST /notifications/channels/{channel_id}/rotate-secret": {
    params: { channel_id: number };
    status: 200;
    data: SecretResult;
  };
  "POST /notifications/events/ticket": {
    status: 200;
    data: EventTicket;
  };
  "POST /notifications/read": {
    status: 200;
    data: ReadResult;
  };
  "POST /profile/avatar": {
    status: 200;
    data: AvatarUrl;
  };
  "POST /profile/email": {
    body: ChangeEmailRequest;
    status: 202;
    data: Message;
  };
  "POST /profile/password": {
    body: ChangePasswordRequest;
    status: 200;
    data: AccessToken;
  };
  "POST /public/forms/{slug}/submissions": {
    params: { slug: string };
    body: FormSubmissionRequest;
    status: 200;
    data: PublicFormSubmission;
  };
  "POST /public/forms/{slug}/uploads": {
    params: { slug: string };
    status: 201;
    data: unknown;
    pagination: Pagination;
  };
  "POST /spaces/{space_id}/events/ticket": {
    params: { space_id: number };
    status: 200;
    data: EventTicket;
  };
  "POST /spaces/{space_id}/folders": {
    params: { space_id: number };
    body: CreateFolderRequest;
    status: 201;
    data: Folder;
  };
  "POST /spaces/{space_id}/lists": {
    params: { space_id: number };
    body: CreateListRequest;
    status: 201;
    data: List;
  };
  "POST /spaces/{space_id}/tags": {
    params: { space_id: number };
    body: CreateTagRequest;
    status: 201;
    data: Tag;
  };
  "POST /spaces/{space_id}/tags/merge": {
    params: { space_id: number };
    body: MergeTagsRequest;
    status: 200;
    data: Message;
  };
  "POST /submissions/{submission_id}/retry-task-creation": {
    params: { submission_id: number };
    status: 200;
    data: TaskCreationState;
  };
  "POST /task-templates/{template_id}/apply": {
    params: { template_id: number };
    body: ApplyTaskTemplateRequest;
    status: 201;
    data: Task;
  };
  "POST /task-templates/{template_id}/versions": {
    params: { template_id: number };
    body: TaskTemplateVersionRequest;
    status: 201;
    data: TaskTemplateVersion;
  };
  "POST /tasks/bulk": {
    status: 200;
    data: BulkTaskResult;
  };
  "POST /tasks/{task_id}/attachments": {
    params: { task_id: number };
    status: 201;
    data: Attachment;
  };
  "POST /tasks/{task_id}/checklists": {
    params: { task_id: number };
    body: ChecklistNameRequest;
    status: 201;
    data: Checklist;
  };
  "POST /tasks/{task_id}/checklists/{checklist_id}/items": {
    params: { checklist_id: number; task_id: number };
    body: CreateChecklistItemRequest;
    status: 201;
    data: ChecklistItem;
  };
  "POST /tasks/{task_id}/comments": {
    params: { task_id: number };
    body: CreateCommentRequest;
    status: 201;
    data: Comment;
  };
  "POST /tasks/{task_id}/move": {
    params: { task_id: number };
    body: MoveTaskRequest;
    status: 200;
    data: Task;
  };
  "POST /tasks/{task_id}/relations": {
    params: { task_id: number };
    status: 201;
    data: TaskRelations;
  };
  "POST /tasks/{task_id}/time": {
    params: { task_id: number };
    status: 201;
    data: TimeEntry;
  };
  "POST /tasks/{task_id}/time/start": {
    params: { task_id: number };
    status: 201;
    data: TimeEntry;
  };
  "POST /tasks/{task_id}/time/stop": {
    params: { task_id: number };
    status: 200;
    data: TimeEntry;
  };
  "POST /workspaces": {
    body: CreateWorkspaceRequest;
    status: 201;
    data: Workspace;
  };
  "POST /workspaces/{workspace_id}/avatar": {
    params: { workspace_id: number };
    status: 200;
    data: AvatarUrl;
  };
  "POST /workspaces/{workspace_id}/custom-fields": {
    params: { workspace_id: number };
    body: CustomFieldRequest;
    status: 201;
    data: CustomField;
  };
  "POST /workspaces/{workspace_id}/forms": {
    params: { workspace_id: number };
    body: FormCreateRequest;
    status: 201;
    data: Form;
  };
  "POST /workspaces/{workspace_id}/forms/{form_id}/archive": {
    params: { form_id: number; workspace_id: number };
    status: 200;
    data: Message;
  };
  "POST /workspaces/{workspace_id}/forms/{form_id}/publish": {
    params: { form_id: number; workspace_id: number };
    status: 200;
    data: Form;
  };
  "POST /workspaces/{workspace_id}/forms/{form_id}/questions": {
    params: { form_id: number; workspace_id: number };
    body: FormQuestionRequest;
    status: 201;
    data: FormQuestion;
  };
  "POST /workspaces/{workspace_id}/images": {
    params: { workspace_id: number };
    status: 201;
    data: ImageUpload;
  };
  "POST /workspaces/{workspace_id}/images/rehost": {
    params: { workspace_id: number };
    body: {
    url: string;
  };
    status: 201;
    data: ImageUpload;
  };
  "POST /workspaces/{workspace_id}/members": {
    params: { workspace_id: number };
    body: AddMemberRequest;
    status: 201;
    data: Message;
  };
  "POST /workspaces/{workspace_id}/spaces": {
    params: { workspace_id: number };
    body: CreateSpaceRequest;
    status: 201;
    data: Space;
  };
  "POST /workspaces/{workspace_id}/task-templates": {
    params: { workspace_id: number };
    body: TaskTemplateRequest;
    status: 201;
    data: TaskTemplate;
  };
  "POST /workspaces/{workspace_id}/views": {
    params: { workspace_id: number };
    status: 201;
    data: SavedView;
  };
  "PUT /lists/{list_id}/statuses/{status_id}": {
    params: { list_id: number; status_id: number };
    body: UpdateStatusRequest;
    status: 200;
    data: TaskStatus;
  };
  "PUT /notifications/preferences": {
    status: 200;
    data: NotificationPreference[];
  };
  "PUT /notifications/settings": {
    status: 200;
    data: NotificationSettings;
  };
  "PUT /spaces/{space_id}/tags/{tag_id}": {
    params: { space_id: number; tag_id: number };
    body: UpdateTagRequest;
    status: 200;
    data: Tag;
  };
  "PUT /tasks/{task_id}/checklists/order": {
    params: { task_id: number };
    body: ReorderRequest;
    status: 200;
    data: Checklist[];
  };
  "PUT /tasks/{task_id}/checklists/{checklist_id}/items/order": {
    params: { checklist_id: number; task_id: number };
    body: ReorderRequest;
    status: 200;
    data: Checklist[];
  };
  "PUT /tasks/{task_id}/custom-fields/{field_id}": {
    params: { field_id: number; task_id: number };
    body: CustomFieldValueRequest;
    status: 200;
    data: Message;
  };
  "PUT /tasks/{task_id}/watch": {
    params: { task_id: number };
    status: 200;
    data: WatchState;
  };
  "PUT /workspaces/{workspace_id}/forms/{form_id}/questions/reorder": {
    params: { form_id: number; workspace_id: number };
    body: ReorderFormQuestionsRequest;
    status: 200;
    data: Message;
  };
  "PUT /workspaces/{workspace_id}/forms/{form_id}/task-config": {
    params: { form_id: number; workspace_id: number };
    body: FormTaskConfigurationRequest;
    status: 200;
    data: FormTaskConfigView;
  };
}
