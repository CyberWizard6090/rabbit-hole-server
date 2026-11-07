package database

import (
	"context"
	"errors"

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
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(hookCtx, transactionContextKey{}, tx))
	})
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

func transactionFromContext(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(transactionContextKey{}).(*gorm.DB)
	return tx
}

var ErrTransactionRequired = errors.New("database transaction required")
