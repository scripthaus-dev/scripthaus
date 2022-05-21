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
)

const PATH_VAR = "SCRIPTHAUS_PATH"

// SCRIPTHAUS_PATH (defaults to $HOME/scripthaus) (note that "." is not in the path by default)
// foo.md <- finds this playbook in the path
// ./foo.md <- finds thi playbook in local directory

// returns list of directories in SCRIPTHAUS_PATH
func GetSCPath() []string {
	scPath := os.Getenv(PATH_VAR)
	if scPath == "" {
		homePath := os.Getenv("HOME")
		if homePath == "" {
			return nil
		}
		return []string{path.Join(homePath, "scripthaus")}
	}
	dirs := strings.Split(scPath, ":")
	return dirs
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
	if strings.HasPrefix(fileName, ".../") {
		baseName := strings.Replace(fileName, ".../", "", 1)
		if baseName == "" || strings.HasSuffix(baseName, "/") {
			return "", fmt.Errorf("invalid %s file path '%s' (no base file)", fileType, fileName)
		}
		workingDir, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot get current working directory: %w", err)
		}
		parentDirs := getParentDirectories(workingDir)
		for _, parentDir := range parentDirs {
			fullPath := path.Join(parentDir, baseName)
			found, err := tryFindFile(fullPath, fileType, true)
			if found {
				if err != nil {
					return "", err
				} else {
					return fullPath, nil
				}
			}
		}
		return "", fmt.Errorf("cannot find %s file '%s'", fileType, fileName)
	}
	if strings.Index(fileName, "/") != -1 {
		found, err := tryFindFile(fileName, fileType, false)
		if !found {
			return "", fmt.Errorf("cannot find %s file '%s'", fileType, fileName)
		}
		return fileName, err
	}
	scPaths := GetSCPath()
	for _, scPath := range scPaths {
		fullPath := path.Join(scPath, fileName)
		found, err := tryFindFile(fullPath, fileType, false)
		if found {
			if err != nil {
				return "", err
			} else {
				return fullPath, nil
			}
		}
	}
	return "", fmt.Errorf("cannot find %s file '%s'", fileType, fileName)
}

func getParentDirectories(dirName string) []string {
	if !strings.HasPrefix(dirName, "/") {
		// should only pass absolute paths
		return nil
	}
	var dirs []string
	for {
		dirs = append(dirs, dirName)
		if len(dirName) > 1 && strings.HasSuffix(dirName, "/") {
			dirName = dirName[:len(dirName)-1]
		}
		if dirName == "" || dirName == "/" {
			break
		}
		dirName = path.Dir(dirName)
	}
	return dirs
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
func tryReadFile(fullPath string, fileType string, ignorePermissionErr bool) (bool, []byte, error) {
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

// returns (real file name, contents, error)
func ReadFileFromPath(fileName string, fileType string) (string, []byte, error) {
	if fileName == "-" || fileName == "<stdin>" {
		rtnBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", nil, fmt.Errorf("cannot read from <stdin>: %w", err)
		}
		return "<stdin>", rtnBytes, nil
	}
	if strings.HasPrefix(fileName, ".../") {
		workingDir, err := os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("cannot get current working directory: %w", err)
		}
		parentDirs := getParentDirectories(workingDir)
		baseName := strings.Replace(fileName, ".../", "", 1)
		if baseName == "" || strings.HasSuffix(baseName, "/") {
			return "", nil, fmt.Errorf("invalid %s path '%s' (no base file)", fileType, fileName)
		}
		for _, parentDir := range parentDirs {
			fullPath := path.Join(parentDir, baseName)
			found, rtnBytes, err := tryReadFile(fullPath, fileType, true)
			if found {
				return fullPath, rtnBytes, err
			}
		}
	}

	if strings.Index(fileName, "/") != -1 {
		// this is an absolute value, read from FS
		found, rtnBytes, err := tryReadFile(fileName, fileType, false)
		if found {
			return fileName, rtnBytes, err
		}
		return "", nil, fmt.Errorf("cannot find %s at '%s' (file not found)", fileType, fileName)
	}

	// look up in path (using SCRIPTHAUS_PATH)
	scPaths := GetSCPath()
	for _, scPath := range scPaths {
		fullPath := path.Join(scPath, fileName)
		found, rtnBytes, err := tryReadFile(fullPath, fileType, false)
		if found {
			return fullPath, rtnBytes, err
		}
	}
	return "", nil, fmt.Errorf("cannot find %s file '%s'", fileType, fileName)
}
