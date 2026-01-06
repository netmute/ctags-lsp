# CTags Language Server

A Language Server Protocol (LSP) implementation using `universal-ctags` as backend, supporting 100+ languages.

This won't replace your dedicated language server, and it doesn't try to. The goal is to have a "better than nothing" language server that's trivial to setup for any language.

## What it does

On startup, `ctags-lsp` runs `universal-ctags` to index your workspace and keeps that index in memory to provide code completion, go-to-definition, and document/workspace symbols.

It never creates or updates tagfiles.

## Installation

brew will automatically install dependencies for you.

```sh
# install with brew
brew install ctags-lsp
```

When installing with mise or go, you must install `universal-ctags` yourself ([ctags installation](https://github.com/universal-ctags/ctags?tab=readme-ov-file#the-latest-build-and-package)).

```sh
# install with mise
mise use -g github:netmute/ctags-lsp
```

```sh
# install with go
go install github.com/netmute/ctags-lsp@latest
```

## Use with your editor

Any editor with a built-in LSP client should work. Working examples are below.

### Neovim

```lua
vim.lsp.config("ctags_lsp", {
	cmd = { "ctags-lsp" },
	filetypes = { "ruby" }, -- Change this to the language(s) nvim should attach the LSP to
})
vim.lsp.enable("ctags_lsp")
```

### Helix

Add this to `~/.config/helix/languages.toml`:

```toml
[language-server.ctags-lsp]
command = "ctags-lsp"

[[language]]
name = "toml"  # Change this to the language helix should attach the LSP to
language-servers = [ "ctags-lsp" ]
```

## Advanced

### Speeding up startup

Most projects are completely indexed in less than 1s. If startup is slow for your workspace:

- Limit which languages are being indexed with `--languages`. The option is passed through to ctags unchanged; for available options see the [universal-ctags manual](https://docs.ctags.io/en/latest/man/ctags.1.html#language-selection-and-mapping-options) on the topic.
- Leverage an existing tagfile so `ctags-lsp` doesn’t have to run `ctags` on startup.

### Tagfiles

On startup the server will look for `tags`, `.tags` or `.git/tags` in the workspace root, and use the first tagfile it finds. In this case, it will read the tagfile and not scan the workspace with `ctags`. This is only intended as a fallback option to improve performance, and should not be used otherwise. `ctags-lsp` will never write or update tagfiles.

You can point to a custom tagfile, instead of the defaults, with `--tagfile`.

For obvious reasons, `--languages` has no effect when using a tagfile.

### CLI options

```
> ctags-lsp --help

CTags Language Server
Provides LSP functionality based on ctags.

Usage:
  ctags-lsp [options]

Options:
  --help               Show this help message
  --version            Show version information
  --ctags-bin <name>   Use custom ctags binary name (default: "ctags")
  --tagfile <path>     Use custom tagfile (default: tries "tags", ".tags" and ".git/tags")
  --languages <value>  Pass through language filter list to ctags
```
