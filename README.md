# gotyper

A Neovim typing game that teaches Go. You type real Go code over ghost text, with real vim motions, and a Go engine compiles and tests what you typed.

## The engine

The Go engine lives in [`engine/`](engine/) (Go 1.26). It judges what you type and tells the Neovim front end what to paint, speaking newline-delimited JSON over stdin/stdout.

```sh
cd engine
go test ./...                                   # run the tests
go run ./cmd/gotyper-engine --selftest          # play the built-in step and print a summary
go build -o gotyper-engine ./cmd/gotyper-engine # build the binary
```

- [`engine/HOW-IT-WORKS.md`](engine/HOW-IT-WORKS.md): a walkthrough of the code and how to drive the engine by hand.
- [`engine/PROTOCOL.md`](engine/PROTOCOL.md): the wire protocol.
