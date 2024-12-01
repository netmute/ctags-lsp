package main

import "fmt"

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

// LSP Symbol Kind Constants
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

// kindMap defines the mapping from ctags kinds to LSP completion item kinds
var kindMap = map[string]int{
	"alias":            CompletionItemKindVariable,
	"arg":              CompletionItemKindVariable,
	"attribute":        CompletionItemKindProperty,
	"boolean":          CompletionItemKindConstant,
	"callback":         CompletionItemKindFunction,
	"category":         CompletionItemKindEnum,
	"ccflag":           CompletionItemKindConstant,
	"cell":             CompletionItemKindVariable,
	"class":            CompletionItemKindClass,
	"collection":       CompletionItemKindClass,
	"command":          CompletionItemKindFunction,
	"component":        CompletionItemKindStruct,
	"config":           CompletionItemKindConstant,
	"const":            CompletionItemKindConstant,
	"constant":         CompletionItemKindConstant,
	"constructor":      CompletionItemKindConstructor,
	"context":          CompletionItemKindVariable,
	"counter":          CompletionItemKindVariable,
	"data":             CompletionItemKindVariable,
	"dataset":          CompletionItemKindVariable,
	"def":              CompletionItemKindFunction,
	"define":           CompletionItemKindConstant,
	"delegate":         CompletionItemKindClass,
	"enum":             CompletionItemKindEnum,
	"enumConstant":     CompletionItemKindEnumMember,
	"enumerator":       CompletionItemKindEnum,
	"environment":      CompletionItemKindVariable,
	"error":            CompletionItemKindEnum,
	"event":            CompletionItemKindEvent,
	"exception":        CompletionItemKindClass,
	"externvar":        CompletionItemKindVariable,
	"face":             CompletionItemKindInterface,
	"feature":          CompletionItemKindProperty,
	"field":            CompletionItemKindField,
	"fn":               CompletionItemKindFunction,
	"fun":              CompletionItemKindFunction,
	"func":             CompletionItemKindFunction,
	"function":         CompletionItemKindFunction,
	"functionVar":      CompletionItemKindVariable,
	"functor":          CompletionItemKindClass,
	"generic":          CompletionItemKindTypeParameter,
	"getter":           CompletionItemKindMethod,
	"global":           CompletionItemKindVariable,
	"globalVar":        CompletionItemKindVariable,
	"group":            CompletionItemKindEnum,
	"guard":            CompletionItemKindVariable,
	"handler":          CompletionItemKindFunction,
	"icon":             CompletionItemKindEnum,
	"id":               CompletionItemKindVariable,
	"implementation":   CompletionItemKindClass,
	"index":            CompletionItemKindVariable,
	"infoitem":         CompletionItemKindVariable,
	"inline":           CompletionItemKindKeyword,
	"inputSection":     CompletionItemKindKeyword,
	"instance":         CompletionItemKindVariable,
	"interface":        CompletionItemKindInterface,
	"it":               CompletionItemKindVariable,
	"jurisdiction":     CompletionItemKindVariable,
	"key":              CompletionItemKindKeyword,
	"keyInMiddle":      CompletionItemKindKeyword,
	"keyword":          CompletionItemKindKeyword,
	"kind":             CompletionItemKindKeyword,
	"l4subsection":     CompletionItemKindKeyword,
	"l5subsection":     CompletionItemKindKeyword,
	"label":            CompletionItemKindKeyword,
	"langdef":          CompletionItemKindKeyword,
	"legal":            CompletionItemKindKeyword,
	"legislation":      CompletionItemKindKeyword,
	"letter":           CompletionItemKindKeyword,
	"library":          CompletionItemKindModule,
	"list":             CompletionItemKindVariable,
	"local":            CompletionItemKindVariable,
	"localVariable":    CompletionItemKindVariable,
	"locale":           CompletionItemKindVariable,
	"localvar":         CompletionItemKindVariable,
	"macro":            CompletionItemKindVariable,
	"macroParameter":   CompletionItemKindVariable,
	"macrofile":        CompletionItemKindFile,
	"macroparam":       CompletionItemKindVariable,
	"makefile":         CompletionItemKindFile,
	"map":              CompletionItemKindVariable,
	"method":           CompletionItemKindMethod,
	"methodSpec":       CompletionItemKindMethod,
	"minorMode":        CompletionItemKindKeyword,
	"misc":             CompletionItemKindVariable,
	"module":           CompletionItemKindModule,
	"name":             CompletionItemKindVariable,
	"namespace":        CompletionItemKindModule,
	"nettype":          CompletionItemKindTypeParameter,
	"newFile":          CompletionItemKindFile,
	"node":             CompletionItemKindVariable,
	"object":           CompletionItemKindClass,
	"oneof":            CompletionItemKindEnum,
	"operator":         CompletionItemKindOperator,
	"option":           CompletionItemKindKeyword,
	"output":           CompletionItemKindVariable,
	"package":          CompletionItemKindModule,
	"param":            CompletionItemKindVariable,
	"parameter":        CompletionItemKindVariable,
	"paramEntity":      CompletionItemKindVariable,
	"part":             CompletionItemKindVariable,
	"pattern":          CompletionItemKindKeyword,
	"placeholder":      CompletionItemKindVariable,
	"port":             CompletionItemKindVariable,
	"process":          CompletionItemKindFunction,
	"property":         CompletionItemKindProperty,
	"prototype":        CompletionItemKindVariable,
	"protocol":         CompletionItemKindClass,
	"provider":         CompletionItemKindClass,
	"publication":      CompletionItemKindVariable,
	"qkey":             CompletionItemKindVariable,
	"receiver":         CompletionItemKindVariable,
	"record":           CompletionItemKindStruct,
	"reference":        CompletionItemKindReference,
	"region":           CompletionItemKindVariable,
	"register":         CompletionItemKindVariable,
	"repoid":           CompletionItemKindVariable,
	"report":           CompletionItemKindVariable,
	"repositoryId":     CompletionItemKindVariable,
	"repr":             CompletionItemKindVariable,
	"resource":         CompletionItemKindVariable,
	"response":         CompletionItemKindFunction,
	"role":             CompletionItemKindClass,
	"rpc":              CompletionItemKindVariable,
	"schema":           CompletionItemKindVariable,
	"script":           CompletionItemKindFile,
	"section":          CompletionItemKindKeyword,
	"selector":         CompletionItemKindKeyword,
	"sequence":         CompletionItemKindVariable,
	"server":           CompletionItemKindClass,
	"service":          CompletionItemKindClass,
	"setter":           CompletionItemKindMethod,
	"signal":           CompletionItemKindFunction,
	"singletonMethod":  CompletionItemKindMethod,
	"slot":             CompletionItemKindVariable,
	"software":         CompletionItemKindClass,
	"sourcefile":       CompletionItemKindFile,
	"standard":         CompletionItemKindVariable,
	"string":           CompletionItemKindText,
	"structure":        CompletionItemKindStruct,
	"stylesheet":       CompletionItemKindVariable,
	"subdir":           CompletionItemKindFolder,
	"submethod":        CompletionItemKindMethod,
	"submodule":        CompletionItemKindModule,
	"subprogram":       CompletionItemKindFunction,
	"subprogspec":      CompletionItemKindVariable,
	"subroutine":       CompletionItemKindFunction,
	"subsection":       CompletionItemKindVariable,
	"subst":            CompletionItemKindVariable,
	"substdef":         CompletionItemKindVariable,
	"tag":              CompletionItemKindVariable,
	"template":         CompletionItemKindVariable,
	"test":             CompletionItemKindVariable,
	"theme":            CompletionItemKindVariable,
	"theorem":          CompletionItemKindVariable,
	"thriftFile":       CompletionItemKindFile,
	"throwsparam":      CompletionItemKindVariable,
	"title":            CompletionItemKindVariable,
	"token":            CompletionItemKindVariable,
	"toplevelVariable": CompletionItemKindVariable,
	"trait":            CompletionItemKindVariable,
	"type":             CompletionItemKindStruct,
	"typealias":        CompletionItemKindVariable,
	"typedef":          CompletionItemKindTypeParameter,
	"typespec":         CompletionItemKindTypeParameter,
	"union":            CompletionItemKindStruct,
	"unit":             CompletionItemKindUnit,
	"username":         CompletionItemKindVariable,
	"val":              CompletionItemKindVariable,
	"value":            CompletionItemKindVariable,
	"var":              CompletionItemKindVariable,
	"variable":         CompletionItemKindVariable,
	"vector":           CompletionItemKindVariable,
	"version":          CompletionItemKindVariable,
	"video":            CompletionItemKindFile,
	"view":             CompletionItemKindVariable,
	"wrapper":          CompletionItemKindVariable,
	"xdata":            CompletionItemKindVariable,
	"xinput":           CompletionItemKindVariable,
	"xtask":            CompletionItemKindVariable,
}

// symbolKindMap defines the mapping from ctags kinds to LSP symbol kinds
var symbolKindMap = map[string]int{
	"alias":            SymbolKindVariable,
	"arg":              SymbolKindVariable,
	"attribute":        SymbolKindProperty,
	"boolean":          SymbolKindConstant,
	"callback":         SymbolKindFunction,
	"category":         SymbolKindEnum,
	"ccflag":           SymbolKindConstant,
	"cell":             SymbolKindVariable,
	"class":            SymbolKindClass,
	"collection":       SymbolKindClass,
	"command":          SymbolKindFunction,
	"component":        SymbolKindStruct,
	"config":           SymbolKindConstant,
	"const":            SymbolKindConstant,
	"constant":         SymbolKindConstant,
	"constructor":      SymbolKindConstructor,
	"context":          SymbolKindVariable,
	"counter":          SymbolKindVariable,
	"data":             SymbolKindVariable,
	"dataset":          SymbolKindVariable,
	"def":              SymbolKindFunction,
	"define":           SymbolKindConstant,
	"delegate":         SymbolKindClass,
	"enum":             SymbolKindEnum,
	"enumConstant":     SymbolKindEnumMember,
	"enumerator":       SymbolKindEnum,
	"environment":      SymbolKindVariable,
	"error":            SymbolKindEnum,
	"event":            SymbolKindEvent,
	"exception":        SymbolKindClass,
	"externvar":        SymbolKindVariable,
	"face":             SymbolKindInterface,
	"feature":          SymbolKindProperty,
	"field":            SymbolKindField,
	"fn":               SymbolKindFunction,
	"fun":              SymbolKindFunction,
	"func":             SymbolKindFunction,
	"function":         SymbolKindFunction,
	"functionVar":      SymbolKindVariable,
	"functor":          SymbolKindClass,
	"generic":          SymbolKindTypeParameter,
	"getter":           SymbolKindMethod,
	"global":           SymbolKindVariable,
	"globalVar":        SymbolKindVariable,
	"group":            SymbolKindEnum,
	"guard":            SymbolKindVariable,
	"handler":          SymbolKindFunction,
	"icon":             SymbolKindEnum,
	"id":               SymbolKindVariable,
	"implementation":   SymbolKindClass,
	"index":            SymbolKindVariable,
	"infoitem":         SymbolKindVariable,
	"instance":         SymbolKindVariable,
	"interface":        SymbolKindInterface,
	"it":               SymbolKindVariable,
	"jurisdiction":     SymbolKindVariable,
	"library":          SymbolKindModule,
	"list":             SymbolKindVariable,
	"local":            SymbolKindVariable,
	"localVariable":    SymbolKindVariable,
	"locale":           SymbolKindVariable,
	"localvar":         SymbolKindVariable,
	"macro":            SymbolKindVariable,
	"macroParameter":   SymbolKindVariable,
	"macrofile":        SymbolKindFile,
	"macroparam":       SymbolKindVariable,
	"makefile":         SymbolKindFile,
	"map":              SymbolKindVariable,
	"method":           SymbolKindMethod,
	"methodSpec":       SymbolKindMethod,
	"misc":             SymbolKindVariable,
	"module":           SymbolKindModule,
	"name":             SymbolKindVariable,
	"namespace":        SymbolKindModule,
	"nettype":          SymbolKindTypeParameter,
	"newFile":          SymbolKindFile,
	"node":             SymbolKindVariable,
	"object":           SymbolKindClass,
	"oneof":            SymbolKindEnum,
	"operator":         SymbolKindOperator,
	"output":           SymbolKindVariable,
	"package":          SymbolKindModule,
	"param":            SymbolKindVariable,
	"parameter":        SymbolKindVariable,
	"paramEntity":      SymbolKindVariable,
	"part":             SymbolKindVariable,
	"placeholder":      SymbolKindVariable,
	"port":             SymbolKindVariable,
	"process":          SymbolKindFunction,
	"property":         SymbolKindProperty,
	"prototype":        SymbolKindVariable,
	"protocol":         SymbolKindClass,
	"provider":         SymbolKindClass,
	"publication":      SymbolKindVariable,
	"qkey":             SymbolKindVariable,
	"receiver":         SymbolKindVariable,
	"record":           SymbolKindStruct,
	"region":           SymbolKindVariable,
	"register":         SymbolKindVariable,
	"repoid":           SymbolKindVariable,
	"report":           SymbolKindVariable,
	"repositoryId":     SymbolKindVariable,
	"repr":             SymbolKindVariable,
	"resource":         SymbolKindVariable,
	"response":         SymbolKindFunction,
	"role":             SymbolKindClass,
	"rpc":              SymbolKindVariable,
	"schema":           SymbolKindVariable,
	"script":           SymbolKindFile,
	"sequence":         SymbolKindVariable,
	"server":           SymbolKindClass,
	"service":          SymbolKindClass,
	"setter":           SymbolKindMethod,
	"signal":           SymbolKindFunction,
	"singletonMethod":  SymbolKindMethod,
	"slot":             SymbolKindVariable,
	"software":         SymbolKindClass,
	"sourcefile":       SymbolKindFile,
	"standard":         SymbolKindVariable,
	"string":           SymbolKindString,
	"structure":        SymbolKindStruct,
	"stylesheet":       SymbolKindVariable,
	"submethod":        SymbolKindMethod,
	"submodule":        SymbolKindModule,
	"subprogram":       SymbolKindFunction,
	"subprogspec":      SymbolKindVariable,
	"subroutine":       SymbolKindFunction,
	"subsection":       SymbolKindVariable,
	"subst":            SymbolKindVariable,
	"substdef":         SymbolKindVariable,
	"tag":              SymbolKindVariable,
	"template":         SymbolKindVariable,
	"test":             SymbolKindVariable,
	"theme":            SymbolKindVariable,
	"theorem":          SymbolKindVariable,
	"thriftFile":       SymbolKindFile,
	"throwsparam":      SymbolKindVariable,
	"title":            SymbolKindVariable,
	"token":            SymbolKindVariable,
	"toplevelVariable": SymbolKindVariable,
	"trait":            SymbolKindVariable,
	"type":             SymbolKindStruct,
	"typealias":        SymbolKindVariable,
	"typedef":          SymbolKindTypeParameter,
	"typespec":         SymbolKindTypeParameter,
	"union":            SymbolKindStruct,
	"username":         SymbolKindVariable,
	"val":              SymbolKindVariable,
	"value":            SymbolKindVariable,
	"var":              SymbolKindVariable,
	"variable":         SymbolKindVariable,
	"vector":           SymbolKindVariable,
	"version":          SymbolKindVariable,
	"video":            SymbolKindFile,
	"view":             SymbolKindVariable,
	"wrapper":          SymbolKindVariable,
	"xdata":            SymbolKindVariable,
	"xinput":           SymbolKindVariable,
	"xtask":            SymbolKindVariable,
}

// GetLSPCompletionKind retrieves the corresponding LSP completion item kind for a given ctags kind string
func GetLSPCompletionKind(ctagsKind string) int {
	if kind, ok := kindMap[ctagsKind]; ok {
		return kind
	}
	return CompletionItemKindText // Default to Text if no match is found
}

// GetLSPSymbolKind retrieves the corresponding LSP symbol kind for a given ctags kind string
func GetLSPSymbolKind(ctagsKind string) (int, error) {
	if kind, ok := symbolKindMap[ctagsKind]; ok {
		return kind, nil
	}

	return 0, fmt.Errorf("no symbol kind for: %v", ctagsKind)
}
