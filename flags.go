package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/doc"
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
	docopt  DocOpt
	w       io.Writer
	name    string
	about   []string
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
	f.docopt.Width = 76
	f.docopt.Indent = "  "
	f.docopt.PreIndent = "\t"
	f.SetOutput(os.Stderr)
	set.Usage = f.help
	return f
}

// About provides a high level summary of the command.
func (f *CmdFlags) About(docs ...string) {
	f.about = docs
}

// Docs provides detailed documentation of the command's behavior.
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
	var buf bytes.Buffer
	f.FlagSet.SetOutput(&buf)
	if len(f.about) > 0 {
		fmt.Fprintln(&buf, strings.Join(f.about, "\n"))
		fmt.Fprintln(&buf)
	}
	fmt.Fprintln(&buf, "usage:")
	if len(f.argsets) == 0 {
		fmt.Fprintln(&buf, f.name)
	} else {
		sets := f.argsets
		for _, set := range sets {
			fmt.Fprintln(&buf, "  "+f.name+" "+strings.Join(set, " "))
		}
	}
	if len(f.argdocs) > 0 {
		fmt.Fprintln(&buf, "arguments and flags:")
	}
	for _, argdoc := range f.argdocs {
		if len(argdoc) == 0 {
			panic("empty argdoc")
		}
		fmt.Fprintf(&buf, "  %s: %s\n", argdoc[0], strings.Join(argdoc[1:], " "))
	}
	f.PrintDefaults()
	if len(f.docs) > 0 {
		fmt.Fprintln(&buf)
		fmt.Fprintln(&buf, strings.Join(f.docs, "\n"))
	}
	doc.ToText(w, buf.String(), f.docopt.Indent, f.docopt.PreIndent, f.docopt.Width)
}

func (f *CmdFlags) SetOutput(w io.Writer) {
	f.w = w
	//f.FlagSet.SetOutput(w)
}

func (f *CmdFlags) Parse(args *[]string) (err error) {
	if args == nil {
		args = &f.args
	}
	return f.FlagSet.Parse(*args)
}
