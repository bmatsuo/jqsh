/*
Command jqsh provides an interactive wrapper to the jq command line utility.

The filter stack

The core concept in jqsh is a stack of jq "filters".  Filters create a larger
filter when joined with pipes "|".  And maintaining a stack of filters allows
exploritory querying of JSON structures.

Filters can be pushed onto the filter stack with ":push" and popped with
":pop".

	> :push .[]
	> :push .type
	> :pop
	> :push [.type,.name]
	> :pop 2

The corresponding jq filter at each step is

	.[]
	.[] | .type
	.[]
	.[] | [.type,.name]
	.

Notice that in the last step the stack was emptied with ":pop 2". But jqsh
leaves a "." on the stack.

Read more about jq filters at the tool's online manual.

	http://stedolan.github.io/jq/manual/#Basicfilters

Shell syntax

The current shell syntax is rudimentory but it suffices.  Lines prefixed with a
colon ':' are commands, other lines are shorthand for specific commands.
Following is a list of all shell syntax in jqsh.

	:<cmd> <arg1> <arg2> ...    execute cmd with the given arguments
	:<cmd> ... +<argN>          execute cmd with an argument containing spaces (argN)
	.                           shorthand for ":write"
	..                          shorthand for ":pop"
	?<filter>                   shorthand for ":peek +<filter>"
	<filter>                    shorthand for ":push +<filter>"

Note that "." is a valid jq filter but pushing it on the filter stack lacks
semantic value.  So "." alone on a line is used as a shorthand for ":write".

Command reference

A list of commands and other interactive help topics can be found through the
"help" command.

	> :help

Individual commands respond to the "-h" flag for usage documentation.
*/
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
)

var ErrNoInput = fmt.Errorf("no input")
var ErrStackEmpty = fmt.Errorf("the stack is empty")

func main() {
	printVersion := flag.Bool("version", false, "print the versions of jqsh and jq then exit")
	flag.Parse()
	args := flag.Args()

	if *printVersion {
		fmt.Println("jqsh" + Version)
	}

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
	jqvers, err := CheckJQVersion(jqbin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *printVersion {
		fmt.Println(jqvers)
		return
	}

	// setup initial commands to play before reading input.  single files are
	// loaded with :load, multple files are loaded with :pipe cat
	var initcmds [][]string
	doexec := func(cache bool, script string) {
		cmd := make([]string, 0, 3+len(args))
		cmd = append(cmd, "pipe")
		if !cache {
			cmd = append(cmd, "-c")
		}
		cmd = append(cmd, script)
		initcmds = append(initcmds, cmd)
	}
	switch {
	case len(args) == 1:
		initcmds = [][]string{
			{"load", args[0]},
		}
	case len(args) > 1:
		// TODO fix filename escaping. probably by wrapping cat in a bash
		// script and doing proper quote escaping. this method should work ok
		// in many situation under bash. it does not work as often under csh.
		doexec(false, fmt.Sprintf("cat %s", strings.Trim(fmt.Sprintf("%q", args), "[]")))
	}

	// create a shell environment and wait for it to receive EOF or a 'quit'
	// command.
	fmt.Println("Welcome to jqsh!")
	fmt.Println()
	fmt.Println("To learn more about the environment type \":help\"")
	fmt.Println()
	fmt.Println("To learn more about jqsh see the online documentation")
	fmt.Println()
	fmt.Println("\thttps://github.com/bmatsuo/jqsh#getting-started")
	fmt.Println()
	sh := NewInitShellReader(nil, "> ", initcmds)
	jq := NewJQShell(jqbin, sh)
	err = jq.Wait()
	if err != nil {
		log.Fatal(err)
	}
}

type InvalidCommandError struct {
	Message string
}

func (err InvalidCommandError) Error() string {
	return err.Message
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
		sh = NewShellReader(nil, "> ")
	}
	st := new(JQStack)
	jq := &JQShell{
		Log:   log.New(os.Stderr, "jqsh: ", 0),
		Stack: st,
		bin:   bin,
		sh:    sh,
	}
	jq.lib = Library(&DocOpt{
		Indent:    "  ",
		PreIndent: "\t",
		Width:     76,
	})
	jq.lib.Register("push", JQShellCommandFunc(cmdPush))
	jq.lib.Register("peek", JQShellCommandFunc(cmdPeek))
	jq.lib.Register("pop", JQShellCommandFunc(cmdPop))
	jq.lib.Register("popall", JQShellCommandFunc(cmdPopAll))
	jq.lib.Register("filter", JQShellCommandFunc(cmdFilter))
	jq.lib.Register("script", JQShellCommandFunc(cmdScript))
	jq.lib.Register("load", JQShellCommandFunc(cmdLoad))
	jq.lib.Register("pipe", JQShellCommandFunc(cmdPipe))
	jq.lib.Register("write", JQShellCommandFunc(cmdWrite))
	jq.lib.Register("raw", JQShellCommandFunc(cmdRaw))
	jq.lib.Register("quit", JQShellCommandFunc(cmdQuit))
	if shdoc, ok := sh.(Documented); ok {
		jq.lib.RegisterHelp("syntax", shdoc.Documentation())
	}

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

func (jq *JQShell) HasInput() bool {
	return jq.filename != "" || jq.inputfn != nil
}

func (jq *JQShell) Input() (io.ReadCloser, error) {
	switch {
	case jq.filename != "":
		return os.Open(jq.filename)
	case jq.inputfn != nil:
		return jq.inputfn()
	default:
		return nil, ErrNoInput
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
				} else if len(cmd.cmd) == 0 {
					jq.Log.Println("empty command")
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

// Documentation is a type that allows plugable components to document
// themselves in help topics.
type Documented interface {
	// Documentation returns a string of documentation the documentation will
	// be formatted according to application specific rules.
	Documentation() string
}
