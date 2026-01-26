package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// -- ### Main ###
// -- CLI flags, startup, and server launch

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, checkCtagsInstallation))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, checkCtags func(string) error) int {
	config, err := parseFlags(args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	if config.showVersion {
		fmt.Fprintf(stdout, "CTags Language Server %s\n", version)
		return 0
	}

	if err := checkCtags(config.ctagsBin); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	server := &Server{
		cache: FileCache{
			content: make(map[string][]string),
		},
		rootlessTags: make(map[string][]TagEntry),
		ctagsBin:     config.ctagsBin,
		tagfilePath:  config.tagfilePath,
		languages:    config.languages,
		jobs:         config.jobs,
		output:       stdout,
	}

	if config.benchmark {
		if err := runBenchmark(server); err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := serve(stdin, server); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func parseFlags(args []string, output io.Writer) (*Config, error) {
	config := &Config{}

	flagset := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flagset.SetOutput(output)
	flagset.Usage = func() {
		flagUsage(output, args[0])
	}
	flagset.BoolVar(&config.showVersion, "version", false, "")
	flagset.BoolVar(&config.benchmark, "benchmark", false, "")
	flagset.StringVar(&config.ctagsBin, "ctags-bin", "ctags", "")
	flagset.StringVar(&config.tagfilePath, "tagfile", "", "")
	flagset.StringVar(&config.languages, "languages", "", "")
	flagset.IntVar(&config.jobs, "jobs", 8, "")

	if err := flagset.Parse(args[1:]); err != nil {
		return nil, err
	}
	if config.jobs < 1 {
		return nil, fmt.Errorf("clown")
	}

	return config, nil
}

func flagUsage(w io.Writer, program string) {
	fmt.Fprintf(w, `CTags Language Server
Provides LSP functionality based on ctags.

Usage:
  %s [options]

Options:
  --help               Show this help message
  --version            Show version information
  --ctags-bin <name>   Use custom ctags binary name (default: ctags)
  --tagfile <path>     Use tagfile instead of scanning
  --languages <value>  Pass through language filter list to ctags
  --jobs <value>       Number of ctags processes (default: 8)
`, program)
}

func checkCtagsInstallation(ctagsBin string) error {
	cmd := exec.Command(ctagsBin, "--version", "--output-format=json")
	output, err := cmd.Output()
	if err != nil || !strings.Contains(string(output), "Universal Ctags") {
		return fmt.Errorf("%s command not found or incorrect version. Universal Ctags with JSON support is required.\nPlease visit https://github.com/universal-ctags/ctags for installation instructions", ctagsBin)
	}

	return nil
}

func runBenchmark(server *Server) error {
	rootDir, err := os.Getwd()
	if err != nil {
		// Who knows where this will be run...
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	mockID := json.RawMessage(`1`)
	mockParams := InitializeParams{RootURI: pathToFileURI(normalizePath("", rootDir))}
	mockParamsBytes, _ := json.Marshal(mockParams)

	mockReq := RPCRequest{
		Jsonrpc: "2.0",
		ID:      &mockID,
		Method:  "initialize",
		Params:  mockParamsBytes,
	}

	handleInitialize(server, mockReq)
	return nil
}

func serve(r io.Reader, server *Server) error {
	reader := bufio.NewReader(r)
	for {
		req, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			server.sendError(nil, -32600, "Malformed request", err.Error())
			continue
		}

		go handleRequest(server, req)
	}
}

// Config holds values parsed from command-line flags.
type Config struct {
	showVersion bool
	benchmark   bool
	ctagsBin    string
	tagfilePath string
	languages   string
	jobs        int
}

var version = "self compiled" // Populated with -X main.version

// -- ### LSP Server ###
// -- Language server request handling and workspace state

func handleRequest(server *Server, req RPCRequest) {
	if !server.initialized && req.Method != "initialize" && req.Method != "shutdown" && req.Method != "exit" {
		if isNotification(req) {
			return
		}
		server.sendError(req.ID, -32002, "Server not initialized", "Received request before successful initialization")
		return
	}

	switch req.Method {
	case "initialize":
		handleInitialize(server, req)
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
		if isNotification(req) {
			return
		}
		message := fmt.Sprintf("Method not found: %s", req.Method)
		server.sendError(req.ID, -32601, message, nil)
	}
}

func handleInitialize(server *Server, req RPCRequest) {
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	rootURI, err := selectRootURI(params)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	if rootURI != "" {
		if err := server.setRootURI(rootURI); err != nil {
			server.sendError(req.ID, -32602, "Invalid params", err.Error())
			return
		}
	}

	result := InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: &TextDocumentSyncOptions{
				Change:    1, // LSP TextDocumentSyncKindFull.
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
		Info: ServerInfo{
			Name:    "ctags-lsp",
			Version: version,
		},
	}

	server.sendResult(req.ID, result)
	server.initialized = true
}

func selectRootURI(params InitializeParams) (string, error) {
	if len(params.WorkspaceFolders) > 0 {
		// TODO: Need to support multiple workspaces in the future.
		normalizedURI, err := normalizeFileURI(params.WorkspaceFolders[0].URI)
		if err != nil {
			return "", err
		}
		return normalizedURI, nil
	}

	if params.RootURI != "" {
		normalizedURI, err := normalizeFileURI(params.RootURI)
		if err != nil {
			return "", err
		}
		return normalizedURI, nil
	}

	if params.RootPath != "" {
		cleanPath := filepath.Clean(params.RootPath)
		absPath, err := filepath.Abs(cleanPath)
		if err != nil {
			return "", err
		}
		return pathToFileURI(absPath), nil
	}

	return "", nil
}

func (server *Server) setRootURI(rootURI string) error {
	server.rootURI = rootURI
	if server.tagfilePath != "" {
		tagsPath := resolveTagfilePath(rootURI, server.tagfilePath)
		if _, err := os.Stat(tagsPath); err != nil {
			// Clients can initialize with a workspace that lacks the configured tagfile, so fail fast.
			return fmt.Errorf("Requested tagfile unavailable: %w", err)
		}
	}
	if err := server.scanWorkspace(rootURI); err != nil {
		// Only happens when workspace is empty, which is not critical. Notify and continue.
		server.showMessage(fmt.Errorf("error while scanning workspace: %v", err))
	}
	return nil
}

func handleShutdown(server *Server, req RPCRequest) {
	server.sendResult(req.ID, nil)
}

func handleExit(_ *Server, _ RPCRequest) {
	os.Exit(0)
}

func handleDidOpen(server *Server, req RPCRequest) {
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	content := strings.Split(params.TextDocument.Text, "\n")

	server.cache.mutex.Lock()
	server.cache.content[normalizedURI] = content
	server.cache.mutex.Unlock()

	if server.rootURI == "" {
		filePath := fileURIToPath(normalizedURI)
		if rootDir, ok := findRootMarkerDir(filePath, ".git"); ok {
			rootURI := pathToFileURI(rootDir)
			if err := server.setRootURI(rootURI); err != nil {
				// Requested tagfile is not present. Don't scan against users wishes. Notify and abort.
				server.showMessage(fmt.Errorf("failed to set root URI for %s: %v", normalizedURI, err))
				server.rootURI = ""
			}
		}
	}

	if server.isRootlessFile(normalizedURI) {
		if err := server.scanRootlessFile(normalizedURI); err != nil {
			// ctags command failed. Very unlikely. Notify and continue.
			server.showMessage(fmt.Errorf("error scanning rootless file %s: %v", normalizedURI, err))
		}
	}
}

func handleDidChange(server *Server, req RPCRequest) {
	var params DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	if len(params.ContentChanges) > 0 {
		content := strings.Split(params.ContentChanges[0].Text, "\n")
		server.cache.mutex.Lock()
		server.cache.content[normalizedURI] = content
		server.cache.mutex.Unlock()
	}
}

func handleDidClose(server *Server, req RPCRequest) {
	var params DidCloseTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	server.cache.mutex.Lock()
	delete(server.cache.content, normalizedURI)
	server.cache.mutex.Unlock()

	server.mutex.Lock()
	delete(server.rootlessTags, normalizedURI)
	server.mutex.Unlock()
}

func handleDidSave(server *Server, req RPCRequest) {
	var params DidSaveTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		return
	}

	if server.isRootlessFile(normalizedURI) {
		if err := server.scanRootlessFile(normalizedURI); err != nil {
			// ctags command failed. Very unlikely. Notify and continue.
			server.showMessage(fmt.Errorf("error rescanning rootless file %s: %v", normalizedURI, err))
		}
		return
	}

	if err := server.scanWorkspaceFile(normalizedURI); err != nil {
		// ctags command failed. Very unlikely. Notify and continue.
		server.showMessage(fmt.Errorf("error rescanning file %s: %v", normalizedURI, err))
	}
}

func handleCompletion(server *Server, req RPCRequest) {
	var params CompletionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}
	filePath := fileURIToPath(normalizedURI)
	currentFileExt := filepath.Ext(filePath)

	server.cache.mutex.RLock()
	lines, ok := server.cache.content[normalizedURI]
	server.cache.mutex.RUnlock()

	if !ok || params.Position.Line >= len(lines) {
		server.sendError(req.ID, -32603, "Internal error", "Line out of range")
		return
	}

	lineContent := lines[params.Position.Line]
	runes := []rune(lineContent)
	isAfterDot := false
	if params.Position.Character > 0 && params.Position.Character-1 < len(runes) {
		prevChar := runes[params.Position.Character-1]
		isAfterDot = prevChar == '.'
	}

	word, err := server.getCurrentWord(normalizedURI, params.Position)
	if err != nil {
		if isAfterDot {
			word = ""
		} else {
			server.sendResult(req.ID, CompletionList{
				IsIncomplete: false,
				Items:        []CompletionItem{},
			})
			return
		}
	}

	var items []CompletionItem
	seenItems := make(map[string]bool)

	server.mutex.Lock()
	tagEntries := server.tagEntries
	if server.isRootlessFile(normalizedURI) {
		tagEntries = server.rootlessTags[normalizedURI]
	}
	for _, entry := range tagEntries {
		if strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(word)) {
			if seenItems[entry.Name] {
				continue
			}

			kind := GetLSPCompletionKind(entry.Kind)

			entryFilePath := fileURIToPath(entry.Path)
			entryFileExt := filepath.Ext(entryFilePath)

			includeEntry := false

			if isAfterDot {
				if (kind == CompletionItemKindMethod || kind == CompletionItemKindFunction) && entryFileExt == currentFileExt {
					includeEntry = true
				}
			} else {
				if kind == CompletionItemKindText {
					includeEntry = true
				} else if entryFileExt == currentFileExt {
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
	server.mutex.Unlock()

	result := CompletionList{
		IsIncomplete: false,
		Items:        items,
	}

	server.sendResult(req.ID, result)
}

func handleDefinition(server *Server, req RPCRequest) {
	var params TextDocumentPositionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	symbol, err := server.getCurrentWord(normalizedURI, params.Position)
	if err != nil {
		server.sendResult(req.ID, nil)
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var locations []Location
	tagEntries := server.tagEntries
	if server.isRootlessFile(normalizedURI) {
		tagEntries = server.rootlessTags[normalizedURI]
	}
	for _, entry := range tagEntries {
		if entry.Name == symbol {
			content, err := server.cache.GetOrLoadFileContent(entry.Path)
			if err != nil {
				// Read error from file that wasn't already in cache.
				server.showMessage(fmt.Errorf("failed to get content for file %s: %v", entry.Path, err))
				continue
			}

			symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

			location := Location{
				URI:   entry.Path,
				Range: symbolRange,
			}
			locations = append(locations, location)
		}
	}

	if len(locations) == 0 {
		server.sendResult(req.ID, nil)
	} else if len(locations) == 1 {
		server.sendResult(req.ID, locations[0])
	} else {
		server.sendResult(req.ID, locations)
	}
}

func handleWorkspaceSymbol(server *Server, req RPCRequest) {
	var params WorkspaceSymbolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	query := params.Query
	var symbols []SymbolInformation

	server.mutex.Lock()
	defer server.mutex.Unlock()

	for _, entry := range server.tagEntries {
		if query != "" && entry.Name != query {
			continue
		}

		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			continue
		}
		content, err := server.cache.GetOrLoadFileContent(entry.Path)
		if err != nil {
			// Read error from file that wasn't already in cache.
			server.showMessage(fmt.Errorf("failed to get content for file %s: %v", entry.Path, err))
			continue
		}

		symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

		symbol := SymbolInformation{
			Name: entry.Name,
			Kind: kind,
			Location: Location{
				URI:   entry.Path,
				Range: symbolRange,
			},
			ContainerName: entry.Scope,
		}
		symbols = append(symbols, symbol)
	}

	sort.SliceStable(symbols, func(i, j int) bool {
		uriI := symbols[i].Location.URI
		uriJ := symbols[j].Location.URI
		if uriI != uriJ {
			return uriI < uriJ
		}
		return symbols[i].Location.Range.Start.Line < symbols[j].Location.Range.Start.Line
	})

	server.sendResult(req.ID, symbols)
}

func handleDocumentSymbol(server *Server, req RPCRequest) {
	var params DocumentSymbolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	normalizedURI, err := normalizeFileURI(params.TextDocument.URI)
	if err != nil {
		server.sendError(req.ID, -32602, "Invalid params", err.Error())
		return
	}

	server.mutex.Lock()
	defer server.mutex.Unlock()

	var symbols []SymbolInformation

	tagEntries := server.tagEntries
	if server.isRootlessFile(normalizedURI) {
		tagEntries = server.rootlessTags[normalizedURI]
	}
	var documentEntries []TagEntry
	for _, entry := range tagEntries {
		if entry.Path != normalizedURI {
			continue
		}
		documentEntries = append(documentEntries, entry)
	}

	sort.SliceStable(documentEntries, func(i, j int) bool {
		return documentEntries[i].Line < documentEntries[j].Line
	})

	for _, entry := range documentEntries {
		kind, err := GetLSPSymbolKind(entry.Kind)
		if err != nil {
			continue
		}

		content, err := server.cache.GetOrLoadFileContent(entry.Path)
		if err != nil {
			// Read error from file that wasn't already in cache.
			server.showMessage(fmt.Errorf("failed to get content for file %s: %v", entry.Path, err))
			continue
		}

		symbolRange := findSymbolRangeInFile(content, entry.Name, entry.Line)

		symbol := SymbolInformation{
			Name:          entry.Name,
			Kind:          kind,
			Location:      Location{URI: entry.Path, Range: symbolRange},
			ContainerName: entry.Scope,
		}

		symbols = append(symbols, symbol)
	}

	server.sendResult(req.ID, symbols)
}

// showMessage sends a window/showMessage notification to the client.
func (server *Server) showMessage(err error) {
	notification := RPCNotification{
		Jsonrpc: "2.0",
		Method:  "window/showMessage",
		Params: ShowMessageParams{
			Type:    messageTypeWarning,
			Message: err.Error(),
		},
	}
	server.sendResponse(notification)
}

// findRootMarkerDir searches for markerName starting from the file directory and walking upwards.
func findRootMarkerDir(filePath, markerName string) (string, bool) {
	dir := filepath.Dir(filePath)
	for {
		markerPath := filepath.Join(dir, markerName)
		if _, err := os.Stat(markerPath); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// isRootlessFile reports whether a document should use per-file tags.
func (server *Server) isRootlessFile(fileURI string) bool {
	if server.rootURI == "" {
		return true
	}
	return !isFileInRoot(server.rootURI, fileURI)
}

// isFileInRoot reports whether fileURI resolves within rootURI.
func isFileInRoot(rootURI, fileURI string) bool {
	rootDir := fileURIToPath(rootURI)
	filePath := fileURIToPath(fileURI)

	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		// filepath.Rel fails on different volumes, which means the file is outside the root.
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// normalizeFileURI expects external URIs.
func normalizeFileURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		// Surface parsing failures so we never normalize malformed URIs.
		return "", fmt.Errorf("failed to parse URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		// The server only supports file:// URIs for filesystem-backed documents.
		return "", fmt.Errorf("expected file:// URI: %q", uri)
	}
	if parsed.Path == "" {
		// Empty paths cannot be resolved to a filesystem location.
		return "", fmt.Errorf("empty file URI")
	}

	path := filepath.Clean(filepath.FromSlash(parsed.Path))

	absPath, err := filepath.Abs(path)
	if err != nil {
		// Avoid emitting a bogus URI if the filesystem path cannot be resolved.
		return "", fmt.Errorf("failed to resolve path %q: %w", path, err)
	}

	return pathToFileURI(absPath), nil
}

// fileURIToPath expects normalized URIs.
func fileURIToPath(uri string) string {
	parsed, _ := url.Parse(uri)
	return filepath.Clean(filepath.FromSlash(parsed.Path))
}

func resolveTagfilePath(rootURI, tagfilePath string) string {
	tagsPath := tagfilePath
	if !filepath.IsAbs(tagsPath) {
		rootDir := fileURIToPath(rootURI)
		tagsPath = filepath.Join(rootDir, tagsPath)
	}
	return filepath.Clean(tagsPath)
}

// normalizePath expects raw filesystem paths from ctags/tagfiles, not file:// URIs.
func normalizePath(baseDir, raw string) string {
	clean := filepath.Clean(raw)
	if !filepath.IsAbs(clean) {
		clean = filepath.Clean(filepath.Join(baseDir, clean))
	}
	return clean
}

func readFileLines(fileURI string) ([]string, error) {
	filePath := fileURIToPath(fileURI)
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(contentBytes), "\n"), nil
}

func (cache *FileCache) GetOrLoadFileContent(filePath string) ([]string, error) {
	cache.mutex.RLock()
	content, ok := cache.content[filePath]
	cache.mutex.RUnlock()
	if ok {
		return content, nil
	}
	lines, err := readFileLines(filePath)
	if err != nil {
		return nil, err
	}
	cache.mutex.Lock()
	cache.content[filePath] = lines
	cache.mutex.Unlock()
	return lines, nil
}

// findSymbolRangeInFile returns a range for `symbolName` on `lineNumber` (1-based).
func findSymbolRangeInFile(lines []string, symbolName string, lineNumber int) Range {
	lineIdx := lineNumber - 1
	if lineIdx < 0 || lineIdx >= len(lines) {
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: 0},
		}
	}

	lineContent := lines[lineIdx]
	startChar := strings.Index(lineContent, symbolName)
	if startChar == -1 {
		return Range{
			Start: Position{Line: lineIdx, Character: 0},
			End:   Position{Line: lineIdx, Character: len([]rune(lineContent))},
		}
	}

	endChar := startChar + len([]rune(symbolName))

	return Range{
		Start: Position{Line: lineIdx, Character: startChar},
		End:   Position{Line: lineIdx, Character: endChar},
	}
}

func (server *Server) getCurrentWord(filePath string, pos Position) (string, error) {
	lines, err := server.cache.GetOrLoadFileContent(filePath)
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

	return string(runes[start:end]), nil
}

func isIdentifierChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}

type InitializeParams struct {
	RootURI          string            `json:"rootUri"`
	RootPath         string            `json:"rootPath"`
	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	Info         ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	TextDocumentSync        *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	CompletionProvider      *CompletionOptions       `json:"completionProvider,omitempty"`
	DefinitionProvider      bool                     `json:"definitionProvider,omitempty"`
	WorkspaceSymbolProvider bool                     `json:"workspaceSymbolProvider,omitempty"`
	DocumentSymbolProvider  bool                     `json:"documentSymbolProvider,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TextDocumentSyncOptions struct {
	Change    int  `json:"change"`
	OpenClose bool `json:"openClose"`
	Save      bool `json:"save"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocument `json:"textDocument"`
}

type TextDocument struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type CompletionParams struct {
	TextDocument PositionParams `json:"textDocument"`
	Position     Position       `json:"position"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type PositionParams struct {
	URI string `json:"uri"`
}

type CompletionItem struct {
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

const messageTypeWarning = 2

// TagEntry matches the JSON entry shape produced by Universal Ctags `--output-format=json`.
// Paths are normalized to absolute file:// URIs once ingested.
type TagEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Kind    string `json:"kind"`
	Line    int    `json:"line"`
	Scope   string `json:"scope,omitempty"`
}

type Server struct {
	tagEntries   []TagEntry
	rootlessTags map[string][]TagEntry
	rootURI      string
	cache        FileCache
	initialized  bool
	ctagsBin     string
	tagfilePath  string
	languages    string
	jobs         int
	output       io.Writer
	mutex        sync.Mutex
}

type FileCache struct {
	mutex   sync.RWMutex
	content map[string][]string
}

// -- ### Ctags Integration ###
// -- Workspace scanning and tag ingestion helpers

// scanWorkspace populates `server.tagEntries` from either:
// - a ctags scan of the workspace, or
// - a tagfile (when `--tagfile` is set).
func (server *Server) scanWorkspace(rootURI string) error {
	rootDir := fileURIToPath(rootURI)
	if server.tagfilePath != "" {
		tagsPath := resolveTagfilePath(rootURI, server.tagfilePath)
		entries, err := parseTagfile(tagsPath)
		if err != nil {
			return err
		}

		server.mutex.Lock()
		server.tagEntries = append(server.tagEntries, entries...)
		server.mutex.Unlock()
		return nil
	}

	files, err := listWorkspaceFiles(rootDir)
	if err != nil {
		return err
	}

	chunks := buildCtagsChunksBySize(rootDir, files, server.jobs)
	var wg sync.WaitGroup
	var workerErr error
	var errOnce sync.Once

	for _, chunk := range chunks {
		wg.Add(1)
		go func(chunk []string) {
			defer wg.Done()

			cmd := exec.Command(server.ctagsBin, server.ctagsArgs("-L", "-")...)
			cmd.Dir = rootDir
			cmd.Stdin = strings.NewReader(strings.Join(chunk, "\n"))

			entries, err := server.runCtags(cmd, rootDir)
			if err != nil {
				// Abort the workspace scan because a failed ctags run means entries are incomplete.
				errOnce.Do(func() { workerErr = err })
				return
			}
			server.mutex.Lock()
			server.tagEntries = append(server.tagEntries, entries...)
			server.mutex.Unlock()
		}(chunk)
	}

	wg.Wait()
	if workerErr != nil {
		return workerErr
	}
	return nil
}

// buildCtagsChunksBySize balances ctags work by file size because filesystem walks can
// yield nondeterministic ordering, which otherwise makes chunk runtimes swing wildly.
func buildCtagsChunksBySize(workspaceRoot string, files []string, workers int) [][]string {
	type fileSizeEntry struct {
		path string
		size int64
	}

	entries := make([]fileSizeEntry, 0, len(files))
	for _, path := range files {
		statPath := path
		if !filepath.IsAbs(statPath) {
			statPath = filepath.Join(workspaceRoot, statPath)
		}
		info, err := os.Stat(statPath)
		size := int64(0)
		if err != nil {
			// Files can disappear between listing and stat; size 0 keeps scheduling stable.
			size = 0
		} else {
			size = info.Size()
		}
		entries = append(entries, fileSizeEntry{path: path, size: size})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].size != entries[j].size {
			return entries[i].size > entries[j].size
		}
		return entries[i].path < entries[j].path
	})

	bucketCount := min(workers, len(entries))
	chunks := make([][]string, bucketCount)
	for i, entry := range entries {
		bucket := i % bucketCount
		chunks[bucket] = append(chunks[bucket], entry.path)
	}

	return chunks
}

// listWorkspaceFiles returns file paths using the first available tool.
// These paths are not normalized and may be relative or absolute.
func listWorkspaceFiles(workspaceRoot string) ([]string, error) {
	output, err := exec.Command("fd", "--type", "file", ".", workspaceRoot).Output()
	if err == nil {
		return parseFileList("fd", output)
	}

	output, err = exec.Command("rg", "--files", workspaceRoot).Output()
	if err == nil {
		return parseFileList("rg", output)
	}

	output, err = exec.Command("git", "-C", workspaceRoot, "ls-files", "-co", "--exclude-standard").Output()
	if err == nil {
		return parseFileList("git", output)
	}

	// WalkDir fallback. Slow, but guaranteed to work everywhere.
	var files []string
	filepath.WalkDir(workspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if len(files) == 0 {
		return nil, fmt.Errorf("empty workspace: no files found")
	}
	return files, nil
}

func parseFileList(toolName string, output []byte) ([]string, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, fmt.Errorf("empty workspace: %s returned no files", toolName)
	}
	return strings.Split(trimmed, "\n"), nil
}

// scanWorkspaceFile rescans a workspace file URI and drops any previous entries for that URI.
func (server *Server) scanWorkspaceFile(fileURI string) error {
	rootDir := fileURIToPath(server.rootURI)
	entries, err := server.scanFile(fileURI, rootDir)
	if err != nil {
		return err
	}

	server.mutex.Lock()
	newEntries := make([]TagEntry, 0, len(server.tagEntries))
	for _, entry := range server.tagEntries {
		if entry.Path != fileURI {
			newEntries = append(newEntries, entry)
		}
	}
	server.tagEntries = append(newEntries, entries...)
	server.mutex.Unlock()
	return nil
}

// scanRootlessFile updates tags for a file outside the workspace root.
func (server *Server) scanRootlessFile(fileURI string) error {
	baseDir := filepath.Dir(fileURIToPath(fileURI))
	entries, err := server.scanFile(fileURI, baseDir)
	if err != nil {
		return err
	}

	server.mutex.Lock()
	server.rootlessTags[fileURI] = entries
	server.mutex.Unlock()
	return nil
}

// scanFile runs ctags for a file.
// baseDir is needed for filepath normalization in `runCtags()`.
func (server *Server) scanFile(fileURI, baseDir string) ([]TagEntry, error) {
	filePath := fileURIToPath(fileURI)
	cmd := exec.Command(server.ctagsBin, server.ctagsArgs(filePath)...)
	cmd.Dir = baseDir
	return server.runCtags(cmd, baseDir)
}

func (server *Server) runCtags(cmd *exec.Cmd, baseDir string) ([]TagEntry, error) {
	stdout, _ := cmd.StdoutPipe()

	cmd.Start()

	scanner := bufio.NewScanner(stdout)
	var entries []TagEntry
	for scanner.Scan() {
		var entry struct {
			Type string `json:"_type"`
			TagEntry
		}
		json.Unmarshal([]byte(scanner.Text()), &entry)
		if entry.Type == "ptag" {
			// Skip pseudo-tags, they are metadata and do not represent symbols.
			continue
		}

		normalized := normalizePath(baseDir, entry.Path)
		entry.Path = pathToFileURI(normalized)

		entries = append(entries, entry.TagEntry)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ctags command failed: %v", err)
	}

	return entries, nil
}

func (server *Server) ctagsArgs(extra ...string) []string {
	args := []string{"--output-format=json", "--fields=+n"}
	if server.languages != "" {
		args = append(args, "--languages="+server.languages)
	}
	return append(args, extra...)
}

// -- ### Tagfile Parsing ###
// -- Mapping tagfile formats into TagEntry records

// parseTagfile reads a tags file and returns entries in the same shape as `runCtags`.
func parseTagfile(tagsPath string) ([]TagEntry, error) {
	file, err := os.Open(tagsPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	kindMap := newTagfileKindMap()
	entries := make([]TagEntry, 0, 1024)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "!") {
			parseTagfileKindDescription(trimmed, kindMap)
			continue
		}

		entry, ok := parseTagfileEntry(line, tagsPath, kindMap)
		if ok {
			entries = append(entries, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// parseTagfileKindDescription records kind letter mappings from tagfile header lines.
func parseTagfileKindDescription(line string, kindMap *tagfileKindMap) {
	if !strings.HasPrefix(line, "!_TAG_KIND_DESCRIPTION") {
		return
	}

	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return
	}

	language := strings.TrimPrefix(fields[0], "!_TAG_KIND_DESCRIPTION")
	if after, ok := strings.CutPrefix(language, "!"); ok {
		language = after
	} else {
		language = ""
	}

	parts := strings.SplitN(fields[1], ",", 2)
	if len(parts) != 2 {
		return
	}

	letter := parts[0]
	kind := parts[1]
	if letter == "" || kind == "" {
		return
	}

	kindMap.add(language, letter, kind)
}

// parseTagfileEntry parses a single tags file line into a TagEntry.
// It skips invalid entries and entries whose paths can't be normalized to file URIs.
func parseTagfileEntry(line, tagsPath string, kindMap *tagfileKindMap) (TagEntry, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 3 {
		return TagEntry{}, false
	}

	entry := TagEntry{
		Name:    fields[0],
		Path:    fields[1],
		Pattern: strings.TrimSuffix(fields[2], ";\""),
	}

	kindField := ""
	language := ""
	scopeKindSet := false
	nextFieldIndex := 3
	if len(fields) > 3 && !strings.Contains(fields[3], ":") {
		kindField = fields[3]
		nextFieldIndex = 4
	}

	for _, field := range fields[nextFieldIndex:] {
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}

		switch key {
		case "line":
			if lineNum, err := strconv.Atoi(value); err == nil {
				entry.Line = lineNum
			}
		case "language":
			language = value
		case "kind":
			kindField = value
		case "scope":
			entry.Scope = value
		case "scopeKind":
			scopeKindSet = true
		default:
			if entry.Scope == "" && !scopeKindSet && kindMap.isKindName(key) {
				entry.Scope = value
				scopeKindSet = true
			}
		}
	}

	if entry.Line == 0 {
		if lineNum, err := strconv.Atoi(entry.Pattern); err == nil {
			entry.Line = lineNum
		}
	}

	if kindField != "" {
		kindField = resolveTagfileKind(kindField, language, kindMap)
		entry.Kind = kindField
	}

	entry.Path = tagfilePathToFileURI(tagsPath, entry.Path)

	return entry, true
}

// resolveTagfileKind maps a kind letter to its kind name using tagfile metadata.
func resolveTagfileKind(kindField, language string, kindMap *tagfileKindMap) string {
	if len(kindField) != 1 {
		return kindField
	}

	if mapped, ok := kindMap.resolve(language, kindField); ok {
		return mapped
	}
	return kindField
}

// tagfilePathToFileURI normalizes a tags-file path to an absolute file URI.
// Relative paths are interpreted relative to the tagfile's directory.
func tagfilePathToFileURI(tagsPath, raw string) string {
	baseDir := filepath.Dir(tagsPath)
	normalized := normalizePath(baseDir, raw)
	return pathToFileURI(normalized)
}

func newTagfileKindMap() *tagfileKindMap {
	return &tagfileKindMap{
		byLanguage: make(map[string]map[string]string),
		any:        make(map[string]string),
		kindNames:  make(map[string]bool),
	}
}

func (kindMap *tagfileKindMap) add(language, letter, kind string) {
	if language == "" {
		language = "default"
	}
	if _, ok := kindMap.byLanguage[language]; !ok {
		kindMap.byLanguage[language] = make(map[string]string)
	}
	kindMap.byLanguage[language][letter] = kind
	if _, ok := kindMap.any[letter]; !ok {
		kindMap.any[letter] = kind
	}
	kindMap.kindNames[kind] = true
}

func (kindMap *tagfileKindMap) resolve(language, letter string) (string, bool) {
	if language != "" {
		if byLang, ok := kindMap.byLanguage[language]; ok {
			if kind, ok := byLang[letter]; ok {
				return kind, true
			}
		}
	}
	if kind, ok := kindMap.any[letter]; ok {
		return kind, true
	}
	return "", false
}

func (kindMap *tagfileKindMap) isKindName(kind string) bool {
	return kindMap.kindNames[kind]
}

type tagfileKindMap struct {
	byLanguage map[string]map[string]string
	any        map[string]string
	kindNames  map[string]bool
}

// -- ### Ctags to LSP ###
// -- Mapping ctags kind strings to LSP kinds

// GetLSPCompletionKind returns the LSP `CompletionItemKind` for a ctags kind.
// Unknown kinds fall back to `CompletionItemKindText`.
func GetLSPCompletionKind(ctagsKind string) int {
	if lspKind, ok := ctagsKindToLspKind[ctagsKind]; ok {
		return lspKind.completion
	}
	return CompletionItemKindText
}

// GetLSPSymbolKind returns the LSP `SymbolKind` for a ctags kind.
// It returns an error for unknown kinds so callers can skip unclassified entries.
func GetLSPSymbolKind(ctagsKind string) (int, error) {
	if lspKind, ok := ctagsKindToLspKind[ctagsKind]; ok {
		return lspKind.symbol, nil
	}
	return 0, fmt.Errorf("no symbol kind for: %v", ctagsKind)
}

// Numeric values match LSP 3.17 `CompletionItemKind`.
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

// Numeric values match LSP 3.17 `SymbolKind`.
const (
	SymbolKindFile          = 1
	SymbolKindModule        = 2
	SymbolKindNamespace     = 3
	SymbolKindPackage       = 4
	SymbolKindClass         = 5
	SymbolKindMethod        = 6
	SymbolKindProperty      = 7
	SymbolKindField         = 8
	SymbolKindConstructor   = 9
	SymbolKindEnum          = 10
	SymbolKindInterface     = 11
	SymbolKindFunction      = 12
	SymbolKindVariable      = 13
	SymbolKindConstant      = 14
	SymbolKindString        = 15
	SymbolKindNumber        = 16
	SymbolKindBoolean       = 17
	SymbolKindArray         = 18
	SymbolKindObject        = 19
	SymbolKindKey           = 20
	SymbolKindNull          = 21
	SymbolKindEnumMember    = 22
	SymbolKindStruct        = 23
	SymbolKindEvent         = 24
	SymbolKindOperator      = 25
	SymbolKindTypeParameter = 26
)

type lspKinds struct {
	completion int
	symbol     int
}

// ctagsKindToLspKind pairs a ctags kind with LSP completion and symbol kinds.
var ctagsKindToLspKind = map[string]lspKinds{
	"alias":            {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"anonMember":       {completion: CompletionItemKindField, symbol: SymbolKindField},
	"anchor":           {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"arg":              {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"array":            {completion: CompletionItemKindValue, symbol: SymbolKindArray},
	"attribute":        {completion: CompletionItemKindProperty, symbol: SymbolKindProperty},
	"boolean":          {completion: CompletionItemKindValue, symbol: SymbolKindBoolean},
	"callback":         {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"category":         {completion: CompletionItemKindClass, symbol: SymbolKindClass},
	"ccflag":           {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"cell":             {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"class":            {completion: CompletionItemKindClass, symbol: SymbolKindClass},
	"collection":       {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"command":          {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"component":        {completion: CompletionItemKindField, symbol: SymbolKindField},
	"config":           {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"const":            {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"constant":         {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"constructor":      {completion: CompletionItemKindConstructor, symbol: SymbolKindConstructor},
	"context":          {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"counter":          {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"data":             {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"dataset":          {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"def":              {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"define":           {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"delegate":         {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"enum":             {completion: CompletionItemKindEnum, symbol: SymbolKindEnum},
	"enumConstant":     {completion: CompletionItemKindEnumMember, symbol: SymbolKindEnumMember},
	"enumerator":       {completion: CompletionItemKindEnumMember, symbol: SymbolKindEnumMember},
	"environment":      {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"error":            {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"event":            {completion: CompletionItemKindEvent, symbol: SymbolKindEvent},
	"exception":        {completion: CompletionItemKindClass, symbol: SymbolKindClass},
	"externvar":        {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"face":             {completion: CompletionItemKindProperty, symbol: SymbolKindProperty},
	"feature":          {completion: CompletionItemKindProperty, symbol: SymbolKindProperty},
	"field":            {completion: CompletionItemKindField, symbol: SymbolKindField},
	"fn":               {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"fun":              {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"func":             {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"function":         {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"functionVar":      {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"functor":          {completion: CompletionItemKindModule, symbol: SymbolKindModule},
	"generic":          {completion: CompletionItemKindTypeParameter, symbol: SymbolKindTypeParameter},
	"getter":           {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"global":           {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"globalVar":        {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"group":            {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"guard":            {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"handler":          {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"header":           {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"icon":             {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"id":               {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"implementation":   {completion: CompletionItemKindClass, symbol: SymbolKindClass},
	"index":            {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"infoitem":         {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"inline":           {completion: CompletionItemKindKeyword, symbol: SymbolKindFunction},
	"instance":         {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"interface":        {completion: CompletionItemKindInterface, symbol: SymbolKindInterface},
	"it":               {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"integer":          {completion: CompletionItemKindValue, symbol: SymbolKindNumber},
	"jurisdiction":     {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"key":              {completion: CompletionItemKindKeyword, symbol: SymbolKindKey},
	"keyword":          {completion: CompletionItemKindKeyword, symbol: SymbolKindFunction},
	"l4subsection":     {completion: CompletionItemKindKeyword, symbol: SymbolKindNamespace},
	"l5subsection":     {completion: CompletionItemKindKeyword, symbol: SymbolKindNamespace},
	"label":            {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"legal":            {completion: CompletionItemKindKeyword, symbol: SymbolKindStruct},
	"legislation":      {completion: CompletionItemKindKeyword, symbol: SymbolKindStruct},
	"letter":           {completion: CompletionItemKindKeyword, symbol: SymbolKindStruct},
	"library":          {completion: CompletionItemKindModule, symbol: SymbolKindPackage},
	"list":             {completion: CompletionItemKindValue, symbol: SymbolKindArray},
	"local":            {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"localVariable":    {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"locale":           {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"localvar":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"macro":            {completion: CompletionItemKindConstant, symbol: SymbolKindConstant},
	"macroParameter":   {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"macrofile":        {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"macroparam":       {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"makefile":         {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"map":              {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"member":           {completion: CompletionItemKindField, symbol: SymbolKindField},
	"method":           {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"methodSpec":       {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"minorMode":        {completion: CompletionItemKindKeyword, symbol: SymbolKindFunction},
	"misc":             {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"module":           {completion: CompletionItemKindModule, symbol: SymbolKindModule},
	"name":             {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"namespace":        {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"nettype":          {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"newFile":          {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"node":             {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"number":           {completion: CompletionItemKindValue, symbol: SymbolKindNumber},
	"object":           {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"oneof":            {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"operator":         {completion: CompletionItemKindOperator, symbol: SymbolKindOperator},
	"option":           {completion: CompletionItemKindKeyword, symbol: SymbolKindVariable},
	"output":           {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"package":          {completion: CompletionItemKindModule, symbol: SymbolKindPackage},
	"packageName":      {completion: CompletionItemKindModule, symbol: SymbolKindPackage},
	"param":            {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"parameter":        {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"paramEntity":      {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"part":             {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"partition":        {completion: CompletionItemKindModule, symbol: SymbolKindModule},
	"placeholder":      {completion: CompletionItemKindClass, symbol: SymbolKindClass},
	"port":             {completion: CompletionItemKindField, symbol: SymbolKindField},
	"process":          {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"property":         {completion: CompletionItemKindProperty, symbol: SymbolKindProperty},
	"prototype":        {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"protocol":         {completion: CompletionItemKindInterface, symbol: SymbolKindInterface},
	"provider":         {completion: CompletionItemKindModule, symbol: SymbolKindModule},
	"publication":      {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"qkey":             {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"receiver":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"record":           {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"reference":        {completion: CompletionItemKindReference, symbol: SymbolKindStruct},
	"region":           {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"register":         {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"repoid":           {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"report":           {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"repositoryId":     {completion: CompletionItemKindProperty, symbol: SymbolKindKey},
	"repr":             {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"resource":         {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"response":         {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"role":             {completion: CompletionItemKindInterface, symbol: SymbolKindInterface},
	"rpc":              {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"root":             {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"schema":           {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"script":           {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"section":          {completion: CompletionItemKindKeyword, symbol: SymbolKindNamespace},
	"selector":         {completion: CompletionItemKindKeyword, symbol: SymbolKindClass},
	"sequence":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"server":           {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"service":          {completion: CompletionItemKindInterface, symbol: SymbolKindInterface},
	"setter":           {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"signal":           {completion: CompletionItemKindEvent, symbol: SymbolKindEvent},
	"singletonMethod":  {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"slot":             {completion: CompletionItemKindField, symbol: SymbolKindField},
	"software":         {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"sourcefile":       {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"standard":         {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"string":           {completion: CompletionItemKindValue, symbol: SymbolKindString},
	"struct":           {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"structure":        {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"stylesheet":       {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"subdir":           {completion: CompletionItemKindFolder, symbol: SymbolKindPackage},
	"submethod":        {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"submodule":        {completion: CompletionItemKindModule, symbol: SymbolKindModule},
	"subprogram":       {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"subprogspec":      {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"subroutine":       {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"subsection":       {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"subst":            {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"substdef":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"tag":              {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"talias":           {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"template":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"tparam":           {completion: CompletionItemKindTypeParameter, symbol: SymbolKindTypeParameter},
	"test":             {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"theme":            {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"theorem":          {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"thriftFile":       {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"throwsparam":      {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"title":            {completion: CompletionItemKindValue, symbol: SymbolKindString},
	"token":            {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
	"toplevelVariable": {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"trait":            {completion: CompletionItemKindInterface, symbol: SymbolKindInterface},
	"type":             {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"typealias":        {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"typedef":          {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"typespec":         {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"union":            {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"unit":             {completion: CompletionItemKindUnit, symbol: SymbolKindModule},
	"username":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"using":            {completion: CompletionItemKindModule, symbol: SymbolKindNamespace},
	"val":              {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"value":            {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"var":              {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"variable":         {completion: CompletionItemKindVariable, symbol: SymbolKindVariable},
	"vector":           {completion: CompletionItemKindValue, symbol: SymbolKindArray},
	"version":          {completion: CompletionItemKindValue, symbol: SymbolKindString},
	"video":            {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"view":             {completion: CompletionItemKindValue, symbol: SymbolKindObject},
	"wrapper":          {completion: CompletionItemKindMethod, symbol: SymbolKindMethod},
	"xdata":            {completion: CompletionItemKindStruct, symbol: SymbolKindStruct},
	"xinput":           {completion: CompletionItemKindFile, symbol: SymbolKindFile},
	"xtask":            {completion: CompletionItemKindFunction, symbol: SymbolKindFunction},
}

// -- ### JSON-RPC ###
// -- Message parsing and response helpers

// readMessage parses a single JSON-RPC message framed by `Content-Length` headers.
// It validates the request `id` shape (string or integer) when present.
func readMessage(reader *bufio.Reader) (RPCRequest, error) {
	contentLength := 0
	for {
		line, err := reader.ReadString('\r')
		if err != nil {
			return RPCRequest{}, fmt.Errorf("error reading header: %w", err)
		}
		b, err := reader.ReadByte()
		if err != nil {
			return RPCRequest{}, fmt.Errorf("error reading header: %w", err)
		}
		if b != '\n' {
			return RPCRequest{}, fmt.Errorf("line endings must be \\r\\n")
		}
		if line == "\r" {
			break
		}
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			clStr := strings.TrimSpace(after)
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
		return RPCRequest{}, fmt.Errorf("error reading body: %w", err)
	}

	var req RPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return RPCRequest{}, fmt.Errorf("invalid JSON-RPC request: %v", err)
	}
	if isInvalidID(req.ID) {
		return RPCRequest{}, fmt.Errorf("id must be a string or integer")
	}

	return req, nil
}

func isInvalidID(id *json.RawMessage) bool {
	if id == nil {
		return false
	}

	var s string
	if json.Unmarshal(*id, &s) == nil {
		return false
	}

	var n int64
	if json.Unmarshal(*id, &n) == nil {
		return false
	}

	return true
}

func isNotification(req RPCRequest) bool {
	return req.ID == nil
}

func (server *Server) sendResult(id *json.RawMessage, result any) {
	response := RPCSuccessResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Result:  result,
	}
	server.sendResponse(response)
}

func (server *Server) sendError(id *json.RawMessage, code int, message string, data any) {
	response := RPCErrorResponse{
		Jsonrpc: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	server.sendResponse(response)
}

// sendResponse writes a JSON-RPC response or notification to `server.output`.
func (server *Server) sendResponse(resp any) {
	body, _ := json.Marshal(resp)

	fmt.Fprintf(server.output, "Content-Length: %d\r\n\r\n%s", len(body), string(body))
}

type RPCRequest struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type RPCNotification struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCSuccessResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result"`
}

type RPCErrorResponse struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Error   *RPCError        `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
