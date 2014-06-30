package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"unicode/utf8"

	"github.com/bmatsuo/go-lexer"
)

var ErrJQNotFound = fmt.Errorf("jq executable not found")

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
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", ErrJQNotFound
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	return path, nil
}

func CheckJQVersion(path string) (string, error) {
	cmd := exec.Command(path, "--version")
	bs, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	lex := lexer.New(scanJQVersion, bs)
	var items []*lexer.Item
	for {
		item := lex.Next()
		//log.Printf("%q", item)
		err := item.Error()
		if err != nil {
			return "", err
		}
		if item.Type == lexer.ItemEOF {
			break
		}
		items = append(items, item)
	}
	if len(items) < 2 {
		panic("expect at least two tokens")
	}
	return string(bs), nil
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
