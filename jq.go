package main

import (
	"fmt"
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
		err := item.Error()
		if err != nil {
			return "", err
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

	if lex.AcceptRun("0123456789") == 0 {
		return lex.Errorf("not a jq version")
	}
	lex.Emit(jqVersionMajor)

	if !lex.Accept(".") {
		return lex.Errorf("not a jq version")
	}
	lex.Ignore()

	if lex.AcceptRun("0123456789") == 0 {
		return lex.Errorf("not a jq version")
	}
	lex.Emit(jqVersionMinor)

	for {
		c, n := lex.Advance()
		if c == utf8.RuneError && n == 1 {
			return lex.Errorf("unvalid utf-8 rune")
		}
		if c == lexer.EOF {
			if lex.Pos() > lex.Start() {
				lex.Emit(jqVersionSuffix)
			}
		}
	}
	lex.Emit(lexer.ItemEOF)
	return nil
}
