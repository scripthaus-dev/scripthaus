// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package commanddef

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/scripthaus-dev/scripthaus/pkg/history"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"
)

type CommandDef struct {
	Playbook    *pathutil.ResolvedPlaybook
	Name        string
	Lang        string
	ScriptText  string
	Info        map[string]string
	HelpText    string
	ShortText   string
	RawCodeText string

	StartIndex  int
	StartLineNo int // 1-indexed

	// directives
	RawDirectives       []RawDirective
	RequireEnvVars      []string
	DirectivesProcessed bool
	Warnings            []string
}

type RawDirective struct {
	Type   string
	Data   string
	LineNo int
}

type SpecType struct {
	// log to history unless NoLog is set (ForceLog is necessary just for combining specs)
	NoLog    bool
	ForceLog bool

	ScriptArgs []string
	ChangeDir  string

	// matches exec.Cmd (each entry is of form key=value)
	Env []string
}

// holds a script-name or playbook-file/playbook-script
type ScriptDef struct {
	PlaybookFile    string
	PlaybookCommand string
}

func (cdef *CommandDef) FullScriptName() string {
	if cdef.Playbook.CanonicalName == "^" || cdef.Playbook.CanonicalName == "." {
		fmt.Sprintf("%s%s", cdef.Playbook.CanonicalName, cdef.Name)
	}
	return fmt.Sprintf("%s::%s", cdef.Playbook.CanonicalName, cdef.Name)
}

func (cdef *CommandDef) OrigScriptName() string {
	if cdef.Playbook.OrigName == "^" || cdef.Playbook.OrigName == "." || cdef.Playbook.OrigName == "" {
		return fmt.Sprintf("%s%s", cdef.Playbook.OrigName, cdef.Name)
	}
	return fmt.Sprintf("%s::%s", cdef.Playbook.OrigName, cdef.Name)
}

type ExecItem struct {
	CmdName        string
	Cmd            *exec.Cmd
	FullScriptName string
	HItem          *history.HistoryItem
}

func (item *ExecItem) CmdShortName() string {
	return fmt.Sprintf("%s %s", item.CmdName, item.FullScriptName)
}

func (def ScriptDef) FullScriptName() string {
	if def.PlaybookFile != "" && def.PlaybookCommand == "" {
		return def.PlaybookFile
	}
	if def.PlaybookFile != "" && def.PlaybookCommand != "" {
		return fmt.Sprintf("%s/%s", def.PlaybookFile, def.PlaybookCommand)
	}
	return ""
}

func (def ScriptDef) IsEmpty() bool {
	return def.PlaybookFile == "" && def.PlaybookCommand == ""
}

type RunOptsType struct {
	Script  ScriptDef
	RunSpec SpecType // specs can be combined (so they are pulled out separately)
}

func setStandardCmdOpts(cmd *exec.Cmd, runSpec SpecType) {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = makeFullEnv(runSpec)
}

func makeOsFileFromString(s string) (*os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	go func() {
		writer.WriteString(s)
		writer.Close()
	}()
	return reader, nil
}

func ValidScriptTypes() []string {
	return []string{"sh", "bash", "python", "python2", "python3", "js", "node"}
}

func IsValidScriptType(scriptType string) bool {
	switch scriptType {
	case "sh", "bash":
		return true

	case "python", "python2", "python3":
		return true

	case "js", "node":
		return true

	default:
		return false
	}
}

func (cdef *CommandDef) buildNormalCommand(ctx context.Context, runSpec SpecType) (*ExecItem, error) {
	if cdef.Lang == "sh" || cdef.Lang == "bash" {
		args := append([]string{"-c", cdef.ScriptText, cdef.FullScriptName()}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdName: cdef.Lang, Cmd: execCmd}, nil
	} else if cdef.Lang == "python" || cdef.Lang == "python3" || cdef.Lang == "python2" {
		args := append([]string{"-c", cdef.ScriptText}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdName: cdef.Lang, Cmd: execCmd}, nil
	} else if cdef.Lang == "node" || cdef.Lang == "js" {
		args := append([]string{"--eval", cdef.ScriptText, "--"}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, "node", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdName: "node", Cmd: execCmd}, nil
	}
	return nil, fmt.Errorf("invalid command language '%s', not supported", cdef.Lang)
}

func combine(rest ...interface{}) []string {
	var list []string
	for _, item := range rest {
		if item == nil {
			continue
		} else if str, ok := item.(string); ok {
			list = append(list, str)
		} else if strArr, ok := item.([]string); ok {
			list = append(list, strArr...)
		} else {
			fmt.Fprintf(os.Stderr, "INTERNAL-ERROR unexpected type %T in appendStrings\n", item)
		}
	}
	return list
}

var DirectiveRe = regexp.MustCompile("^\\s*(\\S+)(?:\\s+(.*))?$")

// technically env vars can have any character, but in practice, this makes more sense
var EnvVarRe = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]*$")

func (cdef *CommandDef) processDirective(dirStr string, lineNo int) error {
	parts := DirectiveRe.FindStringSubmatch(dirStr)
	if len(parts) == 0 {
		warning := fmt.Sprintf("malformed @scripthaus directive '%s' (line %d)", dirStr, lineNo)
		cdef.Warnings = append(cdef.Warnings, warning)
		return nil
	}
	dirType := parts[1]
	dirArgs := parts[2]
	if dirType == "require" {
		envName := strings.TrimSpace(dirArgs)
		if !EnvVarRe.MatchString(envName) {
			warning := fmt.Sprintf("@scripthaus 'require' directive, invalid env var name '%s' (line %d)", envName, lineNo)
			cdef.Warnings = append(cdef.Warnings, warning)
			return nil
		}
		cdef.RequireEnvVars = append(cdef.RequireEnvVars, envName)
		return nil
	} else {
		warning := fmt.Sprintf("invalid @scripthaus directive '%s' (line %d)", dirType, lineNo)
		cdef.Warnings = append(cdef.Warnings, warning)
		return nil
	}
}

// returns (warnings, error)
func (cdef *CommandDef) processDirectives() error {
	if cdef.DirectivesProcessed {
		return nil
	}
	cdef.DirectivesProcessed = true
	if cdef.Lang != "sh" && cdef.Lang != "bash" {
		return nil
	}
	lines := strings.Split(cdef.ScriptText, "\n")
	for idx, line := range lines {
		if strings.HasPrefix(line, "# @scripthaus ") {
			err := cdef.processDirective(line[14:], idx+1)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func addToEnvMap(envMap map[string]string, envEntry string) {
	parts := strings.SplitN(envEntry, "=", 2)
	envMap[parts[0]] = parts[1]
}

func makeEnvMap(fullEnv []string) map[string]string {
	rtn := make(map[string]string)
	for _, envEntry := range fullEnv {
		parts := strings.SplitN(envEntry, "=", 2)
		rtn[parts[0]] = parts[1]
	}
	return rtn
}

func makeFullEnv(runSpec SpecType) []string {
	fullEnv := os.Environ()
	fullEnv = append(fullEnv, runSpec.Env...)
	return fullEnv
}

func (cdef *CommandDef) CheckCommand(runSpec SpecType) error {
	cdef.processDirectives()
	fullEnv := makeFullEnv(runSpec)
	envMap := makeEnvMap(fullEnv)
	if len(cdef.RequireEnvVars) > 0 {
		for _, envName := range cdef.RequireEnvVars {
			if envMap[envName] == "" {
				return fmt.Errorf("required variable '%s' is not set", envName)
			}
		}
	}
	return nil
}

func (cdef *CommandDef) BuildExecCommand(ctx context.Context, runSpec SpecType) (*ExecItem, error) {
	execItem, err := cdef.buildNormalCommand(ctx, runSpec)
	if err != nil {
		return nil, err
	}
	execItem.FullScriptName = cdef.FullScriptName()
	execItem.HItem = history.BuildHistoryItem()
	execItem.HItem.ProjectDir = cdef.Playbook.ProjectDir
	execItem.HItem.PlaybookFile = cdef.Playbook.CanonicalName
	execItem.HItem.PlaybookCommand = cdef.Name
	execItem.HItem.ScriptType = cdef.Lang
	execItem.HItem.EncodeCmdLine(runSpec.ScriptArgs)
	return execItem, nil
}
