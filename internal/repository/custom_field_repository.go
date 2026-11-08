package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

type customFieldRepository struct {
	db *gorm.DB
}

func NewCustomFieldRepository(db *gorm.DB) domain.CustomFieldRepository {
	return &customFieldRepository{db: db}
}

func (r *customFieldRepository) Create(ctx context.Context, field *domain.CustomField) error {
	model := customFieldFromDomain(*field)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Options").Create(&model).Error; err != nil {
			return err
		}
		for i := range model.Options {
			model.Options[i].CustomFieldID = model.ID
		}
		if len(model.Options) > 0 {
			if err := tx.Create(&model.Options).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrCustomFieldNameTaken
		}
		return mapErr(err)
	}
	*field = customFieldToDomain(model)
	return nil
}

func (r *customFieldRepository) ListByWorkspace(ctx context.Context, workspaceID uint) ([]domain.CustomField, error) {
	var rows []persistence.CustomField
	err := database.DBFromContext(ctx, r.db).
		Preload("Options", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC, id ASC") }).
		Where("workspace_id = ?", workspaceID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	return mapSlice(rows, customFieldToDomain), nil
}

func (r *customFieldRepository) GetByID(ctx context.Context, fieldID uint) (*domain.CustomField, error) {
	var row persistence.CustomField
	err := database.DBFromContext(ctx, r.db).
		Preload("Options", func(db *gorm.DB) *gorm.DB { return db.Order("position ASC, id ASC") }).
		First(&row, "id = ?", fieldID).Error
	if err != nil {
		return nil, mapErr(err)
	}
	field := customFieldToDomain(row)
	return &field, nil
}

func (r *customFieldRepository) Update(ctx context.Context, field *domain.CustomField) error {
	model := customFieldFromDomain(*field)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var current persistence.CustomField
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND workspace_id = ?", field.ID, field.WorkspaceID).
			First(&current).Error; err != nil {
			return err
		}
		if current.Type != string(field.Type) {
			var values int64
			if err := tx.Model(&persistence.TaskCustomFieldValue{}).
				Where("custom_field_id = ?", field.ID).Count(&values).Error; err != nil {
				return err
			}
			referenced, err := isCustomFieldReferenced(tx, field.ID)
			if err != nil {
				return err
			}
			if values > 0 {
				return &domain.CustomFieldEditError{
					Cause: domain.ErrCustomFieldInUse, Field: "type", Code: "VALUES_EXIST",
					Params: map[string]any{"count": values},
				}
			}
			if referenced {
				return &domain.CustomFieldEditError{
					Cause: domain.ErrCustomFieldInUse, Field: "type", Code: "REFERENCED",
				}
			}
		} else if err := checkStoredValuesFit(tx, current, field); err != nil {
			return err
		}
		result := tx.Model(&persistence.CustomField{}).
			Where("id = ? AND workspace_id = ?", field.ID, field.WorkspaceID).
			Updates(map[string]any{
				"name": field.Name, "description": field.Description, "type": string(field.Type),
				"required": field.Required, "config": field.Config,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		if err := tx.Where("custom_field_id = ?", field.ID).Delete(&persistence.CustomFieldOption{}).Error; err != nil {
			return err
		}
		for i := range model.Options {
			model.Options[i].CustomFieldID = field.ID
		}
		if len(model.Options) > 0 {
			if err := tx.Create(&model.Options).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrCustomFieldNameTaken
		}
		return mapErr(err)
	}
	return nil
}

func (r *customFieldRepository) Delete(ctx context.Context, workspaceID, fieldID uint) error {
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&persistence.TaskCustomFieldValue{}).
			Where("custom_field_id = ?", fieldID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return domain.ErrCustomFieldInUse
		}
		referenced, err := isCustomFieldReferenced(tx, fieldID)
		if err != nil {
			return err
		}
		if referenced {
			return domain.ErrCustomFieldInUse
		}
		result := tx.Where("workspace_id = ?", workspaceID).Delete(&persistence.CustomField{}, fieldID)
		if result.Error != nil {
			var pgErr *pgconn.PgError
			if errors.As(result.Error, &pgErr) && pgErr.Code == "23503" {
				return domain.ErrCustomFieldInUse
			}
			return result.Error
		}
		if result.RowsAffected == 0 {
			return domain.ErrNotFound
		}
		return nil
	})
	return mapErr(err)
}

const liveFieldValuesSQL = "FROM task_custom_field_values v JOIN tasks t ON t.id = v.task_id " +
	"WHERE v.custom_field_id = ? AND "

func liveFieldValues() string {
	return liveFieldValuesSQL + liveTaskPredicate("t")
}

func countFieldValues(tx *gorm.DB, fieldID uint, condition string, args ...any) (int64, error) {
	var total int64
	err := tx.Raw("SELECT count(*) "+liveFieldValues()+" AND "+condition, append([]any{fieldID}, args...)...).
		Scan(&total).Error
	return total, err
}

func configBound(config map[string]any, key string) (float64, bool) {
	switch number := config[key].(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	return 0, false
}

func checkStoredValuesFit(tx *gorm.DB, current persistence.CustomField, next *domain.CustomField) error {
	switch next.Type {
	case domain.CustomFieldDropdown, domain.CustomFieldMultiSelect:
		return checkOptionsStillCoverValues(tx, current.ID, next)
	case domain.CustomFieldNumber, domain.CustomFieldInteger:
		return checkBounds(tx, current.ID, next.Config, "jsonb_typeof(v.value) = 'number'", "(v.value #>> '{}')::numeric",
			[]boundCheck{
				{key: "min", op: "<", code: "VALUE_BELOW_MIN"},
				{key: "max", op: ">", code: "VALUE_ABOVE_MAX"},
			})
	case domain.CustomFieldText:
		return checkBounds(tx, current.ID, next.Config, "jsonb_typeof(v.value) = 'string'", "char_length(v.value #>> '{}')",
			[]boundCheck{
				{key: "min_length", op: "<", code: "VALUE_TOO_SHORT"},
				{key: "max_length", op: ">", code: "VALUE_TOO_LONG"},
			})
	}
	return nil
}

type boundCheck struct {
	key  string
	op   string
	code string
}

func checkBounds(
	tx *gorm.DB, fieldID uint, config map[string]any, typeGuard, expr string, checks []boundCheck,
) error {
	for _, check := range checks {
		bound, ok := configBound(config, check.key)
		if !ok {
			continue
		}
		count, err := countFieldValues(tx, fieldID, typeGuard+" AND "+expr+" "+check.op+" ?", bound)
		if err != nil {
			return err
		}
		if count > 0 {
			return &domain.CustomFieldEditError{
				Cause: domain.ErrCustomFieldValueOutOfRange, Field: "config." + check.key, Code: check.code,
				Params: map[string]any{"bound": bound, "count": count},
			}
		}
	}
	return nil
}

func checkOptionsStillCoverValues(tx *gorm.DB, fieldID uint, next *domain.CustomField) error {
	kept := make([]string, 0, len(next.Options))
	for _, option := range next.Options {
		kept = append(kept, option.Value)
	}
	var used []string
	query := "SELECT DISTINCT e " +
		"FROM (SELECT v.value #>> '{}' AS e " + liveFieldValues() + " AND jsonb_typeof(v.value) = 'string' " +
		"UNION SELECT jsonb_array_elements_text(v.value) " + liveFieldValues() + " AND jsonb_typeof(v.value) = 'array') used " +
		"WHERE e NOT IN ? ORDER BY e"
	if err := tx.Raw(query, fieldID, fieldID, kept).Scan(&used).Error; err != nil {
		return err
	}
	if len(used) == 0 {
		return nil
	}
	return &domain.CustomFieldEditError{
		Cause: domain.ErrCustomFieldValueOutOfRange, Field: "options", Code: "OPTION_IN_USE",
		Params: map[string]any{"values": used},
	}
}

func isCustomFieldReferenced(tx *gorm.DB, fieldID uint) (bool, error) {
	var referenced bool
	err := tx.Raw(`
		SELECT
			EXISTS (
				SELECT 1 FROM task_template_versions
				WHERE configuration->'custom_fields' @> jsonb_build_array(jsonb_build_object('field_id', ?::bigint))
			) OR EXISTS (
				SELECT 1 FROM form_task_configs
				WHERE configuration->'mappings' @> jsonb_build_array(
					jsonb_build_object('target', jsonb_build_object('type', 'custom_field', 'field_id', ?::bigint))
				)
			) OR EXISTS (
				SELECT 1 FROM forms
				WHERE task_config->'mappings' @> jsonb_build_array(
					jsonb_build_object('target', jsonb_build_object('type', 'custom_field', 'field_id', ?::bigint))
				)
			)`, fieldID, fieldID, fieldID).Scan(&referenced).Error
	return referenced, err
}

func customFieldToDomain(row persistence.CustomField) domain.CustomField {
	options := make([]domain.CustomFieldOption, len(row.Options))
	for i, option := range row.Options {
		options[i] = domain.CustomFieldOption{
			ID: option.ID, Label: option.Label, Value: option.Value, Position: option.Position,
		}
	}
	if row.Config == nil {
		row.Config = map[string]any{}
	}
	return domain.CustomField{
		Model: modelToDomain(row.Model), WorkspaceID: row.WorkspaceID, Name: row.Name,
		Description: row.Description, Type: domain.CustomFieldType(row.Type), Required: row.Required,
		Config: row.Config, Options: options,
	}
}

func customFieldFromDomain(field domain.CustomField) persistence.CustomField {
	options := make([]persistence.CustomFieldOption, len(field.Options))
	for i, option := range field.Options {
		options[i] = persistence.CustomFieldOption{
			ID: option.ID, CustomFieldID: field.ID, Label: option.Label,
			Value: option.Value, Position: option.Position,
		}
	}
	config := field.Config
	if config == nil {
		config = map[string]any{}
	}
	return persistence.CustomField{
		Model: modelFromDomain(field.Model), WorkspaceID: field.WorkspaceID, Name: field.Name,
		Description: field.Description, Type: string(field.Type), Required: field.Required,
		Config: config, Options: options,
	}
}

type taskCustomFieldValueRepository struct {
	db *gorm.DB
}

func NewTaskCustomFieldValueRepository(db *gorm.DB) domain.CustomFieldValueRepository {
	return &taskCustomFieldValueRepository{db: db}
}

func (r *taskCustomFieldValueRepository) SetTaskValue(
	ctx context.Context,
	taskID, fieldID uint,
	value any,
) error {
	raw, err := marshalJSON(value)
	if err != nil {
		return fmt.Errorf("encode custom field value: %w", err)
	}
	result := database.DBFromContext(ctx, r.db).Exec(`
		INSERT INTO task_custom_field_values (task_id, custom_field_id, workspace_id, value, created_at, updated_at)
		SELECT tasks.id, custom_fields.id, spaces.workspace_id, ?::jsonb, now(), now()
		FROM tasks
		JOIN spaces ON spaces.id = tasks.space_id AND spaces.deleted_at IS NULL
		JOIN custom_fields ON custom_fields.workspace_id = spaces.workspace_id
			AND custom_fields.id = ? AND custom_fields.deleted_at IS NULL
		WHERE tasks.id = ? AND tasks.deleted_at IS NULL
		ON CONFLICT (task_id, custom_field_id)
		DO UPDATE SET value = EXCLUDED.value, workspace_id = EXCLUDED.workspace_id, updated_at = now()`,
		string(raw), fieldID, taskID)
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *taskCustomFieldValueRepository) GetTaskWorkspaceID(ctx context.Context, taskID uint) (uint, error) {
	var workspaceID uint
	err := database.DBFromContext(ctx, r.db).Table("tasks").
		Select("spaces.workspace_id").
		Joins("JOIN spaces ON spaces.id = tasks.space_id AND spaces.deleted_at IS NULL").
		Where("tasks.id = ? AND tasks.deleted_at IS NULL", taskID).
		Scan(&workspaceID).Error
	if err != nil {
		return 0, mapErr(err)
	}
	if workspaceID == 0 {
		return 0, domain.ErrNotFound
	}
	return workspaceID, nil
}

func (r *taskCustomFieldValueRepository) DeleteTaskValue(
	ctx context.Context,
	taskID, fieldID uint,
) error {
	result := database.DBFromContext(ctx, r.db).Exec(`
		DELETE FROM task_custom_field_values
		WHERE task_id = ? AND custom_field_id = ?
		  AND EXISTS (
			SELECT 1 FROM tasks
			JOIN spaces ON spaces.id = tasks.space_id AND spaces.deleted_at IS NULL
			JOIN custom_fields ON custom_fields.id = task_custom_field_values.custom_field_id
				AND custom_fields.workspace_id = spaces.workspace_id AND custom_fields.deleted_at IS NULL
			WHERE tasks.id = task_custom_field_values.task_id
		  )`, taskID, fieldID)
	if result.Error != nil {
		return mapErr(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *taskCustomFieldValueRepository) ValidateTaskReferences(
	ctx context.Context,
	taskID uint,
	fieldType domain.CustomFieldType,
	ids []uint,
) error {
	if len(ids) == 0 {
		return nil
	}
	workspaceID, err := r.GetTaskWorkspaceID(ctx, taskID)
	if err != nil {
		return err
	}
	var count int64
	switch fieldType {
	case domain.CustomFieldPeople:
		err = database.DBFromContext(ctx, r.db).Table("workspace_members").
			Joins("JOIN workspaces ON workspaces.id = workspace_members.workspace_id AND workspaces.deleted_at IS NULL").
			Where("workspace_members.workspace_id = ? AND workspace_members.deleted_at IS NULL AND workspace_members.user_id IN ?",
				workspaceID, ids).
			Distinct("workspace_members.user_id").Count(&count).Error
	case domain.CustomFieldFiles:
		err = database.DBFromContext(ctx, r.db).Table("files").
			Where("workspace_id = ? AND kind <> 'avatar' AND id IN ?", workspaceID, ids).
			Distinct("id").Count(&count).Error
	default:
		return domain.ErrInvalidCustomField
	}
	if err != nil {
		return mapErr(err)
	}
	if count != int64(len(ids)) {
		return domain.ErrInvalidCustomField
	}
	return nil
}

func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func unmarshalJSON(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}

func (r *taskCustomFieldValueRepository) GetTaskValues(
	ctx context.Context,
	taskID uint,
) (map[uint]any, error) {
	var rows []persistence.TaskCustomFieldValue
	err := database.DBFromContext(ctx, r.db).Table("task_custom_field_values").
		Select("task_custom_field_values.*").
		Joins("JOIN tasks ON tasks.id = task_custom_field_values.task_id AND tasks.deleted_at IS NULL").
		Joins("JOIN spaces ON spaces.id = tasks.space_id AND spaces.deleted_at IS NULL").
		Where("task_custom_field_values.task_id = ? AND task_custom_field_values.workspace_id = spaces.workspace_id",
			taskID).
		Find(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	values := make(map[uint]any, len(rows))
	for _, row := range rows {
		var value any
		if err := unmarshalJSON(row.Value, &value); err != nil {
			return nil, fmt.Errorf("decode custom field value %d: %w", row.ID, err)
		}
		values[row.CustomFieldID] = value
	}
	if len(rows) == 0 {
		var count int64
		if err := database.DBFromContext(ctx, r.db).Table("tasks").
			Joins("JOIN spaces ON spaces.id = tasks.space_id AND spaces.deleted_at IS NULL").
			Where("tasks.id = ? AND tasks.deleted_at IS NULL", taskID).
			Count(&count).Error; err != nil {
			return nil, mapErr(err)
		}
		if count == 0 {
			return nil, domain.ErrNotFound
		}
	}
	return values, nil
}
