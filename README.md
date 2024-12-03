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

#### Neovim
There is a plugin for easy integration: [netmute/ctags-lsp.nvim](https://github.com/netmute/ctags-lsp.nvim)
```lua
-- lazy.nvim
{
    "neovim/nvim-lspconfig",
    dependencies = "netmute/ctags-lsp.nvim",
    config = function()
        require("lspconfig").ctags_lsp.setup({
            filetypes = { "ruby" }, -- Or whatever language you want to use it for
        })
    end,
}
```

#### Helix
Add this to `~/.config/helix/languages.toml`:
```toml
[language-server.ctags-lsp]
command = "ctags-lsp"

[[language]]
name = "toml"  # Or whatever language you want to use it for
language-servers = [ "ctags-lsp" ]
```
