package collab

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"rabbit-hole-server/internal/domain"
)

var (
	ErrRoomFull           = errors.New("this document has too many open connections")
	ErrTooManyConnections = errors.New("too many open connections for this user")
	ErrManagerClosed      = errors.New("collaboration is shutting down")
)

// Limits bound what one process holds in memory.
type Limits struct {
	// PeersPerRoom is how many connections one document accepts.
	PeersPerRoom int
	// PeersPerUser is how many connections one user may hold in all rooms together.
	PeersPerUser int
	// SendBuffer is how many frames wait for a slow client before it is disconnected.
	SendBuffer int
	// AwarenessEvery is the shortest interval between two presence broadcasts of one connection; newer
	// states replace older ones that have not been sent yet.
	AwarenessEvery time.Duration
}

func (l Limits) withDefaults() Limits {
	if l.PeersPerRoom <= 0 {
		l.PeersPerRoom = 100
	}
	if l.PeersPerUser <= 0 {
		l.PeersPerUser = 20
	}
	if l.SendBuffer <= 0 {
		l.SendBuffer = 256
	}
	if l.AwarenessEvery <= 0 {
		l.AwarenessEvery = 50 * time.Millisecond
	}
	return l
}

// Peer is one open connection to a document room.
type Peer struct {
	ID     uint64
	User   domain.CommentUser
	DocID  uint
	room   *room
	send   chan []byte
	done   chan struct{}
	once   sync.Once
	reason atomic.Value // string

	canWrite atomic.Bool

	mu        sync.Mutex
	awareness json.RawMessage
	timer     *time.Timer
}

// Frames returns the frames to write to the client, in order.
func (p *Peer) Frames() <-chan []byte { return p.send }

// Done is closed when the server wants the connection closed; Reason says why.
func (p *Peer) Done() <-chan struct{} { return p.done }

func (p *Peer) Reason() string {
	if reason, ok := p.reason.Load().(string); ok {
		return reason
	}
	return ""
}

func (p *Peer) CanWrite() bool { return p.canWrite.Load() }

func (p *Peer) SetCanWrite(value bool) { p.canWrite.Store(value) }

func (p *Peer) kick(reason string) {
	p.once.Do(func() {
		p.reason.Store(reason)
		close(p.done)
	})
}

// Enqueue queues a frame for this client. A client whose queue is full is too slow and is disconnected: it
// reconnects with the number of the last update it has and catches up from the database.
func (p *Peer) Enqueue(frame []byte) {
	select {
	case <-p.done:
	case p.send <- frame:
	default:
		p.kick(ReasonSlowClient)
	}
}

func (p *Peer) info() PeerInfo {
	p.mu.Lock()
	state := p.awareness
	p.mu.Unlock()

	return PeerInfo{Conn: p.ID, User: p.User, CanWrite: p.CanWrite(), State: state}
}

type room struct {
	id    uint
	peers map[*Peer]struct{}
}

// Manager keeps the open connections of every document of this process. It is in memory: with several server
// processes, route all connections of one document to one process (for example by hashing the document id).
type Manager struct {
	limits Limits

	mu      sync.Mutex
	rooms   map[uint]*room
	perUser map[uint]map[*Peer]struct{}
	closed  bool
	nextID  atomic.Uint64
	now     func() time.Time
}

func NewManager(limits Limits) *Manager {
	return &Manager{
		limits:  limits.withDefaults(),
		rooms:   make(map[uint]*room),
		perUser: make(map[uint]map[*Peer]struct{}),
		now:     time.Now,
	}
}

func encode(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		// Frames are built from plain structs; a failure here is a programming error.
		panic("collab: encode frame: " + err.Error())
	}

	return raw
}

// Join adds a connection to the room of a document. Its first frame is `ready` with everyone already present;
// frames of later events follow it in order.
func (m *Manager) Join(documentID uint, user domain.CommentUser, canWrite bool) (*Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrManagerClosed
	}

	if len(m.perUser[user.ID]) >= m.limits.PeersPerUser {
		return nil, ErrTooManyConnections
	}

	r := m.rooms[documentID]

	if r == nil {
		r = &room{id: documentID, peers: make(map[*Peer]struct{})}
		m.rooms[documentID] = r
	}

	if len(r.peers) >= m.limits.PeersPerRoom {
		return nil, ErrRoomFull
	}

	peer := &Peer{
		ID:    m.nextID.Add(1),
		User:  user,
		DocID: documentID,
		room:  r,
		send:  make(chan []byte, m.limits.SendBuffer),
		done:  make(chan struct{}),
	}

	peer.SetCanWrite(canWrite)

	others := make([]PeerInfo, 0, len(r.peers))

	for other := range r.peers {
		others = append(others, other.info())
	}

	sort.Slice(others, func(i, j int) bool { return others[i].Conn < others[j].Conn })

	peer.send <- encode(readyFrame{Type: TypeReady, Conn: peer.ID, User: user, CanWrite: canWrite, Peers: others})

	joined := encode(peerJoinedFrame{Type: TypePeerJoined, Conn: peer.ID, User: user, CanWrite: canWrite})

	for other := range r.peers {
		other.Enqueue(joined)
	}

	r.peers[peer] = struct{}{}
	if m.perUser[user.ID] == nil {
		m.perUser[user.ID] = make(map[*Peer]struct{})
	}

	m.perUser[user.ID][peer] = struct{}{}

	return peer, nil
}

// Leave removes a connection and tells the others. It is safe to call more than once.
func (m *Manager) Leave(peer *Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := peer.room

	if _, present := r.peers[peer]; !present {
		return
	}

	delete(r.peers, peer)

	peer.mu.Lock()

	if peer.timer != nil {
		peer.timer.Stop()
	}

	peer.mu.Unlock()

	delete(m.perUser[peer.User.ID], peer)

	if len(m.perUser[peer.User.ID]) == 0 {
		delete(m.perUser, peer.User.ID)
	}

	if len(r.peers) == 0 {
		delete(m.rooms, r.id)
		return
	}

	left := encode(peerLeftFrame{Type: TypePeerLeft, Conn: peer.ID})

	for other := range r.peers {
		other.Enqueue(left)
	}
}

func (m *Manager) broadcast(documentID uint, frame []byte, except *Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.rooms[documentID]

	if r == nil {
		return
	}

	for peer := range r.peers {
		if peer != except {
			peer.Enqueue(frame)
		}
	}
}

// PublishUpdate delivers a stored update to the open connections of its document. The connection that sent it
// (Origin) is skipped: it gets an `ack` from its own handler.
func (m *Manager) PublishUpdate(documentID uint, event UpdateEvent) {
	frame := encode(updateFrame{Type: TypeUpdate, Seq: event.Seq, UserID: event.UserID, Update: event.Update})

	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.rooms[documentID]

	if r == nil {
		return
	}

	for peer := range r.peers {
		if peer.ID != event.Origin {
			peer.Enqueue(frame)
		}
	}
}

// SetAwareness stores the presence state of a connection and broadcasts it, at most once per `AwarenessEvery`;
// states that arrive faster replace the one waiting to be sent, so the last state always goes out.
func (m *Manager) SetAwareness(peer *Peer, state json.RawMessage) {
	peer.mu.Lock()

	if string(state) == "null" {
		state = nil
	}

	peer.awareness = state

	if peer.timer != nil {
		peer.mu.Unlock()
		return
	}

	peer.timer = time.AfterFunc(m.limits.AwarenessEvery, func() {
		peer.mu.Lock()
		latest := peer.awareness
		peer.timer = nil
		peer.mu.Unlock()

		select {
		case <-peer.done:
			return
		default:
		}

		m.broadcast(peer.DocID, encode(awarenessFrame{Type: TypeAwareness, Conn: peer.ID, User: peer.User, State: nullIfEmpty(latest)}), peer)
	})
	peer.mu.Unlock()
}

func nullIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}

	return raw
}

// Presence lists the people in a document, one entry per person even with several tabs.
func (m *Manager) Presence(documentID uint) []PresenceUser {
	m.mu.Lock()
	defer m.mu.Unlock()

	byUser := map[uint]*PresenceUser{}

	if r := m.rooms[documentID]; r != nil {
		for peer := range r.peers {
			entry := byUser[peer.User.ID]

			if entry == nil {
				entry = &PresenceUser{User: peer.User}
				byUser[peer.User.ID] = entry
			}

			entry.Connections++
			entry.CanWrite = entry.CanWrite || peer.CanWrite()
		}
	}

	out := make([]PresenceUser, 0, len(byUser))

	for _, entry := range byUser {
		out = append(out, *entry)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].User.ID < out[j].User.ID })

	return out
}

// Kick disconnects every connection of a user in a document (access was revoked).
func (m *Manager) Kick(documentID, userID uint, reason string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0

	if r := m.rooms[documentID]; r != nil {
		for peer := range r.peers {
			if peer.User.ID == userID {
				peer.kick(reason)

				count++
			}
		}
	}

	return count
}

// CloseDocument disconnects everyone in a document (it was deleted). Clients that get ReasonDocumentClosed do not
// reconnect. It returns how many connections were closed.
func (m *Manager) CloseDocument(documentID uint, reason string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	r := m.rooms[documentID]

	if r == nil {
		return 0
	}

	for peer := range r.peers {
		peer.kick(reason)
	}

	return len(r.peers)
}

// DisconnectUser disconnects every connection of a user in every document (sign-out everywhere, password change).
// The client reconnects with a new ticket, which is refused when the session is gone. It implements
// service.ConnectionRevoker.
func (m *Manager) DisconnectUser(userID uint) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0

	for peer := range m.perUser[userID] {
		peer.kick(ReasonAccessChanged)

		count++
	}

	return count
}

// DisconnectUserFeeds is called when a user's access to workspaces changed (removed, role changed, workspace deleted):
// their document connections are closed and reconnect, which checks their rights again. It implements
// service.ConnectionRevoker.
func (m *Manager) DisconnectUserFeeds(userID uint) int {
	return m.DisconnectUser(userID)
}

// Connections is the number of open connections, for metrics.
func (m *Manager) Connections() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := 0

	for _, r := range m.rooms {
		total += len(r.peers)
	}

	return total
}

// Rooms is the number of documents with at least one open connection.
func (m *Manager) Rooms() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.rooms)
}

// Close disconnects everyone and refuses new connections.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	for _, r := range m.rooms {
		for peer := range r.peers {
			peer.kick(ReasonShuttingDown)
		}
	}
}
