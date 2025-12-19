// tagfile_parser parses ctags tagfiles into TagEntry records, including kind resolution
// and path normalization so tagfile entries match the JSON ctags output shape.
package main

import (
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
)

type tagfileKindMap struct {
	byLanguage map[string]map[string]string
	any        map[string]string
	kindNames  map[string]bool
}

// newTagfileKindMap initializes a kind map for tagfile kind letter resolution.
func newTagfileKindMap() *tagfileKindMap {
	return &tagfileKindMap{
		byLanguage: make(map[string]map[string]string),
		any:        make(map[string]string),
		kindNames:  make(map[string]bool),
	}
}

// add stores a kind letter mapping for a language and tracks the kind name.
func (m *tagfileKindMap) add(language, letter, kind string) {
	if language == "" {
		language = "default"
	}
	if _, ok := m.byLanguage[language]; !ok {
		m.byLanguage[language] = make(map[string]string)
	}
	m.byLanguage[language][letter] = kind
	if _, ok := m.any[letter]; !ok {
		m.any[letter] = kind
	}
	m.kindNames[kind] = true
}

// resolve returns the kind name for a kind letter using language-specific or default mappings.
func (m *tagfileKindMap) resolve(language, letter string) (string, bool) {
	if language != "" {
		if byLang, ok := m.byLanguage[language]; ok {
			if kind, ok := byLang[letter]; ok {
				return kind, true
			}
		}
	}
	if kind, ok := m.any[letter]; ok {
		return kind, true
	}
	return "", false
}

// isKindName reports whether a kind name exists in the tagfile metadata.
func (m *tagfileKindMap) isKindName(kind string) bool {
	return m.kindNames[kind]
}

// parseTagfile reads a ctags tagfile and returns entries in the same shape as processTagsOutput.
func parseTagfile(tagsPath, rootPath string) ([]TagEntry, error) {
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

		entry, ok := parseTagfileEntry(line, tagsPath, rootPath, kindMap)
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

// parseTagfileEntry parses a tagfile line into a TagEntry, skipping invalid or out-of-root entries.
func parseTagfileEntry(line, tagsPath, rootPath string, kindMap *tagfileKindMap) (TagEntry, bool) {
	fields := strings.Split(line, "\t")
	if len(fields) < 3 {
		return TagEntry{}, false
	}

	entry := TagEntry{
		Type:    "tag",
		Name:    fields[0],
		Path:    fields[1],
		Pattern: strings.TrimSuffix(fields[2], ";\""),
	}

	kindField := ""
	next := 3
	if len(fields) > 3 && !strings.Contains(fields[3], ":") {
		kindField = fields[3]
		next = 4
	}

	for _, field := range fields[next:] {
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
			entry.Language = value
		case "kind":
			kindField = value
		case "typeref":
			entry.TypeRef = value
		case "scope":
			entry.Scope = value
		case "scopeKind":
			entry.ScopeKind = value
		default:
			if entry.Scope == "" && entry.ScopeKind == "" && kindMap.isKindName(key) {
				entry.ScopeKind = key
				entry.Scope = value
			}
		}
	}

	if entry.Line == 0 {
		if lineNum, err := strconv.Atoi(entry.Pattern); err == nil {
			entry.Line = lineNum
		}
	}

	if kindField != "" {
		kindField = resolveTagfileKind(kindField, &entry, kindMap)
		entry.Kind = kindField
	}

	relPath, err := tagfilePathToRootRelative(rootPath, tagsPath, entry.Path)
	if err != nil {
		log.Printf("Failed to make path relative for %s: %v", entry.Path, err)
		return TagEntry{}, false
	}
	entry.Path = relPath

	return entry, true
}

// resolveTagfileKind maps a kind letter to its kind name using tagfile metadata.
func resolveTagfileKind(kindField string, entry *TagEntry, kindMap *tagfileKindMap) string {
	if len(kindField) != 1 {
		return kindField
	}

	if mapped, ok := kindMap.resolve(entry.Language, kindField); ok {
		return mapped
	}
	return kindField
}
