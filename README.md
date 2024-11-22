# CTags Language Server

A Language Server Protocol (LSP) implementation using `universal-ctags` for code completion and go-to definition.

This won't replace your dedicated language server, and it doesn't try to. The goal is to have a "better than nothing" language server that's trivial to setup for any language.

## Installation

### With homebrew

```
brew install netmute/tap/ctags-lsp
```

### With go

> You need to install its dependency `universal-ctags` yourself

```
go install github.com/netmute/ctags-lsp@latest
```

## Use

The only supported/tested editor at this point is Neovim.  
I have written a plugin that simplifies integration: [netmute/ctags-lsp.nvim](https://github.com/netmute/ctags-lsp.nvim)
