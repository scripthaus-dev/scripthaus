# ScriptHaus
 
ScriptHaus lets you run bash, Python, and JS snippets from your Markdown files directly
from the command-line. It can turn any Markdown file (including your README.md or
BUILD.md) into a command-line tool, complete with command-line help and documentation.

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
CGO_ENABLED=1 go build -o scripthaus cmd/main.go
```

This will build the `scripthaus` binary in your local directory.  You can then `mv` it to any directory in your path.

To make typing ScriptHaus commands easier, I recommend adding aliasing scripthaus to **s**

```bash
# @scripthaus command make-alias
echo 'alias s="scripthaus"' >> ~/.bash_profile
```

## Playbooks

Any markdown file can be a ScriptHaus playbook.  Commands are found by searching for any code fence
(with a valid language, e.g. "bash", "python", "js", etc.) that have the special ScriptHaus directive:

```
# @scripthaus command [name] - [short-description]
```

You can easily annotate an existing markdown file (README.md or BUILD.md) or create a new playbook with
scripts or one-liners that you want to document and be able to access easily.

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

To see the documentation and code block, run:
````
> scripthaus show commands.md
commands.md
  commands.md::hi - a simple command that just echos "hi" to the console
  
> scripthaus show commands.md::hi
[^scripthaus] show './commands.md::hi'

This is the simplest ScriptHaus command.
The markdown before the command will turn into help text.

```bash
# @scripthaus command hi - a simple command that just echos "hi" to the console
echo hi
```
````

## Project and Home Directories

Playbooks can *always* be referenced by a relative or absolute path to their markdown files.
But, ScriptHaus also supports special syntax that let you access *global*
or *project* playbooks easily.

**Global** commands are placed in $HOME/scripthaus/scripthaus.md (the directory can be overriden
by settng the environment variable $SCRIPTHAUS_HOME).  You can access any command in this
scripthaus.md file as `^[command]`.  Other md files in the ScriptHaus home directory can
be accessed as `^[file.md]::[command]`.

**Project** commands are placed in a "scripthaus.md" file at the root
of your project directory.  When you are in the project directory, or
a sub-directory you can access any command in this scripthaus.md file
as `.[command]`.  Other md files in the project root can be accessed as
`.[file.md]::[script]` (note that scripthaus.md *must* also exist to
mark the project root).

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

ScriptHaus also allows you to add a command directly from the command prompt, which is useful when you want to capture a
command from your shell history.

Here's a simple add command to add your last bash command from history ("!!" refers to the last typed command).
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

To show the raw markdown for any command in a playbook (including the command text) run:

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

