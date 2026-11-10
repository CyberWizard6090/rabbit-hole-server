# API errors

Every failed request answers with the same body. It has **no human readable
text**: the client picks the wording from its own translations by `code` and
fills placeholders from `params`.

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "request_id": "9f1c2a7b4d3e8a105c6b7d2e",
    "details": [
      { "field": "title", "code": "TOO_LONG", "params": { "max": 255 } },
      { "field": "items[2].text", "code": "REQUIRED" }
    ]
  }
}
```

| Field        | Meaning |
|--------------|---------|
| `code`       | Stable identifier of what went wrong. Translate it. |
| `params`     | Values the translation needs, for example `{"retry_after": 30}`. Absent when there are none. |
| `details`    | One entry per invalid input of a request. `field` is the JSON path the client sent (`title`, `items[2].text`, query parameter name); `code` and `params` work like the top level ones. |
| `request_id` | The `X-Request-ID` of the request. Show it next to unexpected errors so support can find the log line. |

The HTTP status says how to react, the `code` says what to tell the user:

| Status | Client reaction |
|--------|-----------------|
| 400 / 409 / 422 | Show the translated `code`; mark the fields named in `details`. |
| 401 `ACCESS_TOKEN_EXPIRED` | Refresh the token silently and repeat the request. |
| 401 other (`UNAUTHORIZED`, `ACCESS_TOKEN_INVALID`) | The session is gone: go to the login page. |
| 400 `INVALID_CURRENT_PASSWORD` | The password typed into a "current password" field is wrong (detail on `current_password`). Show it on the field; the session is fine. |
| 403 `FORBIDDEN` | The user lacks the permission, **or the object does not exist or was deleted**: the answer is identical on purpose so ids cannot be enumerated. The code of the missing permission is deliberately not disclosed. |
| 403 `OWNER_IMMUTABLE` | The workspace owner cannot be demoted, removed or leave. |
| 403 `ADMIN_ROLE_OWNER_ONLY` | Only the workspace owner may grant the Admin role, or demote or remove an Admin. |
| 404 | A child object (form, submission, custom field, tag, comment, ...) is missing inside a workspace, space or task the caller can access. The status is the contract: a response may carry the generic `NOT_FOUND` or a more specific `*_NOT_FOUND` code, so do not branch on the code. |
| 429 `RATE_LIMIT_EXCEEDED` | Wait `params.retry_after` seconds (also sent as the `Retry-After` header). |
| 5xx | Show a generic message with `request_id`. The code only identifies the failing operation. |

## 403 versus 404

Objects addressed by a workspace, space, folder, list, task, task template or form submission id answer
`403 FORBIDDEN` when they are missing or deleted, exactly like when the caller has no access, also for the
workspace owner. Only a missing child object inside a container the caller can access answers `404`. See
"Access policy" and "Roles and permissions" in `docs/openapi.yaml`.

## Detail codes

The codes below appear in `details[].code` and have the same meaning in every
endpoint. Unlisted params are not sent.

| Code | Params | Meaning |
|------|--------|---------|
| `REQUIRED` | | The value is missing or empty. |
| `INVALID_VALUE` | | The value is not acceptable. |
| `INVALID_TYPE` | `expected`: `string`, `number`, `integer`, `boolean`, `array`, `object` | The value has the wrong JSON type. |
| `INVALID_EMAIL`, `INVALID_COLOR`, `INVALID_TIMEZONE`, `INVALID_DATE`, `INVALID_FORMAT`, `INVALID_SLUG`, `INVALID_BOOLEAN` | `INVALID_DATE`: `format` | The value does not have the expected format. |
| `INVALID_CHOICE` | `allowed` (list, when it is short and fixed), `value` | The value is not one of the allowed ones. |
| `INVALID_RANGE`, `OUT_OF_RANGE` | `min`, `max` | The number is outside the range. |
| `TOO_SHORT`, `TOO_LONG` | `min` / `max` | Text length (characters) is out of bounds. |
| `TOO_SMALL`, `TOO_LARGE` | `min` / `max`, `exclusive` | A number is out of bounds. `TOO_LARGE` for a payload also sends `max_bytes`. |
| `TOO_FEW`, `TOO_MANY` | `min` / `max` | A list has too few or too many items. |
| `INVALID_LENGTH` | `length` | The length must be exactly `length`. |
| `INVALID_COUNT` | `min`, `max` | The number of items is outside the range. |
| `DUPLICATE` | `value` | The value is used twice where it must be unique. |
| `NOT_FOUND`, `INVALID_REFERENCE` | `tag_id`, `field_id`, `status_id`, `list_id` | The referenced object does not exist or does not fit here. |
| `NOT_ALLOWED`, `NOT_SUPPORTED` | `question_type` | The field is not allowed in this context. |
| `EXCLUSIVE_FIELDS` | `fields` | Only one of the listed fields may be set. |
| `CONFLICTING_FIELD` | `with` | The field cannot be combined with the one named in `with`. |
| `EXCEEDS_FIELD`, `NOT_LESS_THAN_FIELD`, `AFTER_FIELD` | `other` | The value must not exceed / must be less than / must not come after the field named in `other`. |
| `EXCEEDS_OPTIONS` | `max` | More choices than the question has options. |
| `NOT_A_MEMBER` | `user_id` | The user does not belong to the workspace. |
| `WORKSPACE_MISMATCH` | | The object belongs to another workspace. |
| `NOT_IN_LIST`, `NOT_IN_SPACE`, `NOT_IN_WORKSPACE` | `id` / `ids` | The referenced status, parent task or anchor is not in the task's list, the tag not in its space, the list not in its workspace. |
| `EMPTY` | | An optional answer was sent empty; omit the key instead. |
| `INVALID_FILE` | | The uploaded file id of a form answer is malformed, unknown, expired or already used. |
| `UNKNOWN_QUESTION` | `question_id` | The question is not part of the form (or of the published version). |
| `MISSING` | `question_id` | A question is missing from a list that must contain all of them. |
| `HAS_ANSWERS`, `OPTION_IN_USE` | `answers`, `value` | The type or option of a question cannot change because stored answers use it. |
| `NOT_EARLIER`, `RULE_NOT_EARLIER`, `SELF_REFERENCE` | `question_id`, `depends_on` | A `visible_if` rule must refer to a question placed earlier in the form. |
| `INVALID_SCHEME` | `allowed` | The URL scheme is not allowed. |
| `INCOMPATIBLE_QUESTION` | `target` | The question type does not fit the mapping target. |
| `MISSING_TRANSFORM_ENTRY` | `option` | The enum transform has no entry for this option. |

Top level codes that carry params: `INVALID_ID` (`param`: the path parameter),
`PAYLOAD_TOO_LARGE` (`max_bytes`), `RATE_LIMIT_EXCEEDED` (`retry_after`).

## All codes

Generated from the source; `go test ./internal/architecture` fails when this
list is stale. Regenerate it with `UPDATE_ERROR_CODES=1 go test ./internal/architecture`.

<!-- codes:start -->
- `ACCESS_TOKEN_EXPIRED`
- `ACCESS_TOKEN_INVALID`
- `ACCOUNT_DELETE_FAILED`
- `ACTIVITY_FETCH_FAILED`
- `ADMIN_ROLE_OWNER_ONLY`
- `AFTER_FIELD`
- `ALREADY_LOGGED_OUT`
- `ALREADY_MEMBER`
- `ATTACHMENTS_FETCH_FAILED`
- `ATTACHMENT_DELETE_FAILED`
- `ATTACHMENT_DOWNLOAD_FAILED`
- `ATTACHMENT_UPLOAD_FAILED`
- `AUDIT_LOG_FETCH_FAILED`
- `AVATAR_DELETE_FAILED`
- `AVATAR_UPLOAD_FAILED`
- `BEFORE_FROM`
- `CANNOT_ADD_SELF`
- `CHANNELS_FETCH_FAILED`
- `CHANNEL_ALREADY_CONFIRMED`
- `CHANNEL_CONFIRM_FAILED`
- `CHANNEL_CREATE_FAILED`
- `CHANNEL_DELETE_FAILED`
- `CHANNEL_EXISTS`
- `CHANNEL_HAS_NO_DESTINATION`
- `CHANNEL_HAS_NO_SECRET`
- `CHANNEL_LIMIT_REACHED`
- `CHANNEL_RESEND_FAILED`
- `CHANNEL_ROTATE_FAILED`
- `CHECKLISTS_FETCH_FAILED`
- `CHECKLIST_CREATE_FAILED`
- `CHECKLIST_DELETE_FAILED`
- `CHECKLIST_ITEM_CREATE_FAILED`
- `CHECKLIST_ITEM_DELETE_FAILED`
- `CHECKLIST_ITEM_REORDER_FAILED`
- `CHECKLIST_ITEM_UPDATE_FAILED`
- `CHECKLIST_LIMIT_REACHED`
- `CHECKLIST_NOT_FOUND`
- `CHECKLIST_REORDER_FAILED`
- `CHECKLIST_UPDATE_FAILED`
- `COMMENTS_FETCH_FAILED`
- `COMMENT_CREATE_FAILED`
- `COMMENT_DELETE_FAILED`
- `COMMENT_FORBIDDEN`
- `COMMENT_NOT_FOUND`
- `COMMENT_UPDATE_FAILED`
- `CONFIRMATION_DELIVERY_FAILED`
- `CONFIRMATION_TOO_SOON`
- `CONFLICTING_FIELD`
- `CONFLICTING_TAG_CHANGE`
- `CONFLICTS_WITH`
- `CONTACTS_FETCH_FAILED`
- `CONTACT_DELETE_FAILED`
- `CONTACT_NOT_FOUND`
- `CONTACT_REQUESTS_FETCH_FAILED`
- `CONTACT_REQUEST_ACCEPT_FAILED`
- `CONTACT_REQUEST_CANCEL_FAILED`
- `CONTACT_REQUEST_DECLINE_FAILED`
- `CONTACT_REQUEST_FAILED`
- `CONTACT_REQUEST_LIMIT_REACHED`
- `CONTACT_REQUEST_NOT_FOUND`
- `CUSTOM_FIELDS_FETCH_FAILED`
- `CUSTOM_FIELD_CREATE_FAILED`
- `CUSTOM_FIELD_DELETE_FAILED`
- `CUSTOM_FIELD_FETCH_FAILED`
- `CUSTOM_FIELD_IN_USE`
- `CUSTOM_FIELD_NAME_TAKEN`
- `CUSTOM_FIELD_NOT_FOUND`
- `CUSTOM_FIELD_UPDATE_FAILED`
- `CUSTOM_FIELD_VALUE_OUT_OF_RANGE`
- `EMAIL_CHANGE_COOLDOWN`
- `EMAIL_CHANGE_FAILED`
- `EMAIL_NOT_VERIFIED`
- `EMAIL_TAKEN`
- `EMAIL_VERIFICATION_FAILED`
- `EXCEEDS_FIELD`
- `EXCEEDS_OPTIONS`
- `EXCLUSIVE_FIELDS`
- `FILE_EMPTY`
- `FILE_FORBIDDEN`
- `FILE_NOT_FOUND`
- `FILE_READ_FAILED`
- `FILE_REQUIRED`
- `FILE_TOO_LARGE`
- `FOLDERS_FETCH_FAILED`
- `FOLDER_CREATE_FAILED`
- `FOLDER_DELETE_FAILED`
- `FOLDER_NOT_FOUND`
- `FOLDER_UPDATE_FAILED`
- `FORMS_FETCH_FAILED`
- `FORM_ACCENT_COLOR_INVALID`
- `FORM_ARCHIVE_FAILED`
- `FORM_CLOSED`
- `FORM_COVER_INVALID`
- `FORM_CREATE_FAILED`
- `FORM_DELETE_FAILED`
- `FORM_FETCH_FAILED`
- `FORM_FULL`
- `FORM_HAS_SUBMISSIONS`
- `FORM_LOGO_INVALID`
- `FORM_MAX_SUBMISSIONS_INVALID`
- `FORM_NOTIFY_RECIPIENT_INVALID`
- `FORM_NOT_FOUND`
- `FORM_PUBLISH_FAILED`
- `FORM_QUESTION_CREATE_FAILED`
- `FORM_QUESTION_DELETE_FAILED`
- `FORM_QUESTION_IN_USE`
- `FORM_QUESTION_REFERENCED`
- `FORM_QUESTION_REORDER_FAILED`
- `FORM_QUESTION_REORDER_INVALID`
- `FORM_QUESTION_SETTINGS_INVALID`
- `FORM_QUESTION_UPDATE_FAILED`
- `FORM_QUESTION_VISIBILITY_CYCLE`
- `FORM_QUESTION_VISIBILITY_INVALID`
- `FORM_REDIRECT_URL_INVALID`
- `FORM_SLUG_TAKEN`
- `FORM_SUBMISSIONS_FETCH_FAILED`
- `FORM_SUBMISSIONS_LIMITED`
- `FORM_SUBMISSION_FAILED`
- `FORM_SUBMIT_BUTTON_TEXT_INVALID`
- `FORM_SUCCESS_MESSAGE_INVALID`
- `FORM_TASK_FAILED`
- `FORM_TASK_STATUS_NOT_IN_LIST`
- `FORM_TASK_TEMPLATE_INVALID`
- `FORM_UPDATE_FAILED`
- `FORM_UPLOADS_LIMITED`
- `FORM_UPLOADS_NOT_ACCEPTED`
- `FORM_UPLOAD_FAILED`
- `FORM_UPLOAD_INVALID`
- `HAS_ANSWERS`
- `IDEMPOTENCY_CONFLICT`
- `IMAGE_FETCH_FAILED`
- `IMAGE_REHOST_FAILED`
- `IMAGE_UPLOAD_FAILED`
- `INCOMPATIBLE_QUESTION`
- `INTERNAL_ERROR`
- `INVALID_ASSIGNEE`
- `INVALID_BOOLEAN`
- `INVALID_CHOICE`
- `INVALID_COLOR`
- `INVALID_COMMENT_ID`
- `INVALID_CONTACT_ID`
- `INVALID_COUNT`
- `INVALID_CREDENTIALS`
- `INVALID_CURRENT_PASSWORD`
- `INVALID_CUSTOM_FIELD`
- `INVALID_DATE`
- `INVALID_DATE_RANGE`
- `INVALID_DESTINATION`
- `INVALID_EMAIL`
- `INVALID_ENCODING`
- `INVALID_FILE`
- `INVALID_FILTER`
- `INVALID_FOLDER_ID`
- `INVALID_FOLDER_REFERENCE`
- `INVALID_FORM`
- `INVALID_FORMAT`
- `INVALID_FORM_TASK_CONFIG`
- `INVALID_ID`
- `INVALID_IMAGE`
- `INVALID_JSON`
- `INVALID_LENGTH`
- `INVALID_LIST_ID`
- `INVALID_MENTION`
- `INVALID_OPTION`
- `INVALID_OPTION_VALUE`
- `INVALID_ORDER`
- `INVALID_PAGINATION`
- `INVALID_QUERY`
- `INVALID_RANGE`
- `INVALID_REFERENCE`
- `INVALID_REPLACEMENT_ID`
- `INVALID_REPLACEMENT_STATUS`
- `INVALID_ROLE`
- `INVALID_SCHEME`
- `INVALID_SEARCH_TYPES`
- `INVALID_SLUG`
- `INVALID_SORT`
- `INVALID_SPACE_ID`
- `INVALID_STATUS_ID`
- `INVALID_TARGET`
- `INVALID_TASKS_PER_LIST`
- `INVALID_TASK_ID`
- `INVALID_TASK_REFERENCE`
- `INVALID_TASK_TEMPLATE`
- `INVALID_TASK_TEMPLATE_APPLICATION`
- `INVALID_TICKET`
- `INVALID_TIMEZONE`
- `INVALID_TIME_ENTRY`
- `INVALID_TIME_ENTRY_ID`
- `INVALID_TOKEN`
- `INVALID_TRANSFORM`
- `INVALID_TYPE`
- `INVALID_USER_ID`
- `INVALID_VALUE`
- `INVALID_VIEW_ID`
- `INVALID_WORKSPACE_ID`
- `IN_THE_FUTURE`
- `LISTS_FETCH_FAILED`
- `LIST_CREATE_FAILED`
- `LIST_DELETE_FAILED`
- `LIST_NOT_FOUND`
- `LIST_UPDATE_FAILED`
- `LOGOUT_FAILED`
- `MEMBERS_FETCH_FAILED`
- `MEMBER_ADD_FAILED`
- `MEMBER_LEAVE_FAILED`
- `MEMBER_REMOVE_FAILED`
- `MEMBER_UPDATE_FAILED`
- `METHOD_NOT_ALLOWED`
- `MISSING_TRANSFORM_ENTRY`
- `NOTIFICATIONS_COUNT_FAILED`
- `NOTIFICATIONS_FETCH_FAILED`
- `NOTIFICATIONS_READ_FAILED`
- `NOTIFICATION_DELETE_FAILED`
- `NOT_ALLOWED`
- `NOT_A_CONTACT`
- `NOT_A_MEMBER`
- `NOT_EARLIER`
- `NOT_FOUND`
- `NOT_IN_LIST`
- `NOT_IN_SPACE`
- `NOT_IN_WORKSPACE`
- `NOT_LESS_THAN_FIELD`
- `NOT_SUPPORTED`
- `NO_RUNNING_TIMER`
- `OPTION_IN_USE`
- `OUT_OF_RANGE`
- `OWNER_IMMUTABLE`
- `OWNS_WORKSPACES`
- `PASSWORD_CHANGE_FAILED`
- `PASSWORD_RESET_FAILED`
- `PAYLOAD_TOO_LARGE`
- `PERMISSION_CHECK_FAILED`
- `PREFERENCES_FETCH_FAILED`
- `PREFERENCES_SAVE_FAILED`
- `PROFILE_UPDATE_FAILED`
- `PUBLIC_FORM_FETCH_FAILED`
- `QUERY_REQUIRED`
- `QUERY_TOO_LONG`
- `QUERY_TOO_SHORT`
- `RANGE_TOO_LONG`
- `RATE_LIMIT_EXCEEDED`
- `REGISTRATION_FAILED`
- `REHOST_REFUSED`
- `RELATION_CYCLE`
- `RELATION_EXISTS`
- `RELATION_FAILED`
- `RELATION_LIMIT_REACHED`
- `RELATION_NOT_FOUND`
- `RELATION_SELF`
- `REQUIRED_FOR_NEW_FORM`
- `REQUIRED_UNLESS_TEMPLATE_STATUS`
- `ROUTE_NOT_FOUND`
- `RULE_NOT_EARLIER`
- `SAME_EMAIL`
- `SAME_TASK`
- `SEARCH_FAILED`
- `SELF_REFERENCE`
- `SERVER_BUSY`
- `SESSION_LIST_FAILED`
- `SESSION_NOT_FOUND`
- `SESSION_REVOKE_FAILED`
- `SESSION_UNKNOWN`
- `SETTINGS_FETCH_FAILED`
- `SETTINGS_SAVE_FAILED`
- `SHUTTING_DOWN`
- `SPACES_FETCH_FAILED`
- `SPACE_CREATE_FAILED`
- `SPACE_DELETE_FAILED`
- `SPACE_NAME_REQUIRED`
- `SPACE_NOT_FOUND`
- `SPACE_UPDATE_FAILED`
- `STATUSES_FETCH_FAILED`
- `STATUS_CREATE_FAILED`
- `STATUS_DELETE_FAILED`
- `STATUS_IN_USE`
- `STATUS_NOT_FOUND`
- `STATUS_UPDATE_FAILED`
- `STATUS_USED_BY_FORM`
- `SUBMISSION_FETCH_FAILED`
- `SUBMISSION_FILE_DOWNLOAD_FAILED`
- `SUBMISSION_OR_FORM_NOT_FOUND`
- `SUBMISSION_PERMISSION_FAILED`
- `SUBSCRIBE_FAILED`
- `TAGS_FETCH_FAILED`
- `TAG_CREATE_FAILED`
- `TAG_DELETE_FAILED`
- `TAG_FETCH_FAILED`
- `TAG_MERGE_FAILED`
- `TAG_MERGE_SELF`
- `TAG_NAME_TAKEN`
- `TAG_NOT_FOUND`
- `TAG_UPDATE_FAILED`
- `TASKS_FETCH_FAILED`
- `TASK_CREATE_FAILED`
- `TASK_CREATION_FAILED`
- `TASK_CREATION_UNAVAILABLE`
- `TASK_CUSTOM_FIELDS_FETCH_FAILED`
- `TASK_CUSTOM_FIELD_DELETE_FAILED`
- `TASK_CUSTOM_FIELD_UPDATE_FAILED`
- `TASK_DELETE_FAILED`
- `TASK_FETCH_FAILED`
- `TASK_HAS_SUBTASKS`
- `TASK_MOVE_CONFLICT`
- `TASK_MOVE_FAILED`
- `TASK_NOT_FOUND`
- `TASK_TEMPLATE_ARCHIVED`
- `TASK_TEMPLATE_FAILED`
- `TASK_TEMPLATE_NOT_FOUND`
- `TASK_TEMPLATE_PERMISSION_FAILED`
- `TASK_UPDATE_FAILED`
- `TEMPLATE_UNAVAILABLE`
- `TICKET_ISSUE_FAILED`
- `TIMER_ALREADY_RUNNING`
- `TIME_ENTRY_FORBIDDEN`
- `TIME_ENTRY_NOT_FOUND`
- `TIME_FAILED`
- `TITLE_MAPPING_REQUIRED`
- `TOKEN_EXPIRED`
- `TOKEN_MISSING`
- `TOO_FEW`
- `TOO_LARGE`
- `TOO_LONG`
- `TOO_MANY`
- `TOO_MANY_ATTACHMENTS`
- `TOO_MANY_CONNECTIONS`
- `TOO_MANY_TICKETS`
- `TOO_SHORT`
- `TOO_SMALL`
- `UNKNOWN_CHANNEL`
- `UNKNOWN_FILTER`
- `UNKNOWN_LIST_TEMPLATE`
- `UNKNOWN_NOTIFICATION_TYPE`
- `UNKNOWN_QUESTION`
- `UNSUPPORTED_FILE_TYPE`
- `UNSUPPORTED_LOCALE`
- `UNSUPPORTED_MEDIA_TYPE`
- `UNSUPPORTED_ON_UPDATE`
- `USERNAME_TAKEN`
- `USER_FETCH_FAILED`
- `USER_NOT_FOUND`
- `USER_SEARCH_FAILED`
- `VALIDATION_FAILED`
- `VALUES_EXIST`
- `VALUE_ABOVE_MAX`
- `VALUE_BELOW_MIN`
- `VALUE_TOO_LONG`
- `VALUE_TOO_SHORT`
- `VIEW_FAILED`
- `VIEW_FORBIDDEN`
- `VIEW_LIMIT_REACHED`
- `VIEW_NOT_FOUND`
- `WATCHERS_FETCH_FAILED`
- `WATCH_FAILED`
- `WEAK_PASSWORD`
- `WORKSPACES_FETCH_FAILED`
- `WORKSPACE_AVATAR_DELETE_FAILED`
- `WORKSPACE_AVATAR_UPLOAD_FAILED`
- `WORKSPACE_CREATE_FAILED`
- `WORKSPACE_DELETE_FAILED`
- `WORKSPACE_MISMATCH`
- `WORKSPACE_NAME_REQUIRED`
- `WORKSPACE_NOT_FOUND`
- `WORKSPACE_UPDATE_FAILED`
<!-- codes:end -->
