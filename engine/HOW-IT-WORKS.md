# How the engine works

The engine is a small Go program that judges typing. At start-up it loads the
lessons from disk. The Neovim plugin picks a step and then sends the buffer
on every change. The engine answers with what to paint: red
spans, ghost text and stats. This page follows one keystroke through the code
and shows how to run the engine by hand.

## Which file does what

| File | Role |
|---|---|
| `cmd/gotyper-engine/main.go` | Entry point. Parses flags, loads the lessons, then runs the protocol server on stdin/stdout. |
| `cmd/gotyper-engine/selftest.go` | `--selftest`: lists the lessons, then plays the first step through the real protocol code and checks the results. |
| `protocol/protocol.go` | The wire types (`Request`, `Response`, ...), the protocol version and the error codes. |
| `protocol/server.go` | The NDJSON read loop (`Serve`) and the dispatcher (`Handle`). Keeps the lessons, the handshake flag and the current session. |
| `judge/judge.go` | The result types the front end paints: `Span`, `Ghost`, `Stats`, `Render`. |
| `judge/session.go` | The diff and the scoring: `Session.Update`, `Restart`, `Indents`. |
| `judge/text.go` | Tab helpers: indentation width and tab expansion. |
| `lesson/lesson.go` | The `Step`, `Track` and `Library` types, and `Load`, which reads and validates the lessons directory. |
| `lesson/lessons_test.go` | `TestLessonsOnDisk`: compiles and tests every step on disk. |
| `PROTOCOL.md` | The wire schema. |
| `../lessons/` | The lessons themselves. `../lessons/README.md` describes the format. |

Packages only depend downwards: `main` → `protocol` → `judge` → `lesson`.

## Loading the lessons

The engine reads lessons from the directory given with `--lessons DIR`.
Without the flag it reads `../lessons` next to its own executable, after
resolving symlinks, which is the repository's `lessons/` when the binary is
built into `<repo>/bin/` as the Neovim plugin does. If that directory is
missing or does not load, the engine exits with an error naming the path it
tried. `go run` builds the binary in a temporary directory, so from `engine/`
pass `--lessons ../lessons`.

`lesson.Load` takes an `fs.FS`, the standard library's interface for a
read-only file tree. `main` passes `os.DirFS(dir)`, a real directory. The
loader's tests pass `fstest.MapFS`, an in-memory tree, so they can build
broken lessons without touching the disk.

1. Every directory in the root is a track. Every `NN-slug` directory in a
   track is a step. `fs.ReadDir` returns entries sorted by name, and the two
   digits make that the play order.
2. For each step, `loadStep` decodes `step.json` with
   `json.Decoder.DisallowUnknownFields`, so a misspelt field is an error
   instead of being silently dropped. It checks the title and mode, reads the
   target file into lines, and rejects stray files in the step directory.
3. It walks `hidden/` with `fs.WalkDir` and keeps every file by its path
   relative to `hidden/`.
4. It resolves `carry`. Steps are loaded in order, so `loadTrack` keeps a map
   of the steps loaded so far, and a carry can only name one of those. That
   makes "carry from a later step" an error naturally. A `from` map records
   where each module file came from, so two sources writing the same file
   are reported by name.
5. `Load` does not stop at the first problem. Each problem is an error that
   starts with `step <id>:`, and they are combined with `errors.Join`, so one
   run shows every broken lesson.

`Step.Module` returns the step's whole module as a map from file path to
contents: carried files, target and hidden files. `Step.WriteModule` writes it
to a directory. `TestLessonsOnDisk` does that for every step in a temporary
directory and runs `go vet ./...` and `go test ./...` there, so a lesson whose
tests fail, or whose target doesn't compile with its hidden files, fails the
engine's test suite under the step's id.

## Picking a step

`list` answers with the loaded tracks and steps (id, title, mode).
`start` with `"step":"json-api/01-greet-handler"` looks the id up in the
library and creates a `judge.Session` for that step. A `start` without a step
picks the first step of the first track.

## From a keystroke to a response

1. **The learner types `x` in Neovim.** The plugin sees the buffer change and
   writes one line to the engine's stdin:
   `{"id":4,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}`
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
   `{"id":4,"op":"update","render":{"error_spans":[{"row":0,"col":4,"end_col":5}],...}}`.
   The plugin matches it to its request by `id` and paints it.

`restart` follows the same path. `Handle` calls `Session.Restart`, which clears
the error count, the mistake history and the timer but keeps the step that
`start` picked. The
engine then replies with the step layout and the render of an empty buffer.

## Run it and poke it by hand

From the `engine/` directory:

```sh
go test ./...                              # all tests
go run ./cmd/gotyper-engine --lessons ../lessons --selftest  # list the lessons, play the first step, print a summary
go build -o ../bin/gotyper-engine ./cmd/gotyper-engine     # finds ../lessons on its own
```

Pipe JSON lines in and read the answers:

```sh
printf '%s\n' \
  '{"id":1,"op":"hello","protocol":1}' \
  '{"id":2,"op":"list"}' \
  '{"id":3,"op":"start","step":"json-api/01-greet-handler"}' \
  '{"id":4,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}' \
  '{"id":5,"op":"update","lines":["package main"],"keys":13,"cursor":[0,12]}' \
  '{"id":6,"op":"restart"}' \
  | go run ./cmd/gotyper-engine --lessons ../lessons
```

You can also run it interactively: start `../bin/gotyper-engine`, paste one request
per line, and press Ctrl-D to end. Things worth trying:

- Send `start` before `hello` to get `handshake_required`.
- Send `start` with a made-up `step` to get `unknown_step`.
- Break a lesson, for example by setting `"mode":"recall"` in its
  `step.json`, and start the engine: it prints every problem it found to
  stderr and exits.
- Send `"protocol":2` in the `hello` to get `version_mismatch`.
- Type a line of garbage to get `bad_json`. The engine keeps answering.
- Move `cursor` away from the typo in an update. The span is still red, but
  `stats.errors` doesn't go up.
- Indent a line with spaces instead of tabs. Nothing turns red.

Pipe the output through `jq` to make it readable, for example
`| go run ./cmd/gotyper-engine --lessons ../lessons | jq -c '.render.stats'`.
