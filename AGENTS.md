# Project agent memory

This file is the project's committed home for project-intrinsic agent knowledge: build, test, release, architecture, and sharp-edge notes that should travel with the code.

- Go engine: module `engine/` (`github.com/rokasce/gotyper/engine`). Run `go vet ./...`, `go test ./...` and `go run ./cmd/gotyper-engine --selftest` from `engine/`. `engine/HOW-IT-WORKS.md` maps the files.
- Lessons live in `lessons/<track>/<NN-slug>/` (format: `lessons/README.md`); `engine/lesson`'s `TestLessonsOnDisk` vets and tests every step's assembled module, so run `go test ./...` after touching a lesson.
- The engine wire format is `engine/PROTOCOL.md`. Keep it in sync with `engine/protocol/protocol.go`, and bump `protocol.Version` on any incompatible change.
- The learner reads the engine code to learn Go. Every package and exported identifier gets a doc comment explaining what and why, and non-obvious logic gets a plain-language comment.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
