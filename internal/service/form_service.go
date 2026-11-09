package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/ratelimit"
)

type FormService interface {
	Create(ctx context.Context, workspaceID, userID uint, input FormInput) (*domain.Form, error)
	List(ctx context.Context, workspaceID uint) ([]domain.Form, error)
	Get(ctx context.Context, workspaceID, formID uint) (*domain.Form, error)
	Update(ctx context.Context, workspaceID, formID uint, input FormInput) (*domain.Form, error)
	Delete(ctx context.Context, workspaceID, formID uint) error
	Archive(ctx context.Context, workspaceID, formID uint) error
	CreateQuestion(ctx context.Context, workspaceID, formID uint, input QuestionInput) (*domain.FormQuestion, error)
	UpdateQuestion(ctx context.Context, workspaceID, formID, questionID uint, input QuestionInput) (*domain.FormQuestion, error)
	DeleteQuestion(ctx context.Context, workspaceID, formID, questionID uint) error
	ReorderQuestions(ctx context.Context, workspaceID, formID uint, questionIDs []uint) error
	Publish(ctx context.Context, workspaceID, formID uint) (*domain.Form, error)
	GetPublic(ctx context.Context, slug string) (*PublicForm, error)
	Submit(ctx context.Context, slug, idempotencyKey string, respondentID *uint, answers map[uint]json.RawMessage) (*PublicSubmission, bool, error)
	ListSubmissions(
		ctx context.Context,
		workspaceID, formID uint,
		filter domain.SubmissionFilter,
		limit, offset int,
	) ([]domain.FormSubmission, int64, error)
	GetSubmission(ctx context.Context, workspaceID, submissionID uint) (*domain.FormSubmission, error)
}

type FormInput struct {
	Name        string
	Description string
	Slug        string
	Settings    *domain.FormSettings
	Questions   []QuestionInput
	TaskConfig  *domain.FormTaskConfiguration
}

type QuestionInput struct {
	Type        domain.QuestionType
	Title       string
	Description string
	Required    bool
	Position    *int
	Settings    json.RawMessage
	Options     []domain.QuestionOption
}

type PublicForm struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Slug        string                `json:"slug"`
	Version     int                   `json:"version"`
	Questions   []domain.FormQuestion `json:"questions"`
	Settings    PublicFormSettings    `json:"settings"`
	Closed      bool                  `json:"closed"`
	Full        bool                  `json:"full"`
}

type PublicSubmission struct {
	ID                 uint                      `json:"id"`
	Status             string                    `json:"status"`
	CreatedAt          time.Time                 `json:"created_at"`
	TaskCreationStatus domain.TaskCreationStatus `json:"task_creation_status"`
	SuccessMessage     string                    `json:"success_message"`
	RedirectURL        string                    `json:"redirect_url"`
}

type formService struct {
	repo       domain.FormRepository
	taskConfig FormTaskConfigurationService
	processor  domain.FormTaskProcessor
	limiter    *submissionLimiter

	allowInsecureRedirect bool
	imageBaseURL          string
	notifier              FormSubmissionNotifier
	uploads               domain.FormUploadRepository
	tx                    TransactionRunner
}

const (
	reorderInvalidCode = "FORM_QUESTION_REORDER_INVALID"
	questionInUseCode  = "FORM_QUESTION_IN_USE"

	idempotencyKeyField = "Idempotency-Key"
)

var formSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,254}$`)

func NewFormService(repo domain.FormRepository) *formService {
	return &formService{repo: repo}
}

func (s *formService) WithSubmissionLimit(limiter ratelimit.Limiter, max int, period time.Duration) *formService {
	s.limiter = newSubmissionLimiter(limiter, max, period)
	return s
}

func (s *formService) WithTransactions(tx TransactionRunner) *formService {
	s.tx = tx
	return s
}

func (s *formService) WithTaskIntegration(
	config FormTaskConfigurationService,
	processor domain.FormTaskProcessor,
) *formService {
	s.taskConfig, s.processor = config, processor
	return s
}

func (s *formService) Create(
	ctx context.Context,
	workspaceID, userID uint,
	input FormInput,
) (*domain.Form, error) {
	if workspaceID == 0 || userID == 0 {
		return nil, domain.ErrInvalidForm
	}
	questions, err := normalizeFormQuestions(input.Questions)
	if err != nil {
		return nil, err
	}
	input, err = normalizeFormInput(input)
	if err != nil {
		return nil, err
	}
	form := &domain.Form{
		WorkspaceID: workspaceID, CreatedBy: userID, Name: input.Name,
		Description: input.Description, Slug: input.Slug, Status: domain.FormDraft,
	}
	if input.Settings != nil {
		settings, err := s.normalizeSettings(ctx, workspaceID, *input.Settings)
		if err != nil {
			return nil, err
		}
		form.Settings = settings
	}
	if len(questions) == 0 && input.TaskConfig == nil {
		if err := s.repo.Create(ctx, form); err != nil {
			return nil, fmt.Errorf("create form: %w", err)
		}
		s.decorateForm(form)
		return form, nil
	}
	if err := s.runInTransaction(ctx, func(txCtx context.Context) error {
		return s.createFormWithContent(txCtx, form, questions, input.TaskConfig)
	}); err != nil {
		return nil, err
	}
	created, err := s.repo.GetByID(ctx, form.ID)
	if err != nil {
		return nil, err
	}
	s.decorateForm(created)
	return created, nil
}

func (s *formService) runInTransaction(ctx context.Context, fn func(context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.Run(ctx, fn)
}

func (s *formService) createFormWithContent(
	ctx context.Context,
	form *domain.Form,
	questions []domain.FormQuestion,
	taskConfig *domain.FormTaskConfiguration,
) error {
	if err := s.repo.Create(ctx, form); err != nil {
		return fmt.Errorf("create form: %w", err)
	}
	created := make([]domain.FormQuestion, len(questions))
	for i := range questions {
		question := questions[i]
		question.FormID, question.Position = form.ID, i
		if err := s.repo.CreateQuestion(ctx, &question); err != nil {
			return fmt.Errorf("create form question %d: %w", i, err)
		}
		created[i] = question
	}
	if taskConfig == nil {
		return nil
	}
	if s.taskConfig == nil {
		return domain.NewFormValidationError("task_config", "TASK_CREATION_UNAVAILABLE")
	}
	resolved, err := resolveQuestionIndexes(*taskConfig, created)
	if err != nil {
		return err
	}
	if err := s.taskConfig.Set(ctx, form.WorkspaceID, form.ID, resolved); err != nil {
		return prefixTaskConfigError(err)
	}
	return nil
}

func normalizeFormQuestions(inputs []QuestionInput) ([]domain.FormQuestion, error) {
	if len(inputs) > domain.MaxFormQuestions {
		return nil, domain.NewFormValidationError("questions", "TOO_MANY").WithParams(map[string]any{"max": domain.MaxFormQuestions})
	}
	questions := make([]domain.FormQuestion, len(inputs))
	for i, input := range inputs {
		input.Position = nil
		question, err := normalizeQuestion(1, input)
		if err != nil {
			var validation *domain.FormValidationError
			if errors.As(err, &validation) {
				return nil, validation.WithPrefix(fmt.Sprintf("questions[%d].", i))
			}
			return nil, err
		}
		question.FormID = 0
		questions[i] = *question
	}
	return questions, nil
}

func resolveQuestionIndexes(
	config domain.FormTaskConfiguration,
	questions []domain.FormQuestion,
) (domain.FormTaskConfiguration, error) {
	mappings := make([]domain.FormTaskMapping, len(config.Mappings))
	for i, mapping := range config.Mappings {
		field := fmt.Sprintf("task_config.mappings[%d]", i)
		switch {
		case mapping.QuestionIndex != nil && mapping.QuestionID != 0:
			return config, domain.NewFormValidationError(field, "EXCLUSIVE_FIELDS").WithParams(map[string]any{"fields": []string{"question_id", "question_index"}})
		case mapping.QuestionIndex != nil:
			index := *mapping.QuestionIndex
			if index < 0 || index >= len(questions) {
				return config, domain.NewFormValidationError(field+".question_index", "INVALID_REFERENCE")
			}
			mapping.QuestionID, mapping.QuestionIndex = questions[index].ID, nil
		default:
			return config, domain.NewFormValidationError(field+".question_index", "REQUIRED_FOR_NEW_FORM")
		}
		mappings[i] = mapping
	}
	config.Mappings = mappings
	return config, nil
}

func prefixTaskConfigError(err error) error {
	var validation *domain.FormValidationError
	if errors.As(err, &validation) {
		return validation.WithPrefix("task_config.")
	}
	if errors.Is(err, domain.ErrInvalidForm) {
		return domain.NewFormValidationError("task_config", "INVALID_VALUE")
	}
	return err
}

func (s *formService) List(ctx context.Context, workspaceID uint) ([]domain.Form, error) {
	forms, err := s.repo.ListByWorkspace(ctx, workspaceID)
	for i := range forms {
		s.decorateForm(&forms[i])
	}
	return forms, err
}

func (s *formService) Get(ctx context.Context, workspaceID, formID uint) (*domain.Form, error) {
	form, err := s.repo.GetByID(ctx, formID)
	if err != nil {
		return nil, err
	}
	if form.WorkspaceID != workspaceID {
		return nil, domain.ErrFormNotFound
	}
	s.decorateForm(form)
	return form, nil
}

func (s *formService) Update(
	ctx context.Context,
	workspaceID, formID uint,
	input FormInput,
) (*domain.Form, error) {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	input, err = normalizeFormInput(input)
	if err != nil {
		return nil, err
	}
	form.Name, form.Description, form.Slug = input.Name, input.Description, input.Slug
	if input.Settings != nil {
		settings, err := s.normalizeSettings(ctx, workspaceID, *input.Settings)
		if err != nil {
			return nil, err
		}
		form.Settings = settings
	}
	if err := s.repo.Update(ctx, form); err != nil {
		return nil, fmt.Errorf("update form: %w", err)
	}
	s.decorateForm(form)
	return form, nil
}

func (s *formService) Delete(ctx context.Context, workspaceID, formID uint) error {
	if _, err := s.Get(ctx, workspaceID, formID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, workspaceID, formID)
}

func (s *formService) Archive(ctx context.Context, workspaceID, formID uint) error {
	if _, err := s.Get(ctx, workspaceID, formID); err != nil {
		return err
	}
	return s.repo.Archive(ctx, workspaceID, formID)
}

func (s *formService) CreateQuestion(
	ctx context.Context,
	workspaceID, formID uint,
	input QuestionInput,
) (*domain.FormQuestion, error) {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	if form.Status == domain.FormArchived {
		return nil, domain.ErrInvalidForm
	}
	question, err := normalizeQuestion(formID, input)
	if err != nil {
		return nil, err
	}
	if err := savedQuestionVisibilityError(form.Questions, *question); err != nil {
		return nil, err
	}
	if err := s.repo.CreateQuestion(ctx, question); err != nil {
		return nil, fmt.Errorf("create form question: %w", err)
	}
	return question, nil
}

func (s *formService) UpdateQuestion(
	ctx context.Context,
	workspaceID, formID, questionID uint,
	input QuestionInput,
) (*domain.FormQuestion, error) {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	if form.Status == domain.FormArchived {
		return nil, domain.ErrInvalidForm
	}
	question, err := normalizeQuestion(formID, input)
	if err != nil {
		return nil, err
	}
	question.ID = questionID
	if err := savedQuestionVisibilityError(form.Questions, *question); err != nil {
		return nil, err
	}
	if err := s.checkAnsweredQuestionChange(ctx, form.Questions, *question); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateQuestion(ctx, question); err != nil {
		return nil, fmt.Errorf("update form question: %w", err)
	}
	return question, nil
}

func (s *formService) DeleteQuestion(ctx context.Context, workspaceID, formID, questionID uint) error {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return err
	}
	if form.Status == domain.FormArchived {
		return domain.ErrInvalidForm
	}
	if referencedByVisibility(form.Questions, questionID) {
		return questionReferencedError()
	}
	return s.repo.DeleteQuestion(ctx, formID, questionID)
}

func (s *formService) ReorderQuestions(ctx context.Context, workspaceID, formID uint, questionIDs []uint) error {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return err
	}
	if form.Status == domain.FormArchived {
		return domain.ErrInvalidForm
	}
	if err := reorderSetError(form.Questions, questionIDs); err != nil {
		return err
	}
	if err := reorderVisibilityError(form.Questions, questionIDs); err != nil {
		return err
	}
	return s.repo.ReorderQuestions(ctx, formID, questionIDs)
}

func reorderSetError(questions []domain.FormQuestion, ids []uint) error {
	live := make(map[uint]struct{}, len(questions))
	for _, question := range questions {
		live[question.ID] = struct{}{}
	}
	var problems []domain.FieldProblem
	seen := make(map[uint]struct{}, len(ids))
	for i, id := range ids {
		field := fmt.Sprintf("ids[%d]", i)
		params := map[string]any{"question_id": id}
		if _, duplicate := seen[id]; duplicate {
			problems = append(problems, domain.FieldProblem{Field: field, Reason: "DUPLICATE", Params: params})
			continue
		}
		seen[id] = struct{}{}
		if _, ok := live[id]; !ok {
			problems = append(problems, domain.FieldProblem{Field: field, Reason: "UNKNOWN_QUESTION", Params: params})
		}
	}
	for _, question := range orderedQuestions(questions) {
		if _, ok := seen[question.ID]; !ok {
			problems = append(problems, domain.FieldProblem{
				Field: "ids", Reason: "MISSING", Params: map[string]any{"question_id": question.ID},
			})
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return domain.NewFormError(reorderInvalidCode, "ids must list every question of the form exactly once").
		WithProblems(problems...)
}

func (s *formService) checkAnsweredQuestionChange(
	ctx context.Context,
	existing []domain.FormQuestion,
	updated domain.FormQuestion,
) error {
	var current *domain.FormQuestion
	for i := range existing {
		if existing[i].ID == updated.ID {
			current = &existing[i]
		}
	}
	if current == nil {
		return nil
	}
	typeChanged := current.Type != updated.Type
	var removed []string
	kept := make(map[string]struct{}, len(updated.Options))
	for _, option := range updated.Options {
		kept[option.Value] = struct{}{}
	}
	for _, option := range current.Options {
		if _, ok := kept[option.Value]; !ok {
			removed = append(removed, option.Value)
		}
	}
	if !typeChanged && len(removed) == 0 {
		return nil
	}
	answers, used, err := s.repo.QuestionAnswerUsage(ctx, updated.ID, removed)
	if err != nil {
		return err
	}
	if answers == 0 {
		return nil
	}
	inUse := domain.NewFormError(questionInUseCode, "the question already has answers").WithStatus(http.StatusConflict)
	if typeChanged {
		return inUse.WithProblems(domain.FieldProblem{
			Field: "type", Reason: "HAS_ANSWERS", Params: map[string]any{"answers": answers},
		})
	}
	problems := make([]domain.FieldProblem, 0, len(used))
	for _, value := range used {
		problems = append(problems, domain.FieldProblem{
			Field: "options", Reason: "OPTION_IN_USE", Params: map[string]any{"value": value},
		})
	}
	if len(problems) == 0 {
		return nil
	}
	return inUse.WithProblems(problems...)
}

func (s *formService) Publish(ctx context.Context, workspaceID, formID uint) (*domain.Form, error) {
	form, err := s.Get(ctx, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	for _, question := range form.Questions {
		if err := validateQuestion(question); err != nil {
			return nil, err
		}
	}
	if err := publishVisibilityError(form.Questions); err != nil {
		return nil, err
	}
	if s.taskConfig != nil {
		if err := s.taskConfig.ValidatePublication(ctx, workspaceID, formID); err != nil {
			return nil, err
		}
	}
	published, err := s.repo.Publish(ctx, formID)
	if err != nil {
		return nil, err
	}
	s.decorateForm(published)
	return published, nil
}

func (s *formService) GetPublic(ctx context.Context, slug string) (*PublicForm, error) {
	form, err := s.repo.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if form.CurrentVersion == nil {
		return nil, domain.ErrFormNotPublished
	}
	var snapshot struct {
		Name        string                `json:"name"`
		Description string                `json:"description"`
		Questions   []domain.FormQuestion `json:"questions"`
		Settings    domain.FormSettings   `json:"settings"`
	}
	if err := json.Unmarshal(form.CurrentVersion.SchemaSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("decode published form schema: %w", err)
	}
	for i := range snapshot.Questions {
		options := append([]domain.QuestionOption(nil), snapshot.Questions[i].Options...)
		for j := range options {
			options[j].ID = 0
		}
		snapshot.Questions[i].Options = options
	}
	public := &PublicForm{
		Name: snapshot.Name, Description: snapshot.Description, Slug: form.Slug,
		Version: form.CurrentVersion.Version, Questions: snapshot.Questions,
		Settings: s.publicSettings(snapshot.Settings), Closed: formClosed(snapshot.Settings, time.Now()),
	}
	if snapshot.Settings.MaxSubmissions != nil {
		count, err := s.repo.CountSubmissions(ctx, form.ID)
		if err != nil {
			return nil, err
		}
		public.Full = count >= *snapshot.Settings.MaxSubmissions
	}
	return public, nil
}

func (s *formService) Submit(
	ctx context.Context,
	slug, idempotencyKey string,
	respondentID *uint,
	input map[uint]json.RawMessage,
) (*PublicSubmission, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	switch {
	case idempotencyKey == "":
		return nil, false, domain.InvalidField(domain.ErrInvalidSubmission, idempotencyKeyField, "REQUIRED")
	case len(idempotencyKey) > 255:
		return nil, false, domain.InvalidField(domain.ErrInvalidSubmission, idempotencyKeyField, "TOO_LONG").
			WithParams(map[string]any{"max": 255})
	}
	form, err := s.repo.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return nil, false, err
	}
	if form.CurrentVersion == nil {
		return nil, false, domain.ErrFormNotPublished
	}
	if !s.limiter.allow(ctx, form.ID) {
		return nil, false, domain.ErrFormSubmissionsLimited
	}
	var snapshot struct {
		Name      string                `json:"name"`
		Questions []domain.FormQuestion `json:"questions"`
		Settings  domain.FormSettings   `json:"settings"`
	}
	if err := json.Unmarshal(form.CurrentVersion.SchemaSnapshot, &snapshot); err != nil {
		return nil, false, fmt.Errorf("decode published form schema: %w", err)
	}
	answers, err := validateSubmission(snapshot.Questions, input)
	if err != nil {
		return nil, false, err
	}
	claims, err := fileClaims(snapshot.Questions, answers)
	if err != nil {
		return nil, false, err
	}
	sort.Slice(answers, func(i, j int) bool { return answers[i].QuestionID < answers[j].QuestionID })
	requestBody, err := json.Marshal(answers)
	if err != nil {
		return nil, false, fmt.Errorf("encode submission for idempotency: %w", err)
	}
	hash := sha256.Sum256(requestBody)
	var submission *domain.FormSubmission
	var replay bool
	err = s.runInTransaction(ctx, func(ctx context.Context) error {
		created, existed, createErr := s.repo.CreateSubmission(
			ctx, form.ID, respondentID, idempotencyKey, hex.EncodeToString(hash[:]), answers,
			domain.SubmissionOptions{
				ClosesAt: snapshot.Settings.ClosesAt, MaxSubmissions: snapshot.Settings.MaxSubmissions, Uploads: claims,
			},
		)
		if createErr != nil {
			return createErr
		}
		submission, replay = created, existed
		if existed {
			return nil
		}
		return s.notifySubmission(ctx, form, snapshot.Name, snapshot.Settings, created.ID)
	})
	if err != nil {
		return nil, false, fmt.Errorf("save form submission: %w", err)
	}
	if !replay && s.processor != nil {
		if status, processErr := s.processor.ProcessSubmission(ctx, submission.ID); processErr != nil {
			slog.Warn("form submission task creation failed", "submission_id", submission.ID,
				"status", status, "error", processErr)
		}
		refreshed, refreshErr := s.repo.GetSubmission(ctx, submission.ID)
		if refreshErr != nil {
			return nil, false, fmt.Errorf("reload submission after task processing: %w", refreshErr)
		}
		submission = refreshed
	}
	return &PublicSubmission{
		ID: submission.ID, Status: submission.Status, CreatedAt: submission.SubmittedAt,
		TaskCreationStatus: submission.TaskCreationStatus,
		SuccessMessage:     snapshot.Settings.SuccessMessage, RedirectURL: snapshot.Settings.RedirectURL,
	}, replay, nil
}

func (s *formService) ListSubmissions(
	ctx context.Context,
	workspaceID, formID uint,
	filter domain.SubmissionFilter,
	limit, offset int,
) ([]domain.FormSubmission, int64, error) {
	if _, err := s.Get(ctx, workspaceID, formID); err != nil {
		return nil, 0, err
	}
	if filter.Status != "" && !domain.ValidSubmissionStatus(filter.Status) {
		return nil, 0, domain.NewFormValidationError("status", "INVALID_CHOICE").WithParams(map[string]any{"allowed": []string{"submitted", "processing", "processed", "failed"}})
	}
	if filter.TaskCreationStatus != "" && !domain.ValidTaskCreationStatus(filter.TaskCreationStatus) {
		return nil, 0, domain.NewFormValidationError("task_creation_status", "INVALID_CHOICE").WithParams(map[string]any{"allowed": []string{"pending", "processing", "succeeded", "failed", "skipped"}})
	}
	submissions, total, err := s.repo.ListSubmissions(ctx, formID, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return submissions, total, s.attachSubmissionFiles(ctx, submissions)
}

func (s *formService) GetSubmission(ctx context.Context, workspaceID, submissionID uint) (*domain.FormSubmission, error) {
	submission, err := s.repo.GetSubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	if submission.WorkspaceID != workspaceID {
		return nil, domain.ErrFormNotFound
	}
	withFiles := []domain.FormSubmission{*submission}
	if err := s.attachSubmissionFiles(ctx, withFiles); err != nil {
		return nil, err
	}
	return &withFiles[0], nil
}

func normalizeFormInput(input FormInput) (FormInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Slug = strings.TrimSpace(strings.ToLower(input.Slug))
	switch {
	case input.Name == "":
		return FormInput{}, domain.NewFormValidationError("name", "REQUIRED")
	case utf8.RuneCountInString(input.Name) > 255:
		return FormInput{}, domain.NewFormValidationError("name", "TOO_LONG").WithParams(map[string]any{"max": 255})
	case utf8.RuneCountInString(input.Description) > 10000:
		return FormInput{}, domain.NewFormValidationError("description", "TOO_LONG").WithParams(map[string]any{"max": 10000})
	case !formSlugPattern.MatchString(input.Slug):
		return FormInput{}, domain.NewFormValidationError("slug", "INVALID_SLUG")
	}
	return input, nil
}

func normalizeQuestion(formID uint, input QuestionInput) (*domain.FormQuestion, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	position := domain.AppendQuestionPosition
	if input.Position != nil {
		position = *input.Position
	}
	switch {
	case formID == 0:
		return nil, domain.ErrInvalidForm
	case !supportedQuestionType(input.Type):
		return nil, domain.NewFormValidationError("type", "INVALID_CHOICE")
	case input.Title == "":
		return nil, domain.NewFormValidationError("title", "REQUIRED")
	case len(input.Title) > 1000:
		return nil, domain.NewFormValidationError("title", "TOO_LONG").WithParams(map[string]any{"max": 1000})
	case len(input.Description) > 5000:
		return nil, domain.NewFormValidationError("description", "TOO_LONG").WithParams(map[string]any{"max": 5000})
	case input.Position != nil && position < 0:
		return nil, domain.NewFormValidationError("position", "TOO_SMALL").WithParams(map[string]any{"min": 0})
	}
	if len(input.Settings) == 0 || string(input.Settings) == "null" {
		input.Settings = json.RawMessage(`{}`)
	}
	if !json.Valid(input.Settings) {
		return nil, domain.NewFormValidationError("settings", "INVALID_TYPE").WithParams(map[string]any{"expected": "object"})
	}
	if len(input.Settings) > 16384 {
		return nil, domain.NewFormValidationError("settings", "TOO_LARGE").WithParams(map[string]any{"max_bytes": 16384})
	}
	options := append([]domain.QuestionOption(nil), input.Options...)
	choice := isChoiceQuestion(input.Type)
	if choice && (len(options) < 1 || len(options) > 100) {
		return nil, domain.NewFormValidationError("options", "INVALID_COUNT").WithParams(map[string]any{"min": 1, "max": 100})
	}
	if !choice && len(options) != 0 {
		return nil, domain.NewFormValidationError("options", "NOT_ALLOWED")
	}
	seen := make(map[string]struct{}, len(options))
	for i := range options {
		options[i].Label = strings.TrimSpace(options[i].Label)
		options[i].Value = strings.TrimSpace(options[i].Value)
		options[i].Position = i
		field := fmt.Sprintf("options[%d]", i)
		if options[i].Label == "" || options[i].Value == "" ||
			utf8.RuneCountInString(options[i].Label) > 255 || utf8.RuneCountInString(options[i].Value) > 255 {
			return nil, domain.NewFormValidationError(field, "INVALID_OPTION").WithParams(map[string]any{"max": 255})
		}
		if _, exists := seen[options[i].Value]; exists {
			return nil, domain.NewFormValidationError(field+".value", "DUPLICATE")
		}
		seen[options[i].Value] = struct{}{}
	}
	if err := validateQuestionSettings(input.Type, input.Settings, len(options)); err != nil {
		return nil, err
	}
	if isStaticQuestion(input.Type) && input.Required {
		return nil, domain.NewFormValidationError("required", "NOT_ALLOWED")
	}
	if err := validateFileQuestionSettings(input.Type, input.Settings); err != nil {
		return nil, err
	}
	return &domain.FormQuestion{
		FormID: formID, Type: input.Type, Title: input.Title, Description: input.Description,
		Required: input.Required, Position: position, Settings: input.Settings, Options: options,
	}, nil
}

func validateQuestion(question domain.FormQuestion) error {
	if !supportedQuestionType(question.Type) || strings.TrimSpace(question.Title) == "" {
		return domain.ErrInvalidForm
	}
	if isChoiceQuestion(question.Type) && len(question.Options) == 0 {
		return domain.ErrInvalidForm
	}
	return nil
}

func supportedQuestionType(questionType domain.QuestionType) bool {
	switch questionType {
	case domain.QuestionShortText, domain.QuestionLongText, domain.QuestionNumber,
		domain.QuestionEmail, domain.QuestionPhone, domain.QuestionURL, domain.QuestionDate,
		domain.QuestionDateTime, domain.QuestionSingleChoice, domain.QuestionMultipleChoice,
		domain.QuestionDropdown, domain.QuestionCheckbox, domain.QuestionRating, domain.QuestionScale,
		domain.QuestionFile, domain.QuestionSection, domain.QuestionDescription, domain.QuestionDivider:
		return true
	default:
		return false
	}
}

func isChoiceQuestion(questionType domain.QuestionType) bool {
	return questionType == domain.QuestionSingleChoice ||
		questionType == domain.QuestionMultipleChoice ||
		questionType == domain.QuestionDropdown
}

func isStaticQuestion(questionType domain.QuestionType) bool {
	return questionType == domain.QuestionSection ||
		questionType == domain.QuestionDescription ||
		questionType == domain.QuestionDivider
}

func questionNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		result, err := number.Float64()
		return result, err == nil
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func questionHasOption(options []domain.QuestionOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func isEmptySubmissionValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}
