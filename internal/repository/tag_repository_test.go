package repository

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Run("postgres unique violation", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23505", ConstraintName: "idx_space_tag_name"}
		if !isUniqueViolation(err) {
			t.Fatal("expected unique violation")
		}
	})
	t.Run("other postgres error", func(t *testing.T) {
		err := &pgconn.PgError{Code: "23503"}
		if isUniqueViolation(err) {
			t.Fatal("did not expect unique violation")
		}
	})
	t.Run("wrapped unique violation", func(t *testing.T) {
		err := errors.New("outer")
		wrapped := errors.Join(err, &pgconn.PgError{Code: "23505"})
		if !isUniqueViolation(wrapped) {
			t.Fatal("expected wrapped unique violation")
		}
	})
}
