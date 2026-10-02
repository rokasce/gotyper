// Package protocol is the wire format between the Neovim front end and the
// engine: newline-delimited JSON (NDJSON) over the engine's stdin and stdout.
//
// The front end writes one JSON request per line; the engine answers each one
// with exactly one JSON response line carrying the same id. PROTOCOL.md in the
// engine directory is the human-readable schema; this file is its Go form.
package protocol

import "github.com/rokasce/gotyper/engine/judge"

// Version is the protocol version this engine speaks. The front end sends the
// version it speaks in its hello; if they differ the engine refuses to go on,
// because a front end and an engine that disagree about field meanings would
// paint nonsense rather than fail loudly. Bump it on any incompatible change.
const Version = 1

// EngineVersion identifies this build of the engine, for display and bug
// reports. Unlike Version it carries no compatibility promise.
const EngineVersion = "0.1.0"

// The ops a request can name.
const (
	OpHello   = "hello"
	OpStart   = "start"
	OpUpdate  = "update"
	OpRestart = "restart"
)

// Error codes, in Error.Code. The front end can branch on the code; the
// message is for people.
const (
	CodeBadJSON           = "bad_json"
	CodeUnknownOp         = "unknown_op"
	CodeVersionMismatch   = "version_mismatch"
	CodeHandshakeRequired = "handshake_required"
	CodeNoStep            = "no_step"
)

// Request is one line from the front end. Which fields matter depends on Op;
// the rest are left at their zero values.
type Request struct {
	// ID is chosen by the front end and echoed on the response so it can
	// match answers to questions. It is a pointer so that a missing id can
	// be told apart from an id of 0.
	ID *int64 `json:"id"`
	Op string `json:"op"`

	// Protocol is the version the front end speaks (hello only).
	Protocol int `json:"protocol,omitempty"`

	// Lines, Keys and Cursor describe the buffer (update only): every line
	// of the buffer, the keystroke count since the attempt began, and the
	// 0-based {row, byte column} of the cursor.
	Lines  []string `json:"lines,omitempty"`
	Keys   int      `json:"keys,omitempty"`
	Cursor [2]int   `json:"cursor"`
}

// Response is one line from the engine. Exactly one of the payload fields
// (Hello, Start, Render) or Error is set, except that a failed hello carries
// both Hello and Error so the front end can show which engine it found.
type Response struct {
	ID     *int64        `json:"id"`
	Op     string        `json:"op,omitempty"`
	Hello  *Hello        `json:"hello,omitempty"`
	Start  *Start        `json:"start,omitempty"`
	Render *judge.Render `json:"render,omitempty"`
	Error  *Error        `json:"error,omitempty"`
}

// Hello is the engine's half of the handshake.
type Hello struct {
	Protocol int    `json:"protocol"`
	Engine   string `json:"engine"`
}

// Start describes the step the learner is about to type: what to show beside
// the code and how to lay out the buffer. It is sent by start and by restart.
type Start struct {
	Title string   `json:"title"`
	Intro []string `json:"intro"`
	// Indents is the target indentation of each line in display columns,
	// for the front end's auto-indent on Enter.
	Indents []int `json:"indents"`
	// Lines is the number of target lines.
	Lines int `json:"lines"`
	// Width is the display width of the widest target line, tabs expanded.
	Width int `json:"width"`
}

// Error explains why a request failed. The engine keeps running after
// sending one.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
