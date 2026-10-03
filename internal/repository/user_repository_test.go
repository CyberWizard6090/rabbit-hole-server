package repository

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsEmailUniqueViolation(t *testing.T) {
	for _, name := range []string{"uq_users_email"} {
		if !isEmailUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: name}) {
			t.Fatalf("constraint=%q", name)
		}
	}
	if isEmailUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: "uq_users_username"}) {
		t.Fatal("username constraint must not map to email")
	}
}

func TestIsUsernameUniqueViolation(t *testing.T) {
	for _, name := range []string{"uq_users_username"} {
		if !isUsernameUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: name}) {
			t.Fatalf("constraint=%q", name)
		}
	}
	if isUsernameUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: "uq_users_email"}) {
		t.Fatal("email constraint must not map to username")
	}
}
