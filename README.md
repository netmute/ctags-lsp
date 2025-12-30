# CTags Language Server

A Language Server Protocol (LSP) implementation using `universal-ctags` as backend, supporting 100+ languages.

This won't replace your dedicated language server, and it doesn't try to. The goal is to have a "better than nothing" language server that's trivial to setup for any language.

## What it does

On startup, `ctags-lsp` runs `universal-ctags` to index your workspace and keeps that index in memory to provide code completion, go-to-definition, and document/workspace symbols.

It never creates or updates tagfiles.

## Install

### Homebrew

```sh
brew install netmute/tap/ctags-lsp
```

### Go

```sh
go install github.com/netmute/ctags-lsp@latest
```

> When installing via Go, you must install `universal-ctags` yourself.

## Use with your editor

Any editor with a built-in LSP client is supported. Working examples are below.

### Neovim

```lua
-- lazy.nvim
{
	"neovim/nvim-lspconfig",
	config = function()
		vim.lsp.config("ctags_lsp", {
			cmd = { "ctags-lsp" },
			filetypes = { "ruby" }, -- Or whatever language you want to use it for
			root_dir = vim.uv.cwd(),
		})
		vim.lsp.enable("ctags_lsp")
	end,
},
```

### Helix

Add this to `~/.config/helix/languages.toml`:

```toml
[language-server.ctags-lsp]
command = "ctags-lsp"

[[language]]
name = "toml"  # Or whatever language you want to use it for
language-servers = [ "ctags-lsp" ]
```

## Advanced

### Speeding up startup

Most projects are completely indexed in less than 1s. If startup is slow for your workspace:

- Limit which languages are scanned using `--languages` [^1]
- Leverage an existing tagfile so `ctags-lsp` doesn’t have to run `ctags` on startup.

### Tagfiles (read-only fallback)

If a tagfile is found, `ctags-lsp` reads it instead of running `ctags`. It never writes tagfiles.

- `ctags-lsp` looks in the workspace root and uses the first tagfile found: `tags`, `.tags`, `.git/tags`.
- You can override that default with `--tagfile <path>`, then only that file is used.

For obvious reasons, `--languages` has no effect when using `ctags-lsp` with a tagfile.

### CLI options

Run `ctags-lsp --help`:

- `--languages <value>` / `--languages=<value>`: Pass-through to `ctags` language selection. No effect when a tagfile is used.
- `--tagfile <path>`: Use a specific tagfile.
- `--ctags-bin <name-or-path>`: Which `ctags` executable to run (default: `ctags`).

[^1] The `--languages` value is passed through to ctags unchanged; for available options see the `universal-ctags` manual:
https://docs.ctags.io/en/latest/man/ctags.1.html#language-selection-and-mapping-options
