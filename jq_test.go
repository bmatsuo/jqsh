package main

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

type MockFilter []string

func (filter MockFilter) JQFilter() []string {
	return filter
}

func TestLocateJQ(t *testing.T) {
	jqbin, err := LocateJQ("")
	if err != nil {
		t.Fatal("unable to find jq in PATH: %v", err)
	}
	if jqbin == "" {
		t.Fatalf("no jq binary path returned")
	}
	_jqbin, err := LocateJQ(jqbin)
	if err != nil {
		t.Fatalf("unable to find jq from a concrete path: %v", err)
	}
	if jqbin != _jqbin {
		t.Fatalf("different path returned when from concrete path argument %q (got %q)", jqbin, _jqbin)
	}

	badjqs := []string{
		"/testing/no/jq/here",
		".",
		"jq.go",
		"/bin/bash",
	}
	for _, badjq := range badjqs {
		badbin, err := LocateJQ(badjq)
		if err == nil {
			t.Errorf("no error returned for jq binary path: %q", badjq)
		}
		if badbin != "" {
			t.Errorf("non-empty bin %q return for jq binary path: %q", badbin, badjq)
		}
	}
}

func TestCheckJQVersion(t *testing.T) {
	venv, err := CheckJQVersion("")
	if err != nil {
		t.Fatalf("env jq version: %v", err)
	}
	if venv == "" {
		t.Fatalf("empty env jq version string")
	}
	ispace := strings.LastIndexFunc(venv, unicode.IsSpace)
	if ispace >= 0 {
		cs := []rune(venv[ispace:])
		if len(cs) == 1 {
			t.Fatalf("jq version string has trailing space: %q", venv)
		}
	}
	t.Logf("env jq version: %q", venv)
	jqbin, err := LocateJQ("")
	if err != nil {
		t.Fatalf("locating jq: %v", err)
	}
	v, err := CheckJQVersion(jqbin)
	if err != nil {
		t.Fatalf("%s version: %v", jqbin, venv)
	}
	if v != venv {
		t.Fatalf("different version string return from concrete path argument %q (got %q; expected %q)", jqbin, v, venv)
	}
}

func TestParseJQVersion(t *testing.T) {
	for _, test := range []struct {
		in       string
		s        string
		maj, min int
		suf      string
		err      bool
	}{
		{"jq-1.4\n", "jq-1.4", 1, 4, "", false},
		{"jq version 1.3\n", "jq version 1.3", 1, 3, "", false},
	} {
		s, maj, min, suf, err := ParseJQVersion(test.in)
		if s != test.s {
			t.Errorf("%q unexpected version string: %q", test.in, s)
		}
		if maj != test.maj {
			t.Errorf("%q unexpected major version: %q", test.in, maj)
		}
		if min != test.min {
			t.Errorf("%q unexpected minor version: %q", test.in, min)
		}
		if suf != test.suf {
			t.Errorf("%q unexpected version suffix: %q", test.in, suf)
		}
		if !test.err && err != nil {
			t.Errorf("%q unexpected error: %v", test.in, err)
		}
		if test.err && err == nil {
			t.Errorf("%q expected an error", test.in, err)
		}
	}
}

func TestJoinFilter(t *testing.T) {
	filter := JoinFilter(MockFilter{"hello", "world"})
	if filter != "hello | world" {
		t.Fatalf("incorrect two-value filter: %v", filter)
	}
	filter = JoinFilter(MockFilter(nil))
	if filter != "." {
		t.Fatalf("incorrect zero-value filter: %v", filter)
	}
	filter = JoinFilter(MockFilter{"!"})
	if filter != "!" {
		t.Fatalf("incorrect one-value filter: %v", filter)
	}
}

func TestJQStack(t *testing.T) {
	s := new(JQStack)

	s.Push(MockFilter{"hello"})
	s.Push(MockFilter(nil))
	s.Push(MockFilter{"world"})
	fs := s.JQFilter()
	if !reflect.DeepEqual(fs, []string{"hello", "world"}) {
		t.Fatalf("incorrect filter stack: %v", fs)
	}

	s.Pop()
	fs = s.JQFilter()
	if !reflect.DeepEqual(fs, []string{"hello"}) {
		t.Fatalf("incorrect filter stack: %v", fs)
	}

	s.Pop()
	fs = s.JQFilter()
	if !reflect.DeepEqual(fs, []string{"hello"}) {
		t.Fatalf("incorrect filter stack: %v", fs)
	}

	s.Pop()
	fs = s.JQFilter()
	if len(fs) != 0 {
		t.Fatalf("incorrect filter stack: %v", fs)
	}

	s.Pop()
	fs = s.JQFilter()
	if len(fs) != 0 {
		t.Fatalf("incorrect filter stack: %v", fs)
	}
}
