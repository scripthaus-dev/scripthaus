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
    history         - show command history
    manage          - manage history items
    help            - describe commands and usage
    help [command]  - specific help for particular command

Global Options:
    -p, --playbook [file]    - specify a playbook to use
    -v, --verbose            - more debugging output
    -q, --quiet              - do not show version and command summary info (script output only)

Resources:
    github          - https://github.com/scripthaus-dev/scripthaus
    homepage        - https://www.scripthaus.dev
    discord         - https://discord.gg/XfvZ334gwU
`)

var RunText = strings.TrimSpace(`
Usage: scripthaus run [run-opts] [script-name] [script-arguments]

The 'run' command will run a standalone or playbook script.  If [script-name] is an
executable file it will be executed directly.  If it has a known extension
.py, .sh, or .js it will be executed using python, bash, or node respectively.

Otherwise [script-name] will be interpreted as a playbook command.  Playbook
commands follow the format [playbook]::[command].

The playbook can always be specified as a relative or absolute path.

The playbook can also be a reference to your global ScriptHaus directory
by using "^" or your project ScriptHaus directory by using ".".  When
using the default "scripthaus.md" file you can omit the "::".

Examples:
  scripthaus run ./test.md::hello # runs the 'hello' command from ./test.md
  scripthaus run ^grep-files      # runs the 'grep-files' command from your global scripthaus.md
  scripthaus run .run-webserver   # runs the 'run-webserver command from your project's scripthaus.md file
  scripthaus run .build.md::test  # runs the 'test' command from the build.md file in your project root

If the global '--playbook' option is given, then 'script' will always be 
interpreted as a script inside of the given playbook.

Any arguments after 'script' will be passed verbatim as options to the script.

Run Options:
    --nolog                  - will not log this command to scripthaus history
    --log                    - force logging of command to scripthaus history (default)
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
and list all of their commands.  Playbook can be a relative or absolute path,
or a reference to the global ScriptHaus directory "^" or the project
ScriptHaus directory ".".

List Options:
    none
`)

var ShowText = strings.TrimSpace(`
Usage: scripthaus show [show-opts] [playbook]::[script]
       scripthaus show [show-opts] [playbook]

The 'show' command will show the help for a particular script in a playbook.
By default it will show the markdown text and the code block that
make up the script.

If no script is given, this will behave like the 'list' command and
show all of the scripts in the given playbook file.

Note that playbook may also be specified using the global --playbook option.

Show Options:
    none
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
* Execute - Run and view your commands directly from the command-line
* Share - Save your files in git and share them with your team

Commands:
    run             - runs a standalone or playbook script
    list            - list commands available in playbook
    show            - show help and script text for a playbook script
    add             - adds a command from your history to playbook
    history         - show command history
    help [command]  - describe commands and usage

Resources:
    github          - https://github.com/scripthaus-dev/scripthaus
    homepage        - https://www.scripthaus.dev
    discord         - https://discord.gg/XfvZ334gwU
`)

var AddText = replaceBacktick(strings.TrimSpace(`
Usage: scripthaus add [add-opts] [playbook]::[script] -c "[command]"
       scripthaus add [add-opts] [playbook]::[script] -- [command]...
       scripthaus add [add-opts] [playbook]::[script] - < [command-file]

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
    -m, --message [message]  - add some help text for the command.  markdown, will be added
                               above the code fence.
    -c [script-text]         - the text for the script to be added
    --dry-run                - print messages, but do not modify playbook file
`))

var HistoryText = replaceBacktick(strings.TrimSpace(`
Usage: scripthaus history [history-opts]

The history command will show you the last 50 scripthaus commands.

History Options:
    -n [num]                 - print last n commands
    --all                    - print all history
    --full                   - show full history item (all fields, multiple lines)
    --json                   - output full records in JSON format (can process with jq)
`))

var ManageText = replaceBacktick(strings.TrimSpace(`
Usage: scripthaus manage clear-history
       scripthaus manage delete-db
       scripthaus manage remove-history-range [start-id] [end-id]
       scripthaus manage renumber-history

The manage command contains commands to help manage the history database.

clear-history        - will remove all the history items
delete-db            - will completely delete the scripthaus history database (rm the file)
remove-history-range - removes the history items between start-id and end-id inclusive
renumber-history     - will renumber history items by timestamp (starting at 1)

`))

func replaceBacktick(str string) string {
	return strings.ReplaceAll(str, "[:backtick]", "`")
}
