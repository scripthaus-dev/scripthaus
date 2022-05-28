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

func tryResolve(t *testing.T, resolver Resolver, input string, goodResolve string) {
	rfn, err := resolver.ResolvePlaybook(input)
	if err != nil {
		t.Errorf("Error resolving %s: %v", input, err)
	} else if goodResolve != rfn {
		t.Errorf("Error resolving %s, got [%s], expected [%s]", input, rfn, goodResolve)
	}
}

func TestResolvePlaybook(t *testing.T) {
	scHome, err := GetScHomeDir()
	if err != nil {
		t.Fatalf("error resolving schome: %v\n", err)
		return
	}
	resolver := Resolver{
		TestMode:  true,
		Cwd:       "/*test/home/projectdir/subprojectdir/inner",
		ScHomeDir: "/*test/home/scripthaus",
	}
	tryResolve(t, resolver, "^", "/*test/home/scripthaus/scripthaus.md")
	tryResolve(t, resolver, "^foo.md", "/*test/home/scripthaus/foo.md")
	tryResolve(t, resolver, ".foo.md", "/*test/home/projectdir/subprojectdir/foo.md")
}
