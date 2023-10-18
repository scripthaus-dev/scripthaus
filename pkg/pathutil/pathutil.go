// Copyright 2023 Michael Sawka
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

type ResolvedPlaybook struct {
	OrigName      string // the name passed by the user
	CanonicalName string // canonicalized name (for history)
	ResolvedFile  string // the absolute resolved file name of playbook
	ProjectDir    string // if this is a project playbook, this is the project directory
	ProjectName   string // if this is a project playbook, this is the project name (unused right now)
}

func (pb *ResolvedPlaybook) OrigShowStr() string {
	if pb.OrigName == pb.ResolvedFile {
		return pb.OrigName
	}
	return fmt.Sprintf("'%s' (%s)", pb.OrigName, pb.ResolvedFile)
}

func (pb *ResolvedPlaybook) PlaybookDir() string {
	if pb.CanonicalName == "-" {
		return ""
	}
	return path.Dir(pb.ResolvedFile)
}

// sets overrides for testing
type Resolver struct {
	TestMode        bool
	Cwd             string
	ScHomeDir       string
	TestFiles       []string
	TestDirs        []string
	TestBadPermDirs []string
}

type resolveStatInfo struct {
	IsDir bool
}

// returns IsDir(), err
func (r Resolver) statInfo(fileName string) (resolveStatInfo, error) {
	if !r.TestMode {
		finfo, err := os.Stat(fileName)
		if err != nil {
			return resolveStatInfo{}, err
		}
		return resolveStatInfo{IsDir: finfo.IsDir()}, nil
	} else {
		if !strings.HasPrefix(fileName, "/") {
			fileName = path.Join(r.Cwd, fileName)
		}
		if inSlice(fileName, r.TestFiles) {
			return resolveStatInfo{}, nil
		}
		if len(fileName) > 1 && strings.HasSuffix(fileName, "/") {
			fileName = fileName[:len(fileName)-1]
		}
		if inSlice(fileName, r.TestDirs) {
			return resolveStatInfo{IsDir: true}, nil
		}
		baseName := path.Base(fileName)
		for _, badDir := range r.TestBadPermDirs {
			if fileName == badDir || path.Join(badDir, baseName) == fileName {
				return resolveStatInfo{}, fs.ErrPermission
			}
		}
		return resolveStatInfo{}, fs.ErrNotExist
	}
}

func inSlice(s string, arr []string) bool {
	for _, val := range arr {
		if s == val {
			return true
		}
	}
	return false
}

func (r Resolver) Getwd() (string, error) {
	if r.Cwd != "" {
		return r.Cwd, nil
	}
	return os.Getwd()
}

func (r Resolver) GetScHomeDir() (string, error) {
	if r.ScHomeDir != "" {
		return r.ScHomeDir, nil
	}
	return GetScHomeDir()
}

func DefaultResolver() Resolver {
	return Resolver{}
}

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
	if strings.HasSuffix(scriptName, ".md") {
		return scriptName, "", nil
	}
	if strings.HasPrefix(scriptName, "^") {
		return "^", scriptName[1:], nil
	}
	m := dotPrefixRe.FindStringSubmatch(scriptName)
	if m != nil {
		return m[1], scriptName[len(m[1]):], nil
	}
	if strings.HasPrefix(scriptName, ".") {
		return scriptName, "", nil
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

func (r Resolver) findScRootDir(curDir string, allowCurrent bool) (string, error) {
	if !allowCurrent {
		curDir = parentDir(curDir)
	}
	for curDir != "" {
		fileName := path.Join(curDir, DefaultScFile)
		found, err := r.tryFindFile(fileName, "playbook", true)
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

func (r Resolver) resolvePlaybookInDir(origName string, curDir string, playbookName string) (string, error) {
	if playbookName == "" {
		playbookName = DefaultScFile
	} else if strings.HasSuffix(playbookName, "/") {
		playbookName = playbookName + DefaultScFile
	}
	fullPath := path.Join(curDir, playbookName)
	finfo, err := r.statInfo(fullPath)
	if err == nil && finfo.IsDir {
		fullPath = path.Join(fullPath, DefaultScFile)
		finfo, err = r.statInfo(fullPath)
	}
	displayName := origName
	if displayName == "" {
		displayName = "<default>"
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("playbook not found '%s' (resolved to '%s')", displayName, fullPath)
		}
		if errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("playbook '%s' (resolved to '%s'), permission error: %w", displayName, fullPath, err)
		}
		return "", fmt.Errorf("playbook '%s' (resolved to '%s'), stat error: %w", displayName, fullPath, err)
	}
	if finfo.IsDir {
		return "", fmt.Errorf("playbook '%s' (resolved to '%s'), is a directory not a file", displayName, fullPath)
	}
	return fullPath, nil
}

// prefix is either "^", "[.]*" (can be empty).  empty prefix is the same as "."
func (r Resolver) FindPrefixDir(prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("invalid empty prefix dir passed to FindPrefixDir (internal error)")
	}
	if prefix == "^" {
		return r.GetScHomeDir()
	}
	curDir, err := r.Getwd()
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
		curDir, err = r.findScRootDir(curDir, (depth == 0))
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

func (r Resolver) ResolvePlaybook(playbookName string) (*ResolvedPlaybook, error) {
	if playbookName == "-" {
		// <stdin>
		return &ResolvedPlaybook{
			OrigName:      "-",
			CanonicalName: "-",
			ResolvedFile:  "-",
		}, nil
	}
	prefixMatch := base.PlaybookPrefixRe.FindStringSubmatch(playbookName)
	if prefixMatch != nil {
		// covers ^, [.]+, and also plain non-prefixed names
		prefix := prefixMatch[1]
		if prefix == "" {
			curDir, err := r.Getwd()
			if err != nil {
				return nil, fmt.Errorf("cannot get current working directory: %w", err)
			}
			fullPath := path.Join(curDir, playbookName)
			found, err := r.tryFindFile(fullPath, "playbook", false)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("playbook not found '%s' (resolved to '%s')", playbookName, fullPath)
			}
			return &ResolvedPlaybook{
				OrigName:      playbookName,
				CanonicalName: fullPath,
				ResolvedFile:  fullPath,
			}, nil
		}
		dirName, err := r.FindPrefixDir(prefix)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve directory for playbook '%s': %w", playbookName, err)
		}
		noPrefixName := playbookName[len(prefix):]
		resolvedFile, err := r.resolvePlaybookInDir(playbookName, dirName, noPrefixName)
		if err != nil {
			return nil, err
		}
		if prefix == "^" {
			return &ResolvedPlaybook{
				OrigName:      playbookName,
				CanonicalName: playbookName,
				ResolvedFile:  resolvedFile,
			}, nil
		} else {
			return &ResolvedPlaybook{
				OrigName:      playbookName,
				CanonicalName: "." + noPrefixName,
				ResolvedFile:  resolvedFile,
				ProjectDir:    dirName,
			}, nil
		}
	}
	if strings.HasPrefix(playbookName, "@") {
		// future namespaces
		return nil, fmt.Errorf("cannot resolve playbook '%s', @-prefix not supported", playbookName)
	}
	if strings.HasPrefix(playbookName, "./") || strings.HasPrefix(playbookName, "/") || strings.HasPrefix(playbookName, "../") {
		// absolute/relative path
		var fullPath string
		if strings.HasPrefix(playbookName, "/") {
			fullPath = playbookName
		} else {
			curDir, err := r.Getwd()
			if err != nil {
				return nil, fmt.Errorf("cannot get current working directory: %w", err)
			}
			fullPath = path.Clean(path.Join(curDir, playbookName))
		}
		var resolvedFile string
		var err error
		if strings.HasSuffix(fullPath, "/") {
			resolvedFile, err = r.resolvePlaybookInDir(playbookName, fullPath, "")
		} else {
			dirName := path.Dir(fullPath)
			baseName := path.Base(fullPath)
			resolvedFile, err = r.resolvePlaybookInDir(playbookName, dirName, baseName)
		}
		if err != nil {
			return nil, err
		}
		return &ResolvedPlaybook{
			OrigName:      playbookName,
			CanonicalName: resolvedFile,
			ResolvedFile:  resolvedFile,
		}, nil
	}
	return nil, fmt.Errorf("invalid playbook name '%s'", playbookName)
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

func (r Resolver) tryFindFiles(dirName string, names []string, fileType string, ignorePermissionErr bool) (bool, string, error) {
	for _, fileName := range names {
		fullName := path.Join(dirName, fileName)
		found, err := r.tryFindFile(fullName, fileType, ignorePermissionErr)
		if err != nil {
			return false, "", err
		}
		if found {
			return true, fileName, nil
		}
	}
	return false, "", nil
}

// returns (found, error)
func (r Resolver) tryFindFile(fileName string, fileType string, ignorePermissionErr bool) (bool, error) {
	finfo, err := r.statInfo(fileName)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if ignorePermissionErr && errors.Is(err, fs.ErrPermission) {
		if ignorePermissionErr {
			return false, nil
		}
		return true, fmt.Errorf("cannot access %s file at '%s': %w", fileType, fileName, err)
	}
	if finfo.IsDir {
		return false, nil
	}
	return true, err
}

func (r Resolver) ResolveFileWithPath(fileName string, fileType string) (string, error) {
	if fileName == "-" || fileName == "<stdin>" {
		return "<stdin>", nil
	}
	found, err := r.tryFindFile(fileName, fileType, false)
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
