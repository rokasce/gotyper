# gotyper

A Neovim typing game that teaches Go. You type real Go code over ghost text, with real vim motions, and a Go engine compiles and tests what you typed.

## Play it

You need Neovim 0.10 or newer and Go 1.26 or newer on your `PATH`. From a clone of this repository, run:

```sh
nvim -c 'set rtp^=/path/to/gotyper' -c 'runtime plugin/gotyper.lua' -c Gotyper
```

It loads your own Neovim config, so your motions and colours work as usual. Use `-c`, not `--cmd`: lazy.nvim resets `runtimepath` while your config loads, which would drop a path added with `--cmd`.

The first `:Gotyper` builds the engine into `bin/`, which takes a few seconds. Later launches rebuild only when the engine sources changed.

To install it with [lazy.nvim](https://github.com/folke/lazy.nvim):

```lua
{
  "rokasce/gotyper", -- or: dir = "~/path/to/gotyper"
  cmd = { "Gotyper", "GotyperRestart", "GotyperPanel" },
}
```

### How a game works

`:Gotyper` opens a new tab. Type the italic ghost text. Mistakes turn red; fix them with any vim motion. The bar at the top shows WPM, accuracy, keystrokes, finished lines and charged errors. You never type indentation: pressing `<Enter>` inserts the line's indentation for you. For this game buffer only, gotyper turns off auto-pairing, completion, Copilot and auto-formatting, and keeps gopls from attaching. Your config is left alone everywhere else.

| Key (normal or insert mode) | Command | What it does |
|---|---|---|
| `<F5>` | `:GotyperRestart` | Throw the attempt away and type the step again from an empty buffer. The error count, keystrokes and timer start over. |
| `<F2>` | `:GotyperPanel` | Hide or show the explanation panel, for when it covers code in a small terminal. |
| `:tabclose` or `:q` | | End the game. The engine stops and the game buffer is wiped. |

To use other keys, map `:GotyperRestart` and `:GotyperPanel` to them in your own config.

### Test the plugin

```sh
test/run.sh   # a real headless Neovim (--clean) driven key by key over RPC
```

## The engine

The Go engine lives in [`engine/`](engine/) (Go 1.26). It judges what you type and tells the Neovim front end what to paint, speaking newline-delimited JSON over stdin/stdout.

```sh
cd engine
go test ./...                                              # run the tests
go run ./cmd/gotyper-engine --lessons ../lessons --selftest # play the first lesson step and print a summary
go build -o ../bin/gotyper-engine ./cmd/gotyper-engine     # build the binary; it reads ../lessons beside bin/
```

- [`engine/HOW-IT-WORKS.md`](engine/HOW-IT-WORKS.md): a walkthrough of the code and how to drive the engine by hand.
- [`engine/PROTOCOL.md`](engine/PROTOCOL.md): the wire protocol.

## Lessons

The lessons live in [`lessons/`](lessons/), one directory per track and one per step. [`lessons/README.md`](lessons/README.md) describes the format. `go test ./...` in `engine/` compiles and tests every step.
