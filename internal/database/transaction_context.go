package database

import (
	"context"
	"errors"
	"sync/atomic"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/txhook"
)

type transactionContextKey struct{}

type TransactionRunner struct {
	db *gorm.DB
}

func NewTransactionRunner(db *gorm.DB) *TransactionRunner {
	return &TransactionRunner{db: db}
}

func (r *TransactionRunner) Run(ctx context.Context, fn func(context.Context) error) error {
	return WithTransaction(ctx, r.db, fn)
}

func WithTransaction(ctx context.Context, db *gorm.DB, fn func(context.Context) error) error {
	if tx := transactionFromContext(ctx); tx != nil {
		return fn(ctx)
	}
	hookCtx, collect := txhook.With(ctx)
	handle := &transactionHandle{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		handle.db = tx
		return fn(context.WithValue(hookCtx, transactionContextKey{}, handle))
	})
	handle.finished.Store(true)
	if err != nil {
		return err
	}
	for _, hook := range collect() {
		hook()
	}
	return nil
}

func DBFromContext(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx := transactionFromContext(ctx); tx != nil {
		return tx.WithContext(ctx)
	}
	return fallback.WithContext(ctx)
}

type transactionHandle struct {
	db       *gorm.DB
	finished atomic.Bool
}

func transactionFromContext(ctx context.Context) *gorm.DB {
	handle, _ := ctx.Value(transactionContextKey{}).(*transactionHandle)
	if handle == nil || handle.finished.Load() {
		return nil
	}
	return handle.db
}

var ErrTransactionRequired = errors.New("database transaction required")
