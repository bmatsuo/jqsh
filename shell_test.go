package main

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func StringShellReader(lines string) *SimpleShellReader {
	r := strings.NewReader(lines)
	sh := NewShellReader(r, "")
	return sh
}

func TestShellReaderReadCommand_eof(t *testing.T) {
	sh := StringShellReader("")
	_, eof, err := sh.ReadCommand()
	if err != io.EOF {
		t.Fatal("non-eof error returned")
	}
	if !eof {
		t.Fatal("eof not returned")
	}

	sh = StringShellReader(":hello shell")
	_, eof, err = sh.ReadCommand()
	if err != nil {
		t.Fatal("error returned")
	}
	if !eof {
		t.Fatal("eof not returned")
	}
}

func TestShellReaderReadCommand_multi(t *testing.T) {
	sh := StringShellReader(":hello\n:shell\n")
	cmd, eof, err := sh.ReadCommand()
	if err != nil {
		t.Fatalf("error returned")
	}
	if eof {
		t.Fatalf("eof returned")
	}
	if !reflect.DeepEqual(cmd, []string{"hello"}) {
		t.Fatalf("unexpected command: %v", cmd)
	}

	cmd, eof, err = sh.ReadCommand()
	if err != nil {
		t.Fatalf("error returned")
	}
	if eof {
		t.Fatalf("eof returned")
	}
	if !reflect.DeepEqual(cmd, []string{"shell"}) {
		t.Fatalf("unexpected command: %v", cmd)
	}

	cmd, eof, err = sh.ReadCommand()
	if err != io.EOF {
		t.Fatalf("non-eof error returned")
	}
	if !eof {
		t.Fatalf("eof returned")
	}
	if len(cmd) != 0 {
		t.Fatalf("unexpected command: %v", cmd)
	}
}

func TestShellReaderReadCommand_single(t *testing.T) {
	cmd := func(strs ...string) []string { return strs }
	for i, test := range []struct {
		str string
		cmd []string
	}{
		{":pop", cmd("pop")},
		{":push .items .[]", cmd("push", ".items", ".[]")},
		{":push +.items | .[]", cmd("push", ".items | .[]")},
		{".items | .[]", cmd("push", ".items | .[]")},
		{"\n", cmd("write")},
	} {
		cmd, _, err := StringShellReader(test.str).ReadCommand()
		if err != nil {
			t.Errorf("command %d (%q) %v", i, test.str, err)
			continue
		}
		if !reflect.DeepEqual(cmd, test.cmd) {
			t.Errorf("command %d (%q) got %q (expect %q)", i, test.str, cmd, test.cmd)
		}
	}
}
