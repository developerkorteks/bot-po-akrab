package main

import "testing"

func TestBackendFor(t *testing.T) {
	s := &server{
		khfy: &backendClient{provider: "khfy"},
		ics:  &backendClient{provider: "ics"},
	}
	if s.backendFor("khfy") == nil || s.backendFor("ics") == nil {
		t.Fatal("expected providers to resolve")
	}
	if s.backendFor("none") != nil {
		t.Fatal("unexpected provider resolution")
	}
}
