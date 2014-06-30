package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatsuo/go-lexer"
)

var ErrJQNotFound = fmt.Errorf("jq executable not found")

var jqVersionPrefixBytes = []byte("jq-")

func LocateJQ(path string) (string, error) {
	if path == "" {
		path, err := exec.LookPath("jq")
		if err, ok := err.(*exec.Error); ok && err.Err == exec.ErrNotFound {
			return "", ErrJQNotFound
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
	bs, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", err
	}
	if !bytes.HasPrefix(bs, jqVersionPrefixBytes) {
		return "", fmt.Errorf("executable doesn't look like jq")
	}
	return path, nil
}

func CheckJQVersion(path string) (string, error) {
	var err error
	path, err = LocateJQ(path)
	if err != nil {
		return "", err
	}

	cmd := exec.Command(path, "--version")
	bs, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	lex := lexer.New(scanJQVersion, string(bs))
	var items []*lexer.Item
	for {
		item := lex.Next()
		if item.Type == lexer.ItemError {
			return "", fmt.Errorf("%s", item.Value)
		}
		if item.Type == lexer.ItemEOF {
			break
		}
		items = append(items, item)
	}
	if len(items) < 2 {
		panic("expect at least two tokens")
	}
	vstr := string(bytes.TrimFunc(bs, unicode.IsSpace))
	return vstr, nil
}

const (
	jqVersionMajor lexer.ItemType = iota
	jqVersionMinor
	jqVersionSuffix
)

func scanJQVersion(lex *lexer.Lexer) lexer.StateFn {
	// prefix "jq-"
	if !lex.Accept("j") {
		return lex.Errorf("not a jq version")
	}
	if !lex.Accept("q") {
		return lex.Errorf("not a jq version")
	}
	if !lex.Accept("-") {
		return lex.Errorf("not a jq version")
	}
	lex.Ignore()

	// major version
	if lex.AcceptRun("0123456789") == 0 {
		return lex.Errorf("not a jq version")
	}
	lex.Emit(jqVersionMajor)

	// dot
	if !lex.Accept(".") {
		return lex.Errorf("not a jq version")
	}
	lex.Ignore()

	// minor version
	if lex.AcceptRun("0123456789") == 0 {
		return lex.Errorf("not a jq version")
	}
	lex.Emit(jqVersionMinor)

	// version suffix
	for {
		c, n := lex.Advance()
		if c == utf8.RuneError && n == 1 {
			return lex.Errorf("unvalid utf-8 rune")
		}
		if c == lexer.EOF {
			if lex.Pos() > lex.Start() {
				lex.Emit(jqVersionSuffix)
			}
			break
		}
	}
	lex.Emit(lexer.ItemEOF)
	return nil
}

func Execute(outw, errw io.Writer, in io.Reader, stop <-chan struct{}, jq string, s *JQStack) (int64, int64, error) {
	if jq == "" {
		jq = "jq"
	}
	outcounter := &writeCounter{0, outw}
	errcounter := &writeCounter{0, errw}
	cmd := exec.Command(jq, "-C", JoinFilter(s)) // TODO test if stdout is a terminal
	cmd.Stdin = in
	cmd.Stdout = outcounter
	cmd.Stderr = errcounter
	done := make(chan error, 1)
	err := cmd.Start()
	if err != nil {
		return 0, 0, err
	}
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	select {
	case <-stop:
		err := cmd.Process.Kill()
		if err != nil {
			log.Println("unable to kill process %d", cmd.Process.Pid)
		}
	case err = <-done:
		break
	}
	nout := outcounter.n
	nerr := errcounter.n
	return nout, nerr, err
}

type Filter interface {
	JQFilter() []string
}

var FilterJoinString = " | "

func JoinFilter(filter Filter) string {
	fs := filter.JQFilter()
	if len(fs) == 0 {
		return "."
	}
	return strings.Join(fs, FilterJoinString)
}

type FilterString string

func (s FilterString) JQFilter() []string {
	return []string{string(s)}
}

type JQStack struct {
	pipe []Filter
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

func (s *JQStack) Push(cmd Filter) {
	s.pipe = append(s.pipe, cmd)
}

func (s *JQStack) Pop() (Filter, error) {
	if len(s.pipe) == 0 {
		return nil, ErrStackEmpty
	}
	n := len(s.pipe)
	filt := s.pipe[n-1]
	s.pipe = s.pipe[:n-1]
	return filt, nil
}
