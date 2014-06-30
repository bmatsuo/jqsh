package main

import "testing"

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
