package main

import (
	"bytes"
	"fmt"
	"go/doc"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// DocOpt contains documentation formatting option.. I had to do it.
type DocOpt struct {
	Indent    string // indentation for formatted text
	PreIndent string // indentation for preformatted text
	Width     int    // in unicode runes
}

type Lib struct {
	mut    sync.Mutex
	topics map[string][]string
	cmds   map[string]JQShellCommand
	docs   DocOpt
}

func Library(docs *DocOpt) *Lib {
	lib := new(Lib)
	lib.topics = make(map[string][]string)
	lib.cmds = make(map[string]JQShellCommand)
	if docs != nil {
		lib.docs = *docs
		if lib.docs.Width < 0 {
			panic("negative width")
		}
	}
	return lib
}

func (lib *Lib) help(jq *JQShell, args []string) error {
	flags := Flags("help", args)
	flags.ArgSet("[topic]")
	flags.ArgDoc("topic", "a command name or other help topic")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	switch len(args) {
	case 1:
		return lib.helpName(jq, args[0])
	case 0:
		return lib.helpList()
	default:
		return fmt.Errorf("at most one help topic is allowed")
	}
}

func (lib *Lib) helpName(jq *JQShell, name string) error {
	_, ok := lib.cmds[name]
	if ok {
		return lib.exec(jq, name, []string{"-h"})
	}
	docs, ok := lib.topics[name]
	if ok {
		lib.formatp(os.Stdout, docs)
		return nil
	}
	return fmt.Errorf("unknown topic")
}

func (lib *Lib) helpList() error {
	fmt.Println("available commands:")
	for name := range lib.cmds {
		fmt.Println("\t" + name)
	}
	fmt.Println("\thelp")
	fmt.Println("pass -h to a command for usage details")

	if len(lib.topics) > 0 {
		fmt.Println("additional help topics:")
		for name := range lib.topics {
			fmt.Println("\t" + name)
		}
		fmt.Println("for information on a topic run `help <topic>`")
	}

	return nil
}

func (lib *Lib) Register(name string, cmd JQShellCommand) {
	lib.mut.Lock()
	defer lib.mut.Unlock()
	err := lib.taken(name)
	if err != nil {
		panic(err)
	}
	lib.cmds[name] = cmd
}

func (lib *Lib) RegisterHelp(name string, docs ...string) {
	lib.mut.Lock()
	defer lib.mut.Unlock()
	err := lib.taken(name)
	if err != nil {
		panic(err)
	}
	lib.topics[name] = docs
}

func (lib *Lib) taken(name string) error {
	_, ok := lib.cmds[name]
	if ok {
		return fmt.Errorf("%q command already registered", name)
	}
	_, ok = lib.topics[name]
	if ok {
		return fmt.Errorf("%q help topic already registered", name)
	}
	return nil
}

// formatp joins the strings of doc into a single string, reformating by
// wrapping long lines and normalizing paragram gap. formatp treats the strings
// of doc as if they are separated by newlines. A new paragraph is determined
// by either the first non-empty string or a sequence of two newlines.
func (lib *Lib) formatp(w io.Writer, docs []string) {
	doc.ToText(w, strings.Join(docs, "\n"), lib.docs.Indent, lib.docs.PreIndent, lib.docs.Width)
}

// Execute looks for name as a registered command and executes it. If name is
// "help" then lib's help command is executed.
func (lib *Lib) Execute(jq *JQShell, name string, args []string) error {
	lib.mut.Lock()
	defer lib.mut.Unlock()
	if name == "help" {
		lib.help(jq, args)
		return nil
	}
	return lib.exec(jq, name, args)
}

func (lib *Lib) exec(jq *JQShell, name string, args []string) error {
	cmd, ok := lib.cmds[name]
	if !ok {
		return fmt.Errorf("%v: unknown command", name)
	}
	err := cmd.ExecuteShellCommand(jq, Flags(name, args))
	if err != nil {
		return ExecError{append([]string{name}, args...), err}
	}
	return nil
}

var ShellExit = fmt.Errorf("exit")

type JQShellCommand interface {
	ExecuteShellCommand(*JQShell, *CmdFlags) error
}

type JQShellCommandFunc func(*JQShell, *CmdFlags) error

func (fn JQShellCommandFunc) ExecuteShellCommand(jq *JQShell, flags *CmdFlags) error {
	return fn(jq, flags)
}

func cmdQuit(jq *JQShell, flags *CmdFlags) error {
	err := flags.Parse(nil)
	if err != nil {
		return err
	}
	return ShellExit
}

func cmdScript(jq *JQShell, flags *CmdFlags) error {
	flags.Bool("oneline", false, "do not print a hash-bang (#!) line")
	flags.String("f", "", "specify the file argument to jq")
	flags.Bool("F", false, "use the current file as the argument to jq")
	flags.String("o", "", "path to write executable script")
	flags.Parse(nil)

	var script []string
	script = append(script, "#!/usr/bin/env sh")
	script = append(script, "")

	f := JoinFilter(jq.Stack)
	fesc := shellEscape(f, "'", "\\'")
	bin := "jq"
	cmd := []string{bin}
	cmd = append(cmd, fesc)
	cmd = append(cmd, `"${@}"`)
	script = append(script, strings.Join(cmd, " "))

	fmt.Println(strings.Join(script, "\n"))
	return nil
}

func shellEscape(s, q, qesc string) string {
	return q + strings.Replace(s, q, qesc, -1) + q
}

func cmdFilter(jq *JQShell, flags *CmdFlags) error {
	jqsyntax := flags.Bool("jq", false, "print the filter with jq syntax")
	qchars := flags.String("quote", "", "quote and escaped quote runes the -jq filter string (e.g. \"'\\'\")")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if *jqsyntax {
		var quote, qesc string
		if *qchars != "" {
			qrune, n := utf8.DecodeRuneInString(*qchars)
			if qrune == utf8.RuneError && n == 1 {
				return fmt.Errorf("invalid quote runes %q: %v", *qchars, err)
			}
			quote = string([]rune{qrune})
			qesc = (*qchars)[n:]
			if qesc == "" {
				return fmt.Errorf("missing escape for quote character '%c'", qrune)
			}
		}
		filter := JoinFilter(jq.Stack)
		filter = shellEscape(filter, quote, qesc)
		fmt.Println(filter)
		return nil
	}
	filters := jq.Stack.JQFilter()
	if len(filters) == 0 {
		fmt.Fprintln(os.Stderr, "no filter")
		return nil
	}
	for i, piece := range filters {
		fmt.Printf("[%02d] %v\n", i, piece)
	}
	return nil
}

func cmdPeek(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("filter", "...")
	flags.ArgDoc("filter", "a jq filter (may contain pipes '|')")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	filters := flags.Args()
	for _, f := range filters {
		if f == "" {
			continue
		}
		jq.Stack.Push(FilterString(f))
	}

	err = cmdWrite(jq, Flags("write", nil))

	jq.Stack.Pop(len(filters))
	if err != nil {
		return fmt.Errorf("invalid filter: %v", err)
	}

	return nil
}

func cmdPush(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("filter", "...")
	flags.ArgDoc("filter", "a jq filter (may contain pipes '|')")
	quiet := flags.Bool("q", false, "quiet -- no implicit :write after push")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()
	for _, arg := range args {
		if arg == "" {
			continue
		}
		jq.Stack.Push(FilterString(arg))
	}
	err = testFilter(jq)
	if err != nil {
		jq.Stack.Pop(1)
		return err
	}
	if !*quiet {
		return cmdWrite(jq, Flags("write", []string{}))
	}
	return nil
}

var testFilterTimeout = 10 * time.Second

func testFilter(jq *JQShell) error {
	var empty bytes.Buffer
	var errbuf bytes.Buffer
	var err error
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_, _, err = Execute(ioutil.Discard, &errbuf, &empty, stop, jq.bin, false, jq.Stack)
		close(done)
	}()
	select {
	case <-done:
		if err != nil {
			return fmt.Errorf("%s (%v)", errbuf.Bytes(), err)
		}
		return nil
	case <-time.After(testFilterTimeout):
		close(stop)
		return fmt.Errorf("jq timed out processing the filter")
	}
}

func cmdPopAll(jq *JQShell, flags *CmdFlags) error {
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	jq.Stack.PopAll()
	return nil
}

func cmdPop(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[n]")
	flags.ArgDoc("n=1", "the number items to pop off the stack")
	quiet := flags.Bool("q", false, "quiet -- no implicit :write after pop")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()
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
			return fmt.Errorf("argument must be an integer")
		}
	}
	if n < 0 {
		return fmt.Errorf("argument must be positive")
	}
	_, err = jq.Stack.Pop(n)
	if err != nil {
		return err
	}
	if !*quiet {
		return cmdWrite(jq, Flags("write", nil))
	}
	return nil
}

func cmdLoad(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("filename")
	flags.ArgDoc("filename", "a file contain json data")
	quiet := flags.Bool("q", false, "quiet -- no implicit :write after setting input")
	keepStack := flags.Bool("k", false, "keep the current filter stack after setting input")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()

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
	jq.istmp = false
	if !*keepStack {
		jq.Stack.PopAll()
	}
	if !*quiet {
		return cmdWrite(jq, Flags("write", nil))
	}
	return nil
}

func cmdPipeShell(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("command")
	flags.ArgDoc("command", "a shell command (may contain pipes '|' and output redirection '>')")
	color := flags.Bool("-color", false, "pass colorized json to command")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()
	if len(args) == 0 {
		return fmt.Errorf("missing command")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	shcmd := []string{shell, "-c", args[0]}
	cmd := exec.Command(shell, shcmd[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating pipe: %v", err)
	}
	err = cmd.Start()
	waiterr := make(chan error, 1)
	go func() {
		waiterr <- cmd.Wait()
		stdin.Close()
	}()
	if err != nil {
		return ExecError{shcmd, err}
	}
	_, _, err = cmdWrite_io(jq, stdin, *color, nil)
	if err != nil {
		<-waiterr
		return err
	}
	err = <-waiterr
	if err != nil {
		return ExecError{shcmd, err}
	}
	return nil
}

func cmdWrite(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[filename]")
	flags.ArgDoc("filename", "write to a file instead of stdout/pager")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()
	if len(args) == 0 {
		return cmdWrite_page(jq)
	}
	return cmdWrite_file(jq, args[0])
}

func cmdWrite_page(jq *JQShell) error {
	w, errch := Page(nil)
	select {
	case err := <-errch:
		return err
	default:
		break
	}
	pageerr := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		err := <-errch
		close(stop)
		if err != nil {
			pageerr <- err
		}
		close(pageerr)
	}()
	_, _, err := cmdWrite_io(jq, w, true, stop)
	if err != nil {
		return err
	}
	pageErr := <-pageerr
	if pageErr != nil {
		jq.log("pager: ", pageErr)
	}
	return nil
}

func cmdWrite_file(jq *JQShell, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	nout, _, err := cmdWrite_io(jq, f, false, nil)
	if err == nil {
		jq.Log.Printf("%d bytes written to %q", nout, filename)
	}
	return err
}

func cmdWrite_io(jq *JQShell, w io.WriteCloser, color bool, stop chan struct{}) (int64, int64, error) {
	defer w.Close()
	r, err := jq.Input()
	if err == ErrNoInput {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	defer r.Close()
	nout, nerr, err := Execute(w, os.Stderr, r, stop, jq.bin, color, jq.Stack)
	if err != nil {
		return nout, nerr, ExecError{[]string{"jq"}, err}
	}
	return nout, nerr, err
}

func cmdRaw(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[filename]")
	flags.ArgDoc("filename", "write to a file instead of stdout/pager")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()
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
		pageerr := make(chan error, 1)
		stop := make(chan struct{})
		go func() {
			err := <-errch
			close(stop)
			if err != nil {
				pageerr <- err
			}
			close(pageerr)
		}()
		_, err = io.Copy(w, r)
		w.Close()
		if perr, ok := err.(*os.PathError); ok {
			if perr.Err == syscall.EPIPE {
				//jq.Log.Printf("DEBUG broken pipe")
			}
		} else if err != nil {
			return fmt.Errorf("copying file: %#v", err)
		}
		pageErr := <-pageerr
		if pageErr != nil {
			jq.log("pager: ", pageErr)
		}
		return nil
	}
	return fmt.Errorf("file output not allowed")
}

func cmdExec(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("cmd", "[arg ...]")
	flags.ArgDoc("cmd", "a name in PATH or the path to an executable file")
	flags.ArgDoc("arg", "passed as an argument to cmd")
	quiet := flags.Bool("q", false, "quiet -- no implicit :write after setting input")
	keepStack := flags.Bool("k", false, "keep the current filter stack after setting input")
	ignore := flags.Bool("ignore", false, "ignore process exit status")
	filename := flags.String("o", "", "a json file produced by the command")
	pfilename := flags.String("O", "", "like -O but the file will not be deleted by jqsh")
	nocache := flags.Bool("c", false, "disable caching of results (no effect with -o)")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	args := flags.Args()

	var out io.Writer
	var path string
	var istmp bool
	if *filename != "" && *pfilename != "" {
		return fmt.Errorf("both -o and -O given")
	}
	if *filename != "" {
		path = *filename
		istmp = true
	} else if *pfilename != "" {
		path = *pfilename
	}
	if *nocache {
		jq.SetInput(_cmdExecInput(jq, args[0], args[1:]...))
		return nil
	}
	if path == "" {
		tmpfile, err := ioutil.TempFile("", "jqsh-exec-")
		if err != nil {
			return fmt.Errorf("creating temp file: %v", err)
		}
		path = tmpfile.Name()
		istmp = true
		out = tmpfile
		defer tmpfile.Close()
	} else {
		out = os.Stdout
	}

	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("missing command")
	}
	stdout, err := _cmdExecInput(jq, args[0], args[1:]...)()
	if err != nil && !*ignore {
		os.Remove(path)
		return err
	}
	_, err = io.Copy(out, stdout)
	if err != nil {
		os.Remove(path)
		return err
	}

	jq.SetInputFile(path, istmp)

	if !*keepStack {
		jq.Stack.PopAll()
	}

	if !*quiet {
		return cmdWrite(jq, Flags("write", nil))
	}

	return nil
}

func _cmdExecInput(jq *JQShell, name string, args ...string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		cmd := exec.Command(name, args...)
		//cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}

		err = cmd.Start()
		if err != nil {
			stdout.Close()
			return nil, err
		}
		go func() {
			err := cmd.Wait()
			if err != nil {
				jq.Log.Printf("%v: %v", name, err)
			} else {
				jq.Log.Printf("%v: exit status 0", name)
			}
		}()
		return stdout, nil
	}
}
