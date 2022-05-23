package helptext

import "strings"

var MainHelpText = strings.TrimSpace(`
Usage: scripthaus [global-opts] [command] [command-opts]

Commands:
    version         - print version and exit
    run             - runs a standalone or playbook script
    list            - list commands available in playbook
    add             - quickly add a command to a playbook
    show            - show help and script text for a playbook script
    help            - describe commands and usage
    help [command]  - specific help for particular command

Global Options:
    -v, --verbose   - more debugging output
    -q, --quiet     - do not show version and command summary info (script output only)
`)

var RunText = strings.TrimSpace(`
Usage: scripthaus run [run-opts] [script] [script-arguments]
       scripthaus run [run-opts] [playbook]/[script] [script-arguments]

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

var ListText = strings.TrimSpace(`
Usage: scripthaus [global-opts] list [list-opts] [playbook]

The 'list' command will list the scripts available to run in the given
playbook.  The playbook can optionally be passed via the -p option.

If no playbook is passed list will find all playbooks in the SCRIPTHAUS_PATH
and list all of their commands.

List Options:
    -p, --playbook [file]    - specify a playbook to use
`)

var ShowText = strings.TrimSpace(`
Usage: scripthaus show [show-opts] [playbook]/[script]

The 'show' command will show the help for a particular script in a playbook.
By default it will show the markdown text and the code block that
make up the script.

List Options:
    -p, --playbook [file]    - specify a playbook to use
`)

var VersionText = strings.TrimSpace(`
Usage: scripthaus version

The version command has no options \U0001f643\n\n
`)

var OverviewText = strings.TrimSpace(`
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

var AddText = replaceBacktick(strings.TrimSpace(`
Usage: scripthaus add [show-opts] [playbook]/[script] -c "[command]"
       scripthaus add [show-opts] [playbook]/[script] -- [command]...
       scripthaus add [show-opts] [playbook]/[script] - < [command-file]

The 'add' command will add a command to the playbook specified, and give it
the name [scriptname].  There are three ways to specify a command:

The first, with "-c" passes the command as a single argument which
is appropriate for passing history items, e.g. -c "!!" or -c "[:backtick]fc -ln 500 502[:backtick]"

The second form with a "--" will read all the following arguments
as the command (and separate the arguments with spaces), 
e.g. -- echo -n "hello".

The third form with "-" will read the command from stdin.
This works great for importing an existing script or to grab
a set of history commands e.g. - 

Add Options:
    -t, --type [scripttype]  - (required) the language type for the script (e.g. bash, python3)
    -p, --playbook [file]    - specify a playbook to use
    -m, --message [message]  - add some help text for the command.  markdown, will be added
                               above the code fence.
    -c [script-text]         - the text for the script to be added
    --dry-run                - print messages, but do not modify playbook file
`))

func replaceBacktick(str string) string {
	return strings.ReplaceAll(str, "[:backtick]", "`")
}
