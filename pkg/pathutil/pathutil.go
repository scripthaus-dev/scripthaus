// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package pathutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/scripthaus-dev/scripthaus/pkg/base"
)

var DefaultScFile = "scripthaus.md"

var dotPrefixRe = regexp.MustCompile("^([.]+)[a-zA-Z_]")

func ScriptNameRunType(scriptName string) string {
	if strings.Index(scriptName, "::") != -1 {
		return base.RunTypePlaybook
	}
	if strings.HasSuffix(scriptName, ".py") || strings.HasSuffix(scriptName, ".js") || strings.HasSuffix(scriptName, ".sh") {
		return base.RunTypeScript
	}
	return base.RunTypePlaybook
}

// returns (playbook-name, playbook-script, error)
func SplitScriptName(scriptName string) (string, string, error) {
	if strings.Index(scriptName, "::") != -1 {
		fields := strings.SplitN(scriptName, "::", 2)
		return fields[0], fields[1], nil
	}
	if strings.HasPrefix(scriptName, "^") {
		return "^", scriptName[1:], nil
	}
	m := dotPrefixRe.FindStringSubmatch(scriptName)
	if m != nil {
		return m[1], scriptName[len(m[1]):], nil
	}
	return "", scriptName, nil
}

// return parent directory, dir should be absolute, returns "" when no more parents
func parentDir(dirName string) string {
	// dir must be absolute
	if dirName == "" || dirName == "/" || !strings.HasPrefix(dirName, "/") {
		return ""
	}
	if len(dirName) > 1 && strings.HasSuffix(dirName, "/") {
		dirName = dirName[:len(dirName)-1]
	}
	return path.Dir(dirName)
}

func findScRootDir(curDir string, allowCurrent bool) (string, error) {
	if !allowCurrent {
		curDir = parentDir(curDir)
	}
	for curDir != "" {
		fileName := path.Join(curDir, DefaultScFile)
		found, err := tryFindFile(fileName, "playbook", true)
		if err != nil {
			return "", err
		}
		if found {
			return curDir, nil
		}
		curDir = parentDir(curDir)
	}
	return "", fs.ErrNotExist
}

func resolvePlaybookInDir(rawName string, curDir string, playbookName string) (string, error) {
	if playbookName == "" {
		playbookName = DefaultScFile
	} else if strings.HasSuffix(playbookName, "/") {
		playbookName = playbookName + DefaultScFile
	}
	fullPath := path.Join(curDir, playbookName)
	finfo, err := os.Stat(fullPath)
	if err == nil && finfo.IsDir() {
		fullPath = path.Join(fullPath, DefaultScFile)
		finfo, err = os.Stat(fullPath)
	}
	if rawName == "" {
		rawName = "<default>"
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("playbook not found '%s' (resolved to '%s')", rawName, fullPath)
		}
		if errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("playbook '%s' (resolved to '%s'), permission error: %w", rawName, fullPath, err)
		}
		return "", fmt.Errorf("playbook '%s' (resolved to '%s'), stat error: %w", rawName, fullPath, err)
	}
	if finfo.IsDir() {
		return "", fmt.Errorf("playbook '%s' (resolved to '%s'), is a directory not a file", rawName, fullPath)
	}
	return fullPath, nil
}

// prefix is either "^", "[.]*" (can be empty).  empty prefix is the same as "."
func findPrefixDir(prefix string) (string, error) {
	if prefix == "^" {
		return GetScHomeDir()
	}
	curDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if prefix == "" {
		prefix = "."
	}
	for depth := 0; depth < len(prefix); depth++ {
		if prefix[depth] != '.' {
			return "", fmt.Errorf("invalid prefix character '%c'", prefix[depth])
		}
		lastCurDir := curDir
		curDir, err = findScRootDir(curDir, (depth == 0))
		if errors.Is(err, fs.ErrNotExist) {
			if depth == 0 {
				return "", fmt.Errorf("cannot find scripthaus root (scripthaus.md file) in any parent directory above '%s'", lastCurDir)
			}
			return "", fmt.Errorf("cannot find scripthaus root (scripthaus.md file) above '%s' (depth = %d)", lastCurDir, depth+1)
		}
		if err != nil {
			return "", err
		}
	}
	return curDir, nil
}

func ResolvePlaybook(playbookName string) (string, error) {
	if playbookName == "-" {
		// <stdin>
		return "-", nil
	}
	prefixMatch := base.PlaybookPrefixRe.FindStringSubmatch(playbookName)
	if prefixMatch != nil {
		// covers ^, [.]+, and also plain non-prefixed names
		if prefixMatch[1] == "" {
			found, err := tryFindFile(playbookName, "playbook", false)
			if err != nil {
				return "", err
			}
			if found {
				return "./" + playbookName, nil
			}
		}
		dirName, err := findPrefixDir(prefixMatch[1])
		if err != nil {
			return "", fmt.Errorf("cannot resolve directory for playbook '%s': %w", playbookName, err)
		}
		return resolvePlaybookInDir(playbookName, dirName, playbookName[len(prefixMatch[1]):])
	}
	if strings.HasPrefix(playbookName, "@") {
		// future namespaces
		return "", fmt.Errorf("cannot resolve playbook '%s', @-prefix not supported", playbookName)
	}
	if strings.HasPrefix(playbookName, "./") || strings.HasPrefix(playbookName, "/") || strings.HasPrefix(playbookName, "../") {
		// absolute/relative path
		lastSlash := strings.LastIndex(playbookName, "/")
		dirName := playbookName[:lastSlash+1]
		baseName := playbookName[lastSlash+1:]
		return resolvePlaybookInDir(playbookName, dirName, baseName)
	}
	return "", fmt.Errorf("invalid playbook name '%s'", playbookName)
}

func GetScHomeDir() (string, error) {
	scHome := os.Getenv(base.ScHomeVarName)
	if scHome == "" {
		homeVar := os.Getenv(base.HomeVarName)
		if homeVar == "" {
			return "", fmt.Errorf("Cannot resolve scripthaus home directory (SCRIPTHAUS_HOME and HOME not set)")
		}
		scHome = path.Join(homeVar, "scripthaus")
	}
	return scHome, nil
}

// returns (found, error)
func tryFindFile(fileName string, fileType string, ignorePermissionErr bool) (bool, error) {
	finfo, err := os.Stat(fileName)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if ignorePermissionErr && errors.Is(err, fs.ErrPermission) {
		if ignorePermissionErr {
			return false, nil
		}
		return true, fmt.Errorf("cannot access %s file at '%s': %w", fileType, fileName, err)
	}
	if finfo.IsDir() {
		return false, nil
	}
	return true, err
}

func ResolveFileWithPath(fileName string, fileType string) (string, error) {
	if fileName == "-" || fileName == "<stdin>" {
		return "<stdin>", nil
	}
	found, err := tryFindFile(fileName, fileType, false)
	if !found {
		return "", fmt.Errorf("cannot find %s file '%s'", fileType, fileName)
	}
	return fileName, err
}

const MaxShebangLine = 100

var ShebangRe = regexp.MustCompile("^/[a-zA-Z0-9/_-]+$")

// does not allow spaces, must start with initial '/'
func ReadShebang(data []byte) string {
	firstNl := bytes.Index(data, []byte{'\n'})
	if firstNl == -1 {
		return ""
	}
	if firstNl > MaxShebangLine {
		return ""
	}
	str := string(data[:firstNl])
	if !strings.HasPrefix(str, "#!/") {
		return ""
	}
	str = strings.TrimSpace(str[2:])
	if !ShebangRe.MatchString(str) {
		return ""
	}
	return str
}

func ReadShebangFromFile(fileName string) string {
	fd, err := os.Open(fileName)
	if err != nil {
		return ""
	}
	defer fd.Close()
	data, err := io.ReadAll(fd)
	if err != nil {
		return ""
	}
	return ReadShebang(data)
}

// returns (found, bytes, err)
func TryReadFile(fullPath string, fileType string, ignorePermissionErr bool) (bool, []byte, error) {
	fd, err := os.Open(fullPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil, nil
	}
	if ignorePermissionErr && errors.Is(err, fs.ErrPermission) {
		return false, nil, nil
	}
	if err != nil {
		return true, nil, fmt.Errorf("cannot open %s at '%s': %w", fileType, fullPath, err)
	}
	defer fd.Close()
	rtnBytes, err := io.ReadAll(fd)
	if err != nil {
		return true, nil, fmt.Errorf("cannot read %s at '%s': %w", fileType, fullPath, err)
	}
	return true, rtnBytes, nil
}
