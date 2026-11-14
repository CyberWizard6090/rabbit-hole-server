// Package collab holds what the server needs to store collaborative documents without understanding Yjs.
package collab

import _ "embed"

// FormatVersion is the version of the inkbrick-yjs document format the server stores.
const FormatVersion = 1

//go:embed initial_state.bin
var initialState []byte

// InitialState returns the Yjs state of a new empty document: a page with one note and one empty paragraph.
// The server creates it, never the first client that opens the document, otherwise two clients would create
// the structure twice. The file is produced by the Inkbrick collab package (see its initialState test).
func InitialState() []byte {
	return append([]byte(nil), initialState...)
}
