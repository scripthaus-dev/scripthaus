 # ScriptHaus

ScriptHaus is a command line tool that helps you organize your scripts and bash one-liners
into self-documenting markdown files.

* Stay Organized - Store your bash one-liners in a simple markdown file
* Save Commands - Easily save a command from history to run or view later
* Execute - Run and view your commands directly from the command-line
* Share - Save your files in git and share them with your team

ScriptHaus is open source and licensed under the MPLv2.

## Install

ScriptHaus can be installed on Mac OS X (recommended) or Linux (experimental)
using [homebrew](https://brew.sh).

```
brew tap scripthaus-dev/scripthaus
brew install scripthaus
```

To install from source (requires go version 1.17+):

```
git clone https://github.com/scripthaus-dev/scripthaus.git
cd scripthaus
go build -o scripthaus cmd/main.go
```

This will build the `scripthaus` binary in your local directory.  You can then `mv` it to any directory in your path.

To make typing ScriptHaus commands easier, I recommend adding `alias s="scripthaus"` to your `.bash_profile`.

## Playbooks and Scripts

ScriptHaus allows you to organize your bash one-liners and small Python and JavaScript scripts into Markdown "playbooks"
(playbooks must have the extension ".md").

For safety, ScriptHaus uses a path ($SCRIPTHAUS\_PATH) to find playbooks.  The
current directory "." is not in the default path, so either add "." to your SCRIPTHAUS_PATH or explicitly reference playbooks
in the current directory with "./playbook.md".

Scripts are contained within playbooks.  A script begins with a level 4 header with the name of the command in backticks.
Then add a code fence with the text of your script.  The code fence should have its language set to an allowed scripthaus type
(sh, bash, python, python2, python3, js, or node) with the **extra tag** "scripthaus" after the language.  Any additional markdown between the
header and the code fence is documentation.  Here's a simple example:

````markdown
#### `hi`

A simple command that just echos "hi" to the console.

```bash scripthaus
echo hi
```
````

## Running a Script

To run a script from a playbook use `scripthaus run [playbook]/[script]`.

## Adding a Script to Playbook

You can always edit the markdown files by hand.  That's the recommended way of converting your old text notes with commands
into a runnable ScriptHaus playbook.

But, ScriptHaus also allows you to add a command directly from the command-line, which is useful when you want to capture a
command line from your bash history.

Here's the simplest add command to add your last bash command from history ("!!" refers to the last typed command).
Note that for safety, "./test.md" must already exist (scripthaus add will not create a new file).  If you need to
create a new file, just `touch ./test.md` before running "scripthaus add".

```bash
scripthaus add -t bash ./test.md/useful-command -m "optional command description" -c "!!"
```

## Showing Scripts and Playbooks

To list all the commands in a playbook run:

```
scripthaus show [playbook]
```

To show the raw markdown for any script in a playbook (including the command text) just run:

```
scripthaus show [playbook]/[script]
```

## More Resources

ScriptHaus is under active development.  Please report and bugs here in the GitHub issues tracker.  If you enjoy using
ScriptHaus, or for any questions, feature requests, feeback, or help, please [Join the ScriptHaus Discord](https://discord.gg/XfvZ334gwU)!

