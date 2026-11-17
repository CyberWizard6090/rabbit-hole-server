package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/reearth/ygo/crdt"

	"rabbit-hole-server/internal/collab"
)

// Stress tests of the document rooms. They take minutes and measure limits instead of asserting behaviour, so they
// only run with COLLAB_STRESS=1:
//
//	COLLAB_STRESS=1 go test ./tests/integration -run Stress -v -count=1 -timeout 30m
//
// Each scenario prints lines starting with STRESS; a line that starts with STRESS FAIL is a broken guarantee.
func stressOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("COLLAB_STRESS") == "" {
		t.Skip("set COLLAB_STRESS=1 to run the stress tests")
	}
}

func report(t *testing.T, format string, args ...any) {
	t.Helper()
	line := "STRESS " + fmt.Sprintf(format, args...)
	t.Log(line)
	fmt.Println(line)
}

func broken(t *testing.T, format string, args ...any) {
	t.Helper()
	report(t, "FAIL "+format, args...)
	t.Fail()
}

type stressUser struct {
	id    uint
	token string
	ip    string
}

type stressEnv struct {
	*roomEnv
	users []stressUser
	seq   int
}

func newStressEnv(t *testing.T, compactAfter int64) *stressEnv {
	t.Helper()
	e := &stressEnv{roomEnv: newRoomEnv(t, collab.Options{UpdatesPerSecond: 100000})}
	// The per-user budget is a scenario of its own: the others measure the rest.
	e.rt.container.DocumentCollab.WithUpdateLimit(10_000_000, time.Minute)
	e.rt.container.Documents.WithCompactAfter(compactAfter)
	return e
}

func (e *stressEnv) addUsers(t *testing.T, n int) []stressUser {
	t.Helper()
	var added []stressUser
	for i := 0; i < n; i++ {
		e.seq++
		id, token := newUser(t, e.tc, fmt.Sprintf("stress-%d-%d@example.test", time.Now().UnixNano(), e.seq))
		addContact(t, e.tc, e.owner.token, id)
		rec := request(t, e.tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
			map[string]any{"user_id": id, "role": "Member"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add member: %d: %s", rec.Code, rec.Body.String())
		}
		user := stressUser{id: id, token: token, ip: fmt.Sprintf("10.%d.%d.%d", e.seq/65000, (e.seq/250)%250, e.seq%250+1)}
		e.users = append(e.users, user)
		added = append(added, user)
	}
	return added
}

// ticketFrom asks for a ticket as a client with its own address, so the per-address limit of tickets does not
// hide what the scenarios measure.
func (e *stressEnv) ticketFrom(t *testing.T, user stressUser, docID uint) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/documents/%d/events/ticket", docID), nil)
	req.RemoteAddr = user.ip + ":4000"
	req.Header.Set("Authorization", "Bearer "+user.token)
	rec := httptest.NewRecorder()
	e.rt.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return "", rec.Code
	}
	return dataObject(t, rec)["ticket"].(string), rec.Code
}

type sclient struct {
	conn    *websocket.Conn
	user    stressUser
	onFrame func(frame map[string]any, at time.Time)
	done    chan struct{}
	closed  atomic.Value // closeInfo
	mu      sync.Mutex
}

type closeInfo struct {
	status websocket.StatusCode
	reason string
}

func (e *stressEnv) dialAs(t *testing.T, user stressUser, docID uint, since *int64, onFrame func(map[string]any, time.Time)) (*sclient, error) {
	t.Helper()
	ticket, code := e.ticketFrom(t, user, docID)
	if ticket == "" {
		return nil, fmt.Errorf("ticket: %d", code)
	}
	url := "ws" + strings.TrimPrefix(e.rt.server.URL, "http") + fmt.Sprintf("/api/v1/documents/%d/events?ticket=%s", docID, ticket)
	if since != nil {
		url += fmt.Sprintf("&since=%d", *since)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(16 << 20)
	c := &sclient{conn: conn, user: user, onFrame: onFrame, done: make(chan struct{})}
	t.Cleanup(func() { _ = conn.CloseNow() })
	go func() {
		defer close(c.done)
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				c.closed.Store(closeInfo{websocket.CloseStatus(err), closeReason(err)})
				return
			}
			var frame map[string]any
			if json.Unmarshal(raw, &frame) == nil && c.onFrame != nil {
				c.onFrame(frame, time.Now())
			}
		}
	}()
	return c, nil
}

func closeReason(err error) string {
	var ce websocket.CloseError
	if asClose(err, &ce) {
		return ce.Reason
	}
	return err.Error()
}

func asClose(err error, target *websocket.CloseError) bool {
	for err != nil {
		if ce, ok := err.(websocket.CloseError); ok {
			*target = ce
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (c *sclient) send(frame map[string]any) error {
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, raw)
}

func percentiles(values []time.Duration) string {
	if len(values) == 0 {
		return "n=0"
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	at := func(p float64) time.Duration { return values[int(float64(len(values)-1)*p)] }
	return fmt.Sprintf("n=%d p50=%v p95=%v p99=%v max=%v", len(values),
		at(0.5).Round(time.Millisecond), at(0.95).Round(time.Millisecond), at(0.99).Round(time.Millisecond), values[len(values)-1].Round(time.Millisecond))
}

type memSnapshot struct {
	heapMiB    float64
	goroutines int
}

func memNow() memSnapshot {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memSnapshot{float64(m.HeapAlloc) / (1 << 20), runtime.NumGoroutine()}
}

func waitFor(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return condition()
}

// ---------------------------------------------------------------------------------------------------------------

// S1: one document, up to 100 clients (the room limit), everybody types; every update must reach every other client.
func TestStress_FanOutInOneRoom(t *testing.T) {
	stressOnly(t)
	for _, clients := range []int{10, 50, 100} {
		clients := clients
		t.Run(strconv.Itoa(clients), func(t *testing.T) {
			e := newStressEnv(t, 100000)
			perUser := 10
			users := e.addUsers(t, (clients+perUser-1)/perUser)
			const each = 20
			baseline := memNow()

			sent := sync.Map{}     // label -> time
			byUpdate := sync.Map{} // update b64 -> label
			var labels [][]string
			for c := 0; c < clients; c++ {
				row := make([]string, each)
				for j := range row {
					row[j] = fmt.Sprintf("c%d-u%d", c, j)
					byUpdate.Store(upd(row[j]), row[j])
				}
				labels = append(labels, row)
			}

			var mu sync.Mutex
			var delivery, acks []time.Duration
			received := make([]int64, clients)
			lastSeq := make([]int64, clients)
			outOfOrder := int64(0)
			errorsByCode := map[string]int{}
			ready := int64(0)

			docID := e.docID
			cs := make([]*sclient, clients)
			for c := 0; c < clients; c++ {
				c := c
				client, err := e.dialAs(t, users[c%len(users)], docID, nil, func(frame map[string]any, at time.Time) {
					switch frame["type"] {
					case "ready":
						atomic.AddInt64(&ready, 1)
					case "update":
						atomic.AddInt64(&received[c], 1)
						seq := int64(frame["seq"].(float64))
						if seq <= atomic.SwapInt64(&lastSeq[c], seq) {
							atomic.AddInt64(&outOfOrder, 1)
						}
						if label, ok := byUpdate.Load(frame["update"].(string)); ok {
							if when, ok := sent.Load(label); ok {
								mu.Lock()
								delivery = append(delivery, at.Sub(when.(time.Time)))
								mu.Unlock()
							}
						}
					case "ack":
						if when, ok := sent.Load(frame["ref"]); ok {
							mu.Lock()
							acks = append(acks, at.Sub(when.(time.Time)))
							mu.Unlock()
						}
					case "error":
						mu.Lock()
						errorsByCode[fmt.Sprint(frame["code"])]++
						mu.Unlock()
					}
				})
				if err != nil {
					broken(t, "fan-out %d: client %d cannot connect: %v", clients, c, err)
					return
				}
				cs[c] = client
			}
			if !waitFor(30*time.Second, func() bool { return atomic.LoadInt64(&ready) == int64(clients) }) {
				broken(t, "fan-out %d: only %d clients got ready", clients, atomic.LoadInt64(&ready))
				return
			}
			afterConnect := memNow()

			start := time.Now()
			var wg sync.WaitGroup
			for c := 0; c < clients; c++ {
				wg.Add(1)
				go func(c int) {
					defer wg.Done()
					rnd := rand.New(rand.NewSource(int64(c)))
					for j := 0; j < each; j++ {
						time.Sleep(time.Duration(rnd.Intn(300)) * time.Millisecond)
						label := labels[c][j]
						sent.Store(label, time.Now())
						_ = cs[c].send(map[string]any{"type": "update", "update": upd(label), "ref": label})
					}
				}(c)
			}
			wg.Wait()
			want := int64((clients - 1) * each)
			converged := waitFor(60*time.Second, func() bool {
				for c := range received {
					if atomic.LoadInt64(&received[c]) < want {
						return false
					}
				}
				return true
			})
			elapsed := time.Since(start)
			missing := int64(0)
			for c := range received {
				if r := atomic.LoadInt64(&received[c]); r < want {
					missing += want - r
				}
			}
			mem := memNow()
			report(t, "fan-out clients=%d updates=%d frames_delivered=%d elapsed=%v rate=%.0f updates/s converged=%v missing=%d out_of_order=%d errors=%v",
				clients, clients*each, int64(clients)*want, elapsed.Round(time.Millisecond), float64(clients*each)/elapsed.Seconds(), converged, missing, outOfOrder, errorsByCode)
			report(t, "fan-out clients=%d delivery latency: %s", clients, percentiles(delivery))
			report(t, "fan-out clients=%d ack latency: %s", clients, percentiles(acks))
			report(t, "fan-out clients=%d heap %.1f -> %.1f -> %.1f MiB, goroutines %d -> %d -> %d", clients,
				baseline.heapMiB, afterConnect.heapMiB, mem.heapMiB, baseline.goroutines, afterConnect.goroutines, mem.goroutines)
			if missing > 0 {
				broken(t, "fan-out %d lost %d deliveries", clients, missing)
			}
			if outOfOrder > 0 {
				broken(t, "fan-out %d: %d updates arrived out of order", clients, outOfOrder)
			}
			for _, c := range cs {
				_ = c.conn.CloseNow()
			}
			waitFor(10*time.Second, func() bool { return runtime.NumGoroutine() <= baseline.goroutines+20 })
			report(t, "fan-out clients=%d goroutines after closing everybody: %d (baseline %d)", clients, runtime.NumGoroutine(), baseline.goroutines)
		})
	}
}

// S2: many documents at once, two clients each. The server keeps a copy of 64 documents for its checks, so more
// active documents than that show what a cache miss on every edit costs.
func TestStress_ManyDocuments(t *testing.T) {
	stressOnly(t)
	for _, docs := range []int{30, 150} {
		docs := docs
		t.Run(strconv.Itoa(docs), func(t *testing.T) {
			e := newStressEnv(t, 100000)
			users := e.addUsers(t, (2*docs+17)/18)
			notebook := e.newNotebook(t, "stress")
			ids := make([]uint, docs)
			for i := range ids {
				ids[i] = e.newDocument(t, notebook, fmt.Sprintf("d%d", i))
			}
			const each = 25
			var mu sync.Mutex
			var acks []time.Duration
			errorsByCode := map[string]int{}
			delivered := int64(0)
			sent := sync.Map{}
			var writers []*sclient
			ready := int64(0)
			for i, id := range ids {
				on := func(frame map[string]any, at time.Time) {
					switch frame["type"] {
					case "ready":
						atomic.AddInt64(&ready, 1)
					case "update":
						atomic.AddInt64(&delivered, 1)
					case "ack":
						if when, ok := sent.Load(frame["ref"]); ok {
							mu.Lock()
							acks = append(acks, at.Sub(when.(time.Time)))
							mu.Unlock()
						}
					case "error":
						mu.Lock()
						errorsByCode[fmt.Sprint(frame["code"])]++
						mu.Unlock()
					}
				}
				w, err := e.dialAs(t, users[(2*i)/18%len(users)], id, nil, on)
				if err != nil {
					broken(t, "many docs %d: writer %d: %v", docs, i, err)
					return
				}
				if _, err := e.dialAs(t, users[(2*i+1)/18%len(users)], id, nil, on); err != nil {
					broken(t, "many docs %d: reader %d: %v", docs, i, err)
					return
				}
				writers = append(writers, w)
			}
			if !waitFor(60*time.Second, func() bool { return atomic.LoadInt64(&ready) == int64(2*docs) }) {
				broken(t, "many docs %d: %d of %d clients ready", docs, atomic.LoadInt64(&ready), 2*docs)
				return
			}
			start := time.Now()
			var wg sync.WaitGroup
			for i, w := range writers {
				wg.Add(1)
				go func(i int, w *sclient) {
					defer wg.Done()
					rnd := rand.New(rand.NewSource(int64(i)))
					for j := 0; j < each; j++ {
						time.Sleep(time.Duration(100+rnd.Intn(200)) * time.Millisecond)
						label := fmt.Sprintf("d%d-%d", i, j)
						sent.Store(label, time.Now())
						_ = w.send(map[string]any{"type": "update", "update": upd(label), "ref": label})
					}
				}(i, w)
			}
			wg.Wait()
			want := int64(docs * each)
			ok := waitFor(60*time.Second, func() bool { return atomic.LoadInt64(&delivered) >= want })
			elapsed := time.Since(start)
			report(t, "many-docs docs=%d connections=%d updates=%d delivered_to_readers=%d/%d elapsed=%v rate=%.0f updates/s errors=%v",
				docs, 2*docs, docs*each, atomic.LoadInt64(&delivered), want, elapsed.Round(time.Millisecond), float64(docs*each)/elapsed.Seconds(), errorsByCode)
			report(t, "many-docs docs=%d ack latency: %s", docs, percentiles(acks))
			if !ok {
				broken(t, "many docs %d: readers got %d of %d updates", docs, atomic.LoadInt64(&delivered), want)
			}
		})
	}
}

func buildBigState(blocks int) []byte {
	doc := crdt.New()
	meta := doc.GetMap("meta")
	bl := doc.GetMap("blocks")
	doc.Transact(func(txn *crdt.Transaction) {
		meta.Set(txn, "format", 1)
		meta.Set(txn, "rootId", "root")
	}, nil)
	add := func(id, flavour string, parent any, key string, text string) {
		doc.Transact(func(txn *crdt.Transaction) { bl.Set(txn, id, crdt.NewMapPrelim()) }, nil)
		value, _ := bl.Get(id)
		block := value.(*crdt.YMap)
		doc.Transact(func(txn *crdt.Transaction) {
			block.Set(txn, "flavour", flavour)
			block.Set(txn, "place", map[string]any{"p": parent, "k": key})
			block.Set(txn, "props", crdt.NewMapPrelim())
			block.Set(txn, "text", crdt.NewTextPrelim())
		}, nil)
		if text != "" {
			inner, _ := block.Get("text")
			doc.Transact(func(txn *crdt.Transaction) { inner.(*crdt.YText).Insert(txn, 0, text, nil) }, nil)
		}
	}
	add("root", "block:page", nil, "", "")
	add("note", "block:note", "root", "a", "")
	body := strings.Repeat("Съешь ещё этих мягких булок. ", 7)
	for i := 0; i < blocks; i++ {
		add(fmt.Sprintf("b%06d", i), "block:paragraph", "note", fmt.Sprintf("k%06d", i), body)
	}
	return crdt.EncodeStateAsUpdateV1(doc, nil)
}

// S3: big documents: uploading the state, the first edit after it (the server loads the document), the edits after
// that, the history compaction and reading the state back.
func TestStress_BigDocuments(t *testing.T) {
	stressOnly(t)
	for _, blocks := range []int{2000, 8000, 15000} {
		blocks := blocks
		t.Run(strconv.Itoa(blocks), func(t *testing.T) {
			e := newStressEnv(t, 5)
			users := e.addUsers(t, 1)
			notebook := e.newNotebook(t, "big")
			docID := e.newDocument(t, notebook, "big")
			state := buildBigState(blocks)
			encoded := base64.StdEncoding.EncodeToString(state)
			updates := "/api/v1/documents/" + itoa(docID) + "/updates"

			call := func(method, path string, body any) (*httptest.ResponseRecorder, time.Duration) {
				start := time.Now()
				rec := request(t, e.rt.router, method, path, e.member, body)
				return rec, time.Since(start)
			}
			if rec, _ := call(http.MethodPost, updates, map[string]any{"update": upd("seed")}); rec.Code != http.StatusCreated {
				broken(t, "big %d: first update: %d", blocks, rec.Code)
				return
			}
			rec, took := call(http.MethodPost, "/api/v1/documents/"+itoa(docID)+"/snapshot", map[string]any{"upto_seq": 1, "state": encoded})
			report(t, "big blocks=%d state=%d KiB snapshot upload: status=%d took=%v", blocks, len(state)/1024, rec.Code, took.Round(time.Millisecond))
			if rec.Code != http.StatusOK {
				report(t, "big blocks=%d snapshot refused: %s", blocks, strings.TrimSpace(rec.Body.String()))
				return
			}
			var first time.Duration
			var later []time.Duration
			for i := 0; i < 20; i++ {
				rec, took := call(http.MethodPost, updates, map[string]any{"update": upd(fmt.Sprintf("big-%d", i))})
				if rec.Code != http.StatusCreated {
					broken(t, "big %d: update %d: %d: %s", blocks, i, rec.Code, rec.Body.String())
					return
				}
				if i == 0 {
					first = took
				} else {
					later = append(later, took)
				}
			}
			report(t, "big blocks=%d first update after the upload (the server loads the document): %v; the next 19: %s", blocks, first.Round(time.Millisecond), percentiles(later))
			_ = users
			ok := waitFor(60*time.Second, func() bool { return e.rt.container.Documents.Compactions() > 0 })
			rec, took = call(http.MethodGet, "/api/v1/documents/"+itoa(docID)+"/state", nil)
			report(t, "big blocks=%d GET /state: status=%d took=%v body=%d KiB compacted=%v", blocks, rec.Code, took.Round(time.Millisecond), rec.Body.Len()/1024, ok)
			if !ok {
				report(t, "big blocks=%d the history was NOT compacted within 60 s", blocks)
			}
			mem := memNow()
			report(t, "big blocks=%d heap now %.1f MiB", blocks, mem.heapMiB)
		})
	}
}

// S4: clients that behave badly.
func TestStress_HostileClients(t *testing.T) {
	stressOnly(t)

	t.Run("connection limit per user", func(t *testing.T) {
		e := newStressEnv(t, 100000)
		user := e.addUsers(t, 1)[0]
		opened, refused := 0, 0
		for i := 0; i < 30; i++ {
			if _, err := e.dialAs(t, user, e.docID, nil, nil); err != nil {
				refused++
			} else {
				opened++
			}
		}
		report(t, "hostile one user opens 30 sockets: opened=%d refused=%d (limit 20 per user)", opened, refused)
		if opened != 20 {
			broken(t, "hostile: expected 20 sockets for one user, got %d", opened)
		}
	})

	t.Run("garbage flood does not stop the others", func(t *testing.T) {
		e := newStressEnv(t, 100000)
		users := e.addUsers(t, 2)
		var got int64
		reader, _ := e.dialAs(t, users[0], e.docID, nil, func(f map[string]any, _ time.Time) {
			if f["type"] == "update" {
				atomic.AddInt64(&got, 1)
			}
		})
		attacker, _ := e.dialAs(t, users[1], e.docID, nil, nil)
		time.Sleep(300 * time.Millisecond)
		start := time.Now()
		flood := 20000
		var sentFrames int
		for i := 0; i < flood; i++ {
			var err error
			switch i % 4 {
			case 0:
				err = attacker.send(map[string]any{"type": "dance"})
			case 1:
				err = attacker.send(map[string]any{"type": "update", "update": "!!!not base64!!!"})
			case 2:
				err = attacker.send(map[string]any{"type": "awareness", "state": map[string]any{"x": strings.Repeat("y", 6000)}})
			default:
				err = attacker.send(map[string]any{"type": "sync", "since": -1})
			}
			if err != nil {
				break
			}
			sentFrames++
		}
		<-time.After(300 * time.Millisecond)
		report(t, "hostile garbage flood: sent=%d frames in %v; attacker closed: %v", sentFrames, time.Since(start).Round(time.Millisecond), attacker.closed.Load())
		// Somebody else keeps working while the attacker is at it (or was cut off).
		okUser := e.addUsers(t, 1)[0]
		writer, err := e.dialAs(t, okUser, e.docID, nil, nil)
		if err != nil {
			broken(t, "hostile: a well-behaved client cannot connect after the flood: %v", err)
			return
		}
		_ = writer.send(map[string]any{"type": "update", "update": upd("still-working"), "ref": "w"})
		if !waitFor(5*time.Second, func() bool { return atomic.LoadInt64(&got) >= 1 }) {
			broken(t, "hostile: the reader did not get an update after the flood")
		} else {
			report(t, "hostile: other clients keep working during and after the flood")
		}
		_ = reader
	})

	t.Run("slow reader is cut off, the others are not", func(t *testing.T) {
		e := newStressEnv(t, 100000)
		users := e.addUsers(t, 3)
		slow, err := e.dialAs(t, users[0], e.docID, nil, func(map[string]any, time.Time) {
			time.Sleep(10 * time.Minute) // never reads on: its frames pile up
		})
		if err != nil {
			t.Fatal(err)
		}
		var got int64
		healthy, _ := e.dialAs(t, users[1], e.docID, nil, func(f map[string]any, _ time.Time) {
			if f["type"] == "update" {
				atomic.AddInt64(&got, 1)
			}
		})
		var writerAcks int64
		writerErrors := map[string]int{}
		var writerMu sync.Mutex
		writer, _ := e.dialAs(t, users[2], e.docID, nil, func(f map[string]any, _ time.Time) {
			switch f["type"] {
			case "ack":
				atomic.AddInt64(&writerAcks, 1)
			case "error":
				writerMu.Lock()
				writerErrors[fmt.Sprint(f["code"])]++
				writerMu.Unlock()
			}
		})
		time.Sleep(300 * time.Millisecond)
		total := 12000
		big := strings.Repeat("x", 3000)
		start := time.Now()
		defer func() { report(t, "hostile slow reader: scenario took %v", time.Since(start).Round(time.Second)) }()
		for i := 0; i < total; i++ {
			_ = writer.send(map[string]any{"type": "update", "update": upd(fmt.Sprintf("slow-%d-%s", i, big)), "ref": strconv.Itoa(i)})
			if i%50 == 49 {
				time.Sleep(20 * time.Millisecond) // about 2500 per second: a busy room, not a burst that cuts the writer off
			}
		}
		// Wait until everything arrived or nothing arrives any more for ten seconds.
		var timeline []int64
		stalled, lastGot := 0, int64(-1)
		for stalled < 10 && atomic.LoadInt64(&got) < int64(total) {
			time.Sleep(time.Second)
			now := atomic.LoadInt64(&got)
			timeline = append(timeline, now)
			if now == lastGot {
				stalled++
			} else {
				stalled = 0
			}
			lastGot = now
		}
		report(t, "hostile slow reader: healthy reader progress per second: %v", timeline)
		_ = slow
		healthyInRoom := false
		for _, id := range presenceOf(t, e) {
			if id == users[1].id {
				healthyInRoom = true
			}
		}
		stateRec := request(t, e.rt.router, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/state", e.member, nil)
		writerMu.Lock()
		report(t, "hostile slow reader: the writer got %d acks and errors %v, the server stored last_seq=%v, writer closed=%v",
			atomic.LoadInt64(&writerAcks), writerErrors, dataObject(t, stateRec)["last_seq"], writer.closed.Load())
		writerMu.Unlock()
		report(t, "hostile slow reader: healthy reader still in the room: %v, closed=%v, got=%d of %d", healthyInRoom, healthy.closed.Load(), atomic.LoadInt64(&got), total)
		inRoom := false
		rec := request(t, e.rt.router, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/presence", e.member, nil)
		var presence struct {
			Data []struct {
				User struct {
					ID uint `json:"id"`
				} `json:"user"`
				Connections int `json:"connections"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &presence)
		for _, person := range presence.Data {
			if person.User.ID == users[0].id {
				inRoom = true
			}
		}
		report(t, "hostile slow reader: after %d updates of about 3 KB (about %d MiB) the client that never reads is still in the room: %v", total, total*3/1024, inRoom)
		// What counts is what the server stored (a writer that outruns the server can be cut off itself, see above).
		storedSeq := int64(dataObject(t, stateRec)["last_seq"].(float64))
		if atomic.LoadInt64(&got) != storedSeq {
			broken(t, "hostile: the healthy reader got %d of the %d stored updates", atomic.LoadInt64(&got), storedSeq)
		}
		if inRoom {
			broken(t, "hostile: a client that never reads is still in the room")
		}
	})

	t.Run("update budget and the price of refused updates", func(t *testing.T) {
		e := newStressEnv(t, 100000)
		e.rt.container.DocumentCollab.WithUpdateLimit(collab.UpdatesPerMinute, time.Minute)
		users := e.addUsers(t, 2)
		var acks, limited, retryAfter int64
		client, _ := e.dialAs(t, users[0], e.docID, nil, func(f map[string]any, _ time.Time) {
			switch f["type"] {
			case "ack":
				atomic.AddInt64(&acks, 1)
			case "error":
				if f["code"] == "RATE_LIMITED" {
					atomic.AddInt64(&limited, 1)
					if v, ok := f["retry_after"].(float64); ok {
						atomic.StoreInt64(&retryAfter, int64(v))
					}
				}
			}
		})
		for i := 0; i < 650; i++ {
			_ = client.send(map[string]any{"type": "update", "update": upd(fmt.Sprintf("b-%d", i)), "ref": strconv.Itoa(i)})
		}
		waitFor(20*time.Second, func() bool { return atomic.LoadInt64(&acks)+atomic.LoadInt64(&limited) >= 650 })
		report(t, "hostile budget: 650 updates in a row: acked=%d rate_limited=%d retry_after=%ds (limit %d per minute)",
			atomic.LoadInt64(&acks), atomic.LoadInt64(&limited), atomic.LoadInt64(&retryAfter), collab.UpdatesPerMinute)

		// A second user sends garbage updates: each costs 20 of the budget.
		var refused, garbageLimited int64
		attacker, _ := e.dialAs(t, users[1], e.docID, nil, func(f map[string]any, _ time.Time) {
			if f["type"] == "error" {
				if f["code"] == "INVALID_UPDATE" {
					atomic.AddInt64(&refused, 1)
				} else if f["code"] == "RATE_LIMITED" {
					atomic.AddInt64(&garbageLimited, 1)
				}
			}
		})
		for i := 0; i < 100; i++ {
			_ = attacker.send(map[string]any{"type": "update", "update": base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("garbage-%d", i))), "ref": strconv.Itoa(i)})
		}
		waitFor(10*time.Second, func() bool { return atomic.LoadInt64(&refused)+atomic.LoadInt64(&garbageLimited) >= 100 })
		report(t, "hostile garbage updates: 100 sent: refused as invalid=%d then rate limited=%d (each refusal costs %d of 600)",
			atomic.LoadInt64(&refused), atomic.LoadInt64(&garbageLimited), 20)
	})

	t.Run("tickets: per user and per address", func(t *testing.T) {
		e := newStressEnv(t, 100000)
		users := e.addUsers(t, 18)
		ask := func(user stressUser, ip string) int {
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/documents/%d/events/ticket", e.docID), nil)
			req.RemoteAddr = ip + ":5000"
			req.Header.Set("Authorization", "Bearer "+user.token)
			rec := httptest.NewRecorder()
			e.rt.router.ServeHTTP(rec, req)
			return rec.Code
		}
		// One person asks for tickets and does not use them (a script, or tabs that never connect).
		allowed, limited := 0, 0
		for i := 0; i < 40; i++ {
			if ask(users[0], "203.0.113.1") == http.StatusOK {
				allowed++
			} else {
				limited++
			}
		}
		report(t, "hostile 40 unused tickets of one user: allowed=%d refused=%d (cap: 20 outstanding tickets per user, each lives 30 s)", allowed, limited)

		// Eighteen people behind one address (an office, a school, a mobile network), five tickets each within a minute:
		// opening a document in a few tabs, or all of them reconnecting after a restart of the server.
		allowed, limited = 0, 0
		for round := 0; round < 5; round++ {
			for _, user := range users[1:] {
				if ask(user, "198.51.100.7") == http.StatusOK {
					allowed++
				} else {
					limited++
				}
			}
		}
		report(t, "hostile 17 users behind ONE address, 85 ticket requests in a minute: allowed=%d refused=%d (limit 60 per minute per address)", allowed, limited)
	})
}

func presenceOf(t *testing.T, e *stressEnv) []uint {
	t.Helper()
	rec := request(t, e.rt.router, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/presence", e.member, nil)
	var presence struct {
		Data []struct {
			User struct {
				ID uint `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &presence)
	var ids []uint
	for _, person := range presence.Data {
		ids = append(ids, person.User.ID)
	}
	return ids
}

// S5: writers over the socket and over REST hammer one document while the server compacts its history all the time.
func TestStress_WritersWhileCompacting(t *testing.T) {
	stressOnly(t)
	e := newStressEnv(t, 25)
	users := e.addUsers(t, 8)
	const each = 100
	var wg sync.WaitGroup
	var failures int64
	var mu sync.Mutex
	statuses := map[int]int{}
	for w, user := range users {
		wg.Add(1)
		go func(w int, user stressUser) {
			defer wg.Done()
			if w%2 == 0 {
				client, err := e.dialAs(t, user, e.docID, nil, func(f map[string]any, _ time.Time) {
					if f["type"] == "error" {
						atomic.AddInt64(&failures, 1)
					}
				})
				if err != nil {
					atomic.AddInt64(&failures, each)
					return
				}
				for j := 0; j < each; j++ {
					_ = client.send(map[string]any{"type": "update", "update": upd(fmt.Sprintf("w%d-%d", w, j)), "ref": strconv.Itoa(j)})
					time.Sleep(time.Duration(rand.Intn(8)) * time.Millisecond)
				}
				return
			}
			for j := 0; j < each; j++ {
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/documents/%d/updates", e.docID),
					strings.NewReader(fmt.Sprintf(`{"update":%q}`, upd(fmt.Sprintf("w%d-%d", w, j)))))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+user.token)
				req.RemoteAddr = user.ip + ":1"
				rec := httptest.NewRecorder()
				e.rt.router.ServeHTTP(rec, req)
				mu.Lock()
				statuses[rec.Code]++
				mu.Unlock()
				time.Sleep(time.Duration(rand.Intn(8)) * time.Millisecond)
			}
		}(w, user)
	}
	wg.Wait()
	total := int64(len(users) * each)
	waitFor(30*time.Second, func() bool {
		rec := request(t, e.rt.router, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/state", e.member, nil)
		if rec.Code != http.StatusOK {
			return false
		}
		return int64(dataObject(t, rec)["last_seq"].(float64)) >= total
	})
	time.Sleep(500 * time.Millisecond)
	rec := request(t, e.rt.router, http.MethodGet, "/api/v1/documents/"+itoa(e.docID)+"/state", e.member, nil)
	state := dataObject(t, rec)
	doc := crdt.New()
	snapshot, _ := base64.StdEncoding.DecodeString(state["snapshot"].(string))
	_ = crdt.ApplyUpdateV1(doc, snapshot, nil)
	for _, change := range state["updates"].([]any) {
		raw, _ := base64.StdEncoding.DecodeString(change.(map[string]any)["update"].(string))
		_ = crdt.ApplyUpdateV1(doc, raw, nil)
	}
	scratch := doc.GetText("test-scratch").ToString()
	missing := 0
	for w := range users {
		for j := 0; j < each; j++ {
			if !strings.Contains(scratch, fmt.Sprintf("w%d-%d", w, j)) {
				missing++
			}
		}
	}
	report(t, "hammer writers=%d updates=%d last_seq=%.0f snapshot_seq=%.0f updates_left=%d compactions=%d rest_statuses=%v socket_errors=%d lost_updates=%d",
		len(users), total, state["last_seq"], state["snapshot_seq"], len(state["updates"].([]any)), e.rt.container.Documents.Compactions(), statuses, atomic.LoadInt64(&failures), missing)
	if missing > 0 {
		broken(t, "hammer: %d updates are missing from the document after compaction", missing)
	}
	if e.rt.container.Documents.Compactions() == 0 {
		broken(t, "hammer: the history was never compacted")
	}
}

// S6: the ceiling of one document: writers send as fast as the server acknowledges.
func TestStress_OneDocumentCeiling(t *testing.T) {
	stressOnly(t)
	for _, writers := range []int{4, 16, 40} {
		writers := writers
		t.Run(strconv.Itoa(writers), func(t *testing.T) {
			e := newStressEnv(t, 200)
			users := e.addUsers(t, (writers+9)/10)
			var acks int64
			var mu sync.Mutex
			var latencies []time.Duration
			sentAt := sync.Map{}
			stop := make(chan struct{})
			var wg sync.WaitGroup
			var errorsCount int64
			for w := 0; w < writers; w++ {
				w := w
				ackCh := make(chan struct{}, 1)
				client, err := e.dialAs(t, users[w/10], e.docID, nil, func(f map[string]any, at time.Time) {
					switch f["type"] {
					case "ack":
						if when, ok := sentAt.Load(f["ref"]); ok {
							mu.Lock()
							latencies = append(latencies, at.Sub(when.(time.Time)))
							mu.Unlock()
						}
						atomic.AddInt64(&acks, 1)
						select {
						case ackCh <- struct{}{}:
						default:
						}
					case "error":
						atomic.AddInt64(&errorsCount, 1)
						select {
						case ackCh <- struct{}{}:
						default:
						}
					}
				})
				if err != nil {
					broken(t, "ceiling: writer %d: %v", w, err)
					return
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; ; j++ {
						select {
						case <-stop:
							return
						default:
						}
						ref := fmt.Sprintf("%d-%d", w, j)
						sentAt.Store(ref, time.Now())
						if client.send(map[string]any{"type": "update", "update": upd(ref), "ref": ref}) != nil {
							return
						}
						select {
						case <-ackCh:
						case <-time.After(10 * time.Second):
							return
						case <-stop:
							return
						}
					}
				}()
			}
			time.Sleep(500 * time.Millisecond)
			start := time.Now()
			startAcks := atomic.LoadInt64(&acks)
			time.Sleep(10 * time.Second)
			done := atomic.LoadInt64(&acks) - startAcks
			elapsed := time.Since(start)
			close(stop)
			wg.Wait()
			report(t, "ceiling writers=%d: %.0f updates/s accepted by one document over %v, errors=%d", writers, float64(done)/elapsed.Seconds(), elapsed.Round(time.Millisecond), atomic.LoadInt64(&errorsCount))
			report(t, "ceiling writers=%d ack latency: %s", writers, percentiles(latencies))
		})
	}
}
