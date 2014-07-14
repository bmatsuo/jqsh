package main

import (
	"bytes"
	"fmt"
	"go/doc"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode/utf8"
)

var warnNoInput = "jqsh: no input has been declared"

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
	lib.Register("help", JQShellCommandFunc(lib.help))
	return lib
}

func (lib *Lib) help(jq *JQShell, flags *CmdFlags) error {
	flags.About("Command help browses documentation for jqsh.")
	flags.ArgSet("[topic]")
	flags.ArgDoc("topic", "a command name or other help topic")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}
	switch flags.NArg() {
	case 1:
		return lib.helpName(jq, flags.Arg(0))
	case 0:
		return lib.helpList(jq)
	default:
		return fmt.Errorf("at most one help topic is allowed")
	}
}

func (lib *Lib) helpName(jq *JQShell, name string) error {
	_, ok := lib.cmds[name]
	if ok {
		return lib.exec(nil, jq, name, []string{"-h"})
	}
	docs, ok := lib.topics[name]
	if ok {
		lib.formatp(os.Stdout, docs)
		return nil
	}
	return fmt.Errorf("unknown topic")
}

func (lib *Lib) helpList(jq *JQShell) error {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "commands:")
	var names []string
	for name := range lib.cmds {
		names = append(names, name)
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(&buf, 5, 4, 2, ' ', 0) // TODO chosen more carefully
	for _, name := range names {
		var cbuf bytes.Buffer
		lib.exec(&cbuf, jq, name, []string{"-h"})
		synop := doc.Synopsis(cbuf.String())
		fmt.Fprintln(tw, "  "+name+"\t"+synop)
	}
	tw.Flush()

	// reuse names to print topics
	if len(names) > 0 {
		names = names[:0]
	}
	if len(lib.topics) > 0 {
		fmt.Fprintln(&buf, "other topics:")
		for name := range lib.topics {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			synop := doc.Synopsis(strings.Join(lib.topics[name], "\n"))
			fmt.Fprintln(tw, "  "+name+"\t"+synop)
		}
		tw.Flush()
	}

	fmt.Fprintln(&buf, "for information on a topic run `help <topic>`")

	doc.ToText(os.Stderr, buf.String(), lib.docs.Indent, lib.docs.PreIndent, lib.docs.Width)

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
	return lib.exec(nil, jq, name, args)
}

func (lib *Lib) exec(w io.Writer, jq *JQShell, name string, args []string) error {
	cmd, ok := lib.cmds[name]
	if !ok {
		return fmt.Errorf("%v: unknown command", name)
	}
	flags := Flags(name, args)
	if w != nil {
		flags.SetOutput(w)
	}
	err := cmd.ExecuteShellCommand(jq, flags)
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
	flags.About("Command quit exits jqsh.")
	flags.ArgSet()
	err := flags.Parse(nil)
	if err != nil {
		return err
	}
	return ShellExit
}

func cmdScript(jq *JQShell, flags *CmdFlags) error {
	flags.About("Command script generates a shell script from the current filter.")
	flags.ArgSet()
	flags.Bool("oneline", false, "do not print a hash-bang (#!) line")
	flags.String("f", "", "specify the file argument to jq")
	flags.Bool("F", false, "use the current file as the argument to jq")
	flags.String("o", "", "path to write executable script")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}

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
	flags.About("Command filter prints the current filter stack.")
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
	flags.About("Command peek applies filters without pushing them on the stack.")
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
	flags.About("Command push adds a filter to the stack.")
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
	flags.About("Command popall removes all filters from the stack.")
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
	flags.About("Command pop removes the last filter(s) pushed on the stack.")
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
	flags.About("Command load sets the input to the contents of a file.")
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

func cmdPipe(jq *JQShell, flags *CmdFlags) error {
	flags.About(
		"Command pipe runs a shell command.",
		"The given shell command can process filter output or generate filter input.",
	)
	flags.ArgSet("cmd")
	flags.ArgDoc("cmd", "a shell script to execute")
	pipein := flags.Bool("in", false, "set filter input to cmd's stdout")
	pipeout := flags.Bool("out", false, "write filter output to cmd's stdin")
	quiet := flags.Bool("q", false, "quiet -- no implicit :write after setting input")
	keepStack := flags.Bool("k", false, "keep the current filter stack after setting input")
	ignore := flags.Bool("ignore", false, "ignore process exit status when setting input")
	filename := flags.String("o", "", "a json file produced by the command to use as input")
	pfilename := flags.String("O", "", "like -O but the file will not be deleted by jqsh")
	nocache := flags.Bool("c", false, "disable caching of filter input (no effect with -o)")
	color := flags.Bool("color", false, "allow escape codes in filter output")
	flags.Docs(
		"Currently it is invalid for both -in and -out to be given.",
		"In the future it is likely that this restriction may be lifted.",
	)
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if flags.NArg() == 0 {
		return fmt.Errorf("missing command")
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("expect exactly one command")
	}

	if *pipein && *pipeout {
		return fmt.Errorf("command cannot be both input and output")
	}

	if *filename != "" && *pfilename != "" {
		return fmt.Errorf("both -o and -O given")
	}

	if !*pipein && !*pipeout {
		*pipein = true
	}

	if *pipeout {
		return pipeTo(jq, flags.Arg(0), *color)
	}
	if *pipein {
		options := &InputPipeOptions{
			Quiet:     *quiet,
			KeepStack: *keepStack,
			Ignore:    *ignore,
			NoCache:   *nocache,
		}
		if *filename != "" {
			options.Filename = *filename
			options.Delete = true
		}
		if *pfilename != "" {
			options.Filename = *filename
		}
		return pipeFrom(jq, flags.Arg(0), options)
	}

	panic("unreachable")
}

type InputPipeOptions struct {
	Quiet     bool
	KeepStack bool
	Ignore    bool
	Filename  string
	Delete    bool
	NoCache   bool
}

func pipeFrom(jq *JQShell, script string, options *InputPipeOptions) error {
	var opt InputPipeOptions
	if options != nil {
		opt = *options
	}
	var out io.Writer
	var path string
	var istmp bool
	if opt.Filename != "" {
		path = opt.Filename
		istmp = opt.Delete
	}
	if opt.NoCache {
		jq.SetInput(_pipeInput(jq, "bash", "-c", script))
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

	stdout, err := _pipeInput(jq, "bash", "-c", script)()
	if err != nil && !opt.Ignore {
		os.Remove(path)
		return err
	}
	_, err = io.Copy(out, stdout)
	if err != nil {
		os.Remove(path)
		return err
	}

	jq.SetInputFile(path, istmp)

	if !opt.KeepStack {
		jq.Stack.PopAll()
	}

	if !opt.Quiet {
		return cmdWrite(jq, Flags("write", nil))
	}

	return nil
}

func pipeTo(jq *JQShell, script string, color bool) error {
	// warn if no input has been declared, but continue executing jq and paging
	// output. i think this is the best thing to do.
	// https://github.com/bmatsuo/jqsh/issues/23
	if !jq.HasInput() {
		fmt.Fprintln(os.Stderr, warnNoInput)
	}

	// execute the script under the users preferred shell.  when the process
	// exits close the pipe jq is writing to.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	shcmd := []string{shell, "-c", script}
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

	// write jq output to the process until the pipe closes or there is no more
	// filter output to write. wait for the script process to exit before
	// returning in any case.
	_, _, err = cmdWrite_io(jq, stdin, color, nil)
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
	flags.About("Command write writes filter output to a file or stdout.")
	flags.ArgSet("[filename]")
	flags.ArgDoc("filename", "write to a file instead of stdout/pager")
	err := flags.Parse(nil)
	if IsHelp(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// warn if no input has been declared, but continue executing jq and paging
	// output. i think this is the best thing to do.
	// https://github.com/bmatsuo/jqsh/issues/23
	if !jq.HasInput() {
		fmt.Fprintln(os.Stderr, warnNoInput)
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
	flags.About("Command raw writes input to a file without applying the filter.")
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

func _pipeInput(jq *JQShell, name string, args ...string) func() (io.ReadCloser, error) {
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
