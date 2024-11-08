# CTags Language Server

A Language Server Protocol (LSP) implementation using `ctags` for code completion and go-to definition.

This won't replace dedicated language servers. It is intended as a "better than nothing" polyglot language server for projects were configuring a dedicated lsp isn't worth your time.

## Features

- **Code Completion**: Offers suggestions based on `ctags` output.
- **Go to Definition**: Jump to definitions within project files.

## Installation

Ensure `universal-ctags` is installed:

```sh
brew install universal-ctags
```
