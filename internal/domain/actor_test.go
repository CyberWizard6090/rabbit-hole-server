package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserActorHelpers(t *testing.T) {
	actor := UserActor(7)
	if actor.Type != ActorUser || actor.ID != 7 || actor.Label != "" || !actor.IsUser() || actor.UserID() != 7 {
		t.Fatalf("user actor = %+v", actor)
	}
	if !(Actor{}).IsUser() || (Actor{}).Normalized().Type != ActorUser {
		t.Fatal("the zero actor must behave as a user actor")
	}
	form := FormActor(3, "  Bug report  ")
	if form.Type != ActorForm || form.ID != 3 || form.Label != "Bug report" || form.IsUser() || form.UserID() != 0 {
		t.Fatalf("form actor = %+v", form)
	}
}

func TestActorLabelIsTruncatedAndDroppedForUsers(t *testing.T) {
	long := strings.Repeat("я", MaxActorLabelRunes+50)
	if got := FormActor(1, long).Label; len([]rune(got)) != MaxActorLabelRunes {
		t.Fatalf("label has %d runes, want %d", len([]rune(got)), MaxActorLabelRunes)
	}
	if got := (Actor{Type: ActorUser, ID: 1, Label: "x"}).Normalized().Label; got != "" {
		t.Fatalf("user label = %q, want empty", got)
	}
}

func TestValidActorType(t *testing.T) {
	for _, valid := range []ActorType{ActorUser, ActorForm, ActorAutomation, ActorIntegration, ActorSystem} {
		if !ValidActorType(valid) {
			t.Errorf("%q must be valid", valid)
		}
	}
	if ValidActorType("robot") || ValidActorType("") {
		t.Fatal("unknown actor types must be rejected")
	}
}

func TestActorViewShapes(t *testing.T) {
	user := CommentUser{ID: 4, Username: "ann", FirstName: "Ann", LastName: "Lee"}
	raw, err := json.Marshal(NewActorView(UserActor(4), &user))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	if decoded["type"] != "user" || decoded["id"] != float64(4) || decoded["name"] != "Ann Lee" || decoded["username"] != "ann" {
		t.Fatalf("user view = %s", raw)
	}
	raw, _ = json.Marshal(NewActorView(FormActor(9, "Intake"), nil))
	if string(raw) != `{"type":"form","id":9,"name":"Intake"}` {
		t.Fatalf("form view = %s", raw)
	}
	raw, _ = json.Marshal(NewActorView(Actor{Type: ActorSystem}, nil))
	if string(raw) != `{"type":"system","id":null,"name":""}` {
		t.Fatalf("system view = %s", raw)
	}
}

func TestRequesterJSONNeverCarriesUserEmail(t *testing.T) {
	user := CommentUser{ID: 4, Username: "ann", FirstName: "Ann"}
	id := uint(4)
	raw, _ := json.Marshal(Requester{UserID: &id, User: &user, Email: "ann@example.test"})
	if strings.Contains(string(raw), "example.test") || !strings.Contains(string(raw), `"user"`) {
		t.Fatalf("authenticated requester = %s", raw)
	}
	raw, _ = json.Marshal(Requester{Name: "Guest", Email: "guest@example.test"})
	if string(raw) != `{"email":"guest@example.test","name":"Guest"}` {
		t.Fatalf("anonymous requester = %s", raw)
	}
}

func TestRequesterStorageRoundTrip(t *testing.T) {
	id := uint(5)
	for _, in := range []Requester{{UserID: &id}, {Name: "A", Email: "a@example.test"}, {Email: "only@example.test"}} {
		stored, err := in.Stored()
		if err != nil {
			t.Fatal(err)
		}
		out, err := ParseStoredRequester(stored)
		if err != nil || out == nil {
			t.Fatalf("parse %s: %v", stored, err)
		}
		if out.Name != in.Name || out.Email != in.Email || (in.UserID == nil) != (out.UserID == nil) {
			t.Fatalf("round trip %+v -> %+v", in, out)
		}
	}
	for _, empty := range []string{"", "null", "{}"} {
		if out, err := ParseStoredRequester([]byte(empty)); err != nil || out != nil {
			t.Fatalf("%q must parse to nothing, got %v, %v", empty, out, err)
		}
	}
	if _, err := ParseStoredRequester([]byte("[")); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	if !(Requester{}).Empty() {
		t.Fatal("zero requester must be empty")
	}
}

func TestRevealRequesterOnlyCopiesIntoTheReturnedValue(t *testing.T) {
	task := Task{RequesterInfo: &Requester{Name: "A"}}
	shown := task.RevealRequester()
	if shown.Requester == nil || task.Requester != nil {
		t.Fatal("reveal must not mutate the original task")
	}
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), `"requester"`) {
		t.Fatalf("a task that was not revealed must hide the requester: %s", raw)
	}
}
