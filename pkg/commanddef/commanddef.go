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
	"os/user"
	"path"
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
	DirectivesProcessed bool
	ChangeDir           string
	NoLog               bool
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

func (cdef *CommandDef) OrigScriptName() string {
	if cdef.Playbook.OrigName == "^" || cdef.Playbook.OrigName == "." || cdef.Playbook.OrigName == "" {
		return fmt.Sprintf("%s%s", cdef.Playbook.OrigName, cdef.Name)
	}
	return fmt.Sprintf("%s::%s", cdef.Playbook.OrigName, cdef.Name)
}

func (cdef *CommandDef) FullScriptName() string {
	if cdef.Playbook.CanonicalName == "^" || cdef.Playbook.CanonicalName == "." {
		fmt.Sprintf("%s%s", cdef.Playbook.CanonicalName, cdef.Name)
	}
	return fmt.Sprintf("%s::%s", cdef.Playbook.CanonicalName, cdef.Name)
}

type ExecItem struct {
	CmdName        string
	CmdDef         *CommandDef
	Cmd            *exec.Cmd
	FullScriptName string
	HItem          *history.HistoryItem
}

func (item *ExecItem) CmdShortName() string {
	return fmt.Sprintf("%s %s", item.CmdName, item.CmdDef.OrigScriptName())
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

func (cdef *CommandDef) buildNormalCommand(ctx context.Context, runSpec SpecType) (*ExecItem, error) {
	if cdef.Lang == "sh" || cdef.Lang == "bash" || cdef.Lang == "zsh" || cdef.Lang == "tcsh" || cdef.Lang == "ksh" || cdef.Lang == "fish" {
		args := append([]string{"-c", cdef.ScriptText, cdef.OrigScriptName()}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdDef: cdef, CmdName: cdef.Lang, Cmd: execCmd}, nil
	} else if cdef.Lang == "python" || cdef.Lang == "python3" || cdef.Lang == "python2" {
		args := append([]string{"-c", cdef.ScriptText}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdDef: cdef, CmdName: cdef.Lang, Cmd: execCmd}, nil
	} else if cdef.Lang == "node" || cdef.Lang == "js" {
		args := append([]string{"--eval", cdef.ScriptText, "--"}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, "node", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return &ExecItem{CmdDef: cdef, CmdName: "node", Cmd: execCmd}, nil
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

func (cdef *CommandDef) processDirectives() error {
	if cdef.DirectivesProcessed {
		return nil
	}
	cdef.DirectivesProcessed = true
	for _, dir := range cdef.RawDirectives {
		if dir.Type == "command" {
			continue // already processed
		} else if dir.Type == "cd" {
			dirName := strings.TrimSpace(dir.Data)
			if dirName == ":playbook" {
				cdef.ChangeDir = cdef.Playbook.PlaybookDir()
				continue
			}
			if dirName == ":current" {
				cdef.ChangeDir = ""
				continue
			}
			if strings.HasPrefix(dirName, "~") {
				osUser, _ := user.Current()
				if osUser != nil && osUser.HomeDir != "" {
					cdef.ChangeDir = path.Join(osUser.HomeDir, dirName[1:])
				}
				continue
			}
			if !path.IsAbs(dirName) {
				cdef.Warnings = append(cdef.Warnings, fmt.Sprintf("'cd' directive must be absolute, got '%s' (ignoring)", dirName))
				continue
			}
			cdef.ChangeDir = dirName
		} else if dir.Type == "nolog" {
			cdef.NoLog = true
		} else {
			cdef.Warnings = append(cdef.Warnings, fmt.Sprintf("invalid directive '%s' (ignoring)", dir.Type))
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
	err := cdef.processDirectives()
	if err != nil {
		return err
	}
	return nil
}

func (cdef *CommandDef) BuildExecCommand(ctx context.Context, runSpec SpecType) (*ExecItem, error) {
	execItem, err := cdef.buildNormalCommand(ctx, runSpec)
	if err != nil {
		return nil, err
	}
	if cdef.ChangeDir != "" {
		execItem.Cmd.Dir = cdef.ChangeDir
	}
	execItem.FullScriptName = cdef.FullScriptName()
	shouldLog := true
	if runSpec.NoLog {
		shouldLog = false
	} else if runSpec.ForceLog {
		shouldLog = true
	} else if cdef.NoLog {
		shouldLog = false
	}
	if shouldLog && !history.HistoryDisabledFile() {
		execItem.HItem = history.BuildHistoryItem()
		execItem.HItem.ProjectDir = cdef.Playbook.ProjectDir
		execItem.HItem.PlaybookFile = cdef.Playbook.CanonicalName
		execItem.HItem.PlaybookCommand = cdef.Name
		execItem.HItem.ScriptType = cdef.Lang
		if cdef.ChangeDir != "" {
			execItem.HItem.Cwd = cdef.ChangeDir
		}
		execItem.HItem.EncodeCmdLine(runSpec.ScriptArgs)
	}
	return execItem, nil
}
