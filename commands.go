package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
)

var ShellExit = fmt.Errorf("exit")

type JQShellCommand interface {
	ExecuteShellCommand(*JQShell, []string) error
}

type JQShellCommandFunc func(*JQShell, []string) error

func (fn JQShellCommandFunc) ExecuteShellCommand(jq *JQShell, args []string) error {
	return fn(jq, args)
}

func cmdQuit(jq *JQShell, args []string) error {
	return ShellExit
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
	jq.istmp = false
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
	flags := Flags("exec", args)
	ignore := flags.Bool("ignore", false, "ignore process exit status")
	filename := flags.String("o", "", "a json file produced by the command")
	pfilename := flags.String("O", "", "like -O but the file will not be deleted by jqsh")
	nocache := flags.Bool("c", false, "disable caching of results (no effect with -o)")
	err := flags.Parse(nil)
	if err != nil {
		return err
	}

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
		jq.SetInput(_cmdExecInput(*ignore, args[0], args[1:]...))
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

	args = flags.Args()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("missing command")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil && !*ignore {
		if istmp {
			os.Remove(path)
		}
		return err
	}

	jq.SetInputFile(path, istmp)

	return nil
}

func _cmdExecInput(ignore bool, name string, args ...string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}

		err = cmd.Start()
		if err != nil && !ignore {
			stdout.Close()
			return nil, err
		}
		return stdout, nil
	}
}

type CmdFlags struct {
	*flag.FlagSet
	args []string
}

func Flags(name string, args []string) *CmdFlags {
	set := flag.NewFlagSet(name, flag.PanicOnError)
	return &CmdFlags{set, args}
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
