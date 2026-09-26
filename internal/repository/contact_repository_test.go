package repository

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsContactDuplicate(t *testing.T) {
	if !isContactDuplicate(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("expected duplicate")
	}
	if isContactDuplicate(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("did not expect duplicate")
	}
	if isContactDuplicate(nil) {
		t.Fatal("nil error must not be duplicate")
	}
}
