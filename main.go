package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// RPCRequest represents a JSON-RPC request structure
type RPCRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse represents a JSON-RPC response structure
type RPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// InitializeParams represents parameters for the 'initialize' request
type InitializeParams struct {
	RootURI string `json:"rootUri"`
}

// InitializeResult represents the result of the 'initialize' request
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities defines the capabilities of the language server
type ServerCapabilities struct {
	TextDocumentSync        *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	CompletionProvider      *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider      bool                     `json:"definitionProvider,omitempty"`
	WorkspaceSymbolProvider bool                     `json:"workspaceSymbolProvider,omitempty"`
	DocumentSymbolProvider  bool                     `json:"documentSymbolProvider,omitempty"`
}

// TextDocumentSyncOptions defines options for text document synchronization
type TextDocumentSyncOptions struct {
	Change    int  `json:"change"`
	OpenClose bool `json:"openClose"`
	Save      bool `json:"save"`
}

// CompletionOptions defines options for the completion provider
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// WorkspaceSymbolParams represents the parameters for the 'workspace/symbol' request
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// DocumentSymbolParams represents the parameters for the 'textDocument/documentSymbol' request
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SymbolInformation represents information about a symbol
type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// DidOpenTextDocumentParams represents the 'textDocument/didOpen' notification
type DidOpenTextDocumentParams struct {
	TextDocument TextDocument `json:"textDocument"`
}

// TextDocument represents a text document in the editor
type TextDocument struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentPositionParams represents the parameters used in requests that require a text document and position.
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DidChangeTextDocumentParams represents the 'textDocument/didChange' notification
type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentIdentifier identifies a text document
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentContentChangeEvent represents a change in the text document
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidCloseTextDocumentParams represents the 'textDocument/didClose' notification
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidSaveTextDocumentParams represents the 'textDocument/didSave' notification
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

// CompletionParams represents the 'textDocument/completion' request
type CompletionParams struct {
	TextDocument PositionParams `json:"textDocument"`
	Position     Position       `json:"position"`
}

// Position represents a position in a text document
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// PositionParams holds the URI for position-based requests
type PositionParams struct {
	URI string `json:"uri"`
}

// CompletionItem represents a completion suggestion
type CompletionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

// MarkupContent represents documentation content
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// CompletionList represents a list of completion items
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Location represents a location in a text document
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Range represents a range in a text document
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Server represents the language server
type Server struct {
	tagEntries []TagEntry
	rootPath   string
	cache      FileCache
	mu         sync.Mutex
}

// FileCache stores the content of opened files for quick access
type FileCache struct {
	mu      sync.RWMutex
	content map[string][]string
}

// GetOrLoadFileContent retrieves file content from cache or loads it from disk if not present
func (fc *FileCache) GetOrLoadFileContent(filePath string) ([]string, error) {
	fc.mu.RLock()
	content, ok := fc.content[filePath]
	fc.mu.RUnlock()
	if ok {
		return content, nil
	}
	// Load the file content
	lines, err := readFileLines(filePath)
	if err != nil {
		return nil, err
	}
	// Store content in cache
	fc.mu.Lock()
	fc.content[filePath] = lines
	fc.mu.Unlock()
	return lines, nil
}

// TagEntry represents a single ctags JSON entry
type TagEntry struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Pattern   string `json:"pattern"`
	Kind      string `json:"kind"`
	Line      int    `json:"line"`
	Scope     string `json:"scope,omitempty"`
	ScopeKind string `json:"scopeKind,omitempty"`
	TypeRef   string `json:"typeref,omitempty"`
	Language  string `json:"language,omitempty"`
}

// getInstallInstructions returns OS-specific installation instructions for Universal Ctags
func getInstallInstructions() string {
	switch runtime.GOOS {
	case "darwin":
		return "You can install Universal Ctags with: brew install universal-ctags"
	case "linux":
		return "You can install Universal Ctags with:\n" +
			"- Ubuntu/Debian: sudo apt-get install universal-ctags\n" +
			"- Fedora: sudo dnf install ctags\n" +
			"- Arch Linux: sudo pacman -S ctags"
	case "windows":
		return "You can install Universal Ctags with:\n" +
			"- Chocolatey: choco install universal-ctags\n" +
			"- Scoop: scoop install universal-ctags\n" +
			"Or download from: https://github.com/universal-ctags/ctags-win32/releases"
	default:
		return "Please visit https://github.com/universal-ctags/ctags for installation instructions"
	}
}

// checkCtagsInstallation verifies that Universal Ctags is installed and supports required features
func checkCtagsInstallation() error {
	cmd := exec.Command("ctags", "--version", "--output-format=json")
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "Universal Ctags") {
		return fmt.Errorf("ctags command not found or incorrect version. Universal Ctags with JSON support is required.\n%s", getInstallInstructions())
	}

	return nil
}

var version = "unknown" // Populated with -X main.version

// Main Function
func main() {
	config := parseFlags()

	// Check for ctags installation before proceeding
	if err := checkCtagsInstallation(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if config.showHelp {
		flagUsage()
		os.Exit(0)
	}

	if config.showVersion {
		fmt.Printf("CTags Language Server version %s\n", version)
		os.Exit(0)
	}

	server := &Server{
		cache: FileCache{
			content: make(map[string][]string),
		},
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		req, err := readMessage(reader)
		if err != nil {
			log.Fatalf("Error reading message: %v", err)
		}

		// Handle request in a separate goroutine
		go handleRequest(server, req)
	}
}

// readMessage reads a single JSON-RPC message from the reader
func readMessage(reader *bufio.Reader) (RPCRequest, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return RPCRequest{}, fmt.Errorf("error reading header: %v", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break // End of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			clStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			cl, err := strconv.Atoi(clStr)
			if err != nil {
				return RPCRequest{}, fmt.Errorf("invalid Content-Length: %v", err)
			}
			contentLength = cl
		}
	}

	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return RPCRequest{}, fmt.Errorf("error reading body: %v", err)
	}

	var req RPCRequest
	err = json.Unmarshal(body, &req)
	if err != nil {
		return RPCRequest{}, fmt.Errorf("invalid JSON-RPC request: %v", err)
	}

	return req, nil
}

// Config holds command-line configuration options
type Config struct {
	showHelp    bool
	showVersion bool
}

func parseFlags() *Config {
	config := &Config{}
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "--help":
			config.showHelp = true
		case "-v", "--version":
			config.showVersion = true
		}
	}
	return config
}

func flagUsage() {
	fmt.Printf(`CTags Language Server
Provides LSP functionality using ctags JSON output.

Usage:
  %s [options]

Options:
  -h, --help     Show this help message
  -v, --version  Show version information
`, os.Args[0])
}

// handleRequest routes JSON-RPC requests to appropriate handlers
func handleRequest(server *Server, req RPCRequest) {
	switch req.Method {
	case "initialize":
		handleInitialize(server, req)
	case "initialized":
		handleInitialized(server, req)
	case "shutdown":
		handleShutdown(server, req)
	case "exit":
		handleExit(server, req)
	case "textDocument/didOpen":
		handleDidOpen(server, req)
	case "textDocument/didChange":
		handleDidChange(server, req)
	case "textDocument/didClose":
		handleDidClose(server, req)
	case "textDocument/didSave":
		handleDidSave(server, req)
	case "textDocument/completion":
		handleCompletion(server, req)
	case "textDocument/definition":
		handleDefinition(server, req)
	case "workspace/symbol":
		handleWorkspaceSymbol(server, req)
	case "textDocument/documentSymbol":
		handleDocumentSymbol(server, req)
	default:
		// Method not found
		sendError(req.ID, -32601, "Method not found", nil)
	}
}

// handleInitialize processes the 'initialize' request
func handleInitialize(server *Server, req RPCRequest) {
	var params InitializeParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	// Convert RootURI to filesystem path
	server.rootPath = uriToPath(params.RootURI)
	// Load ctags entries
	if err := server.scanRecursiveTags(); err != nil {
		sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}

	// Define server capabilities
	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				Change:    1, // Full synchronization
				OpenClose: true,
				Save:      true,
			},
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{".", "\""},
			},
			WorkspaceSymbolProvider: true,
			DefinitionProvider:      true,
			DocumentSymbolProvider:  true,
		},
	}

	sendResult(req.ID, result)
}

// handleInitialized processes the 'initialized' notification
func handleInitialized(_ *Server, _ RPCRequest) {
	// 'initialized' is a notification with no response
}

// handleShutdown processes the 'shutdown' request
func handleShutdown(_ *Server, req RPCRequest) {
	sendResult(req.ID, nil)
}

// handleExit processes the 'exit' notification
func handleExit(_ *Server, _ RPCRequest) {
	os.Exit(0)
}

// handleDidOpen processes the 'textDocument/didOpen' notification
func handleDidOpen(server *Server, req RPCRequest) {
	var params DidOpenTextDocumentParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return
	}

	uri := params.TextDocument.URI
	content := strings.Split(params.TextDocument.Text, "\n")

	// Cache the opened document's content
	server.cache.mu.Lock()
	server.cache.content[uriToPath(uri)] = content
	server.cache.mu.Unlock()
}

// handleDidChange processes the 'textDocument/didChange' notification
func handleDidChange(server *Server, req RPCRequest) {
	var params DidChangeTextDocumentParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return
	}

	uri := params.TextDocument.URI
	if len(params.ContentChanges) > 0 {
		content := strings.Split(params.ContentChanges[0].Text, "\n")
		// Update the cached content
		server.cache.mu.Lock()
		server.cache.content[uriToPath(uri)] = content
		server.cache.mu.Unlock()
	}
}

// handleDidClose processes the 'textDocument/didClose' notification
func handleDidClose(server *Server, req RPCRequest) {
	var params DidCloseTextDocumentParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return
	}

	uri := params.TextDocument.URI
	// Remove the document from cache
	server.cache.mu.Lock()
	delete(server.cache.content, uriToPath(uri))
	server.cache.mu.Unlock()
}

// handleDidSave processes the 'textDocument/didSave' notification
func handleDidSave(server *Server, req RPCRequest) {
	var params DidSaveTextDocumentParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		return
	}

	// Get file path from URI
	filePath := uriToPath(params.TextDocument.URI)

	// Scan the file again
	if err := server.scanSingleFileTag(filePath); err != nil {
		log.Printf("Error rescanning file %s: %v", filePath, err)
	}
}

// handleCompletion processes the 'textDocument/completion' request
func handleCompletion(server *Server, req RPCRequest) {
	var params CompletionParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	currentFilePath := uriToPath(params.TextDocument.URI)
	currentFileExt := filepath.Ext(currentFilePath)

	// Get the line content and check if the character before the cursor is a dot
	server.cache.mu.RLock()
	lines, ok := server.cache.content[currentFilePath]
	server.cache.mu.RUnlock()

	if !ok || params.Position.Line >= len(lines) {
		sendError(req.ID, -32603, "Internal error", "Line out of range")
		return
	}

	lineContent := lines[params.Position.Line]
	runes := []rune(lineContent)
	isAfterDot := false
	if params.Position.Character > 0 && params.Position.Character-1 < len(runes) {
		prevChar := runes[params.Position.Character-1]
		isAfterDot = prevChar == '.'
	}

	// Retrieve the current word at the cursor position
	word, err := server.getCurrentWord(params.TextDocument.URI, params.Position)
	if err != nil {
		sendResult(req.ID, CompletionList{
			IsIncomplete: false,
			Items:        []CompletionItem{},
		})
		return
	}

	var items []CompletionItem
	seenItems := make(map[string]bool)

	for _, entry := range server.tagEntries {
		if strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(word)) {
			if seenItems[entry.Name] {
				continue // Avoid duplicate entries
			}

			kind := GetLSPCompletionKind(entry.Kind)

			// Get the file extension of the entry's file
			entryFilePath := filepath.Join(server.rootPath, entry.Path)
			entryFileExt := filepath.Ext(entryFilePath)

			// Decide whether to include this entry
			includeEntry := false

			if isAfterDot {
				// After a dot, only include methods and functions, excluding 'text' items
				if (kind == CompletionItemKindMethod || kind == CompletionItemKindFunction) && entryFileExt == currentFileExt {
					includeEntry = true
				}
			} else {
				// Not after a dot
				if kind == CompletionItemKindText {
					// Always include 'text' items
					includeEntry = true
				} else if entryFileExt == currentFileExt {
					// Include items from files with the same extension
					includeEntry = true
				}
			}

			if includeEntry {
				seenItems[entry.Name] = true
				items = append(items, CompletionItem{
					Label:  entry.Name,
					Kind:   kind,
					Detail: fmt.Sprintf("%s:%d (%s)", entry.Path, entry.Line, entry.Kind),
					Documentation: &MarkupContent{
						Kind:  "plaintext",
						Value: entry.Pattern,
					},
				})
			}
		}
	}

	result := CompletionList{
		IsIncomplete: false,
		Items:        items,
	}

	sendResult(req.ID, result)
}

// handleDefinition processes the 'textDocument/definition' request
func handleDefinition(server *Server, req RPCRequest) {
	var params TextDocumentPositionParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	// Get the current word at the given position
	symbol, err := server.getCurrentWord(params.TextDocument.URI, params.Position)
	if err != nil {
		sendResult(req.ID, nil) // No symbol found at position or error occurred
		return
	}

	// Search for the symbol in the tagEntries
	server.mu.Lock()
	defer server.mu.Unlock()

	var locations []Location
	for _, entry := range server.tagEntries {
		if entry.Name == symbol {
			// Create a Location for the symbol's definition
			filePath := filepath.Join(server.rootPath, entry.Path)
			uri := filepathToURI(filePath)

			// Use the refactored method to get file content
			content, err := server.cache.GetOrLoadFileContent(filePath)
			if err != nil {
				log.Printf("Failed to get content for file %s: %v", filePath, err)
				continue
			}

			// Find the symbol's range within the file
			symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

			location := Location{
				URI:   uri,
				Range: symbolRange,
			}
			locations = append(locations, location)
		}
	}

	// Send the locations back
	if len(locations) == 0 {
		sendResult(req.ID, nil) // No definition found
	} else if len(locations) == 1 {
		sendResult(req.ID, locations[0])
	} else {
		sendResult(req.ID, locations)
	}
}

// handleWorkspaceSymbol processes the 'workspace/symbol' request
func handleWorkspaceSymbol(server *Server, req RPCRequest) {
	var params WorkspaceSymbolParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	query := params.Query
	var symbols []SymbolInformation

	server.mu.Lock()
	defer server.mu.Unlock()

	for _, entry := range server.tagEntries {
		if entry.Name == query {
			kind, err := GetLSPSymbolKind(entry.Kind)
			if err != nil {
				// This tag has no symbol kind, skip
				continue
			}
			filePath := filepath.Join(server.rootPath, entry.Path)
			uri := filepathToURI(filePath)

			// Use the refactored method to get file content
			content, err := server.cache.GetOrLoadFileContent(filePath)
			if err != nil {
				log.Printf("Failed to get content for file %s: %v", filePath, err)
				continue
			}

			// Find the symbol's range within the file
			symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

			symbol := SymbolInformation{
				Name: entry.Name,
				Kind: kind,
				Location: Location{
					URI:   uri,
					Range: symbolRange,
				},
				ContainerName: entry.Scope,
			}
			symbols = append(symbols, symbol)
		}
	}

	sendResult(req.ID, symbols)
}

// handleDocumentSymbol processes the 'textDocument/documentSymbol' request
func handleDocumentSymbol(server *Server, req RPCRequest) {
	var params DocumentSymbolParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	filePath := uriToPath(params.TextDocument.URI)

	server.mu.Lock()
	defer server.mu.Unlock()

	var symbols []SymbolInformation

	for _, entry := range server.tagEntries {
		// Check if the symbol belongs to the requested document
		absolutePath := filepath.Join(server.rootPath, entry.Path)
		absolutePath, err := filepath.Abs(absolutePath)
		if err != nil {
			log.Printf("Failed to get absolute path for %s: %v", entry.Path, err)
			continue
		}

		requestedPath, err := filepath.Abs(filePath)
		if err != nil {
			log.Printf("Failed to get absolute path for %s: %v", filePath, err)
			continue
		}

		if absolutePath != requestedPath {
			continue
		}

		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			// Skip symbols with unknown kinds
			continue
		}

		uri := filepathToURI(absolutePath)

		// Retrieve file content
		content, err := server.cache.GetOrLoadFileContent(absolutePath)
		if err != nil {
			log.Printf("Failed to get content for file %s: %v", absolutePath, err)
			continue
		}

		// Find the symbol's range within the file
		symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

		symbol := SymbolInformation{
			Name:          entry.Name,
			Kind:          kind,
			Location:      Location{URI: uri, Range: symbolRange},
			ContainerName: entry.Scope,
		}

		symbols = append(symbols, symbol)
	}

	sendResult(req.ID, symbols)
}

// readFileLines reads the content of a file and returns it as a slice of lines
func readFileLines(filePath string) ([]string, error) {
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	content := string(contentBytes)
	lines := strings.Split(content, "\n")
	return lines, nil
}

// findSymbolRangeInFile searches for the symbol in the specified line and returns its range
func findSymbolRangeInFile(lines []string, symbolName string, lineNumber int) Range {
	// Adjust line number to zero-based index
	lineIdx := lineNumber - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		// Line number out of range; return a zero range
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: 0},
		}
	}

	lineContent := lines[lineIdx]
	startChar := strings.Index(lineContent, symbolName)
	if startChar == -1 {
		// Symbol not found in the expected line; default to line start
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: len([]rune(lineContent))},
		}
	}

	// Calculate the end character position
	endChar := startChar + len([]rune(symbolName))

	return Range{
		Start: Position{Line: lineIdx, Character: startChar},
		End:   Position{Line: lineIdx, Character: endChar},
	}
}

// sendResult sends a successful JSON-RPC response
func sendResult(id json.RawMessage, result interface{}) {
	response := RPCResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Result:  result,
	}
	sendResponse(response)
}

// sendError sends an error JSON-RPC response
func sendError(id json.RawMessage, code int, message string, data interface{}) {
	response := RPCResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	sendResponse(response)
}

// sendResponse marshals and sends the JSON-RPC response with appropriate headers
func sendResponse(resp RPCResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		return
	}

	// Write headers followed by the JSON body
	fmt.Printf("Content-Length: %d\r\n\r\n%s", len(body), string(body))
}

// uriToPath converts a file URI to a filesystem path
func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return filepath.FromSlash(strings.TrimPrefix(uri, "file://"))
	}
	return uri
}

// filepathToURI converts a filesystem path to a file URI
func filepathToURI(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return "file://" + filepath.ToSlash(absPath)
}

// scanRecursiveTags scans all files in the root path
func (s *Server) scanRecursiveTags() error {
	cmd := exec.Command("ctags", "--output-format=json", "--fields=+n", "-R")
	cmd.Dir = s.rootPath
	return s.processTagsOutput(cmd)
}

// scanSingleFileTag scans a single file, removing previous entries for that file
func (s *Server) scanSingleFileTag(filePath string) error {
	s.mu.Lock()
	// Convert filePath to relative path
	relPath, err := filepath.Rel(s.rootPath, filePath)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to make file path relative: %v", err)
	}

	// Remove previous entries for that file
	newEntries := make([]TagEntry, 0, len(s.tagEntries))
	for _, entry := range s.tagEntries {
		if entry.Path != relPath {
			newEntries = append(newEntries, entry)
		}
	}
	s.tagEntries = newEntries
	s.mu.Unlock()

	cmd := exec.Command("ctags", "--output-format=json", "--fields=+n", relPath)
	cmd.Dir = s.rootPath
	return s.processTagsOutput(cmd)
}

// processTagsOutput handles the ctags command execution and output processing
func (s *Server) processTagsOutput(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout from ctags command: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ctags command: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	var entries []TagEntry
	for scanner.Scan() {
		var entry TagEntry
		if err := json.Unmarshal([]byte(scanner.Text()), &entry); err != nil {
			log.Printf("Failed to parse ctags JSON entry: %v", err)
			continue
		}

		// Normalize the Path to be relative to rootPath
		relPath, err := filepath.Rel(s.rootPath, filepath.Join(s.rootPath, entry.Path))
		if err != nil {
			log.Printf("Failed to make path relative for %s: %v", entry.Path, err)
			continue
		}
		entry.Path = relPath

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ctags output: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ctags command failed: %v", err)
	}

	s.mu.Lock()
	s.tagEntries = append(s.tagEntries, entries...)
	s.mu.Unlock()

	return nil
}

// getCurrentWord retrieves the current word at the given position in the document
func (s *Server) getCurrentWord(uri string, pos Position) (string, error) {
	filePath := uriToPath(uri)
	lines, err := s.cache.GetOrLoadFileContent(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to load file content: %v", err)
	}

	if pos.Line >= len(lines) {
		return "", fmt.Errorf("line %d out of range", pos.Line)
	}

	line := lines[pos.Line]
	runes := []rune(line)
	if pos.Character > len(runes) {
		return "", fmt.Errorf("character %d out of range", pos.Character)
	}

	// Find word boundaries
	start := pos.Character
	for start > 0 && isIdentifierChar(runes[start-1]) {
		start--
	}

	end := pos.Character
	for end < len(runes) && isIdentifierChar(runes[end]) {
		end++
	}

	if start == end {
		return "", fmt.Errorf("no word found at position")
	}

	word := string(runes[start:end])
	return word, nil
}

// isIdentifierChar checks if a rune is a valid identifier character
func isIdentifierChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}
