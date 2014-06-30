package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"unicode/utf8"
)

type Lib struct {
	mut    sync.Mutex
	topics map[string]string
	cmds   map[string]JQShellCommand
}

func Library() *Lib {
	lib := new(Lib)
	lib.topics = make(map[string]string)
	lib.cmds = make(map[string]JQShellCommand)
	return lib
}

func (lib *Lib) help(jq *JQShell, args []string) error {
	flags := Flags("help", args)
	err := flags.Parse(nil)
	if IsHelp(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	if len(args) == 0 {
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
	}
	return nil
}

func (lib *Lib) Register(name string, cmd JQShellCommand) {
	lib.mut.Lock()
	defer lib.mut.Unlock()
	_, ok := lib.cmds[name]
	if ok {
		panic("already registered")
	}
	lib.cmds[name] = cmd
}

func (lib *Lib) Execute(jq *JQShell, name string, args []string) error {
	lib.mut.Lock()
	defer lib.mut.Unlock()
	if name == "help" {
		lib.help(jq, args)
		return nil
	}
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

func cmdPush(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("filter", "...")
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
		jq.Stack.Push(JQFilterString(arg))
	}
	return nil
}

func cmdPop(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[n]")
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
	for i := 0; i < n; i++ {
		jq.Stack.Pop()
	}
	return nil
}

func cmdLoad(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("filename")
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
	return nil
}

func cmdWrite(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[filename]")
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
		_, _, err = Execute(w, os.Stderr, r, stop, jq.bin, jq.Stack)
		w.Close()
		if err != nil {
			return ExecError{[]string{"jq"}, err}
		}
		pageErr := <-pageerr
		if pageErr != nil {
			jq.log("pager: ", pageErr)
		}
		return nil
	}
	return fmt.Errorf("file output not allowed")
}

func cmdRaw(jq *JQShell, flags *CmdFlags) error {
	flags.ArgSet("[filename]")
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
	flags.ArgSet("name", "arg", "...")
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

func IsHelp(err error) bool {
	return err == flag.ErrHelp
}

type CmdFlags struct {
	*flag.FlagSet
	name    string
	args    []string
	argsets [][]string
}

func Flags(name string, args []string) *CmdFlags {
	set := flag.NewFlagSet(name, flag.PanicOnError)
	f := &CmdFlags{set, name, args, nil}
	set.Usage = f.usage
	return f
}

func (f *CmdFlags) ArgSet(args ...string) {
	f.argsets = append(f.argsets, args)
}

func (f *CmdFlags) usage() {
	w := tabwriter.NewWriter(os.Stdout, 5, 2, 2, ' ', 0)
	fmt.Fprintf(w, "usage:\t")
	if len(f.argsets) == 0 {
		fmt.Fprintln(w, f.name)
	} else {
		sets := f.argsets
		fmt.Fprintln(w, f.name+" "+strings.Join(sets[0], " "))
		for _, set := range sets[1:] {
			fmt.Fprintln(w, "\t"+f.name+" "+strings.Join(set, " "))
		}
	}
	w.Flush()
	f.PrintDefaults()
}

func (f *CmdFlags) Parse(args *[]string) (err error) {
	defer func() {
		if e := recover(); e != nil {
			var iserr bool
			err, iserr = e.(error)
			if iserr {
				return
			}
			err = fmt.Errorf("%v", e)
		}
	}()
	if args == nil {
		args = &f.args
	}
	f.FlagSet.Parse(*args)
	return nil
}
