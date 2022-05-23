// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
	"github.com/scripthaus-dev/scripthaus/pkg/helptext"
	"github.com/scripthaus-dev/scripthaus/pkg/mdparser"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"

	"github.com/mattn/go-shellwords"
)

const ScriptHausVersion = "0.1.0"

func runVersionCommand(gopts globalOpts) {
	printVersion()
	fmt.Printf("\n")
}

func runHelpCommand(gopts globalOpts, showVersion bool) {
	if showVersion {
		printVersion()
	}
	subHelpCommand := ""
	if len(gopts.CommandArgs) > 0 {
		subHelpCommand = gopts.CommandArgs[0]
	}
	if subHelpCommand == "run" {
		fmt.Printf("\n%s\n\n", helptext.RunText)
	} else if subHelpCommand == "list" {
		fmt.Printf("\n%s\n\n", helptext.ListText)
	} else if subHelpCommand == "show" {
		fmt.Printf("\n%s\n\n", helptext.ShowText)
	} else if subHelpCommand == "add" {
		fmt.Printf("\n%s\n\n", helptext.AddText)
	} else if subHelpCommand == "version" {
		fmt.Printf("\n%s\n\n", helptext.VersionText)
	} else if subHelpCommand == "overview" {
		fmt.Printf("\n%s\n\n", helptext.OverviewText)
	} else {
		fmt.Printf("\n%s\n\n", helptext.MainHelpText)
	}
}

func runInvalidCommand(gopts globalOpts) {
	fmt.Printf("\n[^scripthaus] ERROR Invalid Command '%s'\n", gopts.CommandName)
	fmt.Printf("\n")
	runHelpCommand(gopts, false)
}

type listOptsType struct {
	PlaybookFile string
}

// returns exitcode, error
func runExecCmd(execCmd *exec.Cmd, cmdShortName string, warnings []string, gopts globalOpts) (int, error) {
	if gopts.Verbose > 0 && len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "WARNING: %s\n", warning)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	startTs := time.Now()
	err := execCmd.Start()
	if err != nil {
		return 1, fmt.Errorf("cannot start command '%s': %w", cmdShortName, err)
	}
	err = execCmd.Wait()
	cmdDuration := time.Since(startTs)
	exitCode := 0
	if err != nil {
		exitCode = err.(*exec.ExitError).ExitCode()
	}
	if !gopts.Quiet {
		var warningsStr string
		if len(warnings) > 0 {
			warningsStr = fmt.Sprintf(" (has warnings)")
		}
		fmt.Printf("\n")
		fmt.Printf("[^scripthaus] ran '%s', duration=%0.3fs, exitcode=%d%s\n", cmdShortName, cmdDuration.Seconds(), exitCode, warningsStr)
	}
	return exitCode, nil
}

// returns (resolvedFileName, foundCommand, err)
func resolvePlaybookCommand(playbookFile string, playbookScriptName string, gopts globalOpts) (string, *commanddef.CommandDef, error) {
	resolvedFileName, mdSource, err := pathutil.ReadFileFromPath(playbookFile, "playbook")
	if err != nil {
		return "", nil, err
	}
	cmdDefs, warnings, err := mdparser.ParseCommands(mdSource)
	if err != nil {
		return "", nil, err
	}
	var foundCommand *commanddef.CommandDef
	for _, cmdDef := range cmdDefs {
		if cmdDef.Name == playbookScriptName {
			foundCommand = &cmdDef
			break
		}
	}
	if foundCommand == nil {
		fmt.Printf("[^scripthaus] ERROR could not find script '%s' inside of playbook '%s'\n", playbookScriptName, resolvedFileName)
		fmt.Printf("\n")
		printWarnings(gopts, warnings, true)
		return "", nil, nil
	}
	return resolvedFileName, foundCommand, nil
}

func runRunCommand(gopts globalOpts) (int, error) {
	runOpts, err := parseRunOpts(gopts)
	if err != nil {
		return 1, err
	}
	ctx := context.Background()
	script := runOpts.Script
	if script.ScriptFile != "" {
		realScriptPath, err := pathutil.ResolveFileWithPath(script.ScriptFile, "script")
		if err != nil {
			return 1, err
		}
		execCmd, err := commanddef.BuildScriptExecCommand(ctx, realScriptPath, runOpts.RunSpec)
		if err != nil {
			return 1, err
		}
		return runExecCmd(execCmd, realScriptPath, nil, gopts)
	} else {
		resolvedFileName, foundCommand, err := resolvePlaybookCommand(script.PlaybookFile, script.PlaybookScript, gopts)
		if foundCommand == nil || err != nil {
			return 1, err
		}
		fullScriptName := fmt.Sprintf("%s/%s", resolvedFileName, script.PlaybookScript)
		err = foundCommand.CheckCommand(fullScriptName, runOpts.RunSpec)
		if err != nil {
			return 1, err
		}
		cmdName, execCmd, err := foundCommand.BuildExecCommand(ctx, fullScriptName, runOpts.RunSpec)
		if err != nil {
			return 1, err
		}
		return runExecCmd(execCmd, fmt.Sprintf("%s %s", cmdName, fullScriptName), foundCommand.Warnings, gopts)
	}
}

func resolveScript(cmdName string, scriptName string, curPlaybookFile string) (commanddef.ScriptDef, error) {
	var emptyRtn commanddef.ScriptDef
	if scriptName == "-" {
		return emptyRtn, fmt.Errorf("invalid script '%s', cannot execute standalone script from <stdin>", scriptName)
	}
	if curPlaybookFile != "" {
		if strings.Index(scriptName, "/") != -1 {
			return emptyRtn, fmt.Errorf("invalid script '%s', no slash allowed when --playbook '%s' is specified", scriptName, curPlaybookFile)
		}
		if !mdparser.IsValidScriptName(scriptName) {
			return emptyRtn, fmt.Errorf("invalid characters in playbook script name '%s'", scriptName)
		}
		return commanddef.ScriptDef{PlaybookFile: curPlaybookFile, PlaybookScript: scriptName}, nil
	}
	if strings.HasSuffix(scriptName, "/") {
		return emptyRtn, fmt.Errorf("invalid script '%s', cannot have a trailing slash", scriptName)
	}
	if strings.HasSuffix(scriptName, ".md") {
		return emptyRtn, fmt.Errorf("no playbook script specified, usage: %s %s/[script]", cmdName, scriptName)
	}
	if strings.Index(scriptName, "/") != -1 {
		dirName, baseName := path.Split(scriptName)
		dirFile := dirName[:len(dirName)-1]
		if dirFile == "-" {
			if !mdparser.IsValidScriptName(baseName) {
				return emptyRtn, fmt.Errorf("invalid characters in playbook script name '%s'", baseName)
			}
			return commanddef.ScriptDef{PlaybookFile: "-", PlaybookScript: baseName}, nil
		} else if path.Ext(dirFile) == ".md" {
			// an ".md" file as a directory means this is a playbook
			if !mdparser.IsValidScriptName(baseName) {
				return emptyRtn, fmt.Errorf("invalid characters in playbook script name '%s'", baseName)
			}
			return commanddef.ScriptDef{PlaybookFile: dirFile, PlaybookScript: baseName}, nil
		} else {
			// "directory" is not a .md file.  So scriptName must be a standalone ScriptFile
			return commanddef.ScriptDef{ScriptFile: scriptName}, nil
		}
	}
	// no slash, so this must be a standalone script file
	return commanddef.ScriptDef{ScriptFile: scriptName}, nil
}

func parseRunOpts(gopts globalOpts) (commanddef.RunOptsType, error) {
	var rtn commanddef.RunOptsType
	var err error
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "-p" || argStr == "--playbook" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.Script.PlaybookFile = iter.Next()
			continue
		}
		if argStr == "--docker-image" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [image]' missing image name", argStr)
			}
			rtn.RunSpec.DockerImage = iter.Next()
			rtn.RunSpec.SpecialMode = "docker"
			continue
		}
		if argStr == "--docker-opts" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [docker-opts]' missing options", argStr)
			}
			dockerOpts, err := shellwords.Parse(iter.Next())
			if err != nil {
				return rtn, fmt.Errorf("%s '%s', error splitting docker-opts: %w", err)
			}
			rtn.RunSpec.DockerOpts = dockerOpts
			continue
		}
		if argStr == "--env" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s \"[VAR=VAL]\" or %s file.env' missing value", argStr)
			}
			envVal := iter.Next()
			if strings.Index(envVal, "=") != -1 {
				envPairs := strings.Split(envVal, ";")
				for _, envPair := range envPairs {
					envPair = strings.TrimSpace(envPair)
					if envPair == "" {
						continue
					}
					if strings.Index(envPair, "=") == -1 {
						// TODO warning?
						continue
					}
					rtn.RunSpec.Env = append(rtn.RunSpec.Env, envPair)
				}
			} else {
				return rtn, fmt.Errorf("'%s [env-file]' not supported", argStr)
			}
			continue
		}
		if argStr == "--nolog" {
			rtn.RunSpec.NoLog = true
			rtn.RunSpec.ForceLog = false
			continue
		}
		if argStr == "--log" {
			rtn.RunSpec.NoLog = false
			rtn.RunSpec.ForceLog = true
			continue
		}
		if strings.HasPrefix(argStr, "-") && argStr != "-" && !strings.HasPrefix(argStr, "-/") {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus run command", argStr)
		}
		rtn.Script, err = resolveScript("run", argStr, rtn.Script.PlaybookFile)
		if err != nil {
			return rtn, err
		}
		rtn.RunSpec.ScriptArgs = iter.Rest()
		break
	}
	if rtn.Script.IsEmpty() {
		return rtn, fmt.Errorf("Usage: scripthaus run [run-opts] [script] [script-opts], no script specified")
	}
	return rtn, nil
}

func parseListOpts(gopts globalOpts) (listOptsType, error) {
	var rtn listOptsType
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "-p" || argStr == "--playbook" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.PlaybookFile = iter.Next()
			continue
		}
		if isOption(argStr) {
			return rtn, fmt.Errorf("Invalid option '%s' passed to scripthaus list command", argStr)
		}
		if rtn.PlaybookFile != "" {
			return rtn, fmt.Errorf("Usage: scripthaus list, playbook already specified with --playbook '%s', cannot list again as '%s'", rtn.PlaybookFile, argStr)
		}
		rtn.PlaybookFile = argStr
		if iter.HasNext() {
			return rtn, fmt.Errorf("Usage: scripthaus list [playbook], too many arguments passed, extras = '%s'", strings.Join(iter.Rest(), " "))
		}
		break
	}
	if rtn.PlaybookFile == "" {
		return rtn, fmt.Errorf("Usage: scripthaus list [playbook], no playbook specified")
	}
	return rtn, nil
}

func printWarnings(gopts globalOpts, warnings []string, spaceAfter bool) {
	if gopts.Quiet || len(warnings) == 0 {
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", warning)
	}
	if spaceAfter {
		fmt.Fprintf(os.Stderr, "\n")
	}
}

func runListCommand(gopts globalOpts) error {
	listOpts, err := parseListOpts(gopts)
	if err != nil {
		return err
	}
	resolvedFileName, mdSource, err := pathutil.ReadFileFromPath(listOpts.PlaybookFile, "playbook")
	if err != nil {
		return err
	}
	commands, warnings, err := mdparser.ParseCommands(mdSource)
	printWarnings(gopts, warnings, true)
	fmt.Printf("%s\n", resolvedFileName)
	for _, command := range commands {
		fmt.Printf("  %s/%s\n", listOpts.PlaybookFile, command.Name)
	}
	return nil
}

type showOptsType struct {
	Script commanddef.ScriptDef
}

func parseShowOpts(gopts globalOpts) (showOptsType, error) {
	var rtn showOptsType
	var err error
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "-p" || argStr == "--playbook" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.Script.PlaybookFile = iter.Next()
			continue
		}
		if isOption(argStr) {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus show command", argStr)
		}
		rtn.Script, err = resolveScript("show", argStr, rtn.Script.PlaybookFile)
		if err != nil {
			return rtn, err
		}
		if iter.HasNext() {
			return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], too many arguments passed, extras = '%s'", strings.Join(iter.Rest(), " "))
		}
		break
	}
	if rtn.Script.PlaybookFile == "" {
		return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], no playbook specified")
	}
	if rtn.Script.PlaybookScript == "" {
		return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], no script specified")
	}
	return rtn, nil
}

func runShowCommand(gopts globalOpts) (int, error) {
	showOpts, err := parseShowOpts(gopts)
	if err != nil {
		return 1, err
	}
	resolvedFileName, foundCommand, err := resolvePlaybookCommand(showOpts.Script.PlaybookFile, showOpts.Script.PlaybookScript, gopts)
	if foundCommand == nil || err != nil {
		return 1, err
	}
	fullScriptName := fmt.Sprintf("%s/%s", resolvedFileName, showOpts.Script.PlaybookScript)
	fmt.Printf("[^scripthaus] show '%s'\n\n", fullScriptName)
	fmt.Printf("%s\n%s\n\n", foundCommand.HelpText, foundCommand.RawCodeText)
	return 0, nil
}

type addOptsType struct {
	Script     commanddef.ScriptDef
	ScriptType string
	ScriptText string
	Message    string
	DryRun     bool
}

func parseAddOpts(opts globalOpts) (addOptsType, error) {
	var rtn addOptsType
	var err error
	iter := &OptsIter{Opts: opts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "-p" || argStr == "--playbook" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.Script.PlaybookFile = iter.Next()
			continue
		}
		if argStr == "-t" || argStr == "--type" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [type]' missing script type", argStr)
			}
			rtn.ScriptType = iter.Next()
			continue
		}
		if argStr == "-m" || argStr == "--message" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [message]' missing message", argStr)
			}
			rtn.Message = iter.Next()
			continue
		}
		if argStr == "-c" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [script-text]' missing script text", argStr)
			}
			rtn.ScriptText = iter.Next()
			continue
		}
		if argStr == "-" {
			rtn.ScriptText = "-" // stdin
			continue
		}
		if argStr == "--dry-run" {
			rtn.DryRun = true
			continue
		}
		if argStr == "--" {
			rtn.ScriptText = strings.Join(iter.Rest(), " ")
			break
		}
		if isOption(argStr) {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus show command", argStr)
		}
		rtn.Script, err = resolveScript("add", argStr, rtn.Script.PlaybookFile)
		if err != nil {
			return rtn, err
		}
		if rtn.Script.ScriptFile != "" {
			return rtn, fmt.Errorf("invalid playbook file '%s' specified (make sure it is a playbook '.md' file)", argStr)
		}
	}
	if rtn.Script.PlaybookFile == "" {
		return rtn, fmt.Errorf("No playbook/script passed to 'add' command.  Usage: scripthaus add [opts] [playbook]/[script]")
	}
	if rtn.ScriptText == "" {
		return rtn, fmt.Errorf("No script text passed to 'add' command.  Use '-c [script-text]', '--' for rest of arguments, or '-' for stdin")
	}
	return rtn, nil
}

func runAddCommand(gopts globalOpts) (errCode int, errRtn error) {
	addOpts, err := parseAddOpts(gopts)
	if err != nil {
		return 1, err
	}
	var realScriptText string
	if addOpts.ScriptText == "-" {
		scriptTextBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return 1, fmt.Errorf("reading from <stdin>: %w", err)
		}
		if len(scriptTextBytes) == 0 {
			return 1, fmt.Errorf("reading script-text from <stdin>, but got empty string", err)
		}
		realScriptText = string(scriptTextBytes)
	} else {
		realScriptText = addOpts.ScriptText
	}
	if len(realScriptText) > 5000 {
		return 1, fmt.Errorf("script-text too long, max-size for add is 5k (edit the file manually if this was not a mistake)")
	}
	if strings.Index(realScriptText, "```") != -1 {
		return 1, fmt.Errorf("script-text cannot contain the the markdown code fence characters \"```\", this block must be added to the .md file manually")
	}
	if addOpts.Script.PlaybookFile == "-" || addOpts.Script.PlaybookFile == "<stdin>" {
		return 1, fmt.Errorf("playbook file cannot be '-' (<stdin>) for 'add' command")
	}
	if addOpts.ScriptType == "" {
		return 1, fmt.Errorf("must specify a script-type using '-t'")
	}
	if !commanddef.IsValidScriptType(addOpts.ScriptType) {
		return 1, fmt.Errorf("must specify a valid script type ('%s' is not valid), must be one of: %s", addOpts.ScriptType, strings.Join(commanddef.ValidScriptTypes(), ", "))
	}
	resolvedFileName, err := pathutil.ResolveFileWithPath(addOpts.Script.PlaybookFile, "playbook")
	if err != nil {
		return 1, err
	}
	cmdDefs, err := readCommandsFromFile(resolvedFileName)
	if err != nil {
		return 1, err
	}
	for _, def := range cmdDefs {
		if def.Name == addOpts.Script.PlaybookScript {
			return 1, fmt.Errorf("script with name '%s' already exists in playbook file '%s'", addOpts.Script.PlaybookScript, resolvedFileName)
		}
	}
	var buf bytes.Buffer
	fmt.Printf("[^scripthaus] adding command '%s' to %s:\n", addOpts.Script.PlaybookScript, resolvedFileName)
	buf.WriteString(fmt.Sprintf("#### `%s`\n\n", addOpts.Script.PlaybookScript))
	if addOpts.Message != "" {
		buf.WriteString(fmt.Sprintf("%s\n\n", addOpts.Message))
	}
	buf.WriteString(fmt.Sprintf("```%s scripthaus\n%s\n```\n", addOpts.ScriptType, addOpts.ScriptText))
	fmt.Printf("%s\n", buf.String())
	if addOpts.DryRun {
		fmt.Printf("[^scripthaus] Not modifying file, --dry-run specified\n")
		return 0, nil
	}
	fd, err := os.OpenFile(resolvedFileName, os.O_APPEND|os.O_WRONLY, 0644)
	defer func() {
		closeErr := fd.Close()
		if closeErr != nil && errRtn == nil {
			errCode = 1
			errRtn = fmt.Errorf("cannot close/write to playbook '%s': %w", resolvedFileName, closeErr)
		}
	}()
	if err != nil {
		fmt.Printf("cannot open playbook '%s' for append: %w", resolvedFileName, err)
	}
	_, err = fd.WriteString(buf.String())
	if err != nil {
		fmt.Printf("cannot write to playbook '%s': %w", resolvedFileName, err)
	}
	return 0, nil
}

func readCommandsFromFile(fileName string) ([]commanddef.CommandDef, error) {
	fd, err := os.Open(fileName)
	if err != nil {
		return nil, fmt.Errorf("cannot open playbook file '%s': %w", fileName, err)
	}
	defer fd.Close()
	fileBytes, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("cannot read playbook file '%s': %w", fileName, err)
	}
	defs, _, err := mdparser.ParseCommands(fileBytes)
	if err != nil {
		return nil, err
	}
	return defs, nil
}

func printVersion() {
	fmt.Printf("^ScriptHaus v%s\n", ScriptHausVersion)
}

type globalOpts struct {
	Verbose      int
	Quiet        bool
	PlaybookFile string
	SpecName     string
	CommandName  string
	CommandArgs  []string
}

func parseGlobalOpts(args []string) (globalOpts, error) {
	var opts globalOpts
	iter := &OptsIter{Opts: args[1:]}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "-v" || argStr == "--verbose" {
			opts.Verbose++
			continue
		}
		if argStr == "-q" || argStr == "--quiet" {
			opts.Quiet = true
			continue
		}
		if isOption(argStr) {
			return opts, fmt.Errorf("Invalid option '%s'", argStr)
		}
		opts.CommandName = argStr
		opts.CommandArgs = iter.Rest()
		break
	}
	return opts, nil
}

type OptsIter struct {
	Pos  int
	Opts []string
}

func isOption(argStr string) bool {
	return strings.HasPrefix(argStr, "-") && argStr != "-" && !strings.HasPrefix(argStr, "-/")
}

func (iter *OptsIter) HasNext() bool {
	return iter.Pos <= len(iter.Opts)-1
}

func (iter *OptsIter) Next() string {
	if iter.Pos >= len(iter.Opts) {
		return ""
	}
	rtn := iter.Opts[iter.Pos]
	iter.Pos++
	return rtn
}

func (iter *OptsIter) Rest() []string {
	return iter.Opts[iter.Pos:]
}

func main() {
	// fmt.Printf("args %#v\n", os.Args)
	gopts, err := parseGlobalOpts(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[^scripthaus] ERROR %v\n\n", err)
		os.Exit(1)
	}
	exitCode := 0
	if gopts.CommandName == "" || gopts.CommandName == "help" {
		runHelpCommand(gopts, true)
	} else if gopts.CommandName == "version" {
		runVersionCommand(gopts)
	} else if gopts.CommandName == "run" {
		exitCode, err = runRunCommand(gopts)
	} else if gopts.CommandName == "show" {
		exitCode, err = runShowCommand(gopts)
	} else if gopts.CommandName == "add" {
		exitCode, err = runAddCommand(gopts)
	} else if gopts.CommandName == "list" {
		err = runListCommand(gopts)
	} else {
		runInvalidCommand(gopts)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[^scripthaus] ERROR %v\n\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}
