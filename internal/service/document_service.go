package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/collab"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/editor"
)

// DocumentPublisher delivers stored updates to open connections of the document (the WebSocket rooms).
type DocumentPublisher interface {
	PublishUpdate(documentID uint, event collab.UpdateEvent)
	// CloseDocument disconnects everyone who has the document open (it was deleted).
	CloseDocument(documentID uint, reason string) int
}

type DocumentService struct {
	repo      domain.DocumentRepository
	publisher DocumentPublisher
	schema    *editor.Schema
	checker   *editor.Checker
	// onRefused is told who sent an update that was refused by the checks.
	onRefused func(userID uint)
	// compactAfter is how many updates may follow the snapshot before the server compacts the history.
	compactAfter int64
	// compactions counts the compactions that replaced a history (tests wait for it).
	compactions atomic.Int64
}

func NewDocumentService(repo domain.DocumentRepository) *DocumentService {
	schema := editor.DefaultSchema()
	return &DocumentService{repo: repo, schema: schema, checker: editor.NewChecker(schema, editor.DefaultCachedDocuments), compactAfter: DefaultCompactAfter}
}

// DefaultCompactAfter is how many updates may follow the snapshot before the server compacts the history. It stays
// well under domain.MaxDocumentPendingUpdates, where a client would be told to compact.
const DefaultCompactAfter = 200

// WithCompactAfter changes after how many updates the history is compacted.
func (s *DocumentService) WithCompactAfter(n int64) *DocumentService {
	s.compactAfter = n
	return s
}

// Compactions is how many times the history of any document was replaced by a snapshot.
func (s *DocumentService) Compactions() int64 { return s.compactions.Load() }

// WithPublisher sets where stored updates are announced. Without it updates are only stored.
func (s *DocumentService) WithPublisher(publisher DocumentPublisher) *DocumentService {
	s.publisher = publisher
	return s
}

// WithRefusedUpdateHook sets what happens when a user sends an update the checks refuse. An editor never produces
// one, and each can make the server reload the document, so the caller charges it to the user's budget.
func (s *DocumentService) WithRefusedUpdateHook(hook func(userID uint)) *DocumentService {
	s.onRefused = hook
	return s
}

type DocumentPatch struct {
	Title      *string
	Position   *int64
	NotebookID *uint
}

func (s *DocumentService) Create(ctx context.Context, notebookID, userID uint, title string) (*domain.Document, error) {
	title = strings.TrimSpace(title)
	if err := nameIssue(domain.ErrInvalidDocument, "title", title, domain.MaxDocumentTitleRunes); err != nil {
		return nil, err
	}
	count, err := s.repo.CountInNotebook(ctx, notebookID)
	if err != nil {
		return nil, err
	}
	if count >= domain.MaxDocumentsPerNotebook {
		return nil, domain.ErrDocumentLimitReached
	}
	return s.repo.Create(ctx, domain.DocumentDraft{NotebookID: notebookID, UserID: userID, Title: title}, collab.InitialState())
}

func (s *DocumentService) Get(ctx context.Context, id uint) (*domain.Document, error) {
	return s.repo.Get(ctx, id)
}

func (s *DocumentService) List(ctx context.Context, notebookID uint, offset, limit int) ([]domain.Document, int64, error) {
	return s.repo.List(ctx, notebookID, offset, limit)
}

func (s *DocumentService) Update(ctx context.Context, id uint, patch DocumentPatch) (*domain.Document, error) {
	update := domain.DocumentUpdate{Position: patch.Position, NotebookID: patch.NotebookID}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if err := nameIssue(domain.ErrInvalidDocument, "title", title, domain.MaxDocumentTitleRunes); err != nil {
			return nil, err
		}
		update.Title = &title
	}
	return s.repo.Update(ctx, id, update)
}

func (s *DocumentService) Delete(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.checker.Forget(id)
	// Open connections learn it now, not at their next edit.
	if s.publisher != nil {
		s.publisher.CloseDocument(id, collab.ReasonDocumentClosed)
	}
	return nil
}

func (s *DocumentService) State(ctx context.Context, id uint) (*domain.DocumentState, error) {
	return s.repo.State(ctx, id)
}

// touchedBlocks drops duplicates and rejects ids the activity table cannot hold.
func touchedBlocks(ids []string) ([]string, error) {
	if len(ids) > domain.MaxTouchedBlocks {
		return nil, domain.InvalidField(domain.ErrInvalidDocumentUpdate, "touched", "TOO_MANY").
			WithParams(map[string]any{"max": domain.MaxTouchedBlocks})
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || utf8.RuneCountInString(id) > domain.MaxBlockIDRunes || !utf8.ValidString(id) || strings.ContainsRune(id, 0) {
			return nil, domain.InvalidField(domain.ErrInvalidDocumentUpdate, "touched", "INVALID_VALUE")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// AppendUpdate checks and stores one Yjs update. The update is applied to the server's copy of the document
// (editor.Checker): bytes that do not decode, or an update after which the document could not be opened, are
// refused and never reach the storage or other clients. touched lists the blocks the update changed; they feed
// the "who edited this block" record, which uses the authenticated user, never an id from the client.
func (s *DocumentService) AppendUpdate(ctx context.Context, id, userID uint, update []byte, touched []string) (int64, error) {
	if len(update) == 0 {
		return 0, domain.InvalidField(domain.ErrInvalidDocumentUpdate, "update", "REQUIRED")
	}
	if len(update) > domain.MaxDocumentUpdateBytes {
		return 0, domain.ErrUpdateTooLarge
	}
	blocks, err := touchedBlocks(touched)
	if err != nil {
		return 0, err
	}
	seq, err := s.checker.Append(ctx, id, update, s.loadForCheck(id), func() (int64, error) {
		seq, err := s.repo.AppendUpdate(ctx, id, userID, update, blocks)
		if err != nil {
			return 0, err
		}
		// The update is committed: announce it to the open connections, the sender (collab.OriginFrom) gets an ack
		// instead. This runs under the lock the checker holds for the document, so announcements leave in the
		// order of the update numbers. Announced after the lock, two writers could swap places, and a client that
		// had seen number 6 but not 5 would ask for "after 6" on its next connect and never get 5.
		if s.publisher != nil {
			s.publisher.PublishUpdate(id, collab.UpdateEvent{
				Seq: seq, UserID: userID, Update: update, Origin: collab.OriginFrom(ctx), At: time.Now(),
			})
		}
		return seq, nil
	})
	if err != nil {
		if errors.Is(err, editor.ErrInvalidDocument) {
			if s.onRefused != nil {
				s.onRefused(userID)
			}
			return 0, domain.InvalidField(domain.ErrInvalidDocumentUpdate, "update", "INVALID_VALUE")
		}
		return 0, err
	}
	s.compactLater(id)
	return seq, nil
}

// compactLater starts a compaction in the background when the history after the snapshot has grown long. It runs
// once at a time per document and never fails the edit that triggered it.
func (s *DocumentService) compactLater(id uint) {
	if s.compactAfter <= 0 || !s.checker.ClaimCompaction(id, s.compactAfter) {
		return
	}
	go func() {
		uptoSeq := int64(0)
		defer func() { s.checker.DoneCompaction(id, uptoSeq) }()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		state, err := s.loadForCheck(id)(ctx)
		if err != nil {
			return
		}
		compacted, err := editor.Compact(s.schema, state, domain.MaxDocumentStateBytes)
		if err != nil {
			slog.Warn("document history was left as it is", "document_id", id, "reason", err)
			return
		}
		// The snapshot covers exactly the updates that were read. Updates stored meanwhile keep their numbers.
		switch err := s.repo.ReplaceSnapshot(ctx, id, state.LastSeq, compacted); {
		case err == nil:
			uptoSeq = state.LastSeq
			s.compactions.Add(1)
		case errors.Is(err, domain.ErrSnapshotConflict), errors.Is(err, domain.ErrDocumentNotFound):
		default:
			slog.Warn("document history compaction failed", "document_id", id, "error", err)
		}
	}()
}

func (s *DocumentService) loadForCheck(id uint) editor.Loader {
	return func(ctx context.Context) (editor.State, error) {
		stored, err := s.repo.State(ctx, id)
		if err != nil {
			return editor.State{}, err
		}
		state := editor.State{Snapshot: stored.Snapshot, LastSeq: stored.LastSeq, SnapshotSeq: stored.SnapshotSeq, Updates: make([][]byte, 0, len(stored.Updates))}
		for _, change := range stored.Updates {
			state.Updates = append(state.Updates, change.Update)
		}
		return state, nil
	}
}

// Since returns the updates after `since`, or says that the history was compacted past it.
func (s *DocumentService) Since(ctx context.Context, id uint, since int64) (collab.SyncResult, error) {
	state, err := s.repo.State(ctx, id)
	if err != nil {
		return collab.SyncResult{}, err
	}
	if since < state.SnapshotSeq {
		return collab.SyncResult{Resync: true, LastSeq: state.LastSeq}, nil
	}
	result := collab.SyncResult{LastSeq: state.LastSeq}
	for _, change := range state.Updates {
		if change.Seq > since {
			result.Updates = append(result.Updates, collab.Change{Seq: change.Seq, UserID: change.UserID, Update: change.Update})
		}
	}
	return result, nil
}

func (s *DocumentService) PublicUser(ctx context.Context, userID uint) (*domain.CommentUser, error) {
	return s.repo.PublicUser(ctx, userID)
}

// ReplaceSnapshot stores a compacted state that already contains the updates up to uptoSeq.
func (s *DocumentService) ReplaceSnapshot(ctx context.Context, id uint, uptoSeq int64, state []byte) error {
	if len(state) == 0 {
		return domain.InvalidField(domain.ErrInvalidDocumentUpdate, "state", "REQUIRED")
	}
	if len(state) > domain.MaxDocumentStateBytes {
		return domain.ErrStateTooLarge
	}
	if uptoSeq < 1 {
		return domain.InvalidField(domain.ErrInvalidDocumentUpdate, "upto_seq", "INVALID_VALUE")
	}
	// A snapshot replaces the whole history, so garbage here would make the document impossible to open.
	if _, err := editor.Inspect(s.schema, state); err != nil {
		return domain.InvalidField(domain.ErrInvalidDocumentUpdate, "state", "INVALID_VALUE")
	}
	if err := s.repo.ReplaceSnapshot(ctx, id, uptoSeq, state); err != nil {
		return err
	}
	// The snapshot may hold more than the updates it replaces: the copy used for checks is rebuilt from it.
	s.checker.Forget(id)
	return nil
}

func (s *DocumentService) BlockActivity(ctx context.Context, id uint, blockID string, limit int) ([]domain.BlockActivity, error) {
	if limit < 1 || limit > domain.MaxActivityBlocks {
		limit = domain.MaxActivityBlocks
	}
	return s.repo.BlockActivity(ctx, id, blockID, limit)
}
