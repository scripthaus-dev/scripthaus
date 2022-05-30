// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testpty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

const keyBell = 7
const keyEot = 4
const keyEoa = 2
const keyEscape = 27

type CodePassthrough struct {
	Input   *os.File
	Output  *os.File
	InEsc   bool
	EscBuf  []byte
	WriteOn bool
	Pos     int
}

// scripthaus escape sequence is \e[<N;N;Ns
// returns (isPrefix, isComplete)
func isScriptHausEsc(escBuf []byte) (bool, bool) {
	for idx, ch := range escBuf {
		if idx == 0 {
			if ch != keyEscape {
				return false, false
			}
			continue
		}
		if idx == 1 {
			if ch != '[' {
				return false, false
			}
			continue
		}
		if idx == 2 {
			if ch != '<' {
				return false, false
			}
			continue
		}
		if idx == len(escBuf)-1 {
			// 's' terminates the escape sequence
			if ch == 's' {
				return true, true
			}
			// fallthough (no continue)
		}
		// parameter bytes for scripthaus can only be [0-9;]
		if !(ch == ';' || (ch >= '0' && ch <= '9')) {
			return false, false
		}
	}
	return true, false
}

func (pass *CodePassthrough) ProcessScriptHausEsc() []byte {
	// already checked by isScriptHausEsc
	escParams := string(pass.EscBuf[3 : len(pass.EscBuf)-1]) // grabs the inner portion of escape \e[<(...)s
	strParams := strings.Split(escParams, ";")
	intParams := make([]int, len(strParams))
	for idx, strParam := range strParams {
		intParams[idx], _ = strconv.Atoi(strParam)
	}
	if len(intParams) >= 2 && intParams[0] == 0 {
		if intParams[1] == 0 {
			pass.WriteOn = false
		} else if intParams[1] == 1 {
			pass.WriteOn = true
		}
	}
	return nil
}

func (pass *CodePassthrough) ProcessBytes(buf []byte) []byte {
	output := make([]byte, 0, len(buf))
	for i := 0; i < len(buf); i++ {
		ch := buf[i]
		if ch == keyEscape {
			output = append(output, pass.EscBuf...)
			pass.EscBuf = []byte{keyEscape}
			pass.InEsc = true
			continue
		}
		if pass.InEsc {
			pass.EscBuf = append(pass.EscBuf, ch)
			isPrefix, isComplete := isScriptHausEsc(pass.EscBuf)
			if !isPrefix {
				output = append(output, pass.EscBuf...)
				pass.EscBuf = nil
				pass.InEsc = false
				continue
			}
			if isComplete {
				escOutput := pass.ProcessScriptHausEsc()
				output = append(output, escOutput...)
			}
			continue
		}
		if pass.WriteOn {
			output = append(output, ch)
		}
	}
	return output
}

func (pass *CodePassthrough) Copy() error {
	readBuf := make([]byte, 1024)
	for {
		nr, err := pass.Input.Read(readBuf)
		if nr > 0 {
			output := pass.ProcessBytes(readBuf[0:nr])
			if len(output) > 0 {
				_, err = pass.Output.Write(output)
			}
			if err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func Run() error {
	fmt.Printf("Test PTY!\n")

	stdinTerm := isatty.IsTerminal(os.Stdin.Fd())
	stdoutTerm := isatty.IsTerminal(os.Stdout.Fd())
	stderrTerm := isatty.IsTerminal(os.Stderr.Fd())
	if !stdinTerm || !stdoutTerm || !stderrTerm {
		return fmt.Errorf("stdin/stdout/stderr must be connected to a tty stdin[%v] stdout[%v] stderr[%v]", stdinTerm, stdoutTerm, stderrTerm)
	}

	newPty, tty, err := pty.Open()
	if err != nil {
		return err
	}
	defer func() {
		_ = tty.Close()
	}()

	cmd := exec.Command("bash", "-l")
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	err = cmd.Start()
	if err != nil {
		_ = newPty.Close()
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			err := pty.InheritSize(os.Stdin, newPty)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error resizing pty: %v\n", err)
			}
		}
	}()
	ch <- syscall.SIGWINCH
	defer func() {
		signal.Stop(ch)
		close(ch)
	}()
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), oldState)
	}()

	fmt.Fprintf(newPty, "PS1=\"\\e[1;32m(scripthaus)\\e[m $PS1\"\n")
	fmt.Fprintf(newPty, "printf '\\033[<0;1s'\n")
	// "\e[1;34m[ \u@imac27 $(git rev-parse --abbrev-ref HEAD 2>/dev/null) \w ]\e[m \n>"

	go func() {
		_, _ = io.Copy(newPty, os.Stdin)
	}()

	pass := &CodePassthrough{
		Input:   newPty,
		Output:  os.Stdout,
		WriteOn: false,
	}
	err = pass.Copy()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "done\n")
	return nil
}
