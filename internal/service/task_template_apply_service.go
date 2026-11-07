package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"rabbit-hole-server/internal/domain"
)

type TransactionRunner interface {
	Run(ctx context.Context, fn func(context.Context) error) error
}

type TaskTemplateApplier interface {
	Apply(ctx context.Context, userID, templateID uint, listID, statusID uint, title, description string) (*domain.Task, error)
}

type taskTemplateApplier struct {
	tx        TransactionRunner
	templates domain.TaskTemplateRepository
	spaces    domain.SpaceRepository
	tasks     TaskService
	fields    CustomFieldService
}

func NewTaskTemplateApplier(
	tx TransactionRunner,
	templates domain.TaskTemplateRepository,
	spaces domain.SpaceRepository,
	tasks TaskService,
	fields CustomFieldService,
) TaskTemplateApplier {
	return &taskTemplateApplier{tx: tx, templates: templates, spaces: spaces, tasks: tasks, fields: fields}
}

func (s *taskTemplateApplier) Apply(
	ctx context.Context,
	userID, templateID uint,
	listID, statusID uint,
	title, description string,
) (*domain.Task, error) {
	var created *domain.Task
	err := s.tx.Run(ctx, func(txCtx context.Context) error {
		template, err := s.templates.Get(txCtx, templateID)
		if err != nil {
			return err
		}
		if template.Status != "active" || len(template.Versions) == 0 {
			return domain.ErrTaskTemplateArchived
		}
		list, err := s.spaces.GetListByID(txCtx, listID)
		if err != nil {
			return err
		}
		space, err := s.spaces.GetByID(txCtx, list.SpaceID)
		if err != nil || space.WorkspaceID != template.WorkspaceID {
			return domain.ErrInvalidListReference
		}
		version := template.Versions[len(template.Versions)-1]
		var config TaskTemplateConfiguration
		if err := json.Unmarshal(version.Configuration, &config); err != nil {
			return fmt.Errorf("decode task template version: %w", err)
		}
		if title == "" && config.Task.Title != nil {
			title = *config.Task.Title
		}
		if title == "" {
			title = template.Name
		}
		if description == "" && config.Task.Description != nil {
			description = *config.Task.Description
		}
		if statusID == 0 && config.Task.StatusID != nil {
			statusID = *config.Task.StatusID
		}
		priority := taskPriorityValue(config.Task.Priority)
		assigneeIDs := []uint{}
		if config.Task.AssigneeID != nil {
			assigneeIDs = append(assigneeIDs, *config.Task.AssigneeID)
		}
		params := CreateTaskParams{
			Title: strings.TrimSpace(title), Description: strings.TrimSpace(description),
			ListID: listID, StatusID: statusID, Priority: priority,
			AssigneeIDs: assigneeIDs, TagIDs: config.Tags,
		}
		created, err = s.tasks.CreateTask(txCtx, userID, params)
		if err != nil {
			return err
		}
		for _, field := range config.CustomFields {
			var value any
			if err := json.Unmarshal(field.Value, &value); err != nil {
				return domain.ErrInvalidTaskTemplate
			}
			if err := s.fields.SetTaskValue(txCtx, created.ID, field.FieldID, value); err != nil {
				return fmt.Errorf("apply custom field %d: %w", field.FieldID, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
