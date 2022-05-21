// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/scripthaus-dev/scripthaus/pkg/commanddef"
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
		fmt.Printf("Usage: scripthaus run [run-opts] [script] [script-arguments]\n")
		fmt.Printf("       scripthaus run [run-opts] [playbook]/[script] [script-arguments]\n")
		fmt.Printf(`
The 'run' command will run a standalone or playbook script.  If [script] is an
executable file it will be executed directly.  If it has a known extension
.py, .sh, or .js it will be executed using python, bash, or node respectively.

It can also execute a playbook command by combining the playbook name
(which must use a .md extension) '/' command name, e.g. 'playbook.md/test1' will
execute the 'test1' script inside of the playbook 'playbook.md'.  If the
'--playbook' option is given, then 'script' will always be interpreted as
a script inside of the given playbook.

If the script name or playbook name contains a slash, it will be looked up
using the relative or absolute pathname given.  If it does not include a
slash $SCRIPTHAUS_PATH will be used to resolve the file (Note: by default
"." is not in the path, so to run a local script use ./[scriptname]).

Any arguments after 'script' will be passed verbatim as options to the script.

Run Options:
    --nolog                  - will not log this command to scripthaus history
    --log                    - force logging of command to scripthaus history (default)
    -p, --playbook [file]    - specify a playbook to use
    --docker-image [image]   - specify a docker image to run this script against (will set --mode inine)
    --docker-opts [opts]     - options to pass to "docker run".  will be split according to shell rules
    --env 'var=val;var=val'  - specify additional environment variables (';' is seperator)
    --env 'file.env'         - special additional environment variables from .env file

`)
	} else if subHelpCommand == "list" {
		fmt.Printf("Usage: scripthaus [global-opts] list [list-opts] [playbook]\n")
		fmt.Printf(`
The 'list' command will list the scripts available to run in the given
playbook.  The playbook can optionally be passed via the -p option.

If no playbook is passed list will find all playbooks in the SCRIPTHAUS_PATH
and list all of their commands.

List Options:
    -p, --playbook [file]    - specify a playbook to use

`)
	} else if subHelpCommand == "show" {
		fmt.Printf("Usage: scripthaus show [show-opts] [playbook]/[script]\n")
		fmt.Printf(`
The 'show' command will show the help for a particular script in a playbook.
By default it will show the markdown text and the code block that
make up the script.

List Options:
    -p, --playbook [file]    - specify a playbook to use

`)
	} else if subHelpCommand == "version" {
		fmt.Printf("Usage: scripthaus version\n\n")
		fmt.Printf("The version command has no options \U0001f643\n\n")
	} else if subHelpCommand == "overview" {
		fmt.Printf(`
ScriptHaus is a command line tool that helps you organize your scripts and bash one-liners
into self-documenting markdown files.

* Stay Organized - Store your bash one-liners in a simple markdown file
* Save Commands - Easily save a command from history to run or view later
* Never Forget - Store history by command, including options, date, cwd, and exitcode
* Share - Save your files in github and share them with your team

Commands:
    run             - runs a standalone or playbook script
    list            - list commands available in playbook
    show            - show help and script text for a playbook script
    add             - adds a command from your history to playbook
    help [command]  - describe commands and usage

`)
	} else {
		fmt.Printf(`
Usage: scripthaus [global-opts] [command] [command-opts]

Commands:
    version         - print version and exit
    run             - runs a standalone or playbook script
    list            - list commands available in playbook
    show            - show help and script text for a playbook script
    help            - describe commands and usage
    help [command]  - specific help for particular command

Global Options:
    -v, --verbose   - more debugging output
    -q, --quiet     - do not show version and command summary info (script output only)

`)
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
	if runOpts.ScriptFile != "" {
		realScriptPath, err := pathutil.ResolveFileWithPath(runOpts.ScriptFile, "script")
		if err != nil {
			return 1, err
		}
		execCmd, err := commanddef.BuildScriptExecCommand(ctx, realScriptPath, runOpts.RunSpec)
		if err != nil {
			return 1, err
		}
		return runExecCmd(execCmd, realScriptPath, nil, gopts)
	} else {
		resolvedFileName, foundCommand, err := resolvePlaybookCommand(runOpts.PlaybookFile, runOpts.PlaybookScript, gopts)
		if foundCommand == nil || err != nil {
			return 1, err
		}
		fullScriptName := fmt.Sprintf("%s/%s", resolvedFileName, runOpts.PlaybookScript)
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

func resolveScript(cmdName string, scriptName string, opts commanddef.IResolveScript) error {
	if scriptName == "-" {
		return fmt.Errorf("invalid script '%s', cannot execute standalone script from <stdin>", scriptName)
	}
	if opts.GetPlaybookFile() != "" {
		if strings.Index(scriptName, "/") != -1 {
			return fmt.Errorf("invalid script '%s', no slash allowed when --playbook '%s' is specified", scriptName, opts.GetPlaybookFile())
		}
		opts.SetFullScriptName(opts.GetPlaybookFile() + "/" + scriptName)
		opts.SetPlaybookScript(scriptName)
		return nil
	}
	opts.SetFullScriptName(scriptName)
	if strings.HasSuffix(scriptName, "/") {
		return fmt.Errorf("invalid script '%s', cannot have a trailing slash", scriptName)
	}
	if strings.HasSuffix(scriptName, ".md") {
		return fmt.Errorf("no playbook script specified, usage: %s %s/[script]", cmdName, scriptName)
	}
	if strings.Index(scriptName, "/") != -1 {
		dirName, baseName := path.Split(scriptName)
		dirFile := dirName[:len(dirName)-1]
		if dirFile == "-" {
			opts.SetPlaybookFile("-")
			opts.SetPlaybookScript(baseName)
			return nil
		} else if path.Ext(dirFile) == ".md" {
			// an ".md" file as a directory means this is a playbook
			opts.SetPlaybookFile(dirFile)
			opts.SetPlaybookScript(baseName)
			return nil
		} else {
			// "directory" is not a .md file.  So scriptName must be a standalone ScriptFile
			opts.SetScriptFile(scriptName)
			return nil
		}
	}
	// no slash, so this must be a standalone script file
	opts.SetScriptFile(scriptName)
	return nil
}

func parseRunOpts(gopts globalOpts) (commanddef.RunOptsType, error) {
	var rtn commanddef.RunOptsType
	for argIdx := 0; argIdx < len(gopts.CommandArgs); {
		isLast := (argIdx == len(gopts.CommandArgs)-1)
		argStr := gopts.CommandArgs[argIdx]
		if argStr == "-p" || argStr == "--playbook" {
			if isLast {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.PlaybookFile = gopts.CommandArgs[argIdx+1]
			argIdx += 2
			continue
		}
		if argStr == "--docker-image" {
			if isLast {
				return rtn, fmt.Errorf("'%s [image]' missing image name", argStr)
			}
			rtn.RunSpec.DockerImage = gopts.CommandArgs[argIdx+1]
			rtn.RunSpec.SpecialMode = "docker"
			argIdx += 2
			continue
		}
		if argStr == "--docker-opts" {
			if isLast {
				return rtn, fmt.Errorf("'%s [docker-opts]' missing options", argStr)
			}
			dockerOpts, err := shellwords.Parse(gopts.CommandArgs[argIdx+1])
			if err != nil {
				return rtn, fmt.Errorf("%s '%s', error splitting docker-opts: %w", err)
			}
			rtn.RunSpec.DockerOpts = dockerOpts
			argIdx += 2
			continue
		}
		if argStr == "--env" {
			if isLast {
				return rtn, fmt.Errorf("'%s \"[VAR=VAL]\" or %s file.env' missing value", argStr)
			}
			envVal := gopts.CommandArgs[argIdx+1]
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
			argIdx += 2
			continue
		}
		if argStr == "--nolog" {
			rtn.RunSpec.NoLog = true
			rtn.RunSpec.ForceLog = false
			argIdx++
			continue
		}
		if argStr == "--log" {
			rtn.RunSpec.NoLog = false
			rtn.RunSpec.ForceLog = true
			argIdx++
			continue
		}
		if strings.HasPrefix(argStr, "-") && argStr != "-" && !strings.HasPrefix(argStr, "-/") {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus run command", argStr)
		}
		err := resolveScript("run", argStr, &rtn)
		if err != nil {
			return rtn, err
		}
		rtn.RunSpec.ScriptArgs = gopts.CommandArgs[argIdx+1:]
		break
	}
	if rtn.PlaybookScript == "" && rtn.ScriptFile == "" {
		return rtn, fmt.Errorf("Usage: scripthaus run [run-opts] [script] [script-opts], no script specified")
	}
	return rtn, nil
}

func parseListOpts(gopts globalOpts) (listOptsType, error) {
	var rtn listOptsType
	for argIdx := 0; argIdx < len(gopts.CommandArgs); {
		isLast := (argIdx == len(gopts.CommandArgs)-1)
		argStr := gopts.CommandArgs[argIdx]
		if argStr == "-p" || argStr == "--playbook" {
			if isLast {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.PlaybookFile = gopts.CommandArgs[argIdx+1]
			argIdx += 2
			continue
		}
		if strings.HasPrefix(argStr, "-") && argStr != "-" {
			return rtn, fmt.Errorf("Invalid option '%s' passed to scripthaus list command", argStr)
		}
		if rtn.PlaybookFile != "" {
			return rtn, fmt.Errorf("Usage: scripthaus list, playbook already specified with --playbook '%s', cannot list again as '%s'", rtn.PlaybookFile, argStr)
		}
		rtn.PlaybookFile = argStr
		if !isLast {
			return rtn, fmt.Errorf("Usage: scripthaus list [playbook], too many arguments passed, extras = '%s'", strings.Join(gopts.CommandArgs[argIdx+1:], " "))
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
	FullScriptName string
	PlaybookFile   string
	PlaybookScript string
}

func (opts *showOptsType) GetPlaybookFile() string {
	return opts.PlaybookFile
}

func (opts *showOptsType) SetScriptFile(val string) {
	// nothing
}

func (opts *showOptsType) SetFullScriptName(val string) {
	opts.FullScriptName = val
}

func (opts *showOptsType) SetPlaybookFile(val string) {
	opts.PlaybookFile = val
}

func (opts *showOptsType) SetPlaybookScript(val string) {
	opts.PlaybookScript = val
}

func parseShowOpts(gopts globalOpts) (showOptsType, error) {
	var rtn showOptsType
	for argIdx := 0; argIdx < len(gopts.CommandArgs); {
		isLast := (argIdx == len(gopts.CommandArgs)-1)
		argStr := gopts.CommandArgs[argIdx]
		if argStr == "-p" || argStr == "--playbook" {
			if isLast {
				return rtn, fmt.Errorf("'%s [playbook]' missing playbook name", argStr)
			}
			rtn.PlaybookFile = gopts.CommandArgs[argIdx+1]
			argIdx += 2
			continue
		}
		if strings.HasPrefix(argStr, "-") && argStr != "-" && !strings.HasPrefix(argStr, "-/") {
			return rtn, fmt.Errorf("invalid option '%s' passed to scripthaus show command", argStr)
		}
		err := resolveScript("show", argStr, &rtn)
		if err != nil {
			return rtn, err
		}
		if !isLast {
			return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], too many arguments passed, extras = '%s'", strings.Join(gopts.CommandArgs[argIdx+1:], " "))
		}
		break
	}
	if rtn.PlaybookFile == "" {
		return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], no playbook specified")
	}
	if rtn.PlaybookScript == "" {
		return rtn, fmt.Errorf("Usage: scripthaus show [playbook]/[script], no script specified")
	}
	return rtn, nil
}

func runShowCommand(gopts globalOpts) (int, error) {
	showOpts, err := parseShowOpts(gopts)
	if err != nil {
		return 1, err
	}
	resolvedFileName, foundCommand, err := resolvePlaybookCommand(showOpts.PlaybookFile, showOpts.PlaybookScript, gopts)
	if foundCommand == nil || err != nil {
		return 1, err
	}
	fullScriptName := fmt.Sprintf("%s/%s", resolvedFileName, showOpts.PlaybookScript)
	fmt.Printf("[^scripthaus] show '%s'\n\n", fullScriptName)
	fmt.Printf("%s\n%s\n\n", foundCommand.HelpText, foundCommand.RawCodeText)
	return 0, nil
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
	for argIdx := 1; argIdx < len(args); {
		// isLast := (argIdx+1 == len(args))
		argStr := args[argIdx]
		if argStr == "-v" || argStr == "--verbose" {
			opts.Verbose++
			argIdx++
			continue
		}
		if argStr == "-q" || argStr == "--quiet" {
			opts.Quiet = true
			argIdx++
			continue
		}
		if strings.HasPrefix(argStr, "-") {
			return opts, fmt.Errorf("Invalid argument '%s'", argStr)
		}
		opts.CommandName = argStr
		opts.CommandArgs = args[argIdx+1:]
		break
	}
	return opts, nil
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
