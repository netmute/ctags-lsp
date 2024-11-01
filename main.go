package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
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
	TextDocumentSync   *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	CompletionProvider *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider bool                     `json:"definitionProvider,omitempty"`
	CodeLensProvider   *CodeLensOptions         `json:"codeLensProvider,omitempty"`
}

// TextDocumentSyncOptions defines options for text document synchronization
type TextDocumentSyncOptions struct {
	Change    int  `json:"change"`
	OpenClose bool `json:"openClose"`
}

// CompletionOptions defines options for the completion provider
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

// CodeLensOptions defines options for the CodeLens provider
type CodeLensOptions struct {
	ResolveProvider bool `json:"resolveProvider,omitempty"`
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

// DefinitionParams represents the 'textDocument/definition' request
type DefinitionParams struct {
	TextDocument PositionParams `json:"textDocument"`
	Position     Position       `json:"position"`
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
	tagsFile   string
	cache      FileCache
	mu         sync.Mutex
}

// FileCache stores the content of opened files for quick access
type FileCache struct {
	mu      sync.RWMutex
	content map[string][]string
}

// TagEntry represents a single ctags entry
type TagEntry struct {
	Name     string
	Path     string
	Pattern  string
	Line     int
	Kind     string
	Language string
}

// kindMap maps single-letter ctags kind codes to full kind names
var kindMap = map[string]string{
	// Lowercase letters
	"a": "alias",
	"b": "block",
	"c": "class",
	"d": "define",
	"e": "enum",
	"f": "function",
	"g": "enum", // General enum
	"h": "header",
	"i": "interface",
	"k": "constant", // 'k' is less common; mapping to 'constant' from Pascal
	"l": "local",    // Local variable or label
	"m": "method",   // Method or member
	"n": "namespace",
	"o": "operator",
	"p": "package",
	"q": "unknown", // 'q' is rare; set as 'unknown'
	"r": "record",
	"s": "struct",
	"t": "type",
	"u": "union",
	"v": "variable",
	"w": "unknown", // 'w' is rare; set as 'unknown'
	"x": "externvar",
	"y": "unknown", // 'y' is rare; set as 'unknown'
	"z": "parameter",

	// Uppercase letters
	"A": "annotation",
	"B": "block",
	"C": "class",
	"D": "define",
	"E": "enum",
	"F": "function",
	"G": "enum",
	"H": "header",
	"I": "interface",
	"J": "unknown",
	"K": "package",
	"L": "label",
	"M": "module",
	"N": "namespace",
	"O": "operator",
	"P": "package",
	"Q": "unknown",
	"R": "record",
	"S": "struct",
	"T": "type",
	"U": "unknown",
	"V": "variable",
	"W": "unknown",
	"X": "unknown",
	"Y": "unknown",
	"Z": "unknown",
}

// Main Function
func main() {
	config := parseFlags()

	if config.showHelp {
		flagUsage()
		os.Exit(0)
	}

	if config.showVersion {
		version, commitHash := getVersionInfo()
		fmt.Printf("CTags Language Server version %s (commit %s)\n", version, commitHash)
		os.Exit(0)
	}

	server := &Server{
		tagsFile: config.tagsFile,
		cache: FileCache{
			content: make(map[string][]string),
		},
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		// Read headers
		contentLength := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				log.Fatalf("Error reading header: %v", err)
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break // End of headers
			}
			if strings.HasPrefix(line, "Content-Length:") {
				clStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
				cl, err := strconv.Atoi(clStr)
				if err != nil {
					log.Fatalf("Invalid Content-Length: %v", err)
				}
				contentLength = cl
			}
		}

		// Read body based on Content-Length
		body := make([]byte, contentLength)
		_, err := io.ReadFull(reader, body)
		if err != nil {
			log.Fatalf("Error reading body: %v", err)
		}

		var req RPCRequest
		err = json.Unmarshal(body, &req)
		if err != nil {
			log.Printf("Invalid JSON-RPC request: %v", err)
			continue
		}

		// Handle request in a separate goroutine
		go handleRequest(server, req)
	}
}

// Config holds command-line configuration options
type Config struct {
	showHelp    bool
	showVersion bool
	tagsFile    string
}

// parseFlags parses command-line arguments
func parseFlags() *Config {
	config := &Config{}
	for i, arg := range os.Args {
		if arg == "-h" || arg == "--help" {
			config.showHelp = true
		}
		if arg == "-v" || arg == "--version" {
			config.showVersion = true
		}
		if !strings.HasPrefix(arg, "-") && i > 0 {
			config.tagsFile = arg
		}
	}
	return config
}

func flagUsage() {
	fmt.Printf(`CTags Language Server
Provides code completion and goto definition functionality using ctags files.

Usage:
  %s [options] [ctags-file]

Options:
  -h, --help     Show this help message
  -v, --version  Show version information

If no ctags file is specified, the server will look for a 'tags' file in the current directory.
`, os.Args[0])
}

func getVersionInfo() (version, commitHash string) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok || buildInfo == nil {
		return "unknown", "unknown"
	}

	version = buildInfo.Main.Version

	var revision, modified string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}

	if len(revision) >= 7 {
		commitHash = revision[:7]
	} else {
		commitHash = revision
	}

	if modified == "true" {
		commitHash += "-dirty"
	}

	return version, commitHash
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
	if err := server.loadTags(); err != nil {
		sendError(req.ID, -32603, "Internal error", err.Error())
		return
	}

	// Define server capabilities
	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				Change:    1, // Full synchronization
				OpenClose: true,
			},
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{".", ":", ">"},
			},
			DefinitionProvider: true,
			CodeLensProvider: &CodeLensOptions{
				ResolveProvider: false,
			},
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
	// Currently not utilized
}

// handleCompletion processes the 'textDocument/completion' request
func handleCompletion(server *Server, req RPCRequest) {
	var params CompletionParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	// Get current document's file extension
	currentExt := filepath.Ext(uriToPath(params.TextDocument.URI))

	// Retrieve the current word at the cursor position
	word := server.getCurrentWord(params.TextDocument.URI, params.Position)
	var items []CompletionItem

	seenItems := make(map[string]bool)
	for _, entry := range server.tagEntries {
		// Only process entries with matching file extension
		if filepath.Ext(entry.Path) != currentExt {
			continue
		}

		if strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(word)) {
			if seenItems[entry.Name] {
				continue // Avoid duplicate entries
			}
			seenItems[entry.Name] = true

			items = append(items, CompletionItem{
				Label:  entry.Name,
				Kind:   getLSPCompletionKind(entry.Kind),
				Detail: fmt.Sprintf("%s (%s)", entry.Path, entry.Kind),
				Documentation: &MarkupContent{
					Kind:  "markdown",
					Value: entry.Pattern,
				},
			})
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
	var params DefinitionParams
	err := json.Unmarshal(req.Params, &params)
	if err != nil {
		sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	// Retrieve the current word at the cursor position
	word := server.getCurrentWord(params.TextDocument.URI, params.Position)
	if word == "" {
		sendResult(req.ID, []Location{})
		return
	}

	var locations []Location
	for _, entry := range server.tagEntries {
		if entry.Name == word {
			loc := Location{
				URI: filepathToURI(filepath.Join(server.rootPath, entry.Path)),
				Range: Range{
					Start: Position{Line: entry.Line - 1, Character: 0},
					End:   Position{Line: entry.Line - 1, Character: 0},
				},
			}
			locations = append(locations, loc)
		}
	}

	sendResult(req.ID, locations)
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

// loadTags loads and parses the ctags file into TagEntry structs
func (s *Server) loadTags() error {
	tagsPath := s.tagsFile
	if tagsPath == "" {
		tagsPath = filepath.Join(s.rootPath, "tags")
	}

	file, err := os.Open(tagsPath)
	if err != nil {
		return fmt.Errorf("error opening tags file: %v", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			return fmt.Errorf("error reading tags file: %v", err)
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue // Skip empty lines and comments
		}

		entry, err := parseCTagsLine(line)
		if err != nil {
			log.Printf("Warning: Skipping invalid tags entry: %v", err)
			continue
		}

		s.tagEntries = append(s.tagEntries, *entry)
	}
	return nil
}

// parseCTagsLine parses a single ctags line into a TagEntry
func parseCTagsLine(line string) (*TagEntry, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid ctags line format")
	}

	entry := &TagEntry{
		Name: fields[0],
		Path: fields[1],
	}

	// Parse the pattern field
	pattern := fields[2]
	if len(pattern) > 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/' {
		entry.Pattern = pattern[1 : len(pattern)-1]
	} else {
		entry.Pattern = pattern
	}

	// Extract line number from pattern if available
	if strings.HasPrefix(entry.Pattern, "^") {
		lineNoStr := regexp.MustCompile(`(\d+)`).FindString(entry.Pattern)
		if lineNo, err := strconv.Atoi(lineNoStr); err == nil {
			entry.Line = lineNo
		}
	}

	if len(fields) > 3 {
		kindCode := fields[3]
		// Map kind code to kind name
		if kind, ok := kindMap[kindCode]; ok {
			entry.Kind = kind
		} else {
			entry.Kind = "unknown"
		}
	}

	return entry, nil
}

// getCurrentWord retrieves the current word at the given position in the document
func (s *Server) getCurrentWord(uri string, pos Position) string {
	s.cache.mu.RLock()
	lines, ok := s.cache.content[uriToPath(uri)]
	s.cache.mu.RUnlock()

	if !ok || pos.Line >= len(lines) {
		return ""
	}

	line := lines[pos.Line]
	if pos.Character > len(line) {
		return ""
	}

	// Find word boundaries
	start := pos.Character
	for start > 0 && isIdentifierChar(rune(line[start-1])) {
		start--
	}

	end := pos.Character
	for end < len(line) && isIdentifierChar(rune(line[end])) {
		end++
	}

	if start == end {
		return ""
	}

	return line[start:end]
}

// isIdentifierChar checks if a rune is a valid identifier character
func isIdentifierChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

// getLSPCompletionKind maps ctags kinds to LSP completion item kinds
func getLSPCompletionKind(ctagsKind string) int {
	switch ctagsKind {
	case "function", "method":
		return 3 // CompletionItemKindFunction
	case "class":
		return 7 // CompletionItemKindClass
	case "struct", "record", "union":
		return 22 // CompletionItemKindStruct
	case "variable", "local", "externvar", "parameter":
		return 6 // CompletionItemKindVariable
	case "constant", "define":
		return 21 // CompletionItemKindConstant
	case "interface":
		return 8 // CompletionItemKindInterface
	case "module", "package", "namespace":
		return 9 // CompletionItemKindModule
	case "enum":
		return 13 // CompletionItemKindEnum
	case "type":
		return 25 // CompletionItemKindTypeParameter
	case "label":
		return 14 // CompletionItemKindKeyword
	case "operator":
		return 24 // CompletionItemKindOperator
	case "annotation":
		return 14 // CompletionItemKindKeyword
	case "header":
		return 17 // CompletionItemKindFile
	case "object":
		return 7 // CompletionItemKindClass
	case "block":
		return 1 // CompletionItemKindText
	default:
		return 1 // CompletionItemKindText
	}
}
