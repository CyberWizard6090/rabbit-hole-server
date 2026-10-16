package service

import (
	"errors"
	"reflect"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestContactServiceDelegatesOperations(t *testing.T) {
	users := []domain.User{{ID: 1, Username: "alice"}}
	var gotUID, gotContact uint
	repo := &contactRepositoryMock{
		getContactsFn: func(uid uint) ([]domain.User, error) { gotUID = uid; return users, nil },
		getContactsPageFn: func(uid uint, limit, offset int) ([]domain.User, int64, error) {
			gotUID = uid
			if limit != 10 || offset != 5 {
				t.Fatalf("limit=%d offset=%d", limit, offset)
			}
			return users, int64(len(users)), nil
		},
		addContactFn:    func(uid, contactID uint) error { gotUID, gotContact = uid, contactID; return nil },
		deleteContactFn: func(uid, contactID uint) error { gotUID, gotContact = uid, contactID; return nil },
		searchUsersFn: func(q string, excludeID uint) ([]domain.User, error) {
			gotUID = excludeID
			if q != "bob" {
				t.Fatalf("query=%q", q)
			}
			return users, nil
		},
	}
	s := NewContactService(repo)

	got, err := s.GetContacts(7)
	if err != nil || !reflect.DeepEqual(got, users) || gotUID != 7 {
		t.Fatalf("GetContacts: got=%v err=%v uid=%d", got, err, gotUID)
	}
	got, total, err := s.GetContactsPage(7, 10, 5)
	if err != nil || !reflect.DeepEqual(got, users) || total != 1 || gotUID != 7 {
		t.Fatalf("GetContactsPage: got=%v total=%d err=%v uid=%d", got, total, err, gotUID)
	}
	if err := s.AddContact(7, 8); err != nil || gotUID != 7 || gotContact != 8 {
		t.Fatalf("AddContact: err=%v uid=%d contact=%d", err, gotUID, gotContact)
	}
	if err := s.DeleteContact(7, 8); err != nil || gotUID != 7 || gotContact != 8 {
		t.Fatalf("DeleteContact: err=%v uid=%d contact=%d", err, gotUID, gotContact)
	}
	got, err = s.SearchUsers(7, "bob")
	if err != nil || !reflect.DeepEqual(got, users) || gotUID != 7 {
		t.Fatalf("SearchUsers: got=%v err=%v uid=%d", got, err, gotUID)
	}
}

func TestContactServicePropagatesRepositoryError(t *testing.T) {
	want := errors.New("db down")
	s := NewContactService(&contactRepositoryMock{
		getContactsFn:     func(uint) ([]domain.User, error) { return nil, want },
		getContactsPageFn: func(uint, int, int) ([]domain.User, int64, error) { return nil, 0, want },
		addContactFn:      func(uint, uint) error { return want },
		deleteContactFn:   func(uint, uint) error { return want },
		searchUsersFn:     func(string, uint) ([]domain.User, error) { return nil, want },
	})
	if _, err := s.GetContacts(1); !errors.Is(err, want) {
		t.Fatalf("GetContacts err=%v", err)
	}
	if _, _, err := s.GetContactsPage(1, 10, 0); !errors.Is(err, want) {
		t.Fatalf("GetContactsPage err=%v", err)
	}
	if err := s.AddContact(1, 2); !errors.Is(err, want) {
		t.Fatalf("AddContact err=%v", err)
	}
	if err := s.DeleteContact(1, 2); !errors.Is(err, want) {
		t.Fatalf("DeleteContact err=%v", err)
	}
	if _, err := s.SearchUsers(1, "x"); !errors.Is(err, want) {
		t.Fatalf("SearchUsers err=%v", err)
	}
}
