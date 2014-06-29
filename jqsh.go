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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

var ShellExit = fmt.Errorf("exit")

var ErrStackEmpty = fmt.Errorf("the stack is empty")

func main() {
	loadfile := flag.String("-exec", "", "executable data should be loaded with (e.g. 'curl')")
	flag.Parse()
	args := flag.Args()
	if *loadfile != "" && len(args) > 0 {
		log.Fatal("arguments cannot be given when flag -f is provided")
	}
	jq := NewJQShell(nil)
	err := jq.Wait()
	if err != nil {
		log.Fatal(err)
	}
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
		if err != nil {
			errch <- err
		}
		close(errch)
		fmt.Print("\033[0m")
	}()
	return stdin, errch
}

func Execute(out, err io.Writer, in io.Reader, jq string, s *JQStack) (int64, int64, error) {
	if jq == "" {
		jq = "jq"
	}
	outcounter := &writeCounter{0, out}
	errcounter := &writeCounter{0, err}
	filter := strings.Join(s.JQFilter(), " | ")
	cmd := exec.Command(jq, "-C", filter) // TODO test if stdout is a terminal
	cmd.Stdin = in
	cmd.Stdout = outcounter
	cmd.Stderr = errcounter
	e := cmd.Run()
	nout := outcounter.n
	nerr := errcounter.n
	return nout, nerr, e
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

type JQShell struct {
	Log      *log.Logger
	Stack    *JQStack
	filename string
	lib      map[string]JQShellCommand
	sh       ShellReader
	err      error
	wg       sync.WaitGroup
}

func NewJQShell(sh ShellReader) *JQShell {
	if sh == nil {
		sh = NewShellReader(nil)
	}
	st := new(JQStack)
	jq := &JQShell{
		Log:   log.New(os.Stderr, "jqsh: ", 0),
		Stack: st,
		sh:    sh,
	}
	jq.lib = map[string]JQShellCommand{
		"":      nil,
		"push":  JQShellCommandFunc(cmdPush),
		"pop":   JQShellCommandFunc(cmdPop),
		"load":  JQShellCommandFunc(cmdLoad),
		"write": JQShellCommandFunc(cmdWrite),
	}
	jq.wg.Add(1)
	go jq.loop()
	return jq
}

func (jq *JQShell) Input() (io.ReadCloser, error) {
	if jq.filename == "" {
		return nil, fmt.Errorf("no input")
	}
	return os.Open(jq.filename)
}

func (jq *JQShell) Wait() error {
	jq.wg.Wait()
	return jq.err
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
				if err == ShellExit {
					_stop()
					return
				}
				if cmd.eof {
					_stop()
					return
				}
				if err != nil {
					jq.log(err)
				} else {
					// TODO clean this up. (cmdPushInteractive, cmdPeek)
					err := jq.execute([]string{"write"}, nil)
					if err != nil {
						jq.log(err)
						if cmd.cmd[0] == "push" {
							npush := len(cmd.cmd) - 1
							if npush == 0 {
								npush = 1
							}
							jq.log("reverting push operation")
							err := jq.execute([]string{"pop", fmt.Sprint(npush)}, nil)
							if err != nil {
								jq.log(err)
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

type JQShellCommand interface {
	ExecuteShellCommand(*JQShell, []string) error
}

type JQShellCommandFunc func(*JQShell, []string) error

func (fn JQShellCommandFunc) ExecuteShellCommand(jq *JQShell, args []string) error {
	return fn(jq, args)
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
	if err, ok := err.(InvalidCommandError); ok {
		jq.log(err)
		return nil
	}
	if err != nil {
		return err
	}
	if len(cmd) > 0 {
		name, args := cmd[0], cmd[1:]
		shellcmd, ok := jq.lib[name]
		if !ok {
			return fmt.Errorf("%s: command unknown", name)
		}
		if shellcmd != nil {
			execerr := shellcmd.ExecuteShellCommand(jq, args)
			if execerr != nil {
				return ExecError{cmd, execerr}
			}
		}
	}
	return nil
}

func cmdPush(jq *JQShell, args []string) error {
	for _, arg := range args {
		if arg == "" {
			continue
		}
		jq.Stack.Push(JQFilterString(arg))
	}
	return nil
}

func cmdPop(jq *JQShell, args []string) error {
	var n int
	if len(args) > 1 {
		return fmt.Errorf("too many arguments given")
	}
	if len(args) == 0 {
		n = 1
	} else {
		var err error
		n, err = strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("argument must me an integer")
		}
	}
	if n < 0 {
		return fmt.Errorf("argument must be positive")
	}
	for i := 0; i < n; i++ {
		jq.Stack.Pop()
	}
	return nil
}

func cmdLoad(jq *JQShell, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expects one filename")
	}
	f, err := os.Open(args[0])
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("error closing file")
	}
	jq.filename = args[0]
	return nil
}

func cmdWrite(jq *JQShell, args []string) error {
	if len(args) == 0 {
		r, err := jq.Input()
		if err != nil {
			return err
		}
		defer r.Close()
		w, errch := Page(nil)
		select {
		case err := <-errch:
			return err
		default:
			break
		}
		_, _, err = Execute(w, os.Stderr, r, "", jq.Stack)
		w.Close()
		pageErr := <-errch
		if err != nil {
			return ExecError{[]string{"jq"}, err}
		}
		if pageErr != nil {
			jq.log("pager: ", pageErr)
		}
		return nil
	}
	return fmt.Errorf("file output not allowed")
}

func cmdExec(jq *JQShell, args []string) error {
	return nil
}

type JQFilter interface {
	JQFilter() []string
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
