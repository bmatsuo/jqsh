package main

import (
	"reflect"
	"testing"
)

type MockFilter []string

func (filter MockFilter) JQFilter() []string {
	return filter
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
