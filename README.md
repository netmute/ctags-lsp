# CTags Language Server

CTags Language Server provides Language Server Protocol (LSP) functionalities, such as code completion, definition navigations, etc., through ctags files. It facilitates easier code editing and navigation in editors that support LSP.

## Features

- **Code Completion**: Provides completion suggestions based on ctags files.
- **goto Definition**: Navigate to the definition of a symbol.
- **File Synchronization**: Supports open, change, close, and save notifications for text documents.

## Installation

### Prerequisites

- Go 1.13 or later

### Build

Clone the repository and build the server:

```
git clone https://github.com/yourusername/ctags-language-server.git
cd ctags-language-server
go build -o ctags-langserver
```

## Usage

Run the server with the path to a ctags file:

```sh
ctags-langserver [options] [path/to/ctags-file]
```

If no ctags file is specified, the server will look for a `tags` file in the current directory.

### Options

- `-h, --help`: Show help message.
- `-v, --version`: Show version information.

## Concepts

- **Tags**: Tags are identifiers in the codebase generated by the `ctags` tool.
- **LSP**: Language Server Protocol provides standard language features to editors and IDEs, improving the development experience.

## Configuration

A default configuration is supported out-of-the-box. Additional configuration can be achieved by passing corresponding flags at launch.

## Protocol

The server communicates via the [JSON-RPC](https://www.jsonrpc.org/specification) and adheres to the LSP protocol for requests and responses.

### Supported Requests

- `initialize`: Initializes the server with workspace details.
- `textDocument/completion`: Fetches completion suggestions.
- `textDocument/definition`: Fetches the location of symbol definitions.

## Contributing

Contributions are welcome! Feel free to open an issue or submit a pull request. When contributing, ensure you follow these guidelines:

1. Fork the repository and create a new branch.
2. Enhance/modify the codebase with clean and maintainable code.
3. Include tests if possible.
4. Submit a detailed pull request.

## License

Distributed under the MIT License. See `LICENSE` for more information.

---

For further details, refer to the code documentation and comments. Feel free to reach out for any queries or suggestions.
