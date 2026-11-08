package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestContactServiceDelegatesOperations(t *testing.T) {
	users := []domain.CommentUser{{ID: 1, Username: "alice"}}
	requests := []domain.ContactRequest{{User: users[0]}}
	repo := &contactRepositoryMock{users: users, requests: requests}
	s := NewContactService(repo)
	ctx := context.Background()

	got, total, err := s.ListContacts(ctx, 7, 10, 5)
	if err != nil || !reflect.DeepEqual(got, users) || total != 1 || repo.gotUser != 7 || repo.gotLimit != 10 || repo.gotOffset != 5 {
		t.Fatalf("ListContacts: got=%v total=%d err=%v repo=%+v", got, total, err, repo)
	}
	if err := s.RequestContact(ctx, 7, 8); err != nil || repo.gotUser != 7 || repo.gotOther != 8 {
		t.Fatalf("RequestContact: err=%v repo=%+v", err, repo)
	}
	gotRequests, _, err := s.ListRequests(ctx, 7, true, 3, 6)
	if err != nil || !reflect.DeepEqual(gotRequests, requests) || !repo.gotIn || repo.gotLimit != 3 || repo.gotOffset != 6 {
		t.Fatalf("ListRequests: got=%v err=%v repo=%+v", gotRequests, err, repo)
	}
	for name, call := range map[string]func(context.Context, uint, uint) error{
		"accept": s.AcceptRequest, "decline": s.DeclineRequest, "cancel": s.CancelRequest, "remove": s.RemoveContact,
	} {
		if err := call(ctx, 7, 9); err != nil || repo.calls[len(repo.calls)-1] != name || repo.gotUser != 7 || repo.gotOther != 9 {
			t.Fatalf("%s: err=%v repo=%+v", name, err, repo)
		}
	}
	got, err = s.SearchUsers(ctx, 7, "  bob ")
	if err != nil || !reflect.DeepEqual(got, users) || repo.gotUser != 7 || repo.gotQuery != "bob" {
		t.Fatalf("SearchUsers: got=%v err=%v repo=%+v", got, err, repo)
	}
}

func TestContactServiceRejectsRequestingSelf(t *testing.T) {
	repo := &contactRepositoryMock{}
	if err := NewContactService(repo).RequestContact(context.Background(), 4, 4); !errors.Is(err, domain.ErrCannotAddSelf) {
		t.Fatalf("err=%v", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("repository was called: %v", repo.calls)
	}
}

func TestContactServicePropagatesRepositoryError(t *testing.T) {
	want := errors.New("db down")
	s := NewContactService(&contactRepositoryMock{err: want})
	ctx := context.Background()
	if _, _, err := s.ListContacts(ctx, 1, 10, 0); !errors.Is(err, want) {
		t.Fatalf("ListContacts err=%v", err)
	}
	if err := s.RequestContact(ctx, 1, 2); !errors.Is(err, want) {
		t.Fatalf("RequestContact err=%v", err)
	}
	if _, _, err := s.ListRequests(ctx, 1, false, 10, 0); !errors.Is(err, want) {
		t.Fatalf("ListRequests err=%v", err)
	}
	for name, call := range map[string]func(context.Context, uint, uint) error{
		"accept": s.AcceptRequest, "decline": s.DeclineRequest, "cancel": s.CancelRequest, "remove": s.RemoveContact,
	} {
		if err := call(ctx, 1, 2); !errors.Is(err, want) {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	if _, err := s.SearchUsers(ctx, 1, "xyz"); !errors.Is(err, want) {
		t.Fatalf("SearchUsers err=%v", err)
	}
}
