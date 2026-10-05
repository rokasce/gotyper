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
  cmd = { "Gotyper", "GotyperRestart", "GotyperPanel", "GotyperSubmit", "GotyperStats" },
}
```

### How a game works

`:Gotyper` lists the lesson steps, each with its track, title and mode, and starts the one you pick. `:Gotyper <step-id>` starts a step directly; `<Tab>` completes the ids, such as `json-api/01-greet-handler`. The game opens in a new tab. Type the italic ghost text. Mistakes turn red; fix them with any vim motion. The bar at the top shows WPM, accuracy, keystrokes, finished lines and charged errors. You never type indentation: pressing `<Enter>` inserts the line's indentation for you. For this game buffer only, gotyper turns off auto-pairing, completion, Copilot and auto-formatting, and keeps gopls from attaching. Your config is left alone everywhere else.

When every line matches, gotyper compiles and tests what you typed (`go vet`, then the step's hidden tests with `go test`) and shows PASS or FAIL with the go output in the panel. The first check builds the standard library packages the step uses, which takes a few seconds; later checks take about one.

Some steps are **recall** steps: you write the file from memory, with no ghost text and nothing turning red, and the bar shows only keystrokes and time. Press `<F6>` to submit. If `go vet` or the tests fail, the panel shows why; fix the code and submit again. The step is done when the check passes; if you edit while the check runs, submit again so the edited code is checked. Pick a recall step from `:Gotyper` like any other, or start one directly, for example `:Gotyper json-api/02-greet-handler-recall`.

Some steps are **drills**, in the `vim-drills` track: refactor drills that practise vim's editing commands on Go code. The buffer opens holding some code, the goal is shown in a read-only split below it, and you start in normal mode. Edit the buffer until it matches the goal (indentation and blank lines aside); lines that differ from the goal are marked red, and only the part of a line that differs. Every key you press counts, motions included, and the bar shows your keys against the drill's par, a reasonable way to do it that the intro shows as a hint. When the buffer matches, the panel shows your keys against par. A drill is not compiled.

| Key (normal or insert mode) | Command | What it does |
|---|---|---|
| `<F5>` | `:GotyperRestart` | Throw the attempt away and type the step again from an empty buffer (in a drill, from its starting code). The error count, keystrokes and timer start over. |
| `<F6>` | `:GotyperSubmit` | Compile and test what is in the buffer now. This is how you finish a recall step. A type-along step is checked automatically when its text matches, and a drill is done when it matches the goal, so there it only says so. |
| `<F2>` | `:GotyperPanel` | Hide or show the explanation panel, for when it covers code in a small terminal. |
| `:tabclose` or `:q` | | End the game. The engine stops and the game buffer is wiped. |

To use other keys, map `:GotyperRestart`, `:GotyperSubmit` and `:GotyperPanel` to them in your own config.

### Your stats

Every time you finish a step (a type-along step whose check passes, a recall step you submit and pass, or a drill whose buffer matches the goal), gotyper records the attempt: WPM, accuracy, keystrokes, charged errors and time. Restarted or abandoned attempts are not recorded. `:GotyperStats` opens a window listing each step you have finished with your best WPM, best accuracy, fewest keystrokes, how many times you finished it and when you last played it; `q` or `<Esc>` closes it. Recall steps and drills have no WPM or accuracy, so those show `-`.

The records live in `gotyper/stats.jsonl` under `$XDG_DATA_HOME`, or under `~/.local/share` when that is not set: the same place Neovim keeps its data. The file has one JSON line per finished step; [`engine/PROTOCOL.md`](engine/PROTOCOL.md) describes them.

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

The lessons live in [`lessons/`](lessons/), one directory per track and one per step. [`lessons/README.md`](lessons/README.md) describes the format. `go test ./...` in `engine/` compiles and tests every step, except drills, which are only checked to be gofmt-clean Go.
