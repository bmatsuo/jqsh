package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
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
	vstr, _, _, _, err := ParseJQVersion(string(bs))
	if err != nil {
		return "", err
	}
	return vstr, nil
}

const (
	jqVersionMajor lexer.ItemType = iota
	jqVersionMinor
	jqVersionSuffix
)

func ParseJQVersion(vstr string) (s string, major, minor int, suffix string, err error) {
	vstr = strings.TrimFunc(vstr, unicode.IsSpace) // BUG this breaks error position information (currently unused)
	lex := lexer.New(scanJQVersion, vstr)
	var items []*lexer.Item
	for {
		item := lex.Next()
		if item.Type == lexer.ItemError {
			return "", 0, 0, "", fmt.Errorf("%s", item.Value)
		}
		if item.Type == lexer.ItemEOF {
			break
		}
		items = append(items, item)
	}
	s = vstr
	if len(items) < 2 {
		panic("expect at least two tokens")
	}
	if items[0].Type != jqVersionMajor {
		err = fmt.Errorf("unexpected token %v", items[0])
		return
	}
	major, err = strconv.Atoi(items[0].String())
	if err != nil {
		err = fmt.Errorf("invalid major version: %v", err)
		return
	}
	if items[1].Type != jqVersionMinor {
		err = fmt.Errorf("unexpected token %v", items[0])
		return
	}
	minor, err = strconv.Atoi(items[1].String())
	if err != nil {
		err = fmt.Errorf("invalid minor version: %v", err)
		return
	}
	if len(items) > 2 {
		if items[2].Type != jqVersionSuffix {
			err = fmt.Errorf("unexpected token: %q (%d)", items[2], items[2].Type)
			return
		}
		suffix = items[2].String()
	}
	return
}

func scanJQVersion(lex *lexer.Lexer) lexer.StateFn {
	// prefix "jq-"
	if !lex.Accept("j") {
		return lex.Errorf("not a jq version")
	}
	if !lex.Accept("q") {
		return lex.Errorf("not a jq version")
	}
	if !lex.Accept("-") {
		if !lex.Accept(" ") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("v") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("e") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("r") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("s") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("i") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("o") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept("n") {
			return lex.Errorf("not a jq version")
		}
		if !lex.Accept(" ") {
			return lex.Errorf("not a jq version")
		}
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

func Execute(outw, errw io.Writer, in io.Reader, stop <-chan struct{}, jq string, color bool, s *JQStack) (int64, int64, error) {
	if jq == "" {
		jq = "jq"
	}
	outcounter := &writeCounter{0, outw}
	errcounter := &writeCounter{0, errw}
	var args []string
	if color {
		args = append(args, "--color-output")
	}
	args = append(args, JoinFilter(s))
	cmd := exec.Command(jq, args...)
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
