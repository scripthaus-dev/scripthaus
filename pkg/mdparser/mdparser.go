// Copyright 2022 Dashborg Inc
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

	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
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

func isValidName(name string) bool {
	return validNameRe.MatchString(name)
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

func isAllowedBlockLanguage(lang string) bool {
	switch lang {
	case "bash", "sh":
		return true

	case "python", "python3":
		return true

	case "node":
		return true

	default:
		return false
	}
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

func ParseCommands(mdSource []byte) ([]commanddef.CommandDef, []string, error) {
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
	var curDef *commanddef.CommandDef

	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		breakNode, _ := node.(*ast.ThematicBreak)
		headingNode, _ := node.(*ast.Heading)
		codeNode, _ := node.(*ast.FencedCodeBlock)

		if breakNode != nil || (headingNode != nil && headingNode.Level <= 4) {
			// command break
			if curDef != nil && curDef.Name != "" {
				warnings = append(warnings, fmt.Sprintf("potential script heading '%s' found with no associated code block (line %d)", curDef.Name, curDef.StartLineNo))
			}
			curDef = nil
		}
		if headingNode != nil && headingNode.Level == 4 {
			child := headingNode.FirstChild()
			if child != nil && child.Kind() == ast.KindCodeSpan {
				startIdx, startLineNo := blockStartIndex(headingNode, mdSource)
				defName := string(child.Text(mdSource))
				if !isValidName(defName) {
					warnings = append(warnings, fmt.Sprintf("potential script heading found but bad script name '%s' is invalid (line %d)", defName, startLineNo))
					continue
				}
				curDef = &commanddef.CommandDef{Name: defName, StartIndex: startIdx, StartLineNo: startLineNo}
			}
		}

		if codeNode != nil {
			infoText := string(codeNode.Info.Text(mdSource))
			lang, blockInfo := parseInfo(infoText)
			if blockInfo["scripthaus"] == "" {
				continue
			}
			if !isAllowedBlockLanguage(lang) {
				lineNo := findLineNo(codeNode.Info.Segment.Start, mdSource)
				warnings = append(warnings, fmt.Sprintf("scripthaus code block found info='%s' with invalid language '%s' (line %d)", infoText, lang, lineNo))
				continue
			}
			if curDef == nil {
				lineNo := findLineNo(codeNode.Info.Segment.Start, mdSource)
				warnings = append(warnings, fmt.Sprintf("scripthaus code block found info='%s' with no level 4 heading to name it (line %d)", infoText, lineNo))
				continue
			}
			curDef.Lang = lang
			curDef.ScriptText = textFromLines(mdSource, codeNode.Lines())
			curDef.Info = blockInfo
			cbStartIdx := mdIndexBackToNewLine(codeNode.Info.Segment.Start, mdSource)
			curDef.HelpText = string(mdSource[curDef.StartIndex:cbStartIdx])
			curDef.RawCodeText = rawCodeText(curDef.Name, codeNode, mdSource)
			defs = append(defs, *curDef)
			curDef = nil
			continue
		}
	}
	return defs, warnings, nil
}
