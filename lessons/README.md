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
| `mode` | yes | How the step is played. Only `type-along` exists: the learner types over ghost text and the step is done when every line matches. `recall` is reserved for later. |
| `file` | yes | The target's file name. It must sit next to `step.json`. |
| `intro` | no | The explanation shown beside the code, as a list of markdown lines. JSON has no multi-line strings, so each line is one list item. |
| `carry` | no | Earlier steps of this track, by directory name, whose targets this step's module includes. |

Unknown fields are an error, so a misspelling doesn't go unnoticed.

The step directory holds only `step.json`, the target and `hidden/`. Any
other file there is an error.

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
`go vet ./...` and `go test ./...` on it. It also checks that a Go target is
gofmt-formatted, because the learner should type canonical code. A failure
names the step id.
