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
	"unicode/utf8"

	"github.com/bmatsuo/go-lexer"
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
Topic syntax describes the simple shell syntax.

Lines prefixed with a colon ':' are commands, other lines are shorthand for
specific commands.  Following is a list of all shell syntax in jqsh.

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

type shellParser struct {
	lex *lexer.Lexer
}

func newShellParser(input string) *shellParser {
	s := new(shellParser)
	s.lex = lexer.New(s.lexStart, input)
	return s
}

const (
	itemColon lexer.ItemType = iota
	itemDot
	itemDotDot
	itemQMark
	itemString
)

func (s *shellParser) lexStart(lex *lexer.Lexer) lexer.StateFn {
	if lex.Accept(":") {
		lex.Emit(itemColon)
		return s.lexCommand
	}
	if lex.Accept("?") {
		lex.Emit(itemQMark)
		return s.lexSlurp
	}
	if lex.AcceptString("..") {
		r, n := lex.Advance()
		if n != 0 {
			return lex.Errorf(`expected end-of-input following ".."; got '%c'`, r)
		}
		lex.Emit(itemDotDot)
		lex.Emit(lexer.ItemEOF)
		return nil
	}
	if lex.Accept(".") {
		r, n := lex.Advance()
		if n != 0 {
			return lex.Errorf(`expected end-of-input following "."; got '%c'`, r)
		}
		lex.Emit(itemDot)
		lex.Emit(lexer.ItemEOF)
		return nil
	}
	return s.lexSlurp
}

func (s *shellParser) lexCommand(lex *lexer.Lexer) lexer.StateFn {
	lex.AcceptRunFunc(unicode.IsSpace)
	lex.Ignore()
	if lex.Accept("+") {
		return s.lexSlurp
	}
	return s.lexStringCont(s.lexCommand)
}

func (s *shellParser) lexStringCont(cont lexer.StateFn) lexer.StateFn {
	return func(lex *lexer.Lexer) lexer.StateFn {
		if lex.Accept("'") {
			lex.Backup()
			return s.lexStringContQuote(cont, '\'', '\\')
		}
		if lex.Accept("\"") {
			lex.Backup()
			return s.lexStringContQuote(cont, '"', '\\')
		}
		for {
			c, n := lex.Advance()
			if lexer.IsEOF(c, n) {
				lex.Emit(itemString)
			}
			if lexer.IsInvalid(c, n) {
				return lex.Errorf("invalid utf-8 rune")
			}
			if unicode.IsSpace(c) {
				lex.Backup()
				lex.Emit(itemString)
				return cont
			}
		}
	}
}

func (s *shellParser) lexStringContQuote(cont lexer.StateFn, q, esc rune) lexer.StateFn {
	qstr := string([]rune{q})
	escstr := string([]rune{esc})
	return func(lex *lexer.Lexer) lexer.StateFn {
		if !lex.Accept(qstr) {
			return lex.Errorf("expected %0x", q)
		}
		for {
			if lex.Accept(qstr) {
				lex.Emit(itemString)
				c, n := lex.Peek()
				if lexer.IsEOF(c, n) {
					return cont
				}
				if lexer.IsInvalid(c, n) {
					return lex.Errorf("invalid utf-8 rune")
				}
				if !unicode.IsSpace(c) {
					return lex.Errorf("string not followed by space or end-of-input %q", c)
				}
				return cont
			}
			if lex.Accept(escstr) {
				c, n := lex.Advance()
				if lexer.IsEOF(c, n) {
					return lex.Errorf("unexpected end-of-input following escape")
				}
				if lexer.IsInvalid(c, n) {
					return lex.Errorf("invalid utf-8 rune")
				}
				continue
			}
		}
	}
}

// slurp emits an ItemString containing the text up to the end of the line.
func (s *shellParser) lexSlurp(lex *lexer.Lexer) lexer.StateFn {
	for {
		c, n := lex.Advance()
		if n == 0 {
			lex.Emit(itemString)
			return nil
		}
		if c == utf8.RuneError && n == 1 {
			return lex.Errorf("invalid utf-8 rune")
		}
	}
	return nil
}
