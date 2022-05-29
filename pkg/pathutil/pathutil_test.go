// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package pathutil

import (
	"testing"
)

func trySplit(t *testing.T, input string, goodPb string, goodPs string) {
	pb, ps, err := SplitScriptName(input)
	if err != nil {
		t.Errorf("Error splitting %s: %v", input, err)
	} else if pb != goodPb || ps != goodPs {
		t.Errorf("Error splitting %s, got [%s] [%s], expected [%s] [%s]", input, pb, ps, goodPb, goodPs)
	}
}

func TestSplitScriptName(t *testing.T) {
	trySplit(t, "^foo", "^", "foo")
	trySplit(t, ".foo", ".", "foo")
	trySplit(t, "..foo", "..", "foo")
	trySplit(t, "hello", "", "hello")
	trySplit(t, "@sawka::foo", "@sawka", "foo")
	trySplit(t, ".hello.md::test", ".hello.md", "test")
	trySplit(t, "./foo.md::bar", "./foo.md", "bar")
}

func tryResolve(t *testing.T, resolver Resolver, input string, goodResolve string, shouldError bool) {
	rfn, err := resolver.ResolvePlaybook(input)
	if shouldError {
		if err == nil {
			t.Errorf("Invalid resolve %s -- should have error, but did not", input)
		}
	} else {
		if err != nil {
			t.Errorf("Error resolving %s: %v", input, err)
		} else if goodResolve != rfn {
			t.Errorf("Error resolving %s, got [%s], expected [%s]", input, rfn, goodResolve)
		}
	}
}

func TestResolvePlaybook(t *testing.T) {
	resolver := Resolver{
		TestMode:  true,
		Cwd:       "/*test/home/project/subproject1/subdir2",
		ScHomeDir: "/*test/home/scripthaus",
		TestDirs: []string{
			"/*test/home",
			"/*test/home/scripthaus",
			"/*test/home/scripthaus/alt",
			"/*test/home/project",
			"/*test/home/project/subproject1",
			"/*test/home/project/subproject2",
			"/*test/home/project/subproject1/subdir1",
			"/*test/home/project/subproject1/subdir2",
		},
		TestBadPermDirs: []string{"/", "/*test"},
		TestFiles: []string{
			"/*test/home/scripthaus/scripthaus.md",
			"/*test/home/scripthaus/foo.md",
			"/*test/home/scripthaus/alt/more.md",
			"/*test/home/project/scripthaus.md",
			"/*test/home/project/p1.md",
			"/*test/home/project/subproject1/scripthaus.md",
			"/*test/home/project/subproject1/commands.md",
			"/*test/home/project/subproject1/subdir2/foo.md",
			"/*test/home/project/subproject2/scripthaus.md",
			"/*test/home/project/subproject2/more-commands.md",
		},
	}
	tryResolve(t, resolver, "-", "-", false)
	tryResolve(t, resolver, "^", "/*test/home/scripthaus/scripthaus.md", false)
	tryResolve(t, resolver, "^foo.md", "/*test/home/scripthaus/foo.md", false)
	tryResolve(t, resolver, "commands.md", "/*test/home/project/subproject1/commands.md", false)
	tryResolve(t, resolver, ".commands.md", "/*test/home/project/subproject1/commands.md", false)
	tryResolve(t, resolver, ".commands.md", "/*test/home/project/subproject1/commands.md", false)
	tryResolve(t, resolver, "foo.md", "/*test/home/project/subproject1/subdir2/foo.md", false)
	tryResolve(t, resolver, "./foo.md", "/*test/home/project/subproject1/subdir2/foo.md", false)
	tryResolve(t, resolver, "@sawka", "", true)
	tryResolve(t, resolver, "@sawka/foo.md", "", true)
	tryResolve(t, resolver, "..", "/*test/home/project/scripthaus.md", false)
	tryResolve(t, resolver, "..p1.md", "/*test/home/project/p1.md", false)
	tryResolve(t, resolver, "..subproject2/more-commands.md", "/*test/home/project/subproject2/more-commands.md", false)
	tryResolve(t, resolver, "..subproject2", "/*test/home/project/subproject2/scripthaus.md", false)
	tryResolve(t, resolver, "..subproject2/", "/*test/home/project/subproject2/scripthaus.md", false)
	tryResolve(t, resolver, "/*test/home/project/p1.md", "/*test/home/project/p1.md", false)
	tryResolve(t, resolver, "/*test/home/project", "/*test/home/project/scripthaus.md", false)
	tryResolve(t, resolver, "/*test/home/project/", "/*test/home/project/scripthaus.md", false)

	tryResolve(t, resolver, "*foo.md", "", true)
	tryResolve(t, resolver, "foo.py", "", true)
}
