package collab

import (
	"encoding/json"
	"strconv"
	"time"

	"rabbit-hole-server/internal/domain"
)

// Topic is the ticket scope of a document room.
func Topic(documentID uint) string { return "document:" + strconv.FormatUint(uint64(documentID), 10) }

// Frames are JSON text messages. Yjs updates travel as base64 strings, like in the REST API.
//
// Client to server:
//
//	{"type":"update","update":"<base64>","touched":["blockId"],"ref":"1"}  one collaborative edit
//	{"type":"awareness","state":{...}}                                      the client's cursor/selection; null clears it
//	{"type":"sync","since":42}                                              resend the updates after number 42
//
// Server to client:
//
//	{"type":"ready","conn":7,"user":{...},"can_write":true,"peers":[...]}   first frame
//	{"type":"update","seq":43,"user_id":3,"update":"<base64>"}              an edit of another client or of the REST API
//	{"type":"synced","last_seq":43}                                         end of the catch-up after connecting or `sync`
//	{"type":"resync","reason":"snapshot"}                                   history was compacted: reload GET /state
//	{"type":"ack","ref":"1","seq":43}                                       the update was stored
//	{"type":"peer_joined"|"awareness"|"peer_left", ...}                     presence
//	{"type":"error","code":"RATE_LIMITED","ref":"1"}                        a request was refused; the connection stays open
const (
	TypeUpdate    = "update"
	TypeAwareness = "awareness"
	TypeSync      = "sync"

	TypeReady      = "ready"
	TypeSynced     = "synced"
	TypeResync     = "resync"
	TypeAck        = "ack"
	TypePeerJoined = "peer_joined"
	TypePeerLeft   = "peer_left"
	TypeError      = "error"
)

// Error codes sent in `error` frames.
const (
	CodeForbidden       = "FORBIDDEN"
	CodeRateLimited     = "RATE_LIMITED"
	CodeInvalidMessage  = "INVALID_MESSAGE"
	CodeInvalidUpdate   = "INVALID_UPDATE"
	CodeUpdateTooLarge  = "UPDATE_TOO_LARGE"
	CodeAwarenessTooBig = "AWARENESS_TOO_LARGE"
	CodeInternal        = "INTERNAL"
)

const (
	// MaxAwarenessBytes bounds the presence state of one connection (cursor, selection, color).
	MaxAwarenessBytes = 4 * 1024
	// MaxFrameBytes bounds one client frame: a 512 KiB update in base64 plus the envelope.
	MaxFrameBytes = 1 << 20

	ReasonSlowClient    = "client too slow"
	ReasonAccessRevoked = "access revoked"
	// ReasonAccessChanged closes a connection so that the client reconnects and its rights are checked again.
	ReasonAccessChanged  = "access changed"
	ReasonShuttingDown   = "server shutting down"
	ReasonDocumentClosed = "document deleted"
)

// ClientMessage is any frame a client sends.
type ClientMessage struct {
	Type    string          `json:"type"`
	Ref     string          `json:"ref,omitempty"`
	Update  []byte          `json:"update,omitempty"`
	Touched []string        `json:"touched,omitempty"`
	State   json.RawMessage `json:"state,omitempty"`
	Since   int64           `json:"since,omitempty"`
}

// Peer describes one connection of a room as other clients see it. The user always comes from the authenticated
// ticket; a client can never choose it.
type PeerInfo struct {
	Conn     uint64             `json:"conn"`
	User     domain.CommentUser `json:"user"`
	CanWrite bool               `json:"can_write"`
	State    json.RawMessage    `json:"state"`
}

type readyFrame struct {
	Type     string             `json:"type"`
	Conn     uint64             `json:"conn"`
	User     domain.CommentUser `json:"user"`
	CanWrite bool               `json:"can_write"`
	Peers    []PeerInfo         `json:"peers"`
}

type updateFrame struct {
	Type   string `json:"type"`
	Seq    int64  `json:"seq"`
	UserID uint   `json:"user_id"`
	Update []byte `json:"update"`
}

type awarenessFrame struct {
	Type  string             `json:"type"`
	Conn  uint64             `json:"conn"`
	User  domain.CommentUser `json:"user"`
	State json.RawMessage    `json:"state"`
}

type peerJoinedFrame struct {
	Type     string             `json:"type"`
	Conn     uint64             `json:"conn"`
	User     domain.CommentUser `json:"user"`
	CanWrite bool               `json:"can_write"`
}

type peerLeftFrame struct {
	Type string `json:"type"`
	Conn uint64 `json:"conn"`
}

type syncedFrame struct {
	Type    string `json:"type"`
	LastSeq int64  `json:"last_seq"`
}

type resyncFrame struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ackFrame struct {
	Type string `json:"type"`
	Ref  string `json:"ref,omitempty"`
	Seq  int64  `json:"seq"`
}

type errorFrame struct {
	Type string `json:"type"`
	Code string `json:"code"`
	Ref  string `json:"ref,omitempty"`
}

// UpdateEvent is a stored update that must reach the open connections of its document.
type UpdateEvent struct {
	Seq    int64
	UserID uint
	Update []byte
	// Origin is the connection that sent the update; it gets an ack instead of the update itself. Zero for REST.
	Origin uint64
	At     time.Time
}

// PresenceUser is one person present in a document; several tabs count as one person.
type PresenceUser struct {
	User        domain.CommentUser `json:"user"`
	Connections int                `json:"connections"`
	CanWrite    bool               `json:"can_write"`
}
