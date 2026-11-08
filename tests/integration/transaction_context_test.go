package integration

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/repository"
)

func TestTransactionRunner_AllRepositoriesJoinTheTransaction(t *testing.T) {
	e := newCncEnv(t)
	runner := database.NewTransactionRunner(e.tc.DB)
	tasks := repository.NewTaskRepository(e.tc.DB)
	folders := repository.NewFolderRepository(e.tc.DB)
	rollback := errors.New("rollback")

	var createdTask, createdFolder uint
	err := runner.Run(context.Background(), func(ctx context.Context) error {
		task := &domain.Task{UserID: e.owner.userID, SpaceID: e.owner.spaceID, ListID: e.listID, StatusID: e.statusA, Title: "inside tx", Priority: 2}
		if err := tasks.Create(ctx, task, nil, nil); err != nil {
			return err
		}
		createdTask = task.ID
		if _, err := tasks.GetByID(ctx, task.ID); err != nil {
			t.Errorf("a repository did not see the uncommitted task: %v", err)
		}
		rename := func(tk *domain.Task) error {
			tk.Title = "renamed inside tx"
			return nil
		}
		if _, _, err := tasks.Update(ctx, task.ID, rename, nil, nil, nil); err != nil {
			return err
		}
		folder := &domain.Folder{Name: "inside tx", SpaceID: e.owner.spaceID}
		if err := folders.Create(ctx, folder); err != nil {
			return err
		}
		createdFolder = folder.ID
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("run error = %v", err)
	}
	var tasksLeft, foldersLeft int64
	if err := e.tc.DB.Raw("SELECT count(*) FROM tasks WHERE id = ?", createdTask).Scan(&tasksLeft).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.tc.DB.Raw("SELECT count(*) FROM folders WHERE id = ?", createdFolder).Scan(&foldersLeft).Error; err != nil {
		t.Fatal(err)
	}
	if tasksLeft != 0 || foldersLeft != 0 {
		t.Fatalf("rolled back rows survived: tasks=%d folders=%d", tasksLeft, foldersLeft)
	}
}

func TestTransactionRunner_ContextOutlivingTheTransactionUsesThePool(t *testing.T) {
	e := newCncEnv(t)
	runner := database.NewTransactionRunner(e.tc.DB)
	tasks := repository.NewTaskRepository(e.tc.DB)
	var captured context.Context
	if err := runner.Run(context.Background(), func(ctx context.Context) error {
		captured = ctx
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.GetByID(captured, e.taskID); err != nil {
		t.Fatalf("a context captured inside a finished transaction must fall back to the pool: %v", err)
	}
}
