# gotyper engine protocol, version 1

The Neovim front end starts `gotyper-engine` as a job and talks to it over the
job's stdin and stdout using **newline-delimited JSON (NDJSON)**:

- Each request is one JSON object on one line, ending in `\n`.
- The engine answers every request with exactly one JSON object on one line.
- Responses come back in request order, except the answer to a `check`,
  which comes when the check finishes (see [Ordering](#ordering)).
- Blank lines are ignored.
- The engine exits cleanly when its stdin closes.
- stdout carries only protocol lines. Diagnostics go to stderr.

The Go form of this schema is `engine/protocol/protocol.go`.

## Common fields

| Field | In | Type | Meaning |
|---|---|---|---|
| `id` | request, response | integer or `null` | Chosen by the front end and echoed on the response. It is `null` on the response when the request line could not be decoded far enough to read its id. |
| `op` | request, response | string | `hello`, `list`, `start`, `update`, `restart` or `check`. The response echoes it. |
| `error` | response | object | Present only when the request failed: `{"code": string, "message": string}`. The engine keeps running after any error. |

Error codes:

| `code` | When |
|---|---|
| `bad_json` | The line is not valid JSON, or a field has the wrong type. |
| `version_mismatch` | `hello` named a protocol version other than 1. |
| `handshake_required` | Any op other than `hello` before a successful `hello`. |
| `no_step` | `update`, `restart` or `check` before any `start`. |
| `unknown_step` | `start` named a step id that `list` does not report, or the engine has no lessons. |
| `unknown_op` | `op` is not one listed here. |

## `hello`: the handshake (must come first)

```json
{"id":1,"op":"hello","protocol":1}
{"id":1,"op":"hello","hello":{"protocol":1,"engine":"0.1.0"}}
```

`protocol` is the version the front end speaks. If it isn't 1, the response
still carries `hello` (so the front end can tell the learner which engine it
found) plus an error, and the engine stays un-greeted:

```json
{"id":1,"op":"hello","hello":{"protocol":1,"engine":"0.1.0"},"error":{"code":"version_mismatch","message":"front end speaks protocol 2 but engine 0.1.0 speaks protocol 1; update whichever is older"}}
```

A later `hello` with the right version still succeeds.

## `list`: the tracks and steps on offer

```json
{"id":2,"op":"list"}
{"id":2,"op":"list","list":{"tracks":[
  {"id":"json-api","steps":[
    {"id":"json-api/01-greet-handler","title":"Step 1 - a JSON handler with errors as values","mode":"type-along"}]}]}}
```

Tracks and their steps come in play order. A step `id` is
`<track>/<step directory>`; pass it to `start`. `mode` says how the step is
played:

| `mode` | How it is played | What completes it |
|---|---|---|
| `type-along` | The learner types over ghost text; mistakes are red. | Every row matches its target: `render.done`. |
| `recall` | The learner writes the file from memory: no ghost text, no red. | A `check` that passes. |

Front ends should be ready for other modes later and may skip steps whose
mode they don't know.

## `start`: begin a step

```json
{"id":3,"op":"start","step":"json-api/01-greet-handler"}
```

`step` is a step id from `list`. Without `step`, the engine begins the first
step of the first track. An id that isn't listed gets `unknown_step`, and the
step already in progress (if any) carries on untouched.

The response carries `start` (the step's layout) and `render` (what to paint
for an empty buffer, see `update`). A second `start` begins a step again from
scratch.

```json
{"id":3,"op":"start",
 "start":{"step":"json-api/01-greet-handler","mode":"type-along","title":"Step 1 - ...","intro":["Type the ghost text. ..."],"indents":[0,0,0,4,...],"lines":28,"width":59},
 "render":{...}}
```

| `start` field | Meaning |
|---|---|
| `step`, `mode` | The id and mode of the step that began, which tells the front end what a `start` without `step` picked. |
| `title`, `intro` | Text to show beside the code. `intro` is a list of markdown lines. |
| `indents` | For each target line, its indentation in display columns (tab = 4). In a type-along step the front end inserts this on Enter, so the learner never types indentation. In a recall step the learner's rows need not line up with the target's, so it doesn't apply. |
| `lines` | Number of target lines. |
| `width` | Display width of the widest target line, tabs expanded. |

## `update`: judge the buffer

Send this on every buffer change.

```json
{"id":4,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}
```

| Request field | Meaning |
|---|---|
| `lines` | The whole buffer, one string per row. |
| `keys` | Keystrokes since this attempt began. The front end counts them and resets its count to 0 on `start` and `restart`. |
| `cursor` | `[row, byte_col]`, both 0-based: where the next typed character goes. |

The response carries `render`:

```json
{"id":4,"op":"update","render":{
  "error_spans":[{"row":0,"col":4,"end_col":5}],
  "ghosts":[{"row":0,"col":5,"text":"ge main"}],
  "ghost_lines":["","import (","    \"encoding/json\"", "..."],
  "stats":{"wpm":0,"accuracy":80,"keys":5,"correct":4,"errors":1,"line":0,"lines":28,"seconds":0.2},
  "done":false,
  "compute_us":9}}
```

| `render` field | Meaning |
|---|---|
| `error_spans` | Wrong bytes to highlight. `row` is 0-based. `col`..`end_col` is a 0-based byte range with the end exclusive. Adjacent wrong bytes are merged into one span. |
| `ghosts` | The untyped rest of a started row, drawn inline at byte `col` (the end of the typed row). On a row that is completely empty, `text` includes the indentation as spaces. |
| `ghost_lines` | Target rows below the end of the buffer, tabs expanded to spaces, to draw as virtual lines. |
| `stats.wpm` | Correct characters / 5 / minutes since the first typed character. It is 0 during the first second. |
| `stats.accuracy` | `100 * correct / (correct + errors)`. |
| `stats.keys` | The `keys` value from the request. |
| `stats.correct` | Characters currently matching, plus one per newline between rows. |
| `stats.errors` | Mistakes charged in this attempt. This never decreases when a mistake is fixed. A mistake is charged only when it is new and ends exactly at `cursor`, so text shifted by a mid-line edit is shown red but not charged. |
| `stats.line`, `stats.lines` | How many leading rows fully match, out of the total. |
| `stats.seconds` | Time since the first typed character. It stops when the attempt is done. |
| `done` | The attempt has met the step's completion condition. For a type-along step that means every row matches its target with indentation ignored. For a recall step it is always `false`; see below. |
| `compute_us` | Engine judging time in microseconds, for latency measurement. |

Comparison rules: leading spaces and tabs are ignored on both sides. Rows are
compared rune by rune by position, with no alignment. Typed rows beyond the
target are errors unless they are blank.

In a **recall** step nothing is compared: a learner writing from memory may
put a blank line or a field somewhere else, and a row-by-row comparison would
then paint correct code red. So `error_spans`, `ghosts` and `ghost_lines`
are always empty, and of `stats` only `keys` and `seconds` are filled in; the
rest are 0. The step is completed by a passing `check`.

## `restart`: try the step again

```json
{"id":5,"op":"restart"}
```

`restart` throws away the current attempt and starts a fresh one at the same
step. The error count and mistake history are cleared, and the timer resets
(it starts again at the next typed character). The engine process keeps
running. The response has the same shape as `start`'s, so the front end can
clear its buffer, reset its key count, and paint the returned `render`.

## `check`: compile and test the buffer

```json
{"id":6,"op":"check","lines":["package main","",...]}
{"id":6,"op":"check","check":{"ok":false,"stage":"test","output":"--- FAIL: TestGreetHandler (0.00s)\n    handler_test.go:29: missing name: body = ...","ms":1048}}
```

`lines` is the whole buffer, as in `update`. The engine builds the step's Go
module in a temporary directory, the same way the lesson tests do
(`lessons/README.md`): the carried files, `lines` as the step's file, and the
hidden files. Then it runs `go vet ./...` and, if that passes, `go test ./...`.

| `check` field | Meaning |
|---|---|
| `ok` | Both `go vet` and `go test` passed. |
| `stage` | Where the check stopped: `setup` (the go command is missing, or the module could not be written), `vet` (which also catches compile errors) or `test`. A pass reports `test`. |
| `output` | What the go command printed for that stage, with the temporary directory's path removed and cut to its first 20 lines. |
| `ms` | How long the check took, in milliseconds. |

A failing check is not an error: `error` is only set when the request
itself is refused (`handshake_required`, `no_step`). Each go command is
stopped after a minute, and the output then says so, so learner code that
loops forever still gets an answer.

The check uses the go command's usual build cache, so compiled packages are
reused by later checks. The first `go vet` + `go test` on a cold cache takes
a few seconds; later ones take about one.

`check` never changes the session. The front end sends it for a type-along
step once the text matches, and for a recall step when the learner submits.
In a recall step a passing check is what completes the step; the front end
decides that from `ok`, and only if the buffer still holds the `lines` it
sent.

## Ordering

The engine reads requests one at a time and answers every op except
`check` before it reads the next line, so those answers come back in request
order.

A `check` takes a second or more, so the engine runs it in the background
and goes straight on to the next request. Its answer is written when it
finishes, which may be after the answers to requests sent later, such as the
`update`s typed meanwhile. Match it to its request by `id`.

A check grades the `lines` it was sent, against the step in progress when
the engine read the request. A `start` or `restart` sent afterwards does not
cancel it, so a front end that has moved on should ignore its answer. When
stdin closes, the engine waits for running checks to answer before it
exits.

## Compatibility

`list`, `unknown_step`, the `step` request field and the `step` and `mode`
fields of `start` were added within version 1. So were the `check` op and the
`recall` mode. They don't change any existing field's meaning: a `start`
without `step` behaves as before, a type-along step is judged as before, and
a front end that doesn't know `recall` can skip those steps, so the version
stayed at 1.

New optional fields may appear in responses within version 1, and front ends
should ignore fields they don't know. Changing or removing a field's meaning
bumps the version.

The completion meaning of `done` is defined per step mode, so new modes can
be added the same way.
