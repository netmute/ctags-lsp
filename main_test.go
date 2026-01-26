package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// -- ### Main ###
// -- Tests for CLI flags and startup configuration

func parseFlagsForTest(t *testing.T, args []string) *Config {
	t.Helper()

	config, err := parseFlags(args, io.Discard)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return config
}

func TestParseFlagsUseTagfilePath(t *testing.T) {
	config := parseFlagsForTest(t, []string{"ctags-lsp", "--tagfile=custom.tags"})
	if config.tagfilePath != "custom.tags" {
		t.Fatalf("expected tagfile path to be %q, got %q", "custom.tags", config.tagfilePath)
	}
}

func TestParseFlagsJobsDefaultAndOverride(t *testing.T) {
	defaultConfig := parseFlagsForTest(t, []string{"ctags-lsp"})
	if defaultConfig.jobs != 8 {
		t.Fatalf("expected default jobs to be %d, got %d", 8, defaultConfig.jobs)
	}

	overrideConfig := parseFlagsForTest(t, []string{"ctags-lsp", "--jobs=3"})
	if overrideConfig.jobs != 3 {
		t.Fatalf("expected jobs override to be %d, got %d", 3, overrideConfig.jobs)
	}
}

func TestRunBenchmarkUsesCWD(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "bench.go")
	source := []byte("package demo\n\ntype Bench struct{}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	server := newTestServer(t)
	server.output = io.Discard

	if err := runBenchmark(server); err != nil {
		t.Fatalf("run benchmark: %v", err)
	}

	expectedRootURI := pathToFileURI(normalizePath("", cwd))
	if server.rootURI != expectedRootURI {
		t.Fatalf("expected root uri %q, got %q", expectedRootURI, server.rootURI)
	}
	if !server.initialized {
		t.Fatal("expected server to be initialized")
	}
	if len(server.tagEntries) == 0 {
		t.Fatal("expected tag entries from benchmark scan")
	}
}

// -- ### LSP Server ###
// -- Tests for initialization and URI normalization

type rpcSuccessEnvelope struct {
	Jsonrpc string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id"`
	Result  InitializeResult `json:"result"`
}

type rpcResultEnvelope struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	config := parseFlagsForTest(t, []string{"ctags-lsp"})
	return &Server{
		cache: FileCache{
			content: make(map[string][]string),
		},
		rootlessTags: make(map[string][]TagEntry),
		ctagsBin:     config.ctagsBin,
		tagfilePath:  config.tagfilePath,
		languages:    config.languages,
		jobs:         config.jobs,
	}
}

func initializeServer(t *testing.T, server *Server, rootPath string) rpcSuccessEnvelope {
	t.Helper()

	rootURI := "file://" + filepath.ToSlash(rootPath)
	return initializeServerWithParams(t, server, InitializeParams{RootURI: rootURI})
}

func initializeServerWithParams(t *testing.T, server *Server, params InitializeParams) rpcSuccessEnvelope {
	t.Helper()

	raw := initializeServerWithParamsRaw(t, server, params)
	return parseLSPResponse(t, raw)
}

func initializeServerWithParamsRaw(t *testing.T, server *Server, params InitializeParams) string {
	t.Helper()

	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	id := json.RawMessage("1")
	req := RPCRequest{
		Jsonrpc: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  paramsBytes,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	message := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	parsedReq, err := readMessage(bufio.NewReader(strings.NewReader(message)))
	if err != nil {
		t.Fatalf("read request: %v", err)
	}

	var output bytes.Buffer
	server.output = &output
	handleRequest(server, parsedReq)

	return output.String()
}

func parseLSPResponse(t *testing.T, raw string) rpcSuccessEnvelope {
	t.Helper()

	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected response with headers and body, got %q", raw)
	}

	contentLength := 0
	for _, line := range strings.Split(parts[0], "\r\n") {
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err := strconv.Atoi(strings.TrimSpace(after))
			if err != nil {
				t.Fatalf("invalid Content-Length: %v", err)
			}
			contentLength = length
			break
		}
	}
	if contentLength == 0 {
		t.Fatalf("missing Content-Length header in %q", parts[0])
	}

	body := parts[1]
	if contentLength != len(body) {
		t.Fatalf("expected Content-Length %d, got %d", contentLength, len(body))
	}

	var resp rpcSuccessEnvelope
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp
}

func parseLSPResult(t *testing.T, raw string) rpcResultEnvelope {
	t.Helper()

	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected response with headers and body, got %q", raw)
	}

	contentLength := 0
	for _, line := range strings.Split(parts[0], "\r\n") {
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err := strconv.Atoi(strings.TrimSpace(after))
			if err != nil {
				t.Fatalf("invalid Content-Length: %v", err)
			}
			contentLength = length
			break
		}
	}
	if contentLength == 0 {
		t.Fatalf("missing Content-Length header in %q", parts[0])
	}

	body := parts[1]
	if contentLength != len(body) {
		t.Fatalf("expected Content-Length %d, got %d", contentLength, len(body))
	}

	var resp rpcResultEnvelope
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp
}

func parseLSPErrorResponse(t *testing.T, raw string) RPCErrorResponse {
	t.Helper()

	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected response with headers and body, got %q", raw)
	}

	contentLength := 0
	for _, line := range strings.Split(parts[0], "\r\n") {
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err := strconv.Atoi(strings.TrimSpace(after))
			if err != nil {
				t.Fatalf("invalid Content-Length: %v", err)
			}
			contentLength = length
			break
		}
	}
	if contentLength == 0 {
		t.Fatalf("missing Content-Length header in %q", parts[0])
	}

	body := parts[1]
	if contentLength != len(body) {
		t.Fatalf("expected Content-Length %d, got %d", contentLength, len(body))
	}

	var resp RPCErrorResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp
}

func openDocument(t *testing.T, server *Server, uri, text string) {
	t.Helper()

	params := DidOpenTextDocumentParams{
		TextDocument: TextDocument{
			URI:  uri,
			Text: text,
		},
	}
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	req := RPCRequest{Params: paramsBytes}
	handleDidOpen(server, req)
}

func hasTag(entries []TagEntry, name, path string) bool {
	for _, entry := range entries {
		if entry.Name == name && entry.Path == path {
			return true
		}
	}
	return false
}

func encodePathForTest(path string) string {
	slashPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()[len("file://"):]
}

func TestInitializeLSPRequest(t *testing.T) {
	tempDir := t.TempDir()

	sourcePath := filepath.Join(tempDir, "hello.go")
	source := []byte("package demo\n\ntype Greeter struct{}\n\nfunc (Greeter) Hello() {}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	server := newTestServer(t)

	resp := initializeServer(t, server, tempDir)

	t.Run("json rpc response", func(t *testing.T) {
		if resp.Jsonrpc != "2.0" {
			t.Fatalf("expected jsonrpc 2.0, got %q", resp.Jsonrpc)
		}
		if string(resp.ID) != "1" {
			t.Fatalf("expected id 1, got %s", resp.ID)
		}
	})

	t.Run("server info", func(t *testing.T) {
		if resp.Result.Info.Name != "ctags-lsp" {
			t.Fatalf("expected server name ctags-lsp, got %q", resp.Result.Info.Name)
		}
	})

	t.Run("text document sync", func(t *testing.T) {
		sync := resp.Result.Capabilities.TextDocumentSync
		if sync == nil {
			t.Fatal("expected text document sync capabilities")
		}
		if sync.Change != 1 {
			t.Fatalf("expected full sync, got %d", sync.Change)
		}
		if !sync.OpenClose {
			t.Fatal("expected open/close support")
		}
		if !sync.Save {
			t.Fatal("expected save support")
		}
	})

	t.Run("server state", func(t *testing.T) {
		expectedRootURI := "file://" + filepath.ToSlash(tempDir)
		if server.rootURI != expectedRootURI {
			t.Fatalf("expected root uri %q, got %q", expectedRootURI, server.rootURI)
		}
		if !server.initialized {
			t.Fatal("expected server to be initialized")
		}
	})

	t.Run("tag entries", func(t *testing.T) {
		if len(server.tagEntries) == 0 {
			t.Fatal("expected tag entries from ctags scan")
		}

		path := "file://" + filepath.ToSlash(filepath.Join(tempDir, "hello.go"))
		cases := []struct {
			name   string
			symbol string
		}{
			{name: "struct", symbol: "Greeter"},
			{name: "method", symbol: "Hello"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if !hasTag(server.tagEntries, tc.symbol, path) {
					t.Fatalf("expected tag entry for %s", tc.symbol)
				}
			})
		}
	})
}

func TestInitializeRootSelection(t *testing.T) {
	rootDir := t.TempDir()
	otherDir := t.TempDir()

	for _, dir := range []string{rootDir, otherDir} {
		path := filepath.Join(dir, "placeholder.go")
		if err := os.WriteFile(path, []byte("package demo\n"), 0o644); err != nil {
			t.Fatalf("write placeholder file: %v", err)
		}
	}

	rootURI := pathToFileURI(rootDir)
	otherURI := pathToFileURI(otherDir)

	cases := []struct {
		name   string
		params InitializeParams
		want   string
	}{
		{
			name: "workspace folders win",
			params: InitializeParams{
				RootURI: rootURI,
				WorkspaceFolders: []WorkspaceFolder{
					{URI: otherURI, Name: "primary"},
				},
			},
			want: otherURI,
		},
		{
			name:   "root uri fallback",
			params: InitializeParams{RootURI: rootURI},
			want:   rootURI,
		},
		{
			name:   "root path fallback",
			params: InitializeParams{RootPath: rootDir},
			want:   rootURI,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)
			initializeServerWithParams(t, server, tc.params)

			if server.rootURI != tc.want {
				t.Fatalf("expected root uri %q, got %q", tc.want, server.rootURI)
			}
		})
	}
}

func TestInitializeRootless(t *testing.T) {
	server := newTestServer(t)

	resp := initializeServerWithParams(t, server, InitializeParams{})

	if resp.Jsonrpc != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", resp.Jsonrpc)
	}
	if server.rootURI != "" {
		t.Fatalf("expected empty root uri, got %q", server.rootURI)
	}
	if !server.initialized {
		t.Fatal("expected server to be initialized")
	}
}

func TestShowMessageNotification(t *testing.T) {
	server := newTestServer(t)
	var output bytes.Buffer
	server.output = &output

	server.showMessage(fmt.Errorf("scan result: %s", "ok"))

	req, err := readMessage(bufio.NewReader(strings.NewReader(output.String())))
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if req.Jsonrpc != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", req.Jsonrpc)
	}
	if req.ID != nil {
		t.Fatalf("expected no id, got %v", req.ID)
	}
	if req.Method != "window/showMessage" {
		t.Fatalf("expected method window/showMessage, got %q", req.Method)
	}

	var params ShowMessageParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Type != 2 {
		t.Fatalf("expected message type %d, got %d", 2, params.Type)
	}
	if params.Message != "scan result: ok" {
		t.Fatalf("expected message %q, got %q", "scan result: ok", params.Message)
	}
}

func TestInitializeRejectsMissingTagfile(t *testing.T) {
	tempDir := t.TempDir()
	server := newTestServer(t)
	server.tagfilePath = "missing.tags"

	resp := initializeServerWithParamsRaw(t, server, InitializeParams{
		RootURI: pathToFileURI(tempDir),
	})
	errorResp := parseLSPErrorResponse(t, resp)

	if errorResp.Error == nil || errorResp.Error.Code != -32602 {
		t.Fatalf("expected invalid params error, got %#v", errorResp.Error)
	}
	if !strings.Contains(fmt.Sprint(errorResp.Error.Data), "missing.tags") {
		t.Fatalf("expected error data to mention missing tagfile, got %v", errorResp.Error.Data)
	}
	if server.initialized {
		t.Fatal("expected server initialization to fail for missing tagfile")
	}
}

func TestDidOpenSetsRootURIFromMarker(t *testing.T) {
	rootDir := t.TempDir()
	markerPath := filepath.Join(rootDir, ".git")
	if err := os.WriteFile(markerPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	nestedDir := filepath.Join(rootDir, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	sourcePath := filepath.Join(nestedDir, "rooted.go")
	source := []byte("package demo\n\ntype Rooted struct{}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	server := newTestServer(t)
	fileURI := pathToFileURI(sourcePath)
	openDocument(t, server, fileURI, string(source))

	expectedRootURI := pathToFileURI(rootDir)
	if server.rootURI != expectedRootURI {
		t.Fatalf("expected root uri %q, got %q", expectedRootURI, server.rootURI)
	}
	if _, ok := server.rootlessTags[fileURI]; ok {
		t.Fatal("expected no rootless tags for rooted file")
	}
}

func TestDidOpenRootlessWithoutMarker(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "rootless.go")
	source := []byte("package demo\n\ntype Rootless struct{}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	server := newTestServer(t)
	fileURI := pathToFileURI(sourcePath)
	openDocument(t, server, fileURI, string(source))

	if server.rootURI != "" {
		t.Fatalf("expected empty root uri, got %q", server.rootURI)
	}
	if !hasTag(server.rootlessTags[fileURI], "Rootless", fileURI) {
		t.Fatal("expected rootless tags for opened file")
	}
}

func TestDidOpenRootlessOutsideWorkspace(t *testing.T) {
	rootDir := t.TempDir()
	rootedPath := filepath.Join(rootDir, "rooted.go")
	if err := os.WriteFile(rootedPath, []byte("package demo\n\ntype RootSymbol struct{}\n"), 0o644); err != nil {
		t.Fatalf("write rooted file: %v", err)
	}

	server := newTestServer(t)
	rootURI := pathToFileURI(rootDir)
	if err := server.setRootURI(rootURI); err != nil {
		t.Fatalf("set root uri: %v", err)
	}

	otherDir := t.TempDir()
	sourcePath := filepath.Join(otherDir, "outside.go")
	source := []byte("package demo\n\ntype Outside struct{}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	fileURI := pathToFileURI(sourcePath)
	openDocument(t, server, fileURI, string(source))

	if server.rootURI != rootURI {
		t.Fatalf("expected root uri %q, got %q", rootURI, server.rootURI)
	}
	if !hasTag(server.rootlessTags[fileURI], "Outside", fileURI) {
		t.Fatal("expected rootless tags for file outside workspace")
	}
}

func TestWorkspaceSymbolsOrderedByPathAndLine(t *testing.T) {
	tempDir := t.TempDir()
	aPath := filepath.Join(tempDir, "a.go")
	bPath := filepath.Join(tempDir, "b.go")

	aURI := pathToFileURI(aPath)
	bURI := pathToFileURI(bPath)

	server := newTestServer(t)
	server.rootURI = pathToFileURI(tempDir)
	server.cache.content[aURI] = strings.Split("package demo\n\nfunc Alpha() {}\n\nfunc Beta() {}\n", "\n")
	server.cache.content[bURI] = strings.Split("package demo\n\nfunc Gamma() {}\n", "\n")
	server.tagEntries = []TagEntry{
		{Name: "Beta", Path: aURI, Kind: "fn", Line: 5},
		{Name: "Alpha", Path: aURI, Kind: "fn", Line: 3},
		{Name: "Gamma", Path: bURI, Kind: "fn", Line: 3},
	}

	params := WorkspaceSymbolParams{Query: ""}
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	id := json.RawMessage("1")
	req := RPCRequest{ID: &id, Params: paramsBytes}
	var output bytes.Buffer
	server.output = &output
	handleWorkspaceSymbol(server, req)

	resp := parseLSPResult(t, output.String())
	var symbols []SymbolInformation
	if err := json.Unmarshal(resp.Result, &symbols); err != nil {
		t.Fatalf("unmarshal symbols: %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}
	got := []string{symbols[0].Name, symbols[1].Name, symbols[2].Name}
	want := []string{"Alpha", "Beta", "Gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected symbol order %v, got %v", want, got)
	}
}

func TestDocumentSymbolsOrderedByLine(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "symbols.go")
	source := []byte("package demo\n\nfunc Alpha() {}\n\nfunc Beta() {}\n\nfunc Gamma() {}\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	fileURI := pathToFileURI(sourcePath)
	server := newTestServer(t)
	server.rootURI = pathToFileURI(tempDir)
	server.tagEntries = []TagEntry{
		{Name: "Gamma", Path: fileURI, Kind: "fn", Line: 7},
		{Name: "Alpha", Path: fileURI, Kind: "fn", Line: 3},
		{Name: "Beta", Path: fileURI, Kind: "fn", Line: 5},
	}

	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI},
	}
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	id := json.RawMessage("1")
	req := RPCRequest{ID: &id, Params: paramsBytes}
	var output bytes.Buffer
	server.output = &output
	handleDocumentSymbol(server, req)

	resp := parseLSPResult(t, output.String())
	var symbols []SymbolInformation
	if err := json.Unmarshal(resp.Result, &symbols); err != nil {
		t.Fatalf("unmarshal symbols: %v", err)
	}
	if len(symbols) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(symbols))
	}
	got := []string{symbols[0].Name, symbols[1].Name, symbols[2].Name}
	want := []string{"Alpha", "Beta", "Gamma"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected symbol order %v, got %v", want, got)
	}
}

func TestNormalizePath(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("relative path", func(t *testing.T) {
		raw := filepath.Join("subdir", "nested", "..", "file.go")
		got := normalizePath(baseDir, raw)

		want := filepath.Join(baseDir, "subdir", "file.go")
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		raw := filepath.Join(baseDir, "dir", "..", "file.go")
		got := normalizePath(baseDir, raw)

		want := filepath.Join(baseDir, "file.go")
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}

func TestPathToFileURI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested dir", "file#1.go")
	got := pathToFileURI(path)
	want := "file://" + encodePathForTest(path)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFileURIToPathPercentDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space dir", "file#1.go")
	uri := "file://" + encodePathForTest(path)
	normalizedURI, err := normalizeFileURI(uri)
	if err != nil {
		t.Fatalf("normalizeFileURI: %v", err)
	}
	got := fileURIToPath(normalizedURI)
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestNormalizeFileURICleansPath(t *testing.T) {
	baseDir := t.TempDir()
	baseURI := pathToFileURI(baseDir)
	rawURI := baseURI + "/dir%20name/../file.go"
	normalizedURI, err := normalizeFileURI(rawURI)
	if err != nil {
		t.Fatalf("normalizeFileURI: %v", err)
	}
	want := pathToFileURI(filepath.Join(baseDir, "file.go"))
	if normalizedURI != want {
		t.Fatalf("expected %q, got %q", want, normalizedURI)
	}
}

func TestNormalizeFileURIInvalidEscape(t *testing.T) {
	_, err := normalizeFileURI("file://%ZZ")
	if err == nil {
		t.Fatal("expected error for invalid escape sequence")
	}
}

func TestNormalizeFileURIEmptyPath(t *testing.T) {
	_, err := normalizeFileURI("file://")
	if err == nil {
		t.Fatal("expected error for empty file URI")
	}
}

func TestNormalizeFileURIEmptyString(t *testing.T) {
	_, err := normalizeFileURI("")
	if err == nil {
		t.Fatal("expected error for empty file URI")
	}
}

// -- ### Ctags Integration ###
// -- Tests for workspace scanning and file discovery

func TestScanWorkspaceExplicitTagfile(t *testing.T) {
	tempDir := t.TempDir()

	sourcePath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte("package demo\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	tagfilePath := filepath.Join(tempDir, "tags")
	tagfileEntryPath := filepath.Join(tempDir, "tagfile_only.go")
	tagfileContent := "TagfileOnly\t" + filepath.Base(tagfileEntryPath) + "\t1;\"\n"
	if err := os.WriteFile(tagfilePath, []byte(tagfileContent), 0o644); err != nil {
		t.Fatalf("write tagfile: %v", err)
	}

	t.Run("ignored without explicit flag", func(t *testing.T) {
		server := newTestServer(t)
		server.rootURI = pathToFileURI(tempDir)
		if err := server.scanWorkspace(server.rootURI); err != nil {
			t.Fatalf("scan workspace: %v", err)
		}
		if hasTag(server.tagEntries, "TagfileOnly", pathToFileURI(tagfileEntryPath)) {
			t.Fatal("expected tagfile entry to be ignored without --tagfile")
		}
	})

	t.Run("uses explicit tagfile", func(t *testing.T) {
		server := newTestServer(t)
		server.rootURI = pathToFileURI(tempDir)
		server.tagfilePath = tagfilePath
		if err := server.scanWorkspace(server.rootURI); err != nil {
			t.Fatalf("scan workspace: %v", err)
		}
		if !hasTag(server.tagEntries, "TagfileOnly", pathToFileURI(tagfileEntryPath)) {
			t.Fatal("expected tagfile entry to be loaded with --tagfile")
		}
	})

	t.Run("uses explicit relative tagfile", func(t *testing.T) {
		server := newTestServer(t)
		server.rootURI = pathToFileURI(tempDir)
		server.tagfilePath = filepath.Base(tagfilePath)
		if err := server.scanWorkspace(server.rootURI); err != nil {
			t.Fatalf("scan workspace: %v", err)
		}
		if !hasTag(server.tagEntries, "TagfileOnly", pathToFileURI(tagfileEntryPath)) {
			t.Fatal("expected tagfile entry to be loaded with relative --tagfile")
		}
	})
}

func TestScanWorkspaceMissingExplicitTagfile(t *testing.T) {
	tempDir := t.TempDir()

	sourcePath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte("package demo\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	server := newTestServer(t)
	server.rootURI = pathToFileURI(tempDir)
	server.tagfilePath = "missing.tags"

	if err := server.scanWorkspace(server.rootURI); err == nil {
		t.Fatal("expected scan workspace to fail for missing tagfile")
	}
}

func TestScanWorkspaceWorkerFailure(t *testing.T) {
	tempDir := t.TempDir()

	sourcePath := filepath.Join(tempDir, "main.go")
	if err := os.WriteFile(sourcePath, []byte("package demo\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	server := newTestServer(t)
	server.rootURI = pathToFileURI(tempDir)
	server.ctagsBin = "ctags-does-not-exist"

	if err := server.scanWorkspace(server.rootURI); err == nil {
		t.Fatal("expected scan workspace to fail when ctags cannot be executed")
	}
}

func TestBuildCtagsChunksBySize(t *testing.T) {
	tempDir := t.TempDir()

	type fileSpec struct {
		name string
		size int
	}

	specs := []fileSpec{
		{name: "large.go", size: 200},
		{name: "medium.go", size: 150},
		{name: "small.go", size: 20},
		{name: "tiny.go", size: 10},
	}

	files := make([]string, 0, len(specs))
	for _, spec := range specs {
		path := filepath.Join(tempDir, spec.name)
		if err := os.WriteFile(path, bytes.Repeat([]byte("a"), spec.size), 0o644); err != nil {
			t.Fatalf("write %s: %v", spec.name, err)
		}
		files = append(files, spec.name)
	}

	chunks := buildCtagsChunksBySize(tempDir, files, 2)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !reflect.DeepEqual(chunks[0], []string{"large.go", "small.go"}) {
		t.Fatalf("unexpected first chunk: %v", chunks[0])
	}
	if !reflect.DeepEqual(chunks[1], []string{"medium.go", "tiny.go"}) {
		t.Fatalf("unexpected second chunk: %v", chunks[1])
	}
}

func TestListWorkspaceFilesCommandOrder(t *testing.T) {
	t.Run("fd wins", func(t *testing.T) {
		workspaceRoot := t.TempDir()
		binDir := t.TempDir()
		writeScript(t, binDir, "fd", "echo \"fd_file.go\"")
		writeScript(t, binDir, "rg", "exit 1")
		writeScript(t, binDir, "git", "exit 1")
		t.Setenv("PATH", binDir)

		files, err := listWorkspaceFiles(workspaceRoot)
		if err != nil {
			t.Fatalf("list workspace files: %v", err)
		}
		if !reflect.DeepEqual(files, []string{"fd_file.go"}) {
			t.Fatalf("expected fd output, got %v", files)
		}
	})

	t.Run("rg fallback", func(t *testing.T) {
		workspaceRoot := t.TempDir()
		binDir := t.TempDir()
		writeScript(t, binDir, "fd", "exit 1")
		writeScript(t, binDir, "rg", "printf \"%s\\n\" \"rg_file.go\" \"rg_other.go\"")
		writeScript(t, binDir, "git", "exit 1")
		t.Setenv("PATH", binDir)

		files, err := listWorkspaceFiles(workspaceRoot)
		if err != nil {
			t.Fatalf("list workspace files: %v", err)
		}
		if !reflect.DeepEqual(files, []string{"rg_file.go", "rg_other.go"}) {
			t.Fatalf("expected rg output, got %v", files)
		}
	})

	t.Run("fd empty output returns error", func(t *testing.T) {
		workspaceRoot := t.TempDir()
		binDir := t.TempDir()
		writeScript(t, binDir, "fd", "exit 0")
		writeScript(t, binDir, "rg", "exit 1")
		writeScript(t, binDir, "git", "exit 1")
		t.Setenv("PATH", binDir)

		if _, err := listWorkspaceFiles(workspaceRoot); err == nil {
			t.Fatalf("expected error for empty workspace output")
		}
	})

	t.Run("walkdir fallback", func(t *testing.T) {
		workspaceRoot := t.TempDir()
		binDir := t.TempDir()
		writeScript(t, binDir, "fd", "exit 1")
		writeScript(t, binDir, "rg", "exit 1")
		writeScript(t, binDir, "git", "exit 1")
		t.Setenv("PATH", binDir)

		firstPath := filepath.Join(workspaceRoot, "first.go")
		if err := os.WriteFile(firstPath, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write first file: %v", err)
		}
		subDir := filepath.Join(workspaceRoot, "subdir")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		secondPath := filepath.Join(subDir, "second.go")
		if err := os.WriteFile(secondPath, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write second file: %v", err)
		}

		files, err := listWorkspaceFiles(workspaceRoot)
		if err != nil {
			t.Fatalf("list workspace files: %v", err)
		}

		sort.Strings(files)
		expected := []string{firstPath, secondPath}
		sort.Strings(expected)
		if !reflect.DeepEqual(files, expected) {
			t.Fatalf("expected walkdir output %v, got %v", expected, files)
		}
	})

	t.Run("walkdir empty workspace returns error", func(t *testing.T) {
		workspaceRoot := t.TempDir()
		binDir := t.TempDir()
		writeScript(t, binDir, "fd", "exit 1")
		writeScript(t, binDir, "rg", "exit 1")
		writeScript(t, binDir, "git", "exit 1")
		t.Setenv("PATH", binDir)

		if _, err := listWorkspaceFiles(workspaceRoot); err == nil {
			t.Fatalf("expected error for empty workspace")
		}
	})
}

// -- ### Ctags to LSP ###
// -- Tests for ctags kind to LSP symbol kind mappings

func TestGetLSPSymbolKindMappings(t *testing.T) {
	tests := map[string]int{
		"boolean": SymbolKindBoolean,
		"integer": SymbolKindNumber,
		"string":  SymbolKindString,
	}

	for kind, expected := range tests {
		got, err := GetLSPSymbolKind(kind)
		if err != nil {
			t.Fatalf("GetLSPSymbolKind(%q): %v", kind, err)
		}
		if got != expected {
			t.Fatalf("GetLSPSymbolKind(%q): expected %d, got %d", kind, expected, got)
		}
	}
}

func TestGetLSPCompletionKindMappings(t *testing.T) {
	tests := map[string]int{
		"boolean": CompletionItemKindValue,
		"number":  CompletionItemKindValue,
		"string":  CompletionItemKindValue,
	}

	for kind, expected := range tests {
		got := GetLSPCompletionKind(kind)
		if got != expected {
			t.Fatalf("GetLSPCompletionKind(%q): expected %d, got %d", kind, expected, got)
		}
	}
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	contents := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}
