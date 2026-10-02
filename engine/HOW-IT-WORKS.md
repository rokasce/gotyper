# How the engine works

The engine is a small Go program that judges typing. At start-up it loads the
lessons from disk. The Neovim plugin picks a step and then sends the buffer
on every change. The engine answers with what to paint: red
spans, ghost text and stats. When asked, it also compiles and tests the
buffer with the go command. This page follows one keystroke and one check
through the code, shows where finished steps are recorded, and shows how to
run the engine by hand.

## Which file does what

| File | Role |
|---|---|
| `cmd/gotyper-engine/main.go` | Entry point. Parses flags, loads the lessons, then runs the protocol server on stdin/stdout. |
| `cmd/gotyper-engine/selftest.go` | `--selftest`: lists the lessons, then plays the first step through the real protocol code and checks the results. |
| `protocol/protocol.go` | The wire types (`Request`, `Response`, ...), the protocol version and the error codes. |
| `protocol/server.go` | The NDJSON read loop (`Serve`) and the dispatcher (`Handle`). Keeps the lessons, the handshake flag and the current session, and runs checks in the background. |
| `judge/judge.go` | The result types the front end paints: `Span`, `Ghost`, `Stats`, `Render`. |
| `judge/session.go` | The diff and the scoring: `Session.Update`, `Restart`, `Indents`. Recall steps skip the diff. |
| `check/check.go` | `check.Run`: builds a step's module with a given version of its file and runs `go vet` and `go test` on it. |
| `stats/stats.go` | The stats file: `Record` (one completed step), `Append`, `Load` (skips damaged lines), `Bests` (per-step bests) and `DefaultPath`. |
| `judge/text.go` | Tab helpers: indentation width and tab expansion. |
| `lesson/lesson.go` | The `Step`, `Track` and `Library` types, and `Load`, which reads and validates the lessons directory. |
| `lesson/lessons_test.go` | `TestLessonsOnDisk`: runs `check.Run` on every step's own target. |
| `PROTOCOL.md` | The wire schema. |
| `../lessons/` | The lessons themselves. `../lessons/README.md` describes the format. |

Packages only depend downwards: `main` → `protocol` → `judge`, `check` and
`stats` → `lesson`.

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

`Step.Module(source)` returns the step's whole module as a map from file
path to contents: carried files, `source` as the step's own file, and hidden
files. `Step.WriteModule` writes it to a directory. `check.Run` builds on
that (see [Checking the code](#checking-the-code)), and `TestLessonsOnDisk`
calls `check.Run` with every step's own target, so a lesson whose tests fail,
or whose target doesn't compile with its hidden files, fails the engine's
test suite under the step's id.

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

In a **recall** step `Session.Update` skips steps 5 and 6. The learner writes
from memory, so their rows need not line up with the target's, and a
row-by-row diff would paint correct code red. It only starts the timer and
reports the keystrokes and the time; there are no spans, ghosts or ghost
lines, and `done` stays false. A passing check completes the step instead.

## Checking the code

A check is how the engine grades code rather than typing: does it compile,
and do the step's hidden tests pass?

1. **The plugin sends `check`** with the whole buffer:
   `{"id":9,"op":"check","lines":["package main",...]}`. It does this by itself
   when a type-along step's text matches, and when the learner presses `<F6>`
   in a recall step.
2. **`handleLine` sees `op` is `check`** (`protocol/server.go`). Running the go
   command takes a second or more, and the learner may keep typing meanwhile,
   so it is not answered in line. `checkJob` reads the step being played and
   the lines right away, on the goroutine that handles every request, and
   returns a function that does the slow part. `handleLine` starts that
   function on a new goroutine and goes straight back to reading requests.
   The function touches no `Server` state, so it can't race with the updates
   handled while it runs.
3. **`check.Run` builds the module** (`check/check.go`). It makes a temporary
   directory and writes `Step.Module(source)` into it, where `source` is the
   typed lines joined with newlines.
4. **It runs `go vet ./...`, then `go test ./...`** in that directory, stopping
   at the first that fails. `GOWORK=off` and an empty `GOFLAGS` keep the
   learner's environment from changing the result.
   Each command is stopped after a minute, in case the learner's code loops
   forever. It is stopped with an interrupt, so the go command can remove its
   work directory; only if it is still running one second later is it
   killed. The interrupt does not reach a test binary the go command is
   running, so `go test` also gets `-timeout=50s`: a looping test panics and
   exits by itself, and its stack trace shows where it was stuck.
5. **The output is tidied**: the temporary directory's path is cut out, so
   errors read `./handler.go:14:22: undefined: errors`, and only the first 20
   lines are kept for the panel.
6. **The goroutine writes the response** through `lineWriter`, whose mutex
   keeps two goroutines from writing over each other's line. The plugin
   matches it to its request by `id`, so it doesn't matter that update
   answers sent later have already gone out.

Why the build cache matters: compiling `net/http` and its dependencies takes
a few seconds; after that the go command reuses them and a check takes about
one second, so only the first check is slow.

When stdin closes, `Serve` waits for the checks still running, so they are
answered. Neovim's `jobstop` also sends SIGTERM, which would end the engine
before any cleanup ran, so `main` catches it with `signal.NotifyContext` and
passes that context to `NewServer`. When it is cancelled, `Serve` returns
without waiting for more input, the running checks are stopped, and
`check.Run` still removes their temporary directories.

## Recording finished steps

The engine remembers every finished step in a file, so the learner can see
their bests later. The file is `gotyper/stats.jsonl` under `$XDG_DATA_HOME`,
or under `~/.local/share` when that variable is not set: the same base
directory Neovim keeps its data in. `stats.DefaultPath` works it out, `main`
passes it to `NewServer`, and the tests pass a temporary file instead.

1. **Remembering the attempt.** The server keeps the latest render it sent
   (`last`), and under a mutex the attempt number (`attempt`, increased by
   every `start` and `restart`), the latest update's buffer (`lines`) and
   whether this attempt is already recorded (`recorded`, cleared by `start`
   and `restart`).
2. **When a check is sent**, `checkJob` notes, on the request goroutine,
   whether the attempt is complete if the check passes: always in a recall
   step, and in a type-along step only if `last.Done`. It also notes the
   stats from `last` and the attempt number.
3. **When the check finishes**, on its own goroutine, it records the step
   only if it passed and `claimRecord` says the same attempt is still going
   with the same buffer and is not recorded yet; `claimRecord` then marks it
   recorded, so a second passing check of the attempt (another F6, or
   retyping the last character of a type-along step) adds nothing. A
   restart while the check runs, or an edit to a recall step meanwhile,
   leaves nothing behind: the learner gave that attempt up, or is no longer
   at the code that passed. The plugin applies
   the same rule when it decides whether a recall step is done.
4. **`stats.Append` writes one line.** The file is JSON Lines, like the
   protocol: one JSON object per line. It opens the file with `O_APPEND`, so
   it only ever adds to the end and never rewrites old lines. If writing
   fails, the error goes to stderr (the plugin shows it as a warning) and the
   check is still answered.

The `stats` op reads the file back. `stats.Load` decodes it line by line and
skips any line that is not a valid record: a crash while appending can leave
half a line at the end, and one bad line should not hide every other result.
A missing file just means nothing was finished yet. `stats.Bests` then folds
the records into one summary per step (best WPM, best accuracy, fewest
keystrokes, completions, last played) and sorts them by step id.

## The Neovim side

The plugin lives outside this directory, in `plugin/` and `lua/gotyper/` at the
repository root. It holds no judging logic; it sends the buffer and paints the
answer.

| File | Role |
|---|---|
| `plugin/gotyper.lua` | Defines `:Gotyper` (with completion of step ids), `:GotyperRestart`, `:GotyperPanel`, `:GotyperSubmit` and `:GotyperStats`. |
| `lua/gotyper/engine.lua` | Builds this engine into `bin/` when its sources are newer than the binary, starts it as a job, and frames NDJSON requests and responses by `id`. Also asks a short-lived engine for `list` and `stats`. |
| `lua/gotyper/init.lua` | The step picker and the session: the game tab and buffer, change tracking, key counting, auto-indent, restart, checks and their results, the panel toggle and teardown. Also `:GotyperStats`. |
| `lua/gotyper/ui.lua` | Painting: error spans and ghosts as extmarks, ghost lines as virtual lines, the stats winbar, the panel and the stats window. |
| `test/run.sh`, `test/drive.lua` | End-to-end test: a real headless Neovim driven key by key over its RPC socket. |

`:Gotyper` without a step id, and completing its argument, run a short-lived
engine that answers `hello` and `list`; the picker (`vim.ui.select`) shows the
listed steps. Starting the chosen step launches the game's own engine and
sends `hello` first. If the engine answers `version_mismatch`, the plugin
closes the game tab and shows the engine's message. Otherwise it sends `start`
with the step id and paints the returned `render`.

On every buffer change Neovim calls the plugin's `on_lines` hook. The plugin
schedules one `update` for when Neovim is next idle, so a paste or a `dd` sends
one request, not one per line. Each update gets a new `id`. An answer whose `id`
is older than the newest update sent is dropped, because a newer buffer state is
already on its way.

What happens when the learner presses the restart key (`<F5>`):

1. `vim.on_key` counts the key, like every key pressed in the game buffer.
2. The buffer-local mapping calls `restart()` in `lua/gotyper/init.lua`. It
   marks every update still in flight as stale, holds back new updates until
   the engine answers, and sends `{"op":"restart"}`. Without the hold, a change
   typed just before the key could reach the engine after the restart and be
   judged as part of the new attempt.
3. The engine clears the attempt (`Session.Restart`) and answers with the step
   layout and the render of an empty buffer.
4. The plugin empties the buffer without making it undoable, sets its key count
   back to 0 as the protocol requires, paints the render (ghost text again, no
   red) and puts the learner back in insert mode on the first line. It also
   counts a new attempt, so the answer to a check sent before the restart is
   ignored when it arrives.

What happens when a check runs:

1. In a type-along step, the render that says `done` makes the plugin leave
   insert mode and call `run_check()` in `lua/gotyper/init.lua`. In a recall
   step `<F6>` (`:GotyperSubmit`) calls it; in a type-along step `<F6>` only
   says that the step is checked when it is finished. Checking early there
   could race the finishing keystroke: the early check's answer would arrive
   for unfinished code after the step was done.
2. `run_check()` shows "Running go vet + go test..." in the panel and sends
   `check` with the buffer. Updates go on being sent and painted meanwhile.
3. When the answer comes, `show_result()` fills the panel: PASS or FAIL, the
   stage that failed, the stats and the go output. In a recall step a pass
   marks the step done; a fail leaves the learner editing, to submit again.
   `run_check()` remembers the buffer's `changedtick` when it sends the
   check, and a pass for a buffer edited since then does not complete the
   step: the panel says the code changed and asks for another submit.

`:GotyperStats` runs a short-lived engine, as the picker does, and sends it
`hello` and `stats`. `ui.show_stats` lists the answer in a floating window,
one row per step; `q` or `<Esc>` closes it.

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
- Break a lesson, for example by setting `"mode":"race"` in its
  `step.json`, and start the engine: it prints every problem it found to
  stderr and exits.
- Send a check:
  `{"id":7,"op":"check","lines":["package main"]}`. It fails at `go vet`
  with `undefined: greetHandler`, because the hidden `main.go` uses a
  function that file doesn't have. Send an `update` straight after it: its
  answer comes first.
- Send `{"id":8,"op":"stats"}` to see your bests. Play with a stats file of
  your own by setting `XDG_DATA_HOME`, for example
  `XDG_DATA_HOME=/tmp/gt go run ./cmd/gotyper-engine --lessons ../lessons`,
  and add a line of garbage to `/tmp/gt/gotyper/stats.jsonl`: it is skipped.
- Send `"protocol":2` in the `hello` to get `version_mismatch`.
- Type a line of garbage to get `bad_json`. The engine keeps answering.
- Move `cursor` away from the typo in an update. The span is still red, but
  `stats.errors` doesn't go up.
- Indent a line with spaces instead of tabs. Nothing turns red.

Pipe the output through `jq` to make it readable, for example
`| go run ./cmd/gotyper-engine --lessons ../lessons | jq -c '.render.stats'`.
