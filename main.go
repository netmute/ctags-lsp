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
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

// LSP Completion Item Kind Constants
const (
	CompletionItemKindText          = 1
	CompletionItemKindMethod        = 2
	CompletionItemKindFunction      = 3
	CompletionItemKindConstructor   = 4
	CompletionItemKindField         = 5
	CompletionItemKindVariable      = 6
	CompletionItemKindClass         = 7
	CompletionItemKindInterface     = 8
	CompletionItemKindModule        = 9
	CompletionItemKindProperty      = 10
	CompletionItemKindUnit          = 11
	CompletionItemKindValue         = 12
	CompletionItemKindEnum          = 13
	CompletionItemKindKeyword       = 14
	CompletionItemKindSnippet       = 15
	CompletionItemKindColor         = 16
	CompletionItemKindFile          = 17
	CompletionItemKindReference     = 18
	CompletionItemKindFolder        = 19
	CompletionItemKindEnumMember    = 20
	CompletionItemKindConstant      = 21
	CompletionItemKindStruct        = 22
	CompletionItemKindEvent         = 23
	CompletionItemKindOperator      = 24
	CompletionItemKindTypeParameter = 25
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
	cache      FileCache
	mu         sync.Mutex
}

// FileCache stores the content of opened files for quick access
type FileCache struct {
	mu      sync.RWMutex
	content map[string][]string
}

// TagEntry represents a single ctags JSON entry
type TagEntry struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Pattern   string `json:"pattern"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope,omitempty"`
	ScopeKind string `json:"scopeKind,omitempty"`
	TypeRef   string `json:"typeref,omitempty"`
	Language  string `json:"language,omitempty"`
	Line      int    `json:"line,omitempty"`
}

// kindMap maps ctags kind strings to LSP completion item kinds using constants
var kindMap = map[string]int{
	"function":   CompletionItemKindFunction,
	"method":     CompletionItemKindMethod,
	"class":      CompletionItemKindClass,
	"struct":     CompletionItemKindStruct,
	"variable":   CompletionItemKindVariable,
	"constant":   CompletionItemKindConstant,
	"interface":  CompletionItemKindInterface,
	"module":     CompletionItemKindModule,
	"package":    CompletionItemKindModule,
	"namespace":  CompletionItemKindModule,
	"enum":       CompletionItemKindEnum,
	"type":       CompletionItemKindTypeParameter,
	"field":      CompletionItemKindField,
	"property":   CompletionItemKindProperty,
	"parameter":  CompletionItemKindVariable,
	"keyword":    CompletionItemKindKeyword,
	"file":       CompletionItemKindFile,
	"reference":  CompletionItemKindReference,
	"folder":     CompletionItemKindFolder,
	"enumMember": CompletionItemKindEnumMember,
	"snippet":    CompletionItemKindSnippet,
	"text":       CompletionItemKindText,
	"operator":   CompletionItemKindOperator,
	"annotation": CompletionItemKindKeyword,
	"member":     CompletionItemKindField,
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
Provides code completion and goto definition functionality using ctags JSON output.

Usage:
  %s [options]

Options:
  -h, --help     Show this help message
  -v, --version  Show version information
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
	if err := server.loadTagsFromJSON(); err != nil {
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
				TriggerCharacters: []string{".", ":", ">", "\""},
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

	// Retrieve the current word at the cursor position
	word := server.getCurrentWord(params.TextDocument.URI, params.Position)
	var items []CompletionItem

	seenItems := make(map[string]bool)
	for _, entry := range server.tagEntries {
		if strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(word)) {
			if seenItems[entry.Name] {
				continue // Avoid duplicate entries
			}
			seenItems[entry.Name] = true

			kind := getLSPCompletionKind(entry.Kind)
			items = append(items, CompletionItem{
				Label:  entry.Name,
				Kind:   kind,
				Detail: fmt.Sprintf("%s (%s)", entry.Path, entry.Kind),
				Documentation: &MarkupContent{
					Kind:  "plaintext",
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
			line := entry.Line
			// If line number is not available, attempt to extract from pattern
			if line == 0 && entry.Pattern != "" {
				line = server.extractLineFromPattern(entry.Pattern)
			}
			loc := Location{
				URI: filepathToURI(filepath.Join(server.rootPath, entry.Path)),
				Range: Range{
					Start: Position{Line: line - 1, Character: 0},
					End:   Position{Line: line - 1, Character: 0},
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

// loadTagsFromJSON executes the ctags command and parses the JSON output
func (s *Server) loadTagsFromJSON() error {
	cmd := exec.Command("ctags", "--output-format=json", "-R")
	cmd.Dir = s.rootPath
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout from ctags command: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ctags command: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		var entry TagEntry
		err := json.Unmarshal([]byte(line), &entry)
		if err != nil {
			log.Printf("Failed to parse ctags JSON entry: %v", err)
			continue
		}
		s.tagEntries = append(s.tagEntries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ctags output: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ctags command failed: %v", err)
	}

	return nil
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
		c == '_' || c == '$'
}

// getLSPCompletionKind maps ctags kinds to LSP completion item kinds
func getLSPCompletionKind(ctagsKind string) int {
	if kind, ok := kindMap[ctagsKind]; ok {
		return kind
	}
	return CompletionItemKindText // Default to Text
}

// extractLineFromPattern attempts to extract line number from the ctags pattern
func (s *Server) extractLineFromPattern(pattern string) int {
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(pattern)
	if match != "" {
		if line, err := strconv.Atoi(match); err == nil {
			return line
		}
	}
	return 0
}
