package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func decodeFormSettings(raw json.RawMessage) domain.FormSettings {
	var settings domain.FormSettings
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &settings)
	}
	if settings.NotifyOnSubmit == nil {
		settings.NotifyOnSubmit = []uint{}
	}
	return settings
}

func encodeFormSettings(settings domain.FormSettings) json.RawMessage {
	settings.LogoURL, settings.CoverURL = "", ""
	if settings.NotifyOnSubmit == nil {
		settings.NotifyOnSubmit = []uint{}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func checkSubmissionOptions(tx *gorm.DB, formID uint, options domain.SubmissionOptions) error {
	if options.ClosesAt != nil && !time.Now().Before(*options.ClosesAt) {
		return domain.ErrFormClosed
	}
	if options.MaxSubmissions != nil {
		var count int64
		if err := tx.Raw("SELECT count(*) FROM form_submissions WHERE form_id = ?", formID).
			Scan(&count).Error; err != nil {
			return err
		}
		if count >= int64(*options.MaxSubmissions) {
			return domain.ErrFormFull
		}
	}
	return nil
}

func (r *formRepository) CountSubmissions(ctx context.Context, formID uint) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Raw("SELECT count(*) FROM form_submissions WHERE form_id = ?", formID).
		Scan(&count).Error; err != nil {
		return 0, mapErr(err)
	}
	return int(count), nil
}

func (r *formRepository) WorkspaceImageExists(ctx context.Context, workspaceID uint, publicID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Raw(
		"SELECT count(*) FROM files WHERE kind = 'image' AND public_id = ? AND workspace_id = ?",
		publicID, workspaceID,
	).Scan(&count).Error; err != nil {
		return false, mapErr(err)
	}
	return count > 0, nil
}

func (r *formRepository) WorkspaceMemberIDs(ctx context.Context, workspaceID uint, userIDs []uint) ([]uint, error) {
	if len(userIDs) == 0 {
		return []uint{}, nil
	}
	var ids []uint
	if err := r.db.WithContext(ctx).Raw(
		"SELECT user_id FROM workspace_members WHERE workspace_id = ? AND deleted_at IS NULL AND user_id IN ? "+
			"ORDER BY user_id",
		workspaceID, userIDs,
	).Scan(&ids).Error; err != nil {
		return nil, mapErr(err)
	}
	return ids, nil
}
