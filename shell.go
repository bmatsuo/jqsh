// shell.go
// a collection of shell and tty utilities

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

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
		pager = []string{"less", "-X", "-r"}
	}
	pagercmd := pager[0]
	pagerargs := pager[1:]
	cmd := exec.Command(pagercmd, pagerargs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdinr, stdinw, err := os.Pipe()
	if err != nil {
		errch <- err
		return nil, errch
	}
	cmd.Stdin = stdinr
	err = cmd.Start()
	if err != nil {
		errch <- err
		stdinw.Close()
		stdinr.Close()
		close(errch)
		return nil, errch
	}
	go func() {
		// wait for the pager and terminate any color mode the terminal happens
		// to be in when the program closes.
		//
		// BUG platform dependent escape code.
		err := cmd.Wait()
		fmt.Print("\033[0m")

		stdinr.Close()
		if err != nil {
			errch <- err
		}
		close(errch)
	}()
	return stdinw, errch
}

// BUG: this is not an idiomatic interface.
type ShellReader interface {
	// ReadCommand reads a command from input and returns it.  ReadCommand
	// returns io.EOF there is no command to be processed.  ReadCommand returns
	// a true value when either there was no command to process or the command
	// was terminated without a newline. If true ReadCommand should not be
	// called again to avoid reprompting the user.
	ReadCommand() (cmd []string, eof bool, err error)
}

const simpleShellReaderDocs = `
The shell syntax is primitive but it suffices.  Lines prefixed with a colon ':'
are commands, other lines are shorthand for specific commands.  Following is a
list of all shell syntax in jqsh.

	:<cmd> <arg1> <arg2> ...    execute cmd with the given arguments
	:<cmd> ... +<argN>          execute cmd with an argument containing spaces (argN)
	.                           shorthand for ":write"
	..                          shorthand for ":pop"
	?<filter>                   shorthand for ":peek +<filter>"
	<filter>                    shorthand for ":push +<filter>"

Note that "." is a valid jq filter but pushing it on the filter stack lacks
semantic value.  So "." alone on a line is used as a shorthand for ":write".
`

type SimpleShellReader struct {
	r      io.Reader
	br     *bufio.Reader
	out    io.Writer
	prompt string
}

var _ ShellReader = (*SimpleShellReader)(nil)
var _ Documented = (*SimpleShellReader)(nil)

func NewShellReader(r io.Reader, prompt string) *SimpleShellReader {
	if r == nil {
		r = os.Stdin
	}
	br := bufio.NewReader(r)
	return &SimpleShellReader{r, br, os.Stdout, prompt}
}

func (s *SimpleShellReader) Documentation() string {
	return simpleShellReaderDocs
}

func (s *SimpleShellReader) SetOutput(w io.Writer) {
	s.out = w
}

func (s *SimpleShellReader) print(v ...interface{}) {
	if s.out != nil {
		fmt.Fprint(s.out, v...)
	}
}

func (s *SimpleShellReader) println(v ...interface{}) {
	if s.out != nil {
		fmt.Fprintln(s.out, v...)
	}
}

func (s *SimpleShellReader) ReadCommand() (cmd []string, eof bool, err error) {
	s.print(s.prompt)
	bs, err := s.br.ReadBytes('\n')
	eof = err == io.EOF
	if eof {
		s.println()
	}
	if err != nil {
		if eof && len(bs) > 0 {
			// this is ok
		} else {
			return nil, eof, err
		}
	}
	bs = bytes.TrimFunc(bs, unicode.IsSpace)

	if len(bs) == 0 {
		if eof {
			return nil, eof, nil
		}
		return s.ReadCommand()
	} else if bytes.Equal(bs, []byte("..")) {
		cmd := []string{"pop"}
		return cmd, eof, nil
	} else if bytes.Equal(bs, []byte{'.'}) {
		cmd := []string{"write"}
		return cmd, eof, nil
	} else if bs[0] == '?' {
		str := string(bs[1:])
		cmd := []string{"peek", str}
		return cmd, eof, nil
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
	return cmd, eof, nil
}

// An InitShellReader works like a SimpleShellReader but runs an init script
// before reading any input.
type InitShellReader struct {
	i    int
	init [][]string
	r    *SimpleShellReader
}

func NewInitShellReader(r io.Reader, prompt string, initcmds [][]string) *InitShellReader {
	return &InitShellReader{0, initcmds, NewShellReader(r, prompt)}
}

func (sh *InitShellReader) Documentation() string {
	return simpleShellReaderDocs
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
