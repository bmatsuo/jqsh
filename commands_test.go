package main

import (
	"bytes"
	"flag"
	"testing"
)

func testFlags(name string, args []string) (*CmdFlags, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	flags := Flags(name, args)
	flags.SetOutput(buf)
	return flags, buf
}

func TestCmdFlags(t *testing.T) {
	// parses good flags correctly
	rawargs := []string{"-a", "b", "-c", "d"}
	flags, _ := testFlags("test", rawargs)
	a := flags.String("a", "", "a string")
	c := flags.Bool("c", false, "a bool")
	err := flags.Parse(nil)
	if err != nil {
		t.Fatalf("parsing flags: %v", err)
	}
	if *a != "b" {
		t.Fatal("bad -a flag: %q", *a)
	}
	if !*c {
		t.Fatal("bad -c flag: %b", *c)
	}
	args := flags.Args()
	if len(args) != 1 || args[0] != "d" {
		t.Fatalf("unexpected args: %q", args)
	}

	// bails out on bad flags
	flags, _ = testFlags("test", rawargs) // TODO test that there is some output ("usage:")
	err = flags.Parse(nil)
	if err == flag.ErrHelp {
		t.Fatal("ErrHelp")
	}
	if err == nil {
		t.Fatal("no error")
	}

	// returns ErrHelp when -h is given
	flags, _ = testFlags("test", []string{"-a", "-h"}) // TODO test help output in another test
	flags.Bool("a", false, "a bool")
	err = flags.Parse(nil)
	if err != flag.ErrHelp {
		t.Fatalf("missing flag.ErrHelp")
	}
}
