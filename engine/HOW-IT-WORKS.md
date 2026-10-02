# How the engine works

The engine is a small Go program that judges typing. The Neovim plugin sends
it the buffer on every change. The engine answers with what to paint: red
spans, ghost text and stats. This page follows one keystroke through the code
and shows how to run the engine by hand.

## Which file does what

| File | Role |
|---|---|
| `cmd/gotyper-engine/main.go` | Entry point. Parses flags, then runs the protocol server on stdin/stdout. |
| `cmd/gotyper-engine/selftest.go` | `--selftest`: plays the built-in step through the real protocol code and checks the results. |
| `protocol/protocol.go` | The wire types (`Request`, `Response`, ...), the protocol version and the error codes. |
| `protocol/server.go` | The NDJSON read loop (`Serve`) and the dispatcher (`Handle`). Keeps the handshake flag and the current session. |
| `judge/judge.go` | The result types the front end paints: `Span`, `Ghost`, `Stats`, `Render`. |
| `judge/session.go` | The diff and the scoring: `Session.Update`, `Restart`, `Indents`. |
| `judge/text.go` | Tab helpers: indentation width and tab expansion. |
| `lesson/lesson.go` | The `Step` type and `Fixture()`, the one built-in step. |
| `lesson/fixture/handler.go.txt` | The code the learner types in that step. It is embedded into the binary with `//go:embed`. |
| `PROTOCOL.md` | The wire schema. |

Packages only depend downwards: `main` → `protocol` → `judge` → `lesson`.

## From a keystroke to a response

1. **The learner types `x` in Neovim.** The plugin sees the buffer change and
   writes one line to the engine's stdin:
   `{"id":3,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}`
2. **`Server.Serve` reads the line** (`protocol/server.go`). It reads bytes up
   to the next `\n` with `bufio.Reader.ReadBytes`, so there is no length limit
   and each line is one request.
3. **`handleLine` decodes the JSON** into a `Request`. If decoding fails, it
   returns a `bad_json` error response and the loop moves on to the next
   line. A bad message never stops the engine.
4. **`Handle` dispatches on `op`.** It refuses everything except `hello` until
   the handshake succeeds. For `update` it calls
   `session.Update(lines, keys, cursor)`.
5. **`Session.Update` diffs the buffer** (`judge/session.go`). For each typed
   row:
   - Strip the leading whitespace from the typed row. The target row was
     stripped once when the session was created. Indentation is never an
     error.
   - Walk both rows together, one rune at a time: the n-th typed rune against
     the n-th target rune. Matches add to `correct`. A mismatch adds an error
     span, or extends the previous span when it is adjacent. Spans are in
     byte columns, because Neovim highlights by byte.
   - Whatever target text is left past the end of the typed row becomes that
     row's **ghost**.

   Target rows below the last buffer row become **ghost lines**, with tabs
   expanded because Neovim virtual text doesn't expand them.
6. **The at-cursor charging rule decides whether a mismatch costs accuracy.**
   A mismatch is charged only when it is new (it wasn't on screen at the
   previous update) and it ends exactly at the cursor, meaning the learner
   just typed it. Here the `x` at byte 4 ends at column 5, which is the
   cursor, so it is charged: `stats.errors` becomes 1. Why the cursor check
   matters: when the learner retypes a word in the middle of a line, the rest
   of the line shifts through wrong positions on every key. Without the check,
   every shifted character would be charged again. That once turned one typo
   into 14 errors. `TestShiftedTextIsNotChargedAgain` pins the rule.
7. **The stats are computed.** The timer starts at the first typed character
   and stops when the attempt is done. WPM is correct characters / 5 /
   minutes. Accuracy is correct / (correct + charged errors). The function
   returns a `Render`.
8. **`Serve` encodes the response** with `json.Encoder`, which writes one line
   ending in `\n`:
   `{"id":3,"op":"update","render":{"error_spans":[{"row":0,"col":4,"end_col":5}],...}}`.
   The plugin matches it to its request by `id` and paints it.

`restart` follows the same path. `Handle` calls `Session.Restart`, which clears
the error count, the mistake history and the timer but keeps the step. The
engine then replies with the step layout and the render of an empty buffer.

## Run it and poke it by hand

From the `engine/` directory:

```sh
go test ./...                              # all tests
go run ./cmd/gotyper-engine --selftest     # play the built-in step, print a summary
go build -o gotyper-engine ./cmd/gotyper-engine
```

Pipe JSON lines in and read the answers:

```sh
printf '%s\n' \
  '{"id":1,"op":"hello","protocol":1}' \
  '{"id":2,"op":"start"}' \
  '{"id":3,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}' \
  '{"id":4,"op":"update","lines":["package main"],"keys":13,"cursor":[0,12]}' \
  '{"id":5,"op":"restart"}' \
  | go run ./cmd/gotyper-engine
```

You can also run it interactively: start `./gotyper-engine`, paste one request
per line, and press Ctrl-D to end. Things worth trying:

- Send `start` before `hello` to get `handshake_required`.
- Send `"protocol":2` in the `hello` to get `version_mismatch`.
- Type a line of garbage to get `bad_json`. The engine keeps answering.
- Move `cursor` away from the typo in an update. The span is still red, but
  `stats.errors` doesn't go up.
- Indent a line with spaces instead of tabs. Nothing turns red.

Pipe the output through `jq` to make it readable, for example
`| go run ./cmd/gotyper-engine | jq -c '.render.stats'`.
