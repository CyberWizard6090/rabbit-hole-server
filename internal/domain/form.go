package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrFormNotFound           = errors.New("form not found")
	ErrFormNotPublished       = errors.New("form is not published")
	ErrInvalidForm            = errors.New("invalid form")
	ErrInvalidSubmission      = errors.New("invalid form submission")
	ErrFormSubmissionsLimited = errors.New("too many submissions for this form, try again later")
	ErrIdempotencyConflict    = errors.New("idempotency key was already used with a different request")
	ErrQuestionInUse          = errors.New("question is referenced by a submission")
	ErrFormHasSubmissions     = errors.New("form has submissions and cannot be deleted")
	ErrFormSlugTaken          = errors.New("form slug already exists")
	ErrInvalidTaskTemplate    = errors.New("invalid task template")
	ErrTaskTemplateNotFound   = errors.New("task template not found")
	ErrTaskTemplateArchived   = errors.New("task template is archived")
	ErrFormClosed             = errors.New("form no longer accepts submissions")
	ErrFormFull               = errors.New("form reached its submission limit")
)

type FormError struct {
	Code    string
	Message string
	Status  int
}

func (e *FormError) Error() string { return e.Message }

func NewFormError(code, message string) *FormError {
	return &FormError{Code: code, Message: message}
}

type FormStatus string

const (
	FormDraft     FormStatus = "draft"
	FormPublished FormStatus = "published"
	FormArchived  FormStatus = "archived"
)

type QuestionType string

const (
	QuestionShortText      QuestionType = "short_text"
	QuestionLongText       QuestionType = "long_text"
	QuestionNumber         QuestionType = "number"
	QuestionEmail          QuestionType = "email"
	QuestionPhone          QuestionType = "phone"
	QuestionURL            QuestionType = "url"
	QuestionDate           QuestionType = "date"
	QuestionDateTime       QuestionType = "datetime"
	QuestionSingleChoice   QuestionType = "single_choice"
	QuestionMultipleChoice QuestionType = "multiple_choice"
	QuestionDropdown       QuestionType = "dropdown"
	QuestionCheckbox       QuestionType = "checkbox"
	QuestionRating         QuestionType = "rating"
	QuestionScale          QuestionType = "scale"
	QuestionFile           QuestionType = "file"
	QuestionSection        QuestionType = "section"
	QuestionDescription    QuestionType = "description"
	QuestionDivider        QuestionType = "divider"
)

const AppendQuestionPosition = -1

const MaxFormQuestions = 100

type SubmissionFilter struct {
	Status             string
	TaskCreationStatus TaskCreationStatus
}

func ValidSubmissionStatus(status string) bool {
	switch status {
	case "submitted", "processing", "processed", "failed":
		return true
	default:
		return false
	}
}

func ValidTaskCreationStatus(status TaskCreationStatus) bool {
	switch status {
	case TaskCreationPending, TaskCreationProcessing, TaskCreationSucceeded,
		TaskCreationFailed, TaskCreationSkipped:
		return true
	default:
		return false
	}
}

type QuestionOption struct {
	ID       uint   `json:"id,omitempty"`
	Label    string `json:"label"`
	Value    string `json:"value"`
	Position int    `json:"position"`
}

type FormQuestion struct {
	ID          uint             `json:"id"`
	FormID      uint             `json:"-"`
	Type        QuestionType     `json:"type"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Required    bool             `json:"required"`
	Position    int              `json:"position"`
	Settings    json.RawMessage  `json:"settings,omitempty"`
	Options     []QuestionOption `json:"options,omitempty"`
}

type FormVersion struct {
	ID             uint            `json:"id"`
	FormID         uint            `json:"-"`
	Version        int             `json:"version"`
	SchemaSnapshot json.RawMessage `json:"schema_snapshot"`
	Status         string          `json:"status"`
	PublishedAt    time.Time       `json:"published_at"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Form struct {
	Model
	WorkspaceID      uint            `json:"workspace_id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Slug             string          `json:"slug"`
	Status           FormStatus      `json:"status"`
	CurrentVersionID *uint           `json:"current_version_id,omitempty"`
	CreatedBy        uint            `json:"created_by"`
	Questions        []FormQuestion  `json:"questions,omitempty"`
	CurrentVersion   *FormVersion    `json:"current_version,omitempty"`
	TaskConfig       json.RawMessage `json:"task_config,omitempty"`
	Settings         FormSettings    `json:"settings"`
}

type FormSettings struct {
	SubmitButtonText string     `json:"submit_button_text"`
	SuccessMessage   string     `json:"success_message"`
	RedirectURL      string     `json:"redirect_url"`
	AccentColor      string     `json:"accent_color"`
	LogoFileID       string     `json:"logo_file_id"`
	CoverFileID      string     `json:"cover_file_id"`
	NotifyOnSubmit   []uint     `json:"notify_on_submit"`
	ClosesAt         *time.Time `json:"closes_at"`
	MaxSubmissions   *int       `json:"max_submissions"`
	LogoURL          string     `json:"logo_url,omitempty"`
	CoverURL         string     `json:"cover_url,omitempty"`
}

type FormUploadClaim struct {
	QuestionID uint
	PublicID   string
}

type SubmissionOptions struct {
	ClosesAt       *time.Time
	MaxSubmissions *int
	Uploads        []FormUploadClaim
}

type SubmissionAnswer struct {
	QuestionID uint            `json:"question_id"`
	Value      json.RawMessage `json:"value"`
}

type FormSubmission struct {
	ID                 uint               `json:"id"`
	FormID             uint               `json:"form_id"`
	FormVersionID      uint               `json:"form_version_id"`
	WorkspaceID        uint               `json:"workspace_id"`
	RespondentID       *uint              `json:"respondent_id,omitempty"`
	Status             string             `json:"status"`
	SubmittedAt        time.Time          `json:"submitted_at"`
	Answers            []SubmissionAnswer `json:"answers"`
	TaskCreationStatus TaskCreationStatus `json:"task_creation_status"`
	TaskCreationError  string             `json:"task_creation_error,omitempty"`
	CreatedBy          uint               `json:"-"`
	FormName           string             `json:"-"`
	Files              []SubmissionFile   `json:"files,omitempty"`
}

type SubmissionFile struct {
	ID               uint   `json:"id"`
	QuestionID       uint   `json:"question_id"`
	Name             string `json:"name"`
	ContentType      string `json:"content_type"`
	Size             int64  `json:"size"`
	TaskAttachmentID *uint  `json:"task_attachment_id,omitempty"`
}

const TaskCreationFailureCode = "TASK_CREATION_FAILED"

type TaskCreationStatus string

const (
	TaskCreationPending    TaskCreationStatus = "pending"
	TaskCreationProcessing TaskCreationStatus = "processing"
	TaskCreationSucceeded  TaskCreationStatus = "succeeded"
	TaskCreationFailed     TaskCreationStatus = "failed"
	TaskCreationSkipped    TaskCreationStatus = "skipped"
)

type FormTaskConfiguration struct {
	Enabled      bool              `json:"enabled"`
	OwnerID      *uint             `json:"owner_id"`
	Target       FormTaskTarget    `json:"target"`
	Template     *FormTaskTemplate `json:"template,omitempty"`
	TaskDefaults FormTaskDefaults  `json:"task_defaults"`
	Mappings     []FormTaskMapping `json:"mappings"`
}

type FormTaskTarget struct {
	WorkspaceID uint `json:"workspace_id"`
	ListID      uint `json:"list_id"`
}

type FormTaskTemplate struct {
	TemplateID uint `json:"template_id"`
	VersionID  uint `json:"version_id"`
}

type FormTaskDefaults struct {
	Priority   string `json:"priority"`
	StatusID   *uint  `json:"status_id"`
	AssigneeID *uint  `json:"assignee_id"`
}

type FormTaskMapping struct {
	QuestionID    uint                `json:"question_id"`
	QuestionIndex *int                `json:"question_index,omitempty"`
	Target        FormMappingTarget   `json:"target"`
	Transform     *FormValueTransform `json:"transform,omitempty"`
}

type FormMappingTarget struct {
	Type    string `json:"type"`
	Field   string `json:"field,omitempty"`
	FieldID uint   `json:"field_id,omitempty"`
}

type FormValueTransform struct {
	Type   string            `json:"type"`
	Values map[string]string `json:"values"`
}

type TaskTemplate struct {
	ID          uint                  `json:"id"`
	WorkspaceID uint                  `json:"workspace_id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Status      string                `json:"status"`
	CreatedBy   uint                  `json:"created_by"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
	Versions    []TaskTemplateVersion `json:"versions,omitempty"`
}

type TaskTemplateVersion struct {
	ID            uint            `json:"id"`
	TemplateID    uint            `json:"template_id"`
	Version       int             `json:"version"`
	Configuration json.RawMessage `json:"configuration"`
	CreatedAt     time.Time       `json:"created_at"`
}

type FormRepository interface {
	Create(ctx context.Context, form *Form) error
	ListByWorkspace(ctx context.Context, workspaceID uint) ([]Form, error)
	GetByID(ctx context.Context, formID uint) (*Form, error)
	Update(ctx context.Context, form *Form) error
	Delete(ctx context.Context, workspaceID, formID uint) error
	Archive(ctx context.Context, workspaceID, formID uint) error
	CreateQuestion(ctx context.Context, question *FormQuestion) error
	UpdateQuestion(ctx context.Context, question *FormQuestion) error
	DeleteQuestion(ctx context.Context, formID, questionID uint) error
	ReorderQuestions(ctx context.Context, formID uint, questionIDs []uint) error
	Publish(ctx context.Context, formID uint) (*Form, error)
	UpdateTaskConfig(ctx context.Context, workspaceID, formID uint, config json.RawMessage) error
	ProcessSubmission(
		ctx context.Context,
		submissionID uint,
		createTask func(context.Context, FormSubmission, FormTaskConfiguration) (uint, json.RawMessage, error),
	) (TaskCreationStatus, error)
	GetSubmissionTask(ctx context.Context, submissionID uint) (uint, error)
	GetSubmissionTaskStatus(ctx context.Context, submissionID uint) (TaskCreationStatus, string, error)
	GetSubmissionWorkspace(ctx context.Context, submissionID uint) (uint, error)
	GetPublishedBySlug(ctx context.Context, slug string) (*Form, error)
	CreateSubmission(
		ctx context.Context,
		formID uint,
		respondentID *uint,
		idempotencyKey string,
		requestHash string,
		answers []SubmissionAnswer,
		options SubmissionOptions,
	) (*FormSubmission, bool, error)
	ListSubmissions(ctx context.Context, formID uint, filter SubmissionFilter, limit, offset int) ([]FormSubmission, int64, error)
	GetSubmission(ctx context.Context, submissionID uint) (*FormSubmission, error)
	CountSubmissions(ctx context.Context, formID uint) (int, error)
	WorkspaceImageExists(ctx context.Context, workspaceID uint, publicID string) (bool, error)
	WorkspaceMemberIDs(ctx context.Context, workspaceID uint, userIDs []uint) ([]uint, error)
}

type TaskTemplateRepository interface {
	Create(ctx context.Context, template *TaskTemplate, configuration json.RawMessage) error
	List(ctx context.Context, workspaceID uint) ([]TaskTemplate, error)
	Get(ctx context.Context, templateID uint) (*TaskTemplate, error)
	Update(ctx context.Context, template *TaskTemplate) error
	Archive(ctx context.Context, templateID uint) error
	CreateVersion(ctx context.Context, templateID uint, configuration json.RawMessage) (*TaskTemplateVersion, error)
	ListVersions(ctx context.Context, templateID uint) ([]TaskTemplateVersion, error)
	GetVersion(ctx context.Context, templateID, versionID uint) (*TaskTemplateVersion, error)
}

type FormTaskProcessor interface {
	ProcessSubmission(ctx context.Context, submissionID uint) (TaskCreationStatus, error)
	RetrySubmission(ctx context.Context, submissionID uint) (TaskCreationStatus, error)
}
