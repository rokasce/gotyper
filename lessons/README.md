# Lessons

Each directory here is a **track**: a sequence of steps that builds one
program. Each numbered directory in a track is a **step**: one file the
learner types, plus everything needed to compile and test it. The engine
loads this directory at start-up (`engine/lesson`) and refuses to start if a
lesson is malformed.

```
lessons/
  json-api/                    track id: lowercase words joined by dashes
    01-greet-handler/          step directory: NN-slug, played in NN order
      step.json                metadata (below)
      handler.go               the target: the file the learner types
      hidden/                  the rest of the module, never shown
        go.mod
        main.go
        handler_test.go
```

A step's id is `<track>/<step directory>`, for example
`json-api/01-greet-handler`. The front end starts a step by its id.

## `step.json`

```json
{
  "title": "Step 1 - a JSON handler with errors as values",
  "mode": "type-along",
  "file": "handler.go",
  "intro": ["Line one of markdown,", "", "and another paragraph."],
  "carry": ["01-greet-handler"]
}
```

| Field | Required | Meaning |
|---|---|---|
| `title` | yes | Shown above the intro. |
| `mode` | yes | How the step is played: `type-along`, `recall` or `drill` (below). |
| `file` | yes | The target's file name. It must sit next to `step.json`. |
| `intro` | no | The explanation shown beside the code, as a list of markdown lines. JSON has no multi-line strings, so each line is one list item. |
| `carry` | no | Earlier steps of this track, by directory name, whose targets this step's module includes. Not for drills. |
| `start` | drills only | The file the drill's buffer starts with. It must sit next to `step.json`, under another name than `file`. |
| `par` | drills only | A keystroke count above 0: the keys of a good way to do the drill, which the learner's keys are shown against. |

Unknown fields are an error, so a misspelling doesn't go unnoticed.

The three modes:

- **`type-along`**: the learner types over the target shown as ghost text,
  and the step is done when every line matches. The game then runs `go vet`
  and `go test` on it and shows the result.
- **`recall`**: the target is never shown. The learner writes the file from
  memory and submits it, and the step is done when `go vet` and `go test`
  pass on the module with their file in place of the target. The target is
  the reference answer: `TestLessonsOnDisk` checks that it passes. Because
  the hidden tests are all that grade a recall step, they must pin down the
  behaviour the intro asks for. The intro should say what to write, since
  there is no ghost text to follow.
- **`drill`**: a vim refactor drill. The buffer opens holding the `start`
  file, the target (the goal) is shown below it, and the learner edits the
  buffer with vim commands until it equals the goal (indentation aside).
  The score is the keystrokes against `par`. Set `par` by counting a
  reasonable way to do it, and show that way in the intro as a hint.

A recall step usually repeats an earlier type-along step's file. It is a
step of its own, with its own copy of the target and hidden files, so it
does not carry the step it recalls (that file would clash with its target).

The step directory holds only `step.json`, the target and `hidden/`. Any
other file there is an error.

A drill is judged by its text alone, so it has no `hidden/` and is never
compiled. Its directory holds only `step.json`, the target and the start
file:

```
lessons/
  vim-drills/
    01-rename-variable/
      step.json                {"mode": "drill", "file": "goal.go", "start": "start.go", "par": 14, ...}
      goal.go                  the target: what the buffer must become
      start.go                 what the buffer holds when the drill begins
```

## The step's module

A step's Go module is assembled in one directory from:

1. the **carried** files: the target of each step named in `carry`, under
   that step's `file` name, exactly as the learner typed it;
2. the step's own **target**;
3. everything under **`hidden/`**, subdirectories included: `go.mod`,
   scaffolding such as `main.go`, and the `*_test.go` files that grade the
   step.

So if step 03 types `store.go` and carries `01-greet-handler` and
`02-routes`, its module holds `handler.go` and `routes.go` from those steps,
`store.go`, and its own hidden files. Carry is explicit, not automatic: list
every earlier target the step needs. Two sources may not provide the same
file. To have the learner extend a file typed earlier, make it this step's
target and don't carry the old version.

## Checking lessons

```sh
cd engine
go test ./lesson -run TestLessonsOnDisk -v
```

For every step this assembles the module in a temporary directory and runs
`go vet ./...` and `go test ./...` on it, with the same code (`engine/check`)
that checks the learner's work in the game. It also checks that a Go target is
gofmt-formatted, because the learner should type canonical code. Drills are
not compiled: for a drill it only checks that the target and the start file
are valid, gofmt-formatted Go. A failure names the step id.
