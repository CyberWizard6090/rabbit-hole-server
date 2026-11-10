package domain

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type ActorType string

const (
	ActorUser        ActorType = "user"
	ActorForm        ActorType = "form"
	ActorAutomation  ActorType = "automation"
	ActorIntegration ActorType = "integration"
	ActorSystem      ActorType = "system"
)

const MaxActorLabelRunes = 200

func ValidActorType(t ActorType) bool {
	switch t {
	case ActorUser, ActorForm, ActorAutomation, ActorIntegration, ActorSystem:
		return true
	default:
		return false
	}
}

type Actor struct {
	Type  ActorType
	ID    uint
	Label string
}

func UserActor(id uint) Actor {
	return Actor{Type: ActorUser, ID: id}
}

func NewActor(actorType ActorType, id uint, label string) Actor {
	return Actor{Type: actorType, ID: id, Label: truncateActorLabel(label)}
}

func FormActor(formID uint, name string) Actor {
	return NewActor(ActorForm, formID, name)
}

func (a Actor) Normalized() Actor {
	if a.Type == "" {
		a.Type = ActorUser
	}
	if a.Type == ActorUser {
		a.Label = ""
	} else {
		a.Label = truncateActorLabel(a.Label)
	}
	return a
}

func (a Actor) IsUser() bool {
	return a.Type == "" || a.Type == ActorUser
}

func (a Actor) UserID() uint {
	if a.IsUser() {
		return a.ID
	}
	return 0
}

func truncateActorLabel(label string) string {
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) <= MaxActorLabelRunes {
		return label
	}
	return string([]rune(label)[:MaxActorLabelRunes])
}

type ActorView struct {
	Type      ActorType `json:"type"`
	ID        *uint     `json:"id"`
	Name      string    `json:"name"`
	Username  string    `json:"username,omitempty"`
	FirstName string    `json:"first_name,omitempty"`
	LastName  string    `json:"last_name,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

func (u CommentUser) DisplayName() string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return name
}

func UserActorView(u CommentUser) ActorView {
	id := u.ID
	return ActorView{
		Type: ActorUser, ID: &id, Name: u.DisplayName(),
		Username: u.Username, FirstName: u.FirstName, LastName: u.LastName, AvatarURL: u.AvatarURL,
	}
}

func NewActorView(actor Actor, user *CommentUser) ActorView {
	actor = actor.Normalized()
	if actor.Type == ActorUser && user != nil {
		return UserActorView(*user)
	}
	view := ActorView{Type: actor.Type, Name: actor.Label}
	if actor.ID != 0 {
		id := actor.ID
		view.ID = &id
	}
	return view
}

type Requester struct {
	UserID *uint
	User   *CommentUser
	Name   string
	Email  string
}

func (r Requester) Empty() bool {
	return r.UserID == nil && r.Name == "" && r.Email == ""
}

func (r Requester) Stored() ([]byte, error) {
	wire := map[string]any{}
	if r.UserID != nil {
		wire["user_id"] = *r.UserID
	}
	if r.Name != "" {
		wire["name"] = r.Name
	}
	if r.Email != "" {
		wire["email"] = r.Email
	}
	return json.Marshal(wire)
}

func ParseStoredRequester(raw []byte) (*Requester, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	wire := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	out := &Requester{}
	if v, ok := wire["user_id"]; ok {
		var id uint
		if err := json.Unmarshal(v, &id); err != nil {
			return nil, err
		}
		if id != 0 {
			out.UserID = &id
		}
	}
	if v, ok := wire["name"]; ok {
		_ = json.Unmarshal(v, &out.Name)
	}
	if v, ok := wire["email"]; ok {
		_ = json.Unmarshal(v, &out.Email)
	}
	if out.Empty() {
		return nil, nil
	}
	return out, nil
}

func (r Requester) MarshalJSON() ([]byte, error) {
	if r.User != nil {
		return json.Marshal(map[string]any{"name": r.User.DisplayName(), "user": r.User})
	}
	wire := map[string]any{"name": r.Name}
	if r.Email != "" {
		wire["email"] = r.Email
	}
	return json.Marshal(wire)
}
