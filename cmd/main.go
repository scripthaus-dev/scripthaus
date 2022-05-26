// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/scripthaus-dev/scripthaus/pkg/base"
	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
	"github.com/scripthaus-dev/scripthaus/pkg/helptext"
	"github.com/scripthaus-dev/scripthaus/pkg/history"
	"github.com/scripthaus-dev/scripthaus/pkg/mdparser"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"

	"github.com/mattn/go-shellwords"
)

func runVersionCommand(gopts globalOptsType) {
	printVersion()
	fmt.Printf("\n")
}

func runHelpCommand(gopts globalOptsType, showVersion bool) {
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
	} else if subHelpCommand == "history" {
		fmt.Printf("\n%s\n\n", helptext.HistoryText)
	} else if subHelpCommand == "manage" {
		fmt.Printf("\n%s\n\n", helptext.ManageText)
	} else if subHelpCommand == "version" {
		fmt.Printf("\n%s\n\n", helptext.VersionText)
	} else if subHelpCommand == "overview" {
		fmt.Printf("\n%s\n\n", helptext.OverviewText)
	} else {
		fmt.Printf("\n%s\n\n", helptext.MainHelpText)
	}
}

func runInvalidCommand(gopts globalOptsType) {
	fmt.Printf("\n[^scripthaus] ERROR Invalid Command '%s'\n", gopts.CommandName)
	fmt.Printf("\n")
	runHelpCommand(gopts, false)
}

type listOptsType struct {
	PlaybookFile string
}

// returns exitcode, error
func runExecItem(execItem *commanddef.ExecItem, warnings []string, gopts globalOptsType) (int, error) {
	err := history.InsertHistoryItem(execItem.HItem)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[^scripthaus] error trying to add run to history db: %v\n", err)
	}
	if gopts.Verbose > 0 && len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "WARNING: %s\n", warning)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	startTs := time.Now()
	err = execItem.Cmd.Start()
	if err != nil {
		return 1, fmt.Errorf("cannot start command '%s': %w", execItem.CmdShortName(), err)
	}
	err = execItem.Cmd.Wait()
	cmdDuration := time.Since(startTs)
	exitCode := 0
	if err != nil {
		exitCode = err.(*exec.ExitError).ExitCode()
	}
	execItem.HItem.ExitCode = sql.NullInt64{Valid: true, Int64: int64(exitCode)}
	execItem.HItem.DurationMs = sql.NullInt64{Valid: true, Int64: cmdDuration.Milliseconds()}
	if !gopts.Quiet {
		var warningsStr string
		if len(warnings) > 0 {
			warningsStr = fmt.Sprintf(" (has warnings)")
		}
		fmt.Printf("\n")
		fmt.Printf("[^scripthaus] ran '%s', duration=%0.3fs, exitcode=%d%s\n", execItem.CmdShortName(), cmdDuration.Seconds(), exitCode, warningsStr)
	}
	err = history.UpdateHistoryItem(execItem.HItem)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[^scripthaus] error trying to update history item in db: %v\n", err)
	}
	return exitCode, nil
}

// returns (resolvedFileName, foundCommand, err)
func resolvePlaybookCommand(playbookFile string, playbookScriptName string, gopts globalOptsType) (string, *commanddef.CommandDef, error) {
	resolvedFileName, mdSource, err := pathutil.ReadFileFromPath(playbookFile, "playbook")
	if err != nil {
		return "", nil, err
	}
	cmdDefs, warnings, err := mdparser.ParseCommands(resolvedFileName, mdSource)
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

func runRunCommand(gopts globalOptsType) (int, error) {
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
		return runExecItem(execCmd, nil, gopts)
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
		execItem, err := foundCommand.BuildExecCommand(ctx, fullScriptName, runOpts.RunSpec)
		if err != nil {
			return 1, err
		}
		return runExecItem(execItem, foundCommand.Warnings, gopts)
	}
}

func resolveScript(cmdName string, scriptName string, curPlaybookFile string, allowBarePlaybook bool) (commanddef.ScriptDef, error) {
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
		if allowBarePlaybook {
			return commanddef.ScriptDef{PlaybookFile: scriptName}, nil
		}
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

func parseRunOpts(gopts globalOptsType) (commanddef.RunOptsType, error) {
	var rtn commanddef.RunOptsType
	var err error
	rtn.Script.PlaybookFile = gopts.PlaybookFile
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
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
		rtn.Script, err = resolveScript("run", argStr, rtn.Script.PlaybookFile, false)
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

func printWarnings(gopts globalOptsType, warnings []string, spaceAfter bool) {
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

func parseListOpts(gopts globalOptsType) (listOptsType, error) {
	var rtn listOptsType
	rtn.PlaybookFile = gopts.PlaybookFile
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if isOption(argStr) {
			return rtn, fmt.Errorf("Invalid option '%s' passed to scripthaus list command", argStr)
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

func runListCommandInternal(gopts globalOptsType, playbookFile string) (int, error) {
	resolvedFileName, mdSource, err := pathutil.ReadFileFromPath(playbookFile, "playbook")
	if err != nil {
		return 1, err
	}
	commands, warnings, err := mdparser.ParseCommands(resolvedFileName, mdSource)
	if err != nil {
		return 1, err
	}
	printWarnings(gopts, warnings, true)
	fmt.Printf("%s\n", resolvedFileName)
	for _, command := range commands {
		fmt.Printf("  %s/%s\n", playbookFile, command.Name)
	}
	return 0, nil
}

func runListCommand(gopts globalOptsType) (int, error) {
	listOpts, err := parseListOpts(gopts)
	if err != nil {
		return 1, err
	}
	return runListCommandInternal(gopts, listOpts.PlaybookFile)
}

type showOptsType struct {
	Script commanddef.ScriptDef
}

func parseShowOpts(gopts globalOptsType) (showOptsType, error) {
	var rtn showOptsType
	var err error
	rtn.Script.PlaybookFile = gopts.PlaybookFile
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if isOption(argStr) {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus show command", argStr)
		}
		rtn.Script, err = resolveScript("show", argStr, rtn.Script.PlaybookFile, true)
		if err != nil {
			return rtn, err
		}
		if iter.HasNext() {
			return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], too many arguments passed, extras = '%s'", strings.Join(iter.Rest(), " "))
		}
		break
	}
	return rtn, nil
}

type historyOptsType struct {
	ShowNum int
	ShowAll bool

	FormatFull bool
	FormatJson bool
}

func parseHistoryOpts(opts globalOptsType) (historyOptsType, error) {
	var rtn historyOptsType
	iter := &OptsIter{Opts: opts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "--all" {
			rtn.ShowAll = true
			continue
		}
		if argStr == "--full" {
			rtn.FormatFull = true
			continue
		}
		if argStr == "--json" {
			rtn.FormatJson = true
			continue
		}
		if argStr == "-n" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [num]' missing num", argStr)
			}
			numStr := iter.Next()
			num, err := strconv.Atoi(numStr)
			if err != nil {
				return rtn, fmt.Errorf("'%s %s' invalid number: %w", argStr, numStr, err)
			}
			rtn.ShowNum = num
			continue
		}
		if isOption(argStr) {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus history command", argStr)
		}
		iter.Pos = iter.Pos - 1
		return rtn, fmt.Errorf("too many arguments passed to scripthaus history command, extras = '%s'", strings.Join(iter.Rest(), " "))
	}
	return rtn, nil
}

func runHistoryCommand(opts globalOptsType) (int, error) {
	historyOpts, err := parseHistoryOpts(opts)
	if err != nil {
		return 1, err
	}
	query := history.HistoryQuery{
		ShowAll: historyOpts.ShowAll,
		ShowNum: historyOpts.ShowNum,
	}
	items, err := history.QueryHistory(query)
	if err != nil {
		return 1, err
	}
	for idx, item := range items {
		if historyOpts.FormatJson {
			barr, err := item.MarshalJSON()
			if err != nil {
				continue
			}
			if idx == 0 {
				fmt.Printf("[")
			}
			fmt.Printf("%s", string(barr))
			if idx == len(items)-1 {
				fmt.Printf("]\n")
			} else {
				fmt.Printf(",\n")
			}
			continue
		} else if historyOpts.FormatFull {
			str := item.FullString()
			fmt.Printf("%s", str)
			continue
		} else {
			str := item.CompactString()
			fmt.Printf("%s", str)
			continue
		}
	}
	return 0, nil
}

type manageOptsType struct {
	ManageCommand string
	StartId       int
	EndId         int
}

func parseManageOpts(opts globalOptsType) (manageOptsType, error) {
	var rtn manageOptsType
	iter := &OptsIter{Opts: opts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if isOption(argStr) {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus manage", argStr)
		}
		rtn.ManageCommand = argStr
		if rtn.ManageCommand == "remove-history-range" {
			if iter.Pos+2 > len(iter.Opts) {
				return rtn, fmt.Errorf("Usage: scripthaus manage remove-history-range [start-id] [end-id], not enough arguments passed")
			}
			var err error
			startStr := iter.Next()
			rtn.StartId, err = strconv.Atoi(startStr)
			if err != nil {
				return rtn, fmt.Errorf("invalid [start-id] '%s' passed to scripthaus manage remove-history-range: %w", startStr, err)
			}
			endStr := iter.Next()
			rtn.EndId, err = strconv.Atoi(endStr)
			if err != nil {
				return rtn, fmt.Errorf("invalid [end-id] '%s' passed to scripthaus manage remove-history-range: %w", endStr, err)
			}
		}
		if iter.HasNext() {
			return rtn, fmt.Errorf("Usage: scripthaus manage, too many arguments passed, extras = '%s'", strings.Join(iter.Rest(), " "))
		}
	}
	return rtn, nil
}

func runManageCommand(opts globalOptsType) (int, error) {
	manageOpts, err := parseManageOpts(opts)
	if err != nil {
		return 1, err
	}
	if manageOpts.ManageCommand == "clear-history" {
		numRemoved, err := history.RemoveHistoryItems(true, 0, 0)
		if err != nil {
			return 1, err
		}
		fmt.Printf("[^scripthaus] all %d history items removed\n\n", numRemoved)
	} else if manageOpts.ManageCommand == "delete-db" {
		err = history.RemoveDB()
		if err != nil {
			return 1, err
		}
		fmt.Printf("[^scripthaus] history db deleted\n\n")
	} else if manageOpts.ManageCommand == "remove-history-range" {
		numRemoved, err := history.RemoveHistoryItems(false, manageOpts.StartId, manageOpts.EndId)
		if err != nil {
			return 1, err
		}
		fmt.Printf("[^scripthaus] %d history items removed\n\n", numRemoved)
	} else if manageOpts.ManageCommand == "renumber-history" {
		err = history.ReNumberHistory()
		if err != nil {
			return 1, err
		}
		fmt.Printf("[^scripthaus] history items renumbered\n\n")
	} else {
		if manageOpts.ManageCommand == "" {
			return 1, fmt.Errorf("no sub-command passed to scripthaus manage")
		} else {
			return 1, fmt.Errorf("invalid manage sub-command '%s'", manageOpts.ManageCommand)
		}
	}
	return 0, nil
}

func runShowCommand(gopts globalOptsType) (int, error) {
	showOpts, err := parseShowOpts(gopts)
	if err != nil {
		return 1, err
	}
	if showOpts.Script.PlaybookFile == "" {
		return 1, fmt.Errorf("Usage: scripthaus show [playbook]/[script], no playbook specified")
	}
	if showOpts.Script.PlaybookScript == "" {
		return runListCommandInternal(gopts, showOpts.Script.PlaybookFile)
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

func parseAddOpts(opts globalOptsType) (addOptsType, error) {
	var rtn addOptsType
	var err error
	rtn.Script.PlaybookFile = opts.PlaybookFile
	iter := &OptsIter{Opts: opts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
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
		rtn.Script, err = resolveScript("add", argStr, rtn.Script.PlaybookFile, false)
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

func runAddCommand(gopts globalOptsType) (errCode int, errRtn error) {
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
	buf.WriteString(fmt.Sprintf("\n#### `%s`\n\n", addOpts.Script.PlaybookScript))
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
	defs, _, err := mdparser.ParseCommands(fileName, fileBytes)
	if err != nil {
		return nil, err
	}
	return defs, nil
}

func printVersion() {
	fmt.Printf("[^scripthaus] v%s\n", base.ScriptHausVersion)
}

type globalOptsType struct {
	Verbose      int
	Quiet        bool
	PlaybookFile string
	SpecName     string
	CommandName  string
	CommandArgs  []string
}

func parseGlobalOpts(args []string) (globalOptsType, error) {
	var opts globalOptsType
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
		if argStr == "-p" || argStr == "--playbook" {
			if !iter.HasNext() {
				return opts, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			opts.PlaybookFile = iter.Next()
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
		exitCode, err = runListCommand(gopts)
	} else if gopts.CommandName == "history" {
		exitCode, err = runHistoryCommand(gopts)
	} else if gopts.CommandName == "manage" {
		exitCode, err = runManageCommand(gopts)
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
