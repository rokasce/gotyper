package main

import (
	"runtime"
	"testing"
	"time"
)

func TestStackEmpty(t *testing.T) {
	var s Stack[int]
	if n := s.Len(); n != 0 {
		t.Fatalf("Len() of the zero Stack = %d, want 0", n)
	}
	if v, ok := s.Pop(); ok || v != 0 {
		t.Fatalf("Pop() on an empty Stack = %d, %v; want 0, false", v, ok)
	}
	if v, ok := s.Peek(); ok || v != 0 {
		t.Fatalf("Peek() on an empty Stack = %d, %v; want 0, false", v, ok)
	}
}

// TestStackLastInFirstOut pushes each list, then pops it back out: the
// values must come out in reverse order, with Peek and Len agreeing at every
// step, and the stack must be empty again at the end.
func TestStackLastInFirstOut(t *testing.T) {
	tests := []struct {
		name string
		push []string
	}{
		{"one element", []string{"a"}},
		{"two elements", []string{"a", "b"}},
		{"many elements", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}},
		{"zero values", []string{"", "", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Stack[string]
			for i, v := range tt.push {
				s.Push(v)
				if n := s.Len(); n != i+1 {
					t.Fatalf("Len() after %d pushes = %d", i+1, n)
				}
				if top, ok := s.Peek(); !ok || top != v {
					t.Fatalf("Peek() after pushing %q = %q, %v; want %q, true", v, top, ok, v)
				}
			}
			for i := len(tt.push) - 1; i >= 0; i-- {
				want := tt.push[i]
				if top, ok := s.Peek(); !ok || top != want {
					t.Fatalf("Peek() = %q, %v; want %q, true", top, ok, want)
				}
				if n := s.Len(); n != i+1 {
					t.Fatalf("Peek() changed Len() to %d, want %d", n, i+1)
				}
				if got, ok := s.Pop(); !ok || got != want {
					t.Fatalf("Pop() = %q, %v; want %q, true (last in, first out)", got, ok, want)
				}
				if n := s.Len(); n != i {
					t.Fatalf("Len() after Pop() = %d, want %d", n, i)
				}
			}
			if v, ok := s.Pop(); ok {
				t.Fatalf("Pop() on the emptied Stack = %q, true; want false", v)
			}
		})
	}
}

// TestStackPushAfterPop mixes pushes and pops, so Push must reuse the room a
// Pop left behind without bringing an old value back.
func TestStackPushAfterPop(t *testing.T) {
	var s Stack[int]
	s.Push(1)
	s.Push(2)
	s.Push(3)
	s.Pop()
	s.Pop()
	s.Push(4)
	s.Push(5)
	for _, want := range []int{5, 4, 1} {
		if got, ok := s.Pop(); !ok || got != want {
			t.Fatalf("Pop() = %d, %v; want %d, true", got, ok, want)
		}
	}
	if s.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", s.Len())
	}
}

type blob struct{ data [1024]byte }

// TestStackPopReleasesValue pops a pointer and drops it while the stack
// lives on. If Pop leaves the pointer in the slice's vacated slot, the
// garbage collector can still reach it and never frees it.
func TestStackPopReleasesValue(t *testing.T) {
	var s Stack[*blob]
	s.Push(&blob{})
	popped := &blob{}
	freed := make(chan struct{})
	runtime.AddCleanup(popped, func(ch chan struct{}) { close(ch) }, freed)
	s.Push(popped)
	popped = nil
	if _, ok := s.Pop(); !ok {
		t.Fatal("Pop() = _, false on a Stack holding two values")
	}
	if !collected(freed) {
		t.Fatal("a popped value was never freed: Pop must set the vacated slot to the zero value")
	}
	runtime.KeepAlive(&s)
}

// collected runs the garbage collector until freed is closed, and reports
// whether it was.
func collected(freed chan struct{}) bool {
	for range 20 {
		runtime.GC()
		select {
		case <-freed:
			return true
		case <-time.After(10 * time.Millisecond):
		}
	}
	return false
}
