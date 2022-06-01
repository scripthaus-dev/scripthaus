 # ScriptHaus

ScriptHaus is a command line tool that helps you organize your scripts and bash one-liners
into self-documenting markdown files.

* **Stay Organized** - Store your bash one-liners in simple markdown playbooks
* **Save Commands** - Easily save a command from history to run or view later
* **Execute** - Run your commands and view documentation directly from the command-lin
* **Never Forget** - Store history by command and playbook, including options, date, cwd, and exitcode
* **Share** - Save your playbooks in git and share them with your team

ScriptHaus is open source and licensed under the MPLv2.

## Install

ScriptHaus can be installed on Mac OS X (recommended) or Linux (experimental)
using [homebrew](https://brew.sh).

```bash
# @scripthaus command brew-install
brew tap scripthaus-dev/scripthaus
brew install scripthaus
```

To install from source (requires go version 1.17+):

```bash
# @scripthaus command source-install
git clone https://github.com/scripthaus-dev/scripthaus.git
cd scripthaus
go build -o scripthaus cmd/main.go
```

This will build the `scripthaus` binary in your local directory.  You can then `mv` it to any directory in your path.

To make typing ScriptHaus commands easier, I recommend adding aliasing scripthaus to **s**

```bash
# @scripthaus command make-alias
echo 'alias s="scripthaus"' >> ~/.bash_profile
```

## Playbooks

ScriptHaus allows you to organize your bash one-liners and small JS and Python scripts into Markdown playbooks.

Commands are contained within playbooks.  You can turn any code fence (with a valid language, e.g. "bash", "python", "js", etc.)
into a ScriptHaus command by adding the ScriptHaus *directive*:

```
# @scripthaus command [name] - [short-description]
```

Any markdown that comes before the command, up until you hit a level 1-4 header, thematic break, or another code fence will
become the command's documentation.

````markdown
This is the simplest ScriptHaus command.
The markdown before the command will turn into help text.

```bash
# @scripthaus command hi - a simple command that just echos "hi" to the console
echo hi
```
````

Assuming that markdown was placed into a file named "commands.md" you can run:
```
> scripthaus run commands.md::hi
hi
```

## Project and Home Directories

Playbooks can *always* be referenced by a relative or absolute path to their markdown files.
But, ScriptHaus also supports special special files that let access *global*
or *project* commands easily.

**Global** commands are placed in $HOME/scripthaus/scripthaus.md (the directory can be overriden
by settng the environment variable $SCRIPTHAUS_HOME).  You can access any command in this
scripthaus.md file as `^[script]`.  Other md files in the ScriptHaus home directory can
be easily accessed as `^[file.md]::[script]`.

**Project** commands are placed in a "scripthaus.md" file at the root of your project directory
(normally a sibling of your .git directory).  When you are in the project directory, or a sub-directory
you can access any command in this scripthaus.md file as `.[script]`.  Other md files in the project
root can be accessed as `.[file.md]::[script]` (note that scripthaus.md *must* also exist).

## Running a Command

To run a command from a playbook use `scripthaus run [playbook]::[command]`.

You can also reference scripts from your *global* or *project* roots.  Here are some examples:

```
scripthaus run ./test.md::hello # runs the 'hello' command from ./test.md
scripthaus run ^grep-files      # runs the 'grep-files' command from your global scripthaus.md
scripthaus run .run-webserver   # runs the 'run-webserver command from your project's scripthaus.md file
scripthaus run .build.md::test  # runs the 'test' command from the build.md file in your project root

# runs the 'test' command from ./build.md if it exists, otherwise trys to find build.md in your project root
scripthaus run build.md::test
```

## Adding a Command to Playbook

You can always edit the markdown files by hand.  That's the recommended way of converting your old text notes with commands
into a runnable ScriptHaus playbook.

ScriptHaus also allows you to add a command directly from the command-line, which is useful when you want to capture a
command line from your bash history.

Here's the simplest add command to add your last bash command from history ("!!" refers to the last typed command).
Note that for safety, "test.md" must already exist (scripthaus add will not create a new file).  If you need to
create a new file, just `touch ./test.md` before running "scripthaus add".  You can also use "^" and "." to add
to your global or project md files.

```bash
scripthaus add -t bash ./test.md::useful-command -m "optional command description" -c "!!"
scripthaus add -t bash ^s3-upload -m "upload files to my s3 bucket" -c "!!"
```

## Showing Scripts and Playbooks

To list all the commands in a playbook run:

```
scripthaus show [playbook]
```

To show the raw markdown for any command in a playbook (including the command text) just run:

```
scripthaus show [playbook]::[script]
```

## Credits

Special thanks to [Adam Bouhenguel @ajbouh](https://github.com/ajbouh) who had the initial idea of 
using Markdown to write and document scripts that can be executed from the command-line.
Adam also contributed the initial proof of concept code.

## More Resources

ScriptHaus is under active development. Please report and bugs here in the GitHub issues tracker.
For more questions, please see the [FAQ](./FAQ.md).

If you enjoy using ScriptHaus, or for any questions, feature requests, feeback,
or help, please [Join the ScriptHaus Discord](https://discord.gg/XfvZ334gwU)!

