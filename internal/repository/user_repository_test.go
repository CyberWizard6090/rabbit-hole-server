package repository

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsEmailUniqueViolation(t *testing.T) {
	for _, name := range []string{"idx_users_email", "uni_users_email", "users_email_key"} {
		if !isEmailUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: name}) {
			t.Fatalf("constraint=%q", name)
		}
	}
	if isEmailUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: "users_username_key"}) {
		t.Fatal("username constraint must not map to email")
	}
}

func TestIsUsernameUniqueViolation(t *testing.T) {
	for _, name := range []string{"idx_users_username", "uni_users_username", "users_username_key"} {
		if !isUsernameUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: name}) {
			t.Fatalf("constraint=%q", name)
		}
	}
	if isUsernameUniqueViolation(&pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}) {
		t.Fatal("email constraint must not map to username")
	}
}
