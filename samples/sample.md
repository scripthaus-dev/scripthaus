## Command Section #1

#### `hi`

This is a simple command. It says hello!

``` bash scripthaus
echo hi
```

#### `greeting`

Another test command, but it cares about *who you are*.

```bash scripthaus
# @scripthaus require NAME
echo hi $NAME, nice to $1 you \(from $0\)
```

## Python / Node

#### `pygreeting`

``` python3 scripthaus
import sys
print(f"Hello from python {sys.argv}")
```

#### `jsgreeting`

``` node scripthaus
console.log("Hello from node")
```

## Other

Counts the number of lines of Go code in current directory and sub-directories.

#### `golines`

```bash scripthaus
find . -name "*.go" | xargs wc  -l
```

#### `ggrep`

Grep for some term in go files (removes node_modules)

```bash scripthaus
find . \( -name node_modules -or -name .git \) -prune -name notpruned -or -name "*.go" | xargs grep "$@"
```

