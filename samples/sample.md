## Command Section #1

This is a simple command. It says hello!

```bash
# @scripthaus command hi
echo hi
```

Another test command, but it cares about *who you are*.

```bash
# @scripthaus command greeting
echo hi $NAME, nice to $1 you \(from $0\)
```

## Python / Node

```python3
# @scripthaus command pygreeting
import sys
print(f"Hello from python {sys.argv}")
```

```node
// @scripthaus command jsgreeting
console.log("Hello from node")
```

## Other

Counts the number of lines of Go code in current directory and sub-directories.

```bash
# @scripthaus command golines
find . -name "*.go" | xargs wc  -l
```

Grep for some term in go files (removes node_modules)

```bash
# @scripthaus command ggrep
find . \( -name node_modules -or -name .git \) -prune -name notpruned -or -name "*.go" | xargs grep "$@"
```

