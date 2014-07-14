package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"text/tabwriter"
)

func IsHelp(err error) bool {
	return err == flag.ErrHelp
}

type CmdFlags struct {
	*flag.FlagSet
	w       io.Writer
	name    string
	args    []string
	argdocs [][]string
	argsets [][]string
}

func Flags(name string, args []string) *CmdFlags {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	f := &CmdFlags{set, nil, name, args, nil, nil}
	f.SetOutput(os.Stderr)
	set.Usage = f.usage
	return f
}

func (f *CmdFlags) ArgSet(args ...string) {
	f.argsets = append(f.argsets, args)
}

func (f *CmdFlags) ArgDoc(arg, help string) {
	f.argdocs = append(f.argdocs, []string{arg, help})
}

func (f *CmdFlags) usage() {
	fw := f.w
	if fw == nil {
		fw = ioutil.Discard
	}
	w := tabwriter.NewWriter(fw, 5, 2, 2, ' ', 0)
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
	for _, argdoc := range f.argdocs {
		if len(argdoc) == 0 {
			panic("empty argdoc")
		}
		fmt.Fprintf(fw, "  %s: %s\n", argdoc[0], strings.Join(argdoc[1:], " "))
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
