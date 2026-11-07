package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type formRepository struct {
	db *gorm.DB
}

func NewFormRepository(db *gorm.DB) domain.FormRepository {
	return &formRepository{db: db}
}

func (r *formRepository) Create(ctx context.Context, form *domain.Form) error {
	model := formToPersistence(*form)
	if err := database.DBFromContext(ctx, r.db).Create(&model).Error; err != nil {
		if isUniqueViolation(err) {
			return domain.ErrFormSlugTaken
		}
		return mapErr(err)
	}
	form.ID, form.CreatedAt, form.UpdatedAt = model.ID, model.CreatedAt, model.UpdatedAt
	return nil
}

func (r *formRepository) ListByWorkspace(ctx context.Context, workspaceID uint) ([]domain.Form, error) {
	var rows []persistence.Form
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).
		Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, formToDomain), nil
}

func (r *formRepository) GetByID(ctx context.Context, formID uint) (*domain.Form, error) {
	var row persistence.Form
	if err := database.DBFromContext(ctx, r.db).First(&row, "id = ?", formID).Error; err != nil {
		return nil, mapErr(err)
	}
	form := formToDomain(row)
	questions, version, err := r.loadFormChildren(ctx, row)
	if err != nil {
		return nil, err
	}
	form.Questions, form.CurrentVersion = questions, version
	return &form, nil
}

func (r *formRepository) loadFormChildren(
	ctx context.Context,
	form persistence.Form,
) ([]domain.FormQuestion, *domain.FormVersion, error) {
	var questionRows []persistence.FormQuestion
	if err := database.DBFromContext(ctx, r.db).
		Preload("Options", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC, id ASC") }).
		Where("form_id = ?", form.ID).
		Order("position ASC, id ASC").Find(&questionRows).Error; err != nil {
		return nil, nil, mapErr(err)
	}
	questions := mapSlice(questionRows, questionToDomain)
	if form.CurrentVersionID == nil {
		return questions, nil, nil
	}
	var version persistence.FormVersion
	if err := database.DBFromContext(ctx, r.db).First(&version, "id = ?", *form.CurrentVersionID).Error; err != nil {
		return nil, nil, mapErr(err)
	}
	return questions, &domain.FormVersion{
		ID: version.ID, FormID: version.FormID, Version: version.Version,
		SchemaSnapshot: version.SchemaSnapshot, Status: version.Status,
		PublishedAt: version.PublishedAt, CreatedAt: version.CreatedAt,
	}, nil
}

func (r *formRepository) Update(ctx context.Context, form *domain.Form) error {
	result := r.db.WithContext(ctx).Model(&persistence.Form{}).
		Where("id = ? AND workspace_id = ?", form.ID, form.WorkspaceID).
		Updates(map[string]any{
			"name": form.Name, "description": form.Description, "slug": form.Slug,
			"settings": encodeFormSettings(form.Settings),
		})
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return domain.ErrFormSlugTaken
		}
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrFormNotFound
	}
	return nil
}

func (r *formRepository) Delete(ctx context.Context, workspaceID, formID uint) error {
	result := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Delete(&persistence.Form{}, formID)
	if result.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(result.Error, &pgErr) && pgErr.Code == "23503" {
			return domain.ErrFormHasSubmissions
		}
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrFormNotFound
	}
	return nil
}

func (r *formRepository) Archive(ctx context.Context, workspaceID, formID uint) error {
	result := r.db.WithContext(ctx).Model(&persistence.Form{}).
		Where("id = ? AND workspace_id = ?", formID, workspaceID).
		Update("status", string(domain.FormArchived))
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrFormNotFound
	}
	return nil
}

func (r *formRepository) CreateQuestion(ctx context.Context, question *domain.FormQuestion) error {
	model := questionToPersistence(*question)
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var form persistence.Form
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&form, "id = ?", question.FormID).Error; err != nil {
			return mapErr(err)
		}
		if form.Status == string(domain.FormArchived) {
			return domain.ErrInvalidForm
		}
		if err := placeQuestion(tx, question.FormID, &model); err != nil {
			return err
		}
		if err := tx.Omit("Options").Create(&model).Error; err != nil {
			return err
		}
		if len(model.Options) > 0 {
			for i := range model.Options {
				model.Options[i].QuestionID = model.ID
			}
			if err := tx.Create(&model.Options).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return mapErr(err)
	}
	*question = questionToDomain(model)
	return nil
}

func placeQuestion(tx *gorm.DB, formID uint, model *persistence.FormQuestion) error {
	if model.Position == domain.AppendQuestionPosition {
		var next int
		if err := tx.Model(&persistence.FormQuestion{}).Where("form_id = ?", formID).
			Select("COALESCE(MAX(position) + 1, 0)").Scan(&next).Error; err != nil {
			return err
		}
		model.Position = next
		return nil
	}
	var taken int64
	if err := tx.Model(&persistence.FormQuestion{}).
		Where("form_id = ? AND position = ?", formID, model.Position).Count(&taken).Error; err != nil {
		return err
	}
	if taken == 0 {
		return nil
	}
	return tx.Model(&persistence.FormQuestion{}).
		Where("form_id = ? AND position >= ?", formID, model.Position).
		UpdateColumn("position", gorm.Expr("position + 1")).Error
}

func (r *formRepository) UpdateQuestion(ctx context.Context, question *domain.FormQuestion) error {
	model := questionToPersistence(*question)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var form persistence.Form
		if err := tx.First(&form, "id = ?", question.FormID).Error; err != nil {
			return mapErr(err)
		}
		if form.Status == string(domain.FormArchived) {
			return domain.ErrInvalidForm
		}
		changes := map[string]any{
			"type": question.Type, "title": question.Title, "description": question.Description,
			"required": question.Required, "settings": question.Settings,
		}
		if question.Position != domain.AppendQuestionPosition {
			changes["position"] = question.Position
		}
		result := tx.Model(&persistence.FormQuestion{}).
			Where("id = ? AND form_id = ?", question.ID, question.FormID).
			Updates(changes)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		if err := tx.Where("question_id = ?", question.ID).Delete(&persistence.FormQuestionOption{}).Error; err != nil {
			return err
		}
		for i := range model.Options {
			model.Options[i].QuestionID = question.ID
		}
		if len(model.Options) > 0 {
			if err := tx.Create(&model.Options).Error; err != nil {
				return err
			}
		}
		if question.Position == domain.AppendQuestionPosition {
			return tx.Model(&persistence.FormQuestion{}).Where("id = ?", question.ID).
				Select("position").Scan(&question.Position).Error
		}
		return nil
	})
	return mapErr(err)
}

func (r *formRepository) DeleteQuestion(ctx context.Context, formID, questionID uint) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var form persistence.Form
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&form, "id = ?", formID).Error; err != nil {
			return mapErr(err)
		}
		var question persistence.FormQuestion
		if err := tx.Where("form_id = ?", formID).First(&question, questionID).Error; err != nil {
			return mapErr(err)
		}
		var answerCount int64
		if err := tx.Model(&persistence.SubmissionAnswer{}).
			Where("question_id = ?", questionID).Count(&answerCount).Error; err != nil {
			return err
		}
		if answerCount > 0 {
			return domain.ErrQuestionInUse
		}
		return tx.Delete(&question).Error
	}))
}

func (r *formRepository) ReorderQuestions(ctx context.Context, formID uint, questionIDs []uint) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var form persistence.Form
		if err := tx.First(&form, "id = ?", formID).Error; err != nil {
			return mapErr(err)
		}
		if form.Status == string(domain.FormArchived) {
			return domain.ErrInvalidForm
		}
		var count int64
		if err := tx.Model(&persistence.FormQuestion{}).
			Where("form_id = ? AND id IN ?", formID, questionIDs).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(questionIDs)) {
			return domain.ErrInvalidForm
		}
		for position, id := range questionIDs {
			if err := tx.Model(&persistence.FormQuestion{}).
				Where("form_id = ? AND id = ?", formID, id).
				Update("position", position).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *formRepository) Publish(ctx context.Context, formID uint) (*domain.Form, error) {
	var published domain.Form
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var form persistence.Form
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&form, "id = ?", formID).Error; err != nil {
			return mapErr(err)
		}
		if form.Status == string(domain.FormArchived) {
			return domain.ErrInvalidForm
		}
		var questions []persistence.FormQuestion
		if err := tx.Preload("Options", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC, id ASC") }).
			Where("form_id = ?", formID).Order("position ASC, id ASC").Find(&questions).Error; err != nil {
			return err
		}
		if len(questions) == 0 {
			return domain.ErrInvalidForm
		}
		questionSchema := mapSlice(questions, questionToDomain)
		snapshot := struct {
			FormID      uint                  `json:"form_id"`
			Name        string                `json:"name"`
			Description string                `json:"description"`
			Questions   []domain.FormQuestion `json:"questions"`
			Settings    domain.FormSettings   `json:"settings"`
		}{form.ID, form.Name, form.Description, questionSchema, decodeFormSettings(form.Settings)}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("encode form schema snapshot: %w", err)
		}
		var latest int
		if err := tx.Model(&persistence.FormVersion{}).
			Where("form_id = ?", formID).Select("COALESCE(MAX(version), 0)").Scan(&latest).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		version := persistence.FormVersion{
			FormID: formID, Version: latest + 1, SchemaSnapshot: raw,
			Status: "published", PublishedAt: now, CreatedAt: now,
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		form.Status = string(domain.FormPublished)
		form.CurrentVersionID = &version.ID
		if err := tx.Model(&form).Updates(map[string]any{
			"status": form.Status, "current_version_id": version.ID,
		}).Error; err != nil {
			return err
		}
		var taskConfig domain.FormTaskConfiguration
		if len(form.TaskConfig) > 0 && json.Unmarshal(form.TaskConfig, &taskConfig) == nil && taskConfig.Enabled {
			configSnapshot, err := json.Marshal(taskConfig)
			if err != nil {
				return fmt.Errorf("encode form task configuration snapshot: %w", err)
			}
			row := persistence.FormTaskConfig{
				FormID: form.ID, FormVersionID: version.ID, WorkspaceID: form.WorkspaceID,
				TargetListID: taskConfig.Target.ListID,
				TemplateVersionID: func() *uint {
					if taskConfig.Template == nil {
						return nil
					}
					return &taskConfig.Template.VersionID
				}(),
				Enabled: true, Configuration: configSnapshot, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		published = formToDomain(form)
		published.Questions = questionSchema
		published.CurrentVersion = &domain.FormVersion{
			ID: version.ID, FormID: version.FormID, Version: version.Version,
			SchemaSnapshot: raw, Status: version.Status, PublishedAt: version.PublishedAt,
			CreatedAt: version.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return &published, nil
}

func (r *formRepository) UpdateTaskConfig(
	ctx context.Context,
	workspaceID, formID uint,
	config json.RawMessage,
) error {
	result := database.DBFromContext(ctx, r.db).Model(&persistence.Form{}).
		Where("id = ? AND workspace_id = ?", formID, workspaceID).
		Update("task_config", config)
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrFormNotFound
	}
	return nil
}

func (r *formRepository) ProcessSubmission(
	ctx context.Context,
	submissionID uint,
	createTask func(context.Context, domain.FormSubmission, domain.FormTaskConfiguration) (uint, json.RawMessage, error),
) (domain.TaskCreationStatus, error) {
	status := domain.TaskCreationSkipped
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var submission persistence.FormSubmission
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&submission, "id = ?", submissionID).Error; err != nil {
			return mapErr(err)
		}
		if submission.TaskCreationStatus == string(domain.TaskCreationSucceeded) ||
			submission.TaskCreationStatus == string(domain.TaskCreationSkipped) {
			status = domain.TaskCreationStatus(submission.TaskCreationStatus)
			return nil
		}
		var configRow persistence.FormTaskConfig
		if err := tx.First(&configRow, "form_id = ? AND form_version_id = ? AND enabled = true",
			submission.FormID, submission.FormVersionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				status = domain.TaskCreationSkipped
				return tx.Model(&submission).Updates(map[string]any{
					"task_creation_status": string(status), "task_creation_error": "",
				}).Error
			}
			return err
		}
		var form persistence.Form
		if err := tx.First(&form, "id = ?", submission.FormID).Error; err != nil {
			return mapErr(err)
		}
		if err := tx.Model(&submission).Updates(map[string]any{
			"task_creation_status": string(domain.TaskCreationProcessing), "task_creation_error": "",
		}).Error; err != nil {
			return err
		}
		submissionDomain := persistenceSubmissionToDomain(submission, nil)
		submissionDomain.CreatedBy = form.CreatedBy
		var answerRows []persistence.SubmissionAnswer
		if err := tx.Where("submission_id = ?", submissionID).Order("id ASC").Find(&answerRows).Error; err != nil {
			return err
		}
		submissionDomain.Answers = persistenceSubmissionToDomain(submission, answerRows).Answers
		var taskConfig domain.FormTaskConfiguration
		if err := json.Unmarshal(configRow.Configuration, &taskConfig); err != nil {
			return fmt.Errorf("decode form task configuration: %w", err)
		}
		taskID, mappingSnapshot, err := createTask(txCtx, submissionDomain, taskConfig)
		if err != nil {
			return err
		}
		templateVersionID := configRow.TemplateVersionID
		relation := persistence.FormSubmissionTask{
			SubmissionID: submissionID, TaskID: taskID,
			TemplateVersionID: templateVersionID, MappingSnapshot: mappingSnapshot,
			CreatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&relation).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]uint{
			"submission_id": submissionID, "task_id": taskID, "form_id": submission.FormID,
		})
		if err != nil {
			return fmt.Errorf("encode task creation outbox event: %w", err)
		}
		if err := tx.Exec(`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload, created_at)
			VALUES (?, 'form_submission', ?, ?::jsonb, now())`,
			"form.submission.task_created", submissionID, string(payload)).Error; err != nil {
			return err
		}
		status = domain.TaskCreationSucceeded
		return tx.Model(&submission).Updates(map[string]any{
			"task_creation_status": string(status), "task_creation_error": "",
		}).Error
	})
	if err != nil {
		updateErr := database.DBFromContext(ctx, r.db).Model(&persistence.FormSubmission{}).
			Where("id = ? AND task_creation_status <> ?", submissionID, string(domain.TaskCreationSucceeded)).
			Updates(map[string]any{
				"task_creation_status": string(domain.TaskCreationFailed),
				"task_creation_error":  truncateTaskError(err.Error()),
			}).Error
		if updateErr != nil {
			return domain.TaskCreationFailed, errors.Join(err, mapErr(updateErr))
		}
		return domain.TaskCreationFailed, nil
	}
	return status, nil
}

func (r *formRepository) GetSubmissionTask(ctx context.Context, submissionID uint) (uint, error) {
	var relation persistence.FormSubmissionTask
	if err := database.DBFromContext(ctx, r.db).First(&relation, "submission_id = ?", submissionID).Error; err != nil {
		return 0, mapErr(err)
	}
	return relation.TaskID, nil
}

func (r *formRepository) GetSubmissionTaskStatus(
	ctx context.Context,
	submissionID uint,
) (domain.TaskCreationStatus, string, error) {
	var row persistence.FormSubmission
	if err := database.DBFromContext(ctx, r.db).First(&row, "id = ?", submissionID).Error; err != nil {
		return "", "", mapErr(err)
	}
	return domain.TaskCreationStatus(row.TaskCreationStatus), row.TaskCreationError, nil
}

func (r *formRepository) GetSubmissionWorkspace(ctx context.Context, submissionID uint) (uint, error) {
	var workspaceID uint
	err := database.DBFromContext(ctx, r.db).Model(&persistence.FormSubmission{}).
		Select("workspace_id").Where("id = ?", submissionID).Scan(&workspaceID).Error
	if err != nil {
		return 0, mapErr(err)
	}
	if workspaceID == 0 {
		return 0, domain.ErrNotFound
	}
	return workspaceID, nil
}

func truncateTaskError(message string) string {
	const maxLength = 2000
	if len(message) > maxLength {
		return message[:maxLength]
	}
	return message
}

func (r *formRepository) GetPublishedBySlug(ctx context.Context, slug string) (*domain.Form, error) {
	var form persistence.Form
	err := r.db.WithContext(ctx).Where("lower(slug) = lower(?) AND status = ?", slug, domain.FormPublished).
		First(&form).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if form.CurrentVersionID == nil {
		return nil, domain.ErrFormNotPublished
	}
	var version persistence.FormVersion
	if err := r.db.WithContext(ctx).
		Where("id = ? AND form_id = ? AND status = 'published'", *form.CurrentVersionID, form.ID).
		First(&version).Error; err != nil {
		return nil, domain.ErrFormNotPublished
	}
	result := formToDomain(form)
	result.CurrentVersion = &domain.FormVersion{
		ID: version.ID, FormID: version.FormID, Version: version.Version,
		SchemaSnapshot: version.SchemaSnapshot, Status: version.Status,
		PublishedAt: version.PublishedAt, CreatedAt: version.CreatedAt,
	}
	return &result, nil
}

func (r *formRepository) CreateSubmission(
	ctx context.Context,
	formID uint,
	respondentID *uint,
	idempotencyKey string,
	requestHash string,
	answers []domain.SubmissionAnswer,
	options domain.SubmissionOptions,
) (*domain.FormSubmission, bool, error) {
	var created domain.FormSubmission
	var replay bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("form_id = ? AND key = ? AND expires_at <= ?", formID, idempotencyKey, time.Now()).
			Delete(&persistence.FormIdempotencyKey{}).Error; err != nil {
			return err
		}
		var existing persistence.FormIdempotencyKey
		lookupErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("form_id = ? AND key = ? AND expires_at > ?", formID, idempotencyKey, time.Now()).
			First(&existing).Error
		if lookupErr == nil {
			if existing.RequestHash != requestHash {
				return domain.ErrIdempotencyConflict
			}
			if existing.SubmissionID == nil || existing.Status != "completed" {
				return domain.ErrInvalidSubmission
			}
			submission, err := loadSubmission(tx, *existing.SubmissionID)
			if err != nil {
				return err
			}
			created, replay = submissionToDomain(submission), true
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}

		var form persistence.Form
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&form, "id = ? AND status = ? AND current_version_id IS NOT NULL", formID, domain.FormPublished).Error; err != nil {
			return domain.ErrFormNotPublished
		}
		if err := checkSubmissionOptions(tx, formID, options); err != nil {
			return err
		}
		idem := persistence.FormIdempotencyKey{
			FormID: formID, Key: idempotencyKey, RequestHash: requestHash,
			Status: "processing", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		}
		if err := tx.Create(&idem).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		submission := persistence.FormSubmission{
			FormID: form.ID, FormVersionID: *form.CurrentVersionID, WorkspaceID: form.WorkspaceID,
			RespondentID: respondentID, Status: "submitted", SubmittedAt: now, CreatedAt: now,
		}
		if err := tx.Create(&submission).Error; err != nil {
			return err
		}
		rows := make([]persistence.SubmissionAnswer, len(answers))
		for i, answer := range answers {
			rows[i] = persistence.SubmissionAnswer{
				SubmissionID: submission.ID, QuestionID: answer.QuestionID,
				Value: answer.Value, CreatedAt: now,
			}
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		if err := claimFormUploads(tx, formID, submission.ID, options.Uploads, now); err != nil {
			return err
		}
		if err := tx.Model(&idem).Updates(map[string]any{
			"status": "completed", "submission_id": submission.ID,
		}).Error; err != nil {
			return err
		}
		created = persistenceSubmissionToDomain(submission, rows)
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			var existing persistence.FormIdempotencyKey
			lookupErr := r.db.WithContext(ctx).Where("form_id = ? AND key = ?", formID, idempotencyKey).First(&existing).Error
			if lookupErr == nil && existing.RequestHash == requestHash && existing.SubmissionID != nil {
				row, loadErr := loadSubmission(r.db.WithContext(ctx), *existing.SubmissionID)
				if loadErr == nil {
					return ptrSubmission(submissionToDomain(row)), true, nil
				}
			}
		}
		return nil, false, mapErr(err)
	}
	return &created, replay, nil
}

func (r *formRepository) ListSubmissions(
	ctx context.Context,
	formID uint,
	filter domain.SubmissionFilter,
	limit, offset int,
) ([]domain.FormSubmission, int64, error) {
	query := r.db.WithContext(ctx).Model(&persistence.FormSubmission{}).Where("form_id = ?", formID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TaskCreationStatus != "" {
		query = query.Where("task_creation_status = ?", string(filter.TaskCreationStatus))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []persistence.FormSubmission
	if err := query.Preload("Answers", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC")
	}).Order("submitted_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	return mapSlice(rows, submissionToDomain), total, nil
}

func (r *formRepository) GetSubmission(ctx context.Context, submissionID uint) (*domain.FormSubmission, error) {
	row, err := loadSubmission(r.db.WithContext(ctx), submissionID)
	if err != nil {
		return nil, mapErr(err)
	}
	submission := submissionToDomain(row)
	return &submission, nil
}

func loadSubmission(db *gorm.DB, submissionID uint) (persistence.FormSubmission, error) {
	var row persistence.FormSubmission
	err := db.Preload("Answers", func(db *gorm.DB) *gorm.DB { return db.Order("id ASC") }).
		First(&row, "id = ?", submissionID).Error
	return row, err
}

func formToDomain(row persistence.Form) domain.Form {
	return domain.Form{
		Model:       domain.Model{ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt},
		WorkspaceID: row.WorkspaceID, Name: row.Name, Description: row.Description, Slug: row.Slug,
		Status: domain.FormStatus(row.Status), CurrentVersionID: row.CurrentVersionID, CreatedBy: row.CreatedBy,
		TaskConfig: row.TaskConfig, Settings: decodeFormSettings(row.Settings),
	}
}

func formToPersistence(form domain.Form) persistence.Form {
	return persistence.Form{
		ID: form.ID, CreatedAt: form.CreatedAt, UpdatedAt: form.UpdatedAt, WorkspaceID: form.WorkspaceID,
		Name: form.Name, Description: form.Description, Slug: form.Slug,
		Status: string(form.Status), CurrentVersionID: form.CurrentVersionID, CreatedBy: form.CreatedBy,
		TaskConfig: form.TaskConfig, Settings: encodeFormSettings(form.Settings),
	}
}

func questionToDomain(row persistence.FormQuestion) domain.FormQuestion {
	options := make([]domain.QuestionOption, len(row.Options))
	for i, option := range row.Options {
		options[i] = domain.QuestionOption{
			ID: option.ID, Label: option.Label, Value: option.Value, Position: option.Position,
		}
	}
	return domain.FormQuestion{
		ID: row.ID, FormID: row.FormID, Type: domain.QuestionType(row.Type), Title: row.Title,
		Description: row.Description, Required: row.Required, Position: row.Position,
		Settings: row.Settings, Options: options,
	}
}

func questionToPersistence(question domain.FormQuestion) persistence.FormQuestion {
	options := make([]persistence.FormQuestionOption, len(question.Options))
	for i, option := range question.Options {
		options[i] = persistence.FormQuestionOption{
			ID: option.ID, QuestionID: question.ID, Label: option.Label, Value: option.Value, Position: i,
		}
	}
	settings := question.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}
	return persistence.FormQuestion{
		Model: gorm.Model{ID: question.ID}, FormID: question.FormID, Type: string(question.Type),
		Title: question.Title, Description: question.Description, Required: question.Required,
		Position: question.Position, Settings: settings, Options: options,
	}
}

func submissionToDomain(row persistence.FormSubmission) domain.FormSubmission {
	return persistenceSubmissionToDomain(row, row.Answers)
}

func persistenceSubmissionToDomain(row persistence.FormSubmission, answers []persistence.SubmissionAnswer) domain.FormSubmission {
	result := domain.FormSubmission{
		ID: row.ID, FormID: row.FormID, FormVersionID: row.FormVersionID, WorkspaceID: row.WorkspaceID,
		RespondentID: row.RespondentID, Status: row.Status, SubmittedAt: row.SubmittedAt,
		TaskCreationStatus: domain.TaskCreationStatus(row.TaskCreationStatus), TaskCreationError: row.TaskCreationError,
		Answers: make([]domain.SubmissionAnswer, len(answers)),
	}
	for i, answer := range answers {
		result.Answers[i] = domain.SubmissionAnswer{QuestionID: answer.QuestionID, Value: answer.Value}
	}
	return result
}

func ptrSubmission(value domain.FormSubmission) *domain.FormSubmission {
	return &value
}
