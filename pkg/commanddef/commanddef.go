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
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/scripthaus-dev/scripthaus/pkg/base"
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
	RequireEnvVars      []string
	DirectivesProcessed bool
	Warnings            []string
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
	SpecialMode    string
	CmdName        string
	Cmd            *exec.Cmd
	FullScriptName string
	HItem          *history.HistoryItem
	RunType        string
}

func (item *ExecItem) CmdShortName() string {
	if item.RunType == base.RunTypeScript {
		return item.FullScriptName
	} else {
		return fmt.Sprintf("%s %s", item.CmdName, item.FullScriptName)
	}
}

type SpecType struct {
	SpecialMode string // "" (normal), "docker", (or "remote", not yet implemented)

	// log to history unless NoLog is set (ForceLog is necessary just for combining specs)
	NoLog    bool
	ForceLog bool

	ScriptArgs []string

	DockerImage string
	DockerOpts  []string

	// matches exec.Cmd (each entry is of form key=value)
	Env []string
}

// holds a script-name or playbook-file/playbook-script
type ScriptDef struct {
	// either ScriptFile or PlaybookFile will be set, not both
	ScriptFile     string
	PlaybookFile   string
	PlaybookScript string
}

func (def ScriptDef) FullScriptName() string {
	if def.ScriptFile != "" {
		return def.ScriptFile
	}
	if def.PlaybookFile != "" && def.PlaybookScript == "" {
		return def.PlaybookFile
	}
	if def.PlaybookFile != "" && def.PlaybookScript != "" {
		return fmt.Sprintf("%s/%s", def.PlaybookFile, def.PlaybookScript)
	}
	return ""
}

func (def ScriptDef) IsEmpty() bool {
	return def.ScriptFile == "" && def.PlaybookFile == ""
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

func (cdef *CommandDef) buildDockerCommand(ctx context.Context, runSpec SpecType) (*ExecItem, error) {
	if runSpec.DockerImage == "" {
		return nil, fmt.Errorf("must supply --docker-image to build docker command")
	}
	dockerEnvArgs := getDockerEnvArgs(cdef, runSpec)
	if cdef.Lang == "sh" || cdef.Lang == "bash" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, cdef.Lang, "-c", cdef.ScriptText, cdef.FullScriptName(), runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		cmdName := fmt.Sprintf("docker run %s %s", runSpec.DockerImage, cdef.Lang)
		return &ExecItem{CmdName: cmdName, Cmd: execCmd}, nil
	} else if cdef.Lang == "python" || cdef.Lang == "python3" || cdef.Lang == "python2" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, cdef.Lang, "-c", cdef.ScriptText, runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		cmdName := fmt.Sprintf("docker run %s %s", runSpec.DockerImage, cdef.Lang)
		return &ExecItem{CmdName: cmdName, Cmd: execCmd}, nil
	} else if cdef.Lang == "node" || cdef.Lang == "js" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, "node", "--eval", cdef.ScriptText, "--", runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		cmdName := fmt.Sprintf("docker run %s node", runSpec.DockerImage)
		return &ExecItem{CmdName: cmdName, Cmd: execCmd}, nil
	}
	return nil, fmt.Errorf("invalid command language '%s', not supported", cdef.Lang)
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

func getDockerEnvArgs(cdef *CommandDef, runSpec SpecType) []string {
	dockerEnvMap := make(map[string]string)
	for _, envEntry := range runSpec.Env {
		addToEnvMap(dockerEnvMap, envEntry)
	}
	if len(cdef.RequireEnvVars) > 0 {
		fullEnvMap := makeEnvMap(makeFullEnv(runSpec))
		for _, envVar := range cdef.RequireEnvVars {
			dockerEnvMap[envVar] = fullEnvMap[envVar]
		}
	}
	var rtn []string
	for envName, envVal := range dockerEnvMap {
		rtn = append(rtn, "-e", fmt.Sprintf("%s=%s", envName, envVal))
	}
	return rtn
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
	var err error
	var execItem *ExecItem
	if runSpec.SpecialMode == "docker" {
		execItem, err = cdef.buildDockerCommand(ctx, runSpec)
		if execItem != nil {
			execItem.SpecialMode = "docker"
		}
	} else {
		execItem, err = cdef.buildNormalCommand(ctx, runSpec)
	}
	if err != nil {
		return nil, err
	}
	execItem.RunType = base.RunTypePlaybook
	execItem.FullScriptName = cdef.FullScriptName()
	execItem.HItem = history.BuildHistoryItem()
	execItem.HItem.RunType = base.RunTypePlaybook
	// execItem.HItem.ScriptPath, _ = filepath.Abs(cdef.PlaybookPath)
	// execItem.HItem.ScriptFile = path.Base(cdef.PlaybookPath)
	// execItem.HItem.ScriptName = cdef.Name
	execItem.HItem.ProjectDir = ""
	execItem.HItem.PlaybookFile = ""
	execItem.HItem.PlaybookScript = cdef.Name
	execItem.HItem.ScriptType = cdef.Lang
	execItem.HItem.EncodeCmdLine(runSpec.ScriptArgs)
	return execItem, nil
}

func BuildScriptExecCommand(ctx context.Context, scriptPath string, runSpec SpecType) (*ExecItem, error) {
	if runSpec.SpecialMode == "docker" {
		return nil, fmt.Errorf("docker mode not supported for bare commands (must use a playbook)")
	}
	execCmd := exec.CommandContext(ctx, scriptPath, runSpec.ScriptArgs...)
	setStandardCmdOpts(execCmd, runSpec)
	execItem := &ExecItem{CmdName: scriptPath, Cmd: execCmd, FullScriptName: scriptPath, RunType: base.RunTypeScript}
	execItem.HItem = history.BuildHistoryItem()
	execItem.HItem.RunType = base.RunTypeScript
	execItem.HItem.ScriptPath, _ = filepath.Abs(scriptPath)
	execItem.HItem.ScriptFile = path.Base(scriptPath)
	execItem.HItem.EncodeCmdLine(runSpec.ScriptArgs)
	return execItem, nil
}
