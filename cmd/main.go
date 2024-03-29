// Copyright 2023 Michael Sawka
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
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/scripthaus-dev/scripthaus/pkg/base"
	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
	"github.com/scripthaus-dev/scripthaus/pkg/helptext"
	"github.com/scripthaus-dev/scripthaus/pkg/history"
	"github.com/scripthaus-dev/scripthaus/pkg/mdparser"
	"github.com/scripthaus-dev/scripthaus/pkg/pathutil"
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
	if execItem.HItem != nil {
		err := history.InsertHistoryItem(execItem.HItem)
		if err != nil {
			// keep going, this is just a warning, should not stop the command from running
			fmt.Fprintf(os.Stderr, "[^scripthaus] error trying to add run to history db: %v\n", err)
		}
	}
	if gopts.Verbose > 0 && len(warnings) > 0 {
		for _, warning := range warnings {
			fmt.Fprintf(os.Stderr, "WARNING: %s\n", warning)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	startTs := time.Now()
	err := execItem.Cmd.Start()
	if err != nil {
		return 1, fmt.Errorf("cannot start command '%s': %w", execItem.CmdShortName(), err)
	}
	err = execItem.Cmd.Wait()
	cmdDuration := time.Since(startTs)
	exitCode := 0
	if err != nil {
		exitCode = err.(*exec.ExitError).ExitCode()
	}
	if execItem.HItem != nil {
		execItem.HItem.ExitCode = sql.NullInt64{Valid: true, Int64: int64(exitCode)}
		execItem.HItem.DurationMs = sql.NullInt64{Valid: true, Int64: cmdDuration.Milliseconds()}
	}
	if gopts.ShowSummary {
		var warningsStr string
		var noLogStr string
		if len(warnings) > 0 {
			warningsStr = fmt.Sprintf(" (has warnings)")
		}
		if execItem.HItem == nil {
			noLogStr = fmt.Sprintf(" (not logged)")
		}
		fmt.Printf("\n")
		fmt.Printf("[^scripthaus] ran '%s', duration=%0.3fs, exitcode=%d%s%s\n", execItem.CmdShortName(), cmdDuration.Seconds(), exitCode, noLogStr, warningsStr)
	}
	if execItem.HItem != nil {
		err = history.UpdateHistoryItem(execItem.HItem)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[^scripthaus] error trying to update history item in db: %v\n", err)
		}
	}
	return exitCode, nil
}

// returns (foundCommand, err)
func resolvePlaybookCommand(playbookFile string, playbookScriptName string, gopts globalOptsType) (*commanddef.CommandDef, error) {
	resolvedPlaybook, err := pathutil.DefaultResolver().ResolvePlaybook(playbookFile)
	if err != nil {
		return nil, err
	}
	found, mdSource, err := pathutil.TryReadFile(resolvedPlaybook.ResolvedFile, "playbook", false)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("cannot find playbook '%s' (resolved to '%s')", playbookFile, resolvedPlaybook.ResolvedFile)
	}
	cmdDefs, warnings, err := mdparser.ParseCommands(resolvedPlaybook, mdSource)
	if err != nil {
		return nil, err
	}
	var foundCommand *commanddef.CommandDef
	for _, cmdDef := range cmdDefs {
		if cmdDef.Name == playbookScriptName {
			foundCommand = &cmdDef
			break
		}
	}
	if foundCommand == nil {
		fmt.Printf("[^scripthaus] ERROR could not find script '%s' inside of playbook '%s'\n", playbookScriptName, resolvedPlaybook.ResolvedFile)
		fmt.Printf("\n")
		printWarnings(gopts, warnings, true)
		return nil, nil
	}
	return foundCommand, nil
}

func runRunCommand(gopts globalOptsType) (int, error) {
	runOpts, err := parseRunOpts(gopts)
	if err != nil {
		return 1, err
	}
	ctx := context.Background()
	script := runOpts.Script
	foundCommand, err := resolvePlaybookCommand(script.PlaybookFile, script.PlaybookCommand, gopts)
	if foundCommand == nil || err != nil {
		return 1, err
	}
	err = foundCommand.CheckCommand(runOpts.RunSpec)
	if err != nil {
		return 1, err
	}
	execItem, err := foundCommand.BuildExecCommand(ctx, runOpts.RunSpec)
	if err != nil {
		return 1, err
	}
	return runExecItem(execItem, foundCommand.Warnings, gopts)

}

func resolveScript(cmdName string, scriptName string, curPlaybookFile string, allowBarePlaybook bool) (commanddef.ScriptDef, error) {
	var emptyRtn commanddef.ScriptDef
	if scriptName == "-" {
		return emptyRtn, fmt.Errorf("invalid script '%s', must specify a [command] to run from <stdin>", scriptName)
	}
	if curPlaybookFile != "" {
		if !mdparser.IsValidScriptName(scriptName) {
			return emptyRtn, fmt.Errorf("invalid characters in playbook command name '%s' (playbook specified with --playbook '%s')", scriptName, curPlaybookFile)
		}
		return commanddef.ScriptDef{PlaybookFile: curPlaybookFile, PlaybookCommand: scriptName}, nil
	}
	if strings.HasSuffix(scriptName, "/") {
		return emptyRtn, fmt.Errorf("invalid script '%s', cannot have a trailing slash", scriptName)
	}
	if strings.HasSuffix(scriptName, ".md") {
		if allowBarePlaybook {
			return commanddef.ScriptDef{PlaybookFile: scriptName}, nil
		}
		return emptyRtn, fmt.Errorf("no playbook script specified, usage: %s %s::[command]", cmdName, scriptName)
	}
	playFile, playCommand, err := pathutil.SplitScriptName(scriptName)
	if err != nil {
		return emptyRtn, err
	}
	if playFile == "" {
		playFile = "."
	}
	if !allowBarePlaybook && playCommand == "" {
		return emptyRtn, fmt.Errorf("playbook command name cannot be empty")
	}
	if playCommand != "" && !mdparser.IsValidScriptName(playCommand) {
		return emptyRtn, fmt.Errorf("invalid characters in playbook command name '%s'", playCommand)
	}
	return commanddef.ScriptDef{PlaybookFile: playFile, PlaybookCommand: playCommand}, nil
}

func parseRunOpts(gopts globalOptsType) (commanddef.RunOptsType, error) {
	var rtn commanddef.RunOptsType
	var err error
	rtn.Script.PlaybookFile = gopts.PlaybookFile
	iter := &OptsIter{Opts: gopts.CommandArgs}
	for iter.HasNext() {
		argStr := iter.Next()
		if argStr == "--env" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s \"[VAR=VAL]\" or %s file.env' missing value", argStr, argStr)
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
				envFile := iter.Next()
				envMap, err := godotenv.Read(envFile)
				if err != nil {
					return rtn, fmt.Errorf("%s '%s', cannot read env file: %w", argStr, envFile, err)
				}
				for envVar, envVal := range envMap {
					rtn.RunSpec.Env = append(rtn.RunSpec.Env, fmt.Sprintf("%s=%s", envVar, envVal))
				}
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
	if rtn.Script.PlaybookFile == "" {
		return rtn, fmt.Errorf("Usage: scripthaus run [run-opts] [playbook]::[command] [script-opts], no playbook specified")
	}
	if rtn.Script.PlaybookCommand == "" {
		return rtn, fmt.Errorf("Usage: scripthaus run [run-opts] [playbook]::[command] [script-opts], no command specified")
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
	resolvedPlaybook, err := pathutil.DefaultResolver().ResolvePlaybook(playbookFile)
	if err != nil {
		return 1, err
	}
	found, mdSource, err := pathutil.TryReadFile(resolvedPlaybook.ResolvedFile, "playbook", false)
	if err != nil {
		return 1, err
	}
	if !found {
		return 1, fmt.Errorf("cannot find playbook '%s' (resolved to '%s')", playbookFile, resolvedPlaybook.ResolvedFile)
	}
	commands, warnings, err := mdparser.ParseCommands(resolvedPlaybook, mdSource)
	if err != nil {
		return 1, err
	}
	printWarnings(gopts, warnings, true)
	fmt.Printf("%s\n", resolvedPlaybook.OrigShowStr())
	maxScriptNameLen := 0
	for _, command := range commands {
		origScriptName := command.OrigScriptName()
		if len(origScriptName) > maxScriptNameLen {
			maxScriptNameLen = len(origScriptName)
		}
	}
	if maxScriptNameLen > 40 {
		maxScriptNameLen = 40
	}
	for _, command := range commands {
		if command.ShortText != "" {
			shortText := command.ShortText
			if len(shortText) > 80 {
				shortText = shortText[0:77] + "..."
			}
			fmt.Printf("  %-*s - %s\n", maxScriptNameLen, command.OrigScriptName(), shortText)
		} else {
			fmt.Printf("  %-*s\n", maxScriptNameLen, command.OrigScriptName())
		}
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
			return rtn, fmt.Errorf("Usage: scripthaus show [playbook]::[script], too many arguments passed, extras = '%s'", strings.Join(iter.Rest(), " "))
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
	// ignore error (just use "")
	henv := history.MakeHistoryEnv()
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
			str := item.FullString(henv)
			fmt.Printf("%s", str)
			continue
		} else {
			str := item.CompactString(henv)
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
		return 1, fmt.Errorf("Usage: scripthaus show [playbook]::[script], no playbook specified")
	}
	if showOpts.Script.PlaybookCommand == "" {
		return runListCommandInternal(gopts, showOpts.Script.PlaybookFile)
	}
	foundCommand, err := resolvePlaybookCommand(showOpts.Script.PlaybookFile, showOpts.Script.PlaybookCommand, gopts)
	if foundCommand == nil || err != nil {
		return 1, err
	}
	fmt.Printf("[^scripthaus] show '%s'\n\n", foundCommand.FullScriptName())
	fmt.Printf("%s\n\n%s\n\n", foundCommand.HelpText, foundCommand.RawCodeText)
	return 0, nil
}

type addOptsType struct {
	Script     commanddef.ScriptDef
	ScriptType string
	ScriptText string
	ShortDesc  string
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
		if argStr == "-s" || argStr == "--short-desc" {
			if !iter.HasNext() {
				return rtn, fmt.Errorf("'%s [desc]' missing short-description string", argStr)
			}
			rtn.ShortDesc = iter.Next()
			if strings.Index(rtn.ShortDesc, "\n") != -1 {
				return rtn, fmt.Errorf("'%s [desc]' short description cannot contain a newline character", argStr)
			}
			if len(rtn.ShortDesc) > 80 {
				return rtn, fmt.Errorf("'%s [desc]' short description cannot be more than 80 characters", argStr)
			}
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
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus add command", argStr)
		}
		rtn.Script, err = resolveScript("add", argStr, rtn.Script.PlaybookFile, false)
		if err != nil {
			return rtn, err
		}
	}
	if rtn.Script.PlaybookFile == "" {
		return rtn, fmt.Errorf("No playbook/script passed to 'add' command.  Usage: scripthaus add [opts] [playbook]::[script]")
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
			return 1, fmt.Errorf("reading script-text from <stdin>, but got empty string")
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
	if !base.IsValidScriptType(addOpts.ScriptType) {
		return 1, fmt.Errorf("must specify a valid script type ('%s' is not valid), must be one of: %s", addOpts.ScriptType, strings.Join(base.ValidScriptTypes(), ", "))
	}
	resolvedPlaybook, err := pathutil.DefaultResolver().ResolvePlaybook(addOpts.Script.PlaybookFile)
	if err != nil {
		if strings.Index(err.Error(), "not found") != -1 {
			fmt.Printf("[^scripthaus] add will not create a new markdown file.  touch the file and re-run the add if this was your intention\n")
		}
		return 1, err
	}
	cmdDefs, err := readCommandsFromFile(resolvedPlaybook)
	if err != nil {
		return 1, err
	}
	for _, def := range cmdDefs {
		if def.Name == addOpts.Script.PlaybookCommand {
			return 1, fmt.Errorf("script with name '%s' already exists in playbook file %s", addOpts.Script.PlaybookCommand, resolvedPlaybook.OrigShowStr())
		}
	}
	var buf bytes.Buffer
	fmt.Printf("[^scripthaus] adding command '%s' to %s:\n", addOpts.Script.PlaybookCommand, resolvedPlaybook.OrigShowStr())
	buf.WriteString("\n")
	if addOpts.Message != "" {
		buf.WriteString(fmt.Sprintf("%s\n\n", addOpts.Message))
	}
	buf.WriteString(fmt.Sprintf("```%s\n", addOpts.ScriptType))
	shortDesc := addOpts.ShortDesc
	if shortDesc != "" {
		shortDesc = " - " + shortDesc
	}
	buf.WriteString(fmt.Sprintf("%s @scripthaus command %s%s\n", base.GetCommentString(addOpts.ScriptType), addOpts.Script.PlaybookCommand, shortDesc))
	buf.WriteString(fmt.Sprintf("%s\n", addOpts.ScriptText))
	buf.WriteString(fmt.Sprintf("```\n"))
	fmt.Printf("%s\n", buf.String())
	if addOpts.DryRun {
		fmt.Printf("[^scripthaus] Not modifying file, --dry-run specified\n")
		return 0, nil
	}
	fd, err := os.OpenFile(resolvedPlaybook.ResolvedFile, os.O_APPEND|os.O_WRONLY, 0666)
	defer func() {
		closeErr := fd.Close()
		if closeErr != nil && errRtn == nil {
			errCode = 1
			errRtn = fmt.Errorf("cannot close/write to playbook %s: %w", resolvedPlaybook.OrigShowStr(), closeErr)
		}
	}()
	if err != nil {
		fmt.Printf("cannot open playbook %s for append: %v", resolvedPlaybook.OrigShowStr(), err)
	}
	_, err = fd.WriteString(buf.String())
	if err != nil {
		fmt.Printf("cannot write to playbook %s: %v", resolvedPlaybook.OrigShowStr(), err)
	}
	return 0, nil
}

func readCommandsFromFile(playbook *pathutil.ResolvedPlaybook) ([]commanddef.CommandDef, error) {
	fd, err := os.Open(playbook.ResolvedFile)
	if err != nil {
		return nil, fmt.Errorf("cannot open playbook file %s: %w", playbook.OrigShowStr(), err)
	}
	defer fd.Close()
	fileBytes, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("cannot read playbook file %s: %w", playbook.OrigShowStr(), err)
	}
	defs, _, err := mdparser.ParseCommands(playbook, fileBytes)
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
	ShowSummary  bool
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
		if argStr == "-s" || argStr == "--summary" {
			opts.ShowSummary = true
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
