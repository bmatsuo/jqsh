/*
Command jqsh provides an interactive wrapper to the jq command line utility.

Shell syntax

The current shell syntax is rudimentory but it suffices.  Commands are prefixed
with a colon ':' and a command name followed by a space separated list of
arguments.

	> :load test.json

The above loads the file "test.json" into the jqsh cache for inspection.  There
is no quoting of arguments.  A plus '+' may be used on the last argument to
include all charactors up to (but excluding) the next newline character.

	> :push +.items[] | select(.name | contains("hello"))

The above pushes the filter `.items[] | select(.name | contains("hello"))` on
to the jqsh filter stack. This is such a common operation that it has a special
shorthand.  A non-empty line that does not start with a colon causes the line's
contents to be pushed on the filter stack. So the above line could be
simplified.

	> .[] | select(.gender == "Female")

Blank lines are also a shorthand, printing the working filter stack applied to
the input, equivalent to the "write" command.

	> :write

That is all of the syntax in jqsh.

The filter stack

The core concept in jqsh in a stack of jq filters.  Filters create a larger
filter when joined with pipes "|".  And maintaining a stack of filters allows
exploritory querying of JSON structures.

In the previous section the "push" command was demonstrated.  Pushing filters
will effectively drill down into JSON structures.  To zoom out (or undo) the
application of filters the "pop" command is used.

	> :pop

Called without arguments, "pop" will remove the most recent filter pushed onto
the stack.  Multiple items can be popped off the stack by passing "pop" an
integer argument (the number of items to pop).

	> :pop 3

Command reference

A list of commands and other interactive help topics can be found through the
"help" command.

	> :help

Individual commands respond to the "-h" flag for usage documentation.
*/
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

var ErrStackEmpty = fmt.Errorf("the stack is empty")

func main() {
	flag.Parse()
	args := flag.Args()

	jqbin, err := LocateJQ("")
	if err == ErrJQNotFound {
		fmt.Fprintln(os.Stderr, "Unable to locate the jq executable. Make sure it's installed.")
		fmt.Fprintln(os.Stderr)
		switch runtime.GOOS {
		case "darwin":
			fmt.Fprintln(os.Stderr, "The easiest way to install jq on OS X is with homebrew.")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "\tbrew install jq")
		default:
			fmt.Fprintln(os.Stderr, "See the jq homepage for download and install instructions")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "\thttp://stedolan.github.io/jq/")
		}
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "locating jq:", err)
		os.Exit(1)
	}
	_, err = CheckJQVersion(jqbin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// setup initial commands to play before reading input.  single files are
	// loaded with :load, multple files are loaded with :exec cat
	var initcmds [][]string
	doexec := func(cache bool, name string, args ...string) {
		cmd := make([]string, 0, 3+len(args))
		cmd = append(cmd, "exec")
		if !cache {
			cmd = append(cmd, "-c")
		}
		cmd = append(cmd, name)
		cmd = append(cmd, args...)
		initcmds = append(initcmds, cmd)
	}
	switch {
	case len(args) == 1:
		initcmds = [][]string{
			{"load", args[0]},
		}
	case len(args) > 1:
		doexec(false, "cat", args...)
	}

	// create a shell environment and wait for it to receive EOF or a 'quit'
	// command.
	fmt.Println("Welcome to jqsh!")
	fmt.Println()
	fmt.Println("To learn more about the environment type \":help\"")
	fmt.Println()
	fmt.Println("To learn more about jqsh see the online documentation")
	fmt.Println()
	fmt.Println("\thttps://github.com/bmatsuo/jqsh")
	fmt.Println()
	sh := NewInitShellReader(nil, initcmds)
	jq := NewJQShell(jqbin, sh)
	err = jq.Wait()
	if err != nil {
		log.Fatal(err)
	}
}

// Page returns an io.Writer whose input will be written to the pager program.
// The returned channel should be checked for an error using select before the
// writer is used.
//	w, errch := Page("less")
//	select {
//	case err := <-errch:
//		return err
//	default:
//		w.Write([]byte("boom"))
//	}
func Page(pager []string) (io.WriteCloser, <-chan error) {
	errch := make(chan error, 1)
	if len(pager) == 0 {
		pager = []string{"more", "-r"}
	}
	pagercmd := pager[0]
	pagerargs := pager[1:]
	cmd := exec.Command(pagercmd, pagerargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		errch <- err
		return nil, errch
	}
	go func() {
		err := cmd.Run()
		stdin.Close()
		if err != nil {
			errch <- err
		}
		close(errch)
		fmt.Print("\033[0m")
	}()
	return stdin, errch
}

type writeCounter struct {
	n int64
	w io.Writer
}

func (w *writeCounter) Write(bs []byte) (int, error) {
	n, err := w.w.Write(bs)
	if n > 0 {
		atomic.AddInt64(&w.n, int64(n))
	}
	return n, err
}

type InvalidCommandError struct {
	Message string
}

func (err InvalidCommandError) Error() string {
	return err.Message
}

type ShellReader interface {
	ReadCommand() (cmd []string, eof bool, err error)
}

type SimpleShellReader struct {
	r  io.Reader
	br *bufio.Reader
}

func NewShellReader(r io.Reader) *SimpleShellReader {
	if r == nil {
		r = os.Stdin
	}
	br := bufio.NewReader(r)
	return &SimpleShellReader{r, br}
}

func (s *SimpleShellReader) ReadCommand() (cmd []string, eof bool, err error) {
	fmt.Print("> ")
	bs, err := s.br.ReadBytes('\n')
	eof = err == io.EOF
	if err != nil {
		if err == io.EOF && len(bs) > 0 {
			// this is ok
		} else {
			return nil, eof, err
		}
	}
	bs = bytes.TrimFunc(bs, unicode.IsSpace)

	if len(bs) == 0 {
		return []string{}, eof, nil
	} else if bs[0] != ':' {
		str := string(bs)
		cmd := []string{"push", str}
		return cmd, eof, nil
	}

	bs = bs[1:]
	plusi := bytes.Index(bs, []byte{'+'})
	var last *[]byte
	if plusi > 0 {
		lastp := bs[plusi+1:]
		last = &lastp
		bs = bs[:plusi]
	}
	cmd = strings.Fields(string(bs))
	if last != nil {
		cmd = append(cmd, string(*last))
	}
	if len(cmd) == 0 {
		cmd = []string{"write"}
	}
	return cmd, eof, nil
}

// An InitShellReader works like a SimpleShellReader but runs an init script
// before reading any input.
type InitShellReader struct {
	i    int
	init [][]string
	r    *SimpleShellReader
}

func NewInitShellReader(r io.Reader, initcmds [][]string) *InitShellReader {
	return &InitShellReader{0, initcmds, NewShellReader(r)}
}

func (sh *InitShellReader) ReadCommand() ([]string, bool, error) {
	if sh == nil {
		panic("nil shell")
	}
	if sh.i < len(sh.init) {
		cmd := sh.init[sh.i]
		sh.i++
		return cmd, false, nil
	}
	return sh.r.ReadCommand()
}

type JQShell struct {
	Log      *log.Logger
	Stack    *JQStack
	bin      string
	inputfn  func() (io.ReadCloser, error)
	filename string
	istmp    bool // the filename at path should be deleted when changed
	lib      *Lib
	sh       ShellReader
	err      error
	wg       sync.WaitGroup
}

func NewJQShell(bin string, sh ShellReader) *JQShell {
	if sh == nil {
		sh = NewShellReader(nil)
	}
	st := new(JQStack)
	jq := &JQShell{
		Log:   log.New(os.Stderr, "jqsh: ", 0),
		Stack: st,
		bin:   bin,
		sh:    sh,
	}
	jq.lib = Library()
	jq.lib.Register("push", JQShellCommandFunc(cmdPush))
	jq.lib.Register("pop", JQShellCommandFunc(cmdPop))
	jq.lib.Register("filter", JQShellCommandFunc(cmdFilter))
	jq.lib.Register("script", JQShellCommandFunc(cmdScript))
	jq.lib.Register("load", JQShellCommandFunc(cmdLoad))
	jq.lib.Register("exec", JQShellCommandFunc(cmdExec))
	jq.lib.Register("write", JQShellCommandFunc(cmdWrite))
	jq.lib.Register("raw", JQShellCommandFunc(cmdRaw))
	jq.lib.Register("quit", JQShellCommandFunc(cmdQuit))
	jq.wg.Add(1)
	go jq.loop()
	return jq
}

func (jq *JQShell) SetInputFile(path string, istmp bool) {
	jq.ClearInput()
	jq.inputfn = nil
	jq.filename = path
	jq.istmp = istmp
}

func (jq *JQShell) SetInput(fn func() (io.ReadCloser, error)) {
	jq.ClearInput()
	jq.inputfn = fn
}

func (jq *JQShell) Input() (io.ReadCloser, error) {
	switch {
	case jq.filename != "":
		jq.Log.Println("open", jq.filename)
		return os.Open(jq.filename)
	case jq.inputfn != nil:
		return jq.inputfn()
	default:
		return nil, fmt.Errorf("no input")
	}
}

func (jq *JQShell) Wait() error {
	jq.wg.Wait()
	return jq.err
}

func isShellExit(err error) bool {
	if err == nil {
		return false
	}
	if err == ShellExit {
		return true
	}
	if err, ok := err.(ExecError); ok {
		return isShellExit(err.err)
	}
	return false
}

func (jq *JQShell) ClearInput() {
	if jq.inputfn != nil {
		jq.inputfn = nil
	}
	if jq.filename != "" && jq.istmp {
		err := os.Remove(jq.filename)
		if err != nil {
			// not a critical error
			jq.Log.Printf("removingtemporary file %v: %v", jq.filename, err)
		}
	}
}

func (jq *JQShell) loop() {
	stop := make(chan struct{})
	_stop := func() { close(stop) }
	ready := make(chan struct{}, 1)
	ready <- struct{}{}
	type cmdin struct {
		cmd []string
		eof bool
		err error
	}
	cmdch := make(chan cmdin)
	for {
		select {
		case <-stop:
			// remove any temporary file
			if jq.filename != "" && jq.istmp {
				err := os.Remove(jq.filename)
				if err != nil {
					// not a critical error
					jq.Log.Printf("removingtemporary file %v: %v", jq.filename, err)
				}
			}
			jq.wg.Done()
			return
		case <-ready:
			go func() {
				cmd, eof, err := jq.sh.ReadCommand()
				cmdch <- cmdin{cmd, eof, err}
			}()
		case cmd := <-cmdch:
			if err, ok := cmd.err.(InvalidCommandError); ok {
				jq.Log.Println(err)
				ready <- struct{}{}
				continue
			}
			go func() {
				err := cmd.err
				if err == io.EOF {
					err = nil
				}
				err = jq.execute(cmd.cmd, err)
				if isShellExit(err) {
					_stop()
					return
				}
				if cmd.eof {
					_stop()
					return
				}
				if err != nil {
					jq.Log.Print(err)
				} else if cmd.cmd[0] != "write" && cmd.cmd[0] != "raw" && cmd.cmd[0] != "filter" && cmd.cmd[0] != "script" && cmd.cmd[0] != "help" {
					// TODO clean this up. (cmdPushInteractive, cmdPeek)
					err := jq.execute([]string{"write"}, nil)
					if err != nil {
						jq.Log.Print(err)
						if cmd.cmd[0] == "push" {
							npush := len(cmd.cmd) - 1
							if npush == 0 {
								npush = 1
							}
							jq.Log.Print("reverting push operation")
							err := jq.execute([]string{"pop", fmt.Sprint(npush)}, nil)
							if err != nil {
								jq.Log.Print(err)
							}
						}
					}
				}
				ready <- struct{}{}
			}()
		}
	}
	panic("unreachable")
}

func (jq *JQShell) log(v ...interface{}) {
	jq.Log.Print(v...)
}

func (jq *JQShell) logf(format string, v ...interface{}) {
	jq.Log.Printf(format, v...)
}

type ExecError struct {
	cmd []string
	err error
}

func (err ExecError) Error() string {
	return fmt.Sprintf("%s: %v", err.cmd[0], err.err)
}

func (jq *JQShell) execute(cmd []string, err error) error {
	if isShellExit(err) {
		return err
	}
	if err, ok := err.(InvalidCommandError); ok {
		jq.Log.Print(err)
		return nil
	}
	if err != nil {
		return err
	}
	if len(cmd) > 0 {
		name, args := cmd[0], cmd[1:]
		return jq.lib.Execute(jq, name, args)
	}
	return nil
}

type JQFilter interface {
	JQFilter() []string
}

var JQFilterJoin = " | "

func JoinFilter(filter JQFilter) string {
	return strings.Join(filter.JQFilter(), JQFilterJoin)
}

type JQFilterString string

func (s JQFilterString) JQFilter() []string {
	return []string{string(s)}
}

type JQStack struct {
	pipe []JQFilter
}

// Args returns arguments for the jq command line utility.
func (s *JQStack) JQFilter() []string {
	if s == nil {
		return []string{"."}
	}
	var args []string
	for _, cmd := range s.pipe {
		args = append(args, cmd.JQFilter()...)
	}
	return args
}

func (s *JQStack) Push(cmd JQFilter) {
	s.pipe = append(s.pipe, cmd)
}

func (s *JQStack) Pop() (JQFilter, error) {
	if len(s.pipe) == 0 {
		return nil, ErrStackEmpty
	}
	n := len(s.pipe)
	filt := s.pipe[n-1]
	s.pipe = s.pipe[:n-1]
	return filt, nil
}
