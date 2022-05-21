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
)

type CommandDef struct {
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

type IResolveScript interface {
	SetScriptFile(val string)
	SetFullScriptName(val string)
	SetPlaybookFile(val string)
	SetPlaybookScript(val string)
	GetPlaybookFile() string
}

type RunOptsType struct {
	FullScriptName string

	// either ScriptFile or PlaybookFile will be set, not both
	ScriptFile     string
	PlaybookFile   string
	PlaybookScript string

	RunSpec SpecType // specs can be combined (so they are pulled out separately)
}

func (opts *RunOptsType) GetPlaybookFile() string {
	return opts.PlaybookFile
}

func (opts *RunOptsType) SetScriptFile(val string) {
	opts.ScriptFile = val
}

func (opts *RunOptsType) SetFullScriptName(val string) {
	opts.FullScriptName = val
}

func (opts *RunOptsType) SetPlaybookFile(val string) {
	opts.PlaybookFile = val
}

func (opts *RunOptsType) SetPlaybookScript(val string) {
	opts.PlaybookScript = val
}

func setStandardCmdOpts(cmd *exec.Cmd, runSpec SpecType) {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = makeFullEnv(runSpec)
}

func BuildScriptExecCommand(ctx context.Context, scriptPath string, runSpec SpecType) (*exec.Cmd, error) {
	if runSpec.SpecialMode == "docker" {
		return nil, fmt.Errorf("docker mode not supported for bare commands (must use a playbook)")
	}
	execCmd := exec.CommandContext(ctx, scriptPath, runSpec.ScriptArgs...)
	setStandardCmdOpts(execCmd, runSpec)
	return execCmd, nil
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

func (cdef *CommandDef) buildNormalCommand(ctx context.Context, fullScriptName string, runSpec SpecType) (string, *exec.Cmd, error) {
	if cdef.Lang == "sh" || cdef.Lang == "bash" {
		args := append([]string{"-c", cdef.ScriptText, fullScriptName}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return cdef.Lang, execCmd, nil
	} else if cdef.Lang == "python" || cdef.Lang == "python3" || cdef.Lang == "python2" {
		args := append([]string{"-c", cdef.ScriptText}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, cdef.Lang, args...)
		setStandardCmdOpts(execCmd, runSpec)
		return cdef.Lang, execCmd, nil
	} else if cdef.Lang == "node" || cdef.Lang == "js" {
		args := append([]string{"--eval", cdef.ScriptText, "--"}, runSpec.ScriptArgs...)
		execCmd := exec.CommandContext(ctx, "node", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return "node", execCmd, nil
	}
	return "", nil, fmt.Errorf("invalid command language '%s', not supported", cdef.Lang)
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

func (cdef *CommandDef) buildDockerCommand(ctx context.Context, fullScriptName string, runSpec SpecType) (string, *exec.Cmd, error) {
	if runSpec.DockerImage == "" {
		return "", nil, fmt.Errorf("must supply --docker-image to build docker command")
	}
	dockerEnvArgs := getDockerEnvArgs(cdef, runSpec)
	if cdef.Lang == "sh" || cdef.Lang == "bash" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, cdef.Lang, "-c", cdef.ScriptText, fullScriptName, runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return fmt.Sprintf("docker run %s %s", runSpec.DockerImage, cdef.Lang), execCmd, nil
	} else if cdef.Lang == "python" || cdef.Lang == "python3" || cdef.Lang == "python2" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, cdef.Lang, "-c", cdef.ScriptText, runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return fmt.Sprintf("docker run %s %s", runSpec.DockerImage, cdef.Lang), execCmd, nil
	} else if cdef.Lang == "node" || cdef.Lang == "js" {
		args := combine("run", runSpec.DockerOpts, dockerEnvArgs, runSpec.DockerImage, "node", "--eval", cdef.ScriptText, "--", runSpec.ScriptArgs)
		execCmd := exec.CommandContext(ctx, "docker", args...)
		setStandardCmdOpts(execCmd, runSpec)
		return fmt.Sprintf("docker run %s node", runSpec.DockerImage), execCmd, nil
	}
	return "", nil, fmt.Errorf("invalid command language '%s', not supported", cdef.Lang)
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

func (cdef *CommandDef) CheckCommand(fullScriptName string, runSpec SpecType) error {
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

// returns (cmdName, *exec.Cmd, error)
func (cdef *CommandDef) BuildExecCommand(ctx context.Context, fullScriptName string, runSpec SpecType) (string, *exec.Cmd, error) {
	if runSpec.SpecialMode == "docker" {
		return cdef.buildDockerCommand(ctx, fullScriptName, runSpec)
	} else {
		return cdef.buildNormalCommand(ctx, fullScriptName, runSpec)
	}
}
