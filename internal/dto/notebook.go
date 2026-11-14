package dto

import "rabbit-hole-server/internal/domain"

type CreateNotebookRequest struct {
	Name        string  `json:"name" binding:"required,min=1,max=1020"`
	Description *string `json:"description" binding:"omitempty,max=8000"`
}

type UpdateNotebookRequest struct {
	Name        *string                 `json:"name" binding:"omitempty,min=1,max=1020"`
	Description domain.Nullable[string] `json:"description"`
	Position    *int64                  `json:"position" binding:"omitempty,gte=0"`
}

type CreateDocumentRequest struct {
	Title string `json:"title" binding:"required,min=1,max=1020"`
}

type UpdateDocumentRequest struct {
	Title      *string `json:"title" binding:"omitempty,min=1,max=1020"`
	Position   *int64  `json:"position" binding:"omitempty,gte=0"`
	NotebookID *uint   `json:"notebook_id" binding:"omitempty,gt=0"`
}

// DocumentUpdateRequest carries one Yjs update (base64 in JSON) and the ids of the blocks it changed.
type DocumentUpdateRequest struct {
	Update  []byte   `json:"update" binding:"required"`
	Touched []string `json:"touched" binding:"max=200"`
}

// DocumentSnapshotRequest replaces the compacted state: State already contains every update up to UptoSeq.
type DocumentSnapshotRequest struct {
	UptoSeq int64  `json:"upto_seq" binding:"required,gt=0"`
	State   []byte `json:"state" binding:"required"`
}

type DocumentUpdateResult struct {
	Seq int64 `json:"seq"`
}
