package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
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
