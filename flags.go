package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

func IsHelp(err error) bool {
	return err == flag.ErrHelp
}

type CmdFlags struct {
	*flag.FlagSet
	w       io.Writer
	name    string
	docs    []string
	args    []string
	argdocs [][]string
	argsets [][]string
}

func Flags(name string, args []string) *CmdFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	f := new(CmdFlags)
	f.FlagSet = set
	f.name = name
	f.args = args
	f.SetOutput(os.Stderr)
	set.Usage = f.help
	return f
}

// Docs sets sets the command's documentation
func (f *CmdFlags) Docs(docs ...string) {
	f.docs = docs
}

func (f *CmdFlags) ArgSet(args ...string) {
	f.argsets = append(f.argsets, args)
}

func (f *CmdFlags) ArgDoc(arg, help string) {
	f.argdocs = append(f.argdocs, []string{arg, help})
}

func (f *CmdFlags) help() {
	w := f.w
	if w == nil {
		w = ioutil.Discard
	}
	if len(f.docs) > 0 {
		fmt.Fprintln(w, strings.Join(f.docs, "\n"))
		fmt.Fprintln(w)
	}
	if len(f.argsets) == 0 {
		fmt.Fprintln(w, f.name)
	} else {
		sets := f.argsets
		for _, set := range sets {
			fmt.Fprintln(w, "  "+f.name+" "+strings.Join(set, " "))
		}
	}
	fmt.Fprintln(w)
	for _, argdoc := range f.argdocs {
		if len(argdoc) == 0 {
			panic("empty argdoc")
		}
		fmt.Fprintf(w, "  %s: %s\n", argdoc[0], strings.Join(argdoc[1:], " "))
	}
	f.PrintDefaults()
}

func (f *CmdFlags) SetOutput(w io.Writer) {
	f.w = w
	f.FlagSet.SetOutput(w)
}

func (f *CmdFlags) Parse(args *[]string) (err error) {
	if args == nil {
		args = &f.args
	}
	return f.FlagSet.Parse(*args)
}
