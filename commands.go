package main

import (
	"fmt"
	"os"
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
