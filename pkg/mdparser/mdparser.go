// Copyright 2023 Michael Sawka
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package mdparser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/scripthaus-dev/scripthaus/pkg/base"
	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	textm "github.com/yuin/goldmark/text"
)

// TODO more efficient (create newline position array, binary search)
// 1-indexed
func findLineNo(pos int, mdSource []byte) int {
	return bytes.Count(mdSource[:pos], []byte{'\n'}) + 1
}

// TODO more efficient
// lineNo is 1-indexed
func findLinePos(lineNo int, mdSource []byte) int {
	if lineNo <= 1 {
		return 0
	}
	curLine := 1
	for idx := 0; idx < len(mdSource); idx++ {
		if mdSource[idx] == '\n' {
			curLine++
			if curLine == lineNo {
				return idx
			}
		}
	}
	return len(mdSource)
}

func mdIndexBackToNewLine(mdIdx int, mdSource []byte) int {
	for ; mdIdx >= 0 && mdSource[mdIdx] != '\n'; mdIdx-- {
	}
	mdIdx++
	return mdIdx
}

var validNameRe = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_-]*$")

func IsValidScriptName(name string) bool {
	return base.PlaybookScriptNameRe.MatchString(name)
}

func parseInfo(info string) (string, map[string]string) {
	split := strings.Split(info, " ")
	language := split[0]

	fields := map[string]string{}
	for _, field := range split[1:] {
		splitField := strings.SplitN(field, "=", 2)
		if len(splitField) > 1 {
			fields[splitField[0]] = splitField[1]
		} else {
			fields[splitField[0]] = "1"
		}
	}

	return language, fields
}

func rawCodeText(name string, block *ast.FencedCodeBlock, mdSource []byte) string {
	lines := block.Lines()
	startPos := mdIndexBackToNewLine(block.Info.Segment.Start, mdSource)
	if lines.Len() == 0 {
		infoLineNo := findLineNo(block.Info.Segment.Start, mdSource)
		endPos := findLinePos(infoLineNo+2, mdSource)
		return string(mdSource[startPos:endPos])
	}
	lastSeg := lines.At(lines.Len() - 1)
	lastCodeLine := findLineNo(lastSeg.Start, mdSource)
	endPos := findLinePos(lastCodeLine+2, mdSource)
	return string(mdSource[startPos:endPos])
}

func textFromLines(mdSource []byte, lines *textm.Segments) string {
	var buf bytes.Buffer
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(mdSource))
	}
	return buf.String()
}

// returns (pos, lineno)
// lineno is 1-indexed
func blockStartIndex(block ast.Node, mdSource []byte) (int, int) {
	if block.Type() != ast.TypeBlock {
		return -1, 0
	}
	segs := block.Lines()
	if segs.Len() == 0 {
		return -1, 0
	}
	mdIdx := mdIndexBackToNewLine(segs.At(0).Start, mdSource)
	return mdIdx, findLineNo(mdIdx, mdSource)
}

var directiveRe = regexp.MustCompile("^(?:#|//)\\s+@scripthaus\\s+(\\S+)(?:\\s+(.*))?")

func ExtractRawDirectives(codeText string) []commanddef.RawDirective {
	var rtn []commanddef.RawDirective
	lines := strings.Split(codeText, "\n")
	for idx, line := range lines {
		m := directiveRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rawDir := commanddef.RawDirective{}
		rawDir.LineNo = idx + 1
		rawDir.Type = m[1]
		rawDir.Data = strings.TrimSpace(m[2])
		rtn = append(rtn, rawDir)
	}
	return rtn
}

var hasDashPrefix = regexp.MustCompile("^\\s+-\\s+(.*)")

func GetCommandDirective(dirs []commanddef.RawDirective) (string, string) {
	for _, dir := range dirs {
		if dir.Type != "command" {
			continue
		}
		firstSpace := strings.Index(dir.Data, " ")
		if firstSpace == -1 {
			return dir.Data, ""
		}
		commandName := dir.Data[0:firstSpace]
		restStr := dir.Data[firstSpace:]
		var commandShortDesc string
		m := hasDashPrefix.FindStringSubmatch(restStr)
		if m != nil {
			commandShortDesc = m[1]
		}
		return commandName, commandShortDesc
	}
	return "", ""
}

func ParseCommands(playbook *pathutil.ResolvedPlaybook, mdSource []byte) ([]commanddef.CommandDef, []string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)
	node := md.Parser().Parse(textm.NewReader(mdSource))
	doc, isDoc := node.(*ast.Document)
	if !isDoc {
		return nil, nil, fmt.Errorf("Invalid MD parse, did not return valid document (type)")
	}

	// interesting things about the goldmark parser and Nodes
	// * FencedCodeBlocks do not have children (they contain Text and Info)
	// * Text always seems to be broken into 2 parts (even for a simple header like "### Hello World"), last word will be in separate node
	// * FencedCodeBlocks will always be children of the document.  If code is inline (single or tripple backticks) it will be a CodeSpan
	// * FencedCodeBlock.Lines() returns only the *inner* text position (maybe outer is in Info text?)
	// * Calling Emphasis.Text() gets only the text, does not include "*" characters
	// * List nodes don't have any Lines()?
	// * ListItems don't have segment info either, only text, so there is no Segment that holds the "*"
	// * rendering the raw markdown text to the console for "help" feels currently impossible with goldmark
	// * gomarkdown is not going to work either because it does not parse fenced code block infos correctly

	var defs []commanddef.CommandDef
	var warnings []string

	breakIdx := -1
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		breakNode, _ := node.(*ast.ThematicBreak)
		headingNode, _ := node.(*ast.Heading)
		codeNode, _ := node.(*ast.FencedCodeBlock)

		if breakNode != nil {
			breakIdx = -1
			continue
		}
		if headingNode != nil && headingNode.Level < 4 {
			breakIdx = -1
			continue
		}
		if headingNode != nil && headingNode.Level == 4 {
			breakIdx, _ = blockStartIndex(headingNode, mdSource)
			continue
		}

		if codeNode != nil && codeNode.Info != nil {
			lineNo := findLineNo(codeNode.Info.Segment.Start, mdSource)
			scriptText := textFromLines(mdSource, codeNode.Lines())
			rawDirs := ExtractRawDirectives(scriptText)
			name, shortDesc := GetCommandDirective(rawDirs)
			if name == "" {
				if len(rawDirs) != 0 {
					warnings = append(warnings, fmt.Sprintf("code block has scripthaus directives, but no 'command' directive (line %d)", lineNo))
				}
				// not a scripthaus code block.  reset breakIdx
				breakIdx = -1
				continue
			}
			// this is a scripthaus code block
			infoText := string(codeNode.Info.Text(mdSource))
			lang, blockInfo := parseInfo(infoText)
			if !base.IsValidScriptType(lang) {
				warnings = append(warnings, fmt.Sprintf("scripthaus code block found info='%s' with invalid language '%s' (line %d)", infoText, lang, lineNo))
				continue
			}
			newDef := &commanddef.CommandDef{Playbook: playbook}
			newDef.Name = name
			newDef.ShortText = shortDesc
			newDef.Lang = lang
			newDef.ScriptText = scriptText
			newDef.Info = blockInfo
			newDef.RawDirectives = rawDirs
			cbStartIdx := mdIndexBackToNewLine(codeNode.Info.Segment.Start, mdSource)
			if breakIdx == -1 {
				newDef.StartIndex = cbStartIdx
				newDef.StartLineNo = findLineNo(cbStartIdx, mdSource)
				// no HelpText in this case
			} else {
				newDef.StartIndex = breakIdx
				newDef.StartLineNo = findLineNo(cbStartIdx, mdSource)
				newDef.HelpText = strings.TrimSpace(string(mdSource[breakIdx:cbStartIdx]))
			}
			newDef.RawCodeText = strings.TrimSpace(rawCodeText(newDef.Name, codeNode, mdSource))
			defs = append(defs, *newDef)
			breakIdx = -1
			continue
		}

		if breakIdx == -1 && node.Type() == ast.TypeBlock {
			breakIdx, _ = blockStartIndex(node, mdSource)
		}

	}
	return defs, warnings, nil
}
