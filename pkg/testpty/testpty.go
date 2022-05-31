// Copyright 2022 Dashborg Inc
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package testpty

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

const keyBell = 7
const keyEot = 4
const keyEoa = 2
const keyEscape = 27
const keySt = 156

const ModeNormal = ""
const ModeEsc = "esc"
const ModeCommand = "command"

const SHCSISuffix = 's'

var SHCSIPrefix = []byte{keyEscape, '[', '<'}
var SHOSCPrefix = []byte{keyEscape, ']', '1', '9', '8', ';'}

type ShPty struct {
	Name     string
	Mode     string
	Output   *os.File
	EscBuf   []byte
	WriteOn  bool
	Pos      int
	PromptCh chan int
	SigCh    chan os.Signal
	Cmd      *exec.Cmd
	Pty      *os.File
	Tty      *os.File

	RemotePty *os.File

	InPrompt bool
	Ready    bool
}

func getCharCodes(barr []byte) string {
	var output bytes.Buffer
	for idx, ch := range barr {
		if idx != 0 {
			output.WriteString(" ")
		}
		if ch == keyEscape {
			output.WriteString("\\e")
			continue
		}
		if ch == keyBell {
			output.WriteString("BEL")
			continue
		}
		if ch == keySt {
			output.WriteString("ST")
			continue
		}
		if ch >= 32 && ch <= 126 {
			output.WriteByte(ch)
			continue
		}
		output.WriteString("(" + strconv.Itoa(int(ch)) + ")")
	}
	return output.String()
}

func partialMatch(barr []byte, m []byte) bool {
	for idx := 0; idx < len(barr) && idx < len(m); idx++ {
		if barr[idx] != m[idx] {
			return false
		}
	}
	return true
}

// scripthaus CSI escape sequence is \e[<N;N;Ns
//   \e[<0;0s  - turn off output
//   \e[<0;1s  - turn on output
//   \e[<2;0s  - start of command prompt
//   \e[<2;1s  - end of command prompt
//   \e[<2;3s  - start of command
// scripthaus (S=19,H=8) OSC escape sequence is \e]198;...BEL
//   \e]198;1;remote;...BEL  - run a remote command
//   \e]198;2;(history command)BEL - log history command
// returns (isPrefix, isComplete)
func isScriptHausEsc(escBuf []byte) (bool, bool) {
	isCSIPrefix := partialMatch(escBuf, SHCSIPrefix)
	if isCSIPrefix {
		if escBuf[len(escBuf)-1] == SHCSISuffix {
			return true, true
		}
		return true, false
	}
	isOSCPrefix := partialMatch(escBuf, SHOSCPrefix)
	if isOSCPrefix {
		if escBuf[len(escBuf)-1] == keyBell || escBuf[len(escBuf)-1] == keySt {
			return true, true
		}
		return true, false
	}
	return false, false
}

func (pass *ShPty) ProcessScriptHausEsc() []byte {
	// already checked by isScriptHausEsc
	if partialMatch(pass.EscBuf, SHCSIPrefix) {
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
		if len(intParams) >= 2 && intParams[0] == 1 {
			exitCode := intParams[1]
			if pass.PromptCh != nil {
				pass.PromptCh <- exitCode
			}
		}
		if len(intParams) >= 2 && intParams[0] == 2 {
			if intParams[1] == 0 {
				pass.InPrompt = true
			} else if intParams[1] == 1 {
				pass.InPrompt = false
				pass.Ready = true
			}
		}
		return nil
	}
	if partialMatch(pass.EscBuf, SHOSCPrefix) {
		escParams := string(pass.EscBuf[6 : len(pass.EscBuf)-1])
		fields := strings.SplitN(escParams, ";", 2)
		if len(fields) != 2 {
			return nil
		}
		if fields[0] == "1" {
			fmt.Printf("\r\nESC-REMOTE[%s]\r\n", fields[1])
			if pass.RemotePty != nil {
				fmt.Fprintf(pass.RemotePty, "%s\n", fields[1])
			}
		} else if fields[0] == "2" {
			historyFields := strings.SplitN(fields[1], ";", 3)
			if len(historyFields) != 3 {
				return nil
			}
			exitCode, err1 := strconv.Atoi(strings.TrimSpace(historyFields[0]))
			histNum, err2 := strconv.Atoi(strings.TrimSpace(historyFields[1]))
			if err1 != nil || err2 != nil {
				return nil
			}
			histCmd := strings.TrimSpace(historyFields[2])
			_ = fmt.Sprintf("\r\nESC-HISTORY[(%d|%d) %s]\r\n", histNum, exitCode, histCmd)
		}
	}

	return nil
}

func (p *ShPty) canWrite() bool {
	return p.WriteOn && (p.Name != "remote" || !p.InPrompt)
}

func (pass *ShPty) ProcessBytes(buf []byte) []byte {
	output := make([]byte, 0, len(buf))
	for i := 0; i < len(buf); i++ {
		ch := buf[i]
		if pass.Mode == ModeNormal {
			if ch == keyEscape {
				output = append(output, pass.EscBuf...)
				pass.Mode = ModeEsc
				pass.EscBuf = []byte{keyEscape}
				continue
			}
			if pass.canWrite() {
				output = append(output, ch)
			}
		} else if pass.Mode == ModeEsc {
			pass.EscBuf = append(pass.EscBuf, ch)
			isPrefix, isComplete := isScriptHausEsc(pass.EscBuf)
			if !isPrefix {
				if pass.canWrite() {
					output = append(output, pass.EscBuf...)
				}
				pass.Mode = ModeNormal
				pass.EscBuf = nil
				continue
			}
			if isComplete {
				escOutput := pass.ProcessScriptHausEsc()
				if pass.canWrite() {
					output = append(output, escOutput...)
				}
				pass.Mode = ModeNormal
				pass.EscBuf = nil
			}
			continue
		} else if pass.Mode == ModeCommand {

		}
	}
	return output
}

func (p *ShPty) RunInput(input io.Reader) error {
	_, _ = io.Copy(p.Pty, input)
	return nil
}

func (pass *ShPty) RunOutput() error {
	// fmt.Printf("run output %s\r\n", pass.Name)
	readBuf := make([]byte, 1024)
	for {
		nr, err := pass.Pty.Read(readBuf)
		if nr > 0 {
			output := pass.ProcessBytes(readBuf[0:nr])
			if len(output) > 0 {
				if pass.Name == "remote" {
					newOutput := &bytes.Buffer{}
					newOutput.WriteString("\r\n\u001b[41mREMOTE OUTPUT\u001b[0m\r\n")
					newOutput.Write(output)
					newOutput.WriteString("\r\n\u001b[41mEND REMOTE OUTPUT\u001b[0m\r\n")
					output = newOutput.Bytes()
				}
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

func (p *ShPty) Close() {
	if p.Pty != nil {
		_ = p.Pty.Close()
	}
	if p.Tty != nil {
		_ = p.Tty.Close()
	}
	if p.SigCh != nil {
		signal.Stop(p.SigCh)
		close(p.SigCh)
	}
}

func (p *ShPty) RunInitCommands() {
	if p.Name == "remote" {
		fmt.Fprintf(p.Pty, "PS1=\"\\e[<2;0s(scripthaus %s)\\e[<2;1s\"\n", p.Name)
		fmt.Fprintf(p.Pty, "stty -echo\n")
	} else {
		fmt.Fprintf(p.Pty, "PS1=\"\\e[<2;0s\\e[1;32m(scripthaus %s)\\e[0m $PS1\\e[<2;1s\"\n", p.Name)
		fmt.Fprintf(p.Pty, "hist_prompt() { local exitcode=\"$?\"; printf '\\033]198;2;'; echo -n $exitcode \"; \" \"$(HISTTIMEFORMAT='; ' history 1)\"; printf '\\a'; }\n")
		fmt.Fprintf(p.Pty, "PROMPT_COMMAND=\"hist_prompt\"\n")
	}
	fmt.Fprintf(p.Pty, "printf '\\033[<0;1s'\n")
}

func MakeShPty(name string, cmd *exec.Cmd, outputFile *os.File, inputFile *os.File, initialWriteOn bool) (*ShPty, error) {
	newPty, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	rtn := &ShPty{
		Name:    name,
		Pty:     newPty,
		Tty:     tty,
		Output:  outputFile,
		WriteOn: initialWriteOn,
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	err = cmd.Start()
	if err != nil {
		rtn.Close()
		return nil, err
	}
	rtn.Cmd = cmd
	rtn.SigCh = make(chan os.Signal, 1)
	signal.Notify(rtn.SigCh, syscall.SIGWINCH)
	go func() {
		for range rtn.SigCh {
			err := pty.InheritSize(inputFile, rtn.Pty)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resizing pty (%s): %v\n", rtn.Name, err)
			}
		}
	}()
	rtn.SigCh <- syscall.SIGWINCH
	return rtn, nil
}

func Run() error {
	fmt.Printf("Test PTY!\n")

	stdinTerm := isatty.IsTerminal(os.Stdin.Fd())
	stdoutTerm := isatty.IsTerminal(os.Stdout.Fd())
	stderrTerm := isatty.IsTerminal(os.Stderr.Fd())
	if !stdinTerm || !stdoutTerm || !stderrTerm {
		return fmt.Errorf("stdin/stdout/stderr must be connected to a tty stdin[%v] stdout[%v] stderr[%v]", stdinTerm, stdoutTerm, stderrTerm)
	}

	localCmd := exec.Command("bash", "-l")
	localPty, err := MakeShPty("local", localCmd, os.Stdout, os.Stdin, false)
	if err != nil {
		return err
	}
	defer localPty.Close()
	localPty.RunInitCommands()

	remoteCmd := exec.Command("ssh", "-tt", "-i", "/Users/mike/aws/mfmt.pem", "ubuntu@test01.ec2")
	remotePty, err := MakeShPty("remote", remoteCmd, os.Stdout, os.Stdin, false)
	if err != nil {
		return err
	}
	defer remotePty.Close()
	go func() {
		time.Sleep(2 * time.Second)
		remotePty.RunInitCommands()
	}()
	localPty.RemotePty = remotePty.Pty

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer func() {
		_ = term.Restore(int(os.Stdin.Fd()), oldState)
	}()

	go func() {
		inputErr := localPty.RunInput(os.Stdin)
		if inputErr != nil {
			fmt.Printf("ERROR - local pty input error: %v\n", inputErr)
		}
	}()
	go func() {
		remoteRunErr := remotePty.RunOutput()
		if remoteRunErr != nil {
			fmt.Printf("ERROR - remote pty error: %v\n", remoteRunErr)
		}
	}()
	go func() {
		time.Sleep(5 * time.Second)
		fmt.Fprintf(remotePty.Pty, "ls -l\n")
	}()
	localRunErr := localPty.RunOutput()
	if localRunErr != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "done\r\n")
	return nil
}
