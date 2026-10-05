package main

import (
	"runtime"
	"testing"
	"time"
)

func TestQueueEmpty(t *testing.T) {
	var q Queue[int]
	if n := q.Len(); n != 0 {
		t.Fatalf("Len() of the zero Queue = %d, want 0", n)
	}
	if v, ok := q.Dequeue(); ok || v != 0 {
		t.Fatalf("Dequeue() on an empty Queue = %d, %v; want 0, false", v, ok)
	}
}

// TestQueueFirstInFirstOut enqueues each list, then dequeues it: the values
// must come out in the order they went in, and the queue must then be empty.
func TestQueueFirstInFirstOut(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"one element", 1},
		{"two elements", 2},
		{"several growths", 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var q Queue[int]
			for i := range tt.n {
				q.Enqueue(i)
				if n := q.Len(); n != i+1 {
					t.Fatalf("Len() after %d Enqueues = %d", i+1, n)
				}
			}
			for i := range tt.n {
				if got, ok := q.Dequeue(); !ok || got != i {
					t.Fatalf("Dequeue() = %d, %v; want %d, true (first in, first out)", got, ok, i)
				}
				if n := q.Len(); n != tt.n-i-1 {
					t.Fatalf("Len() after Dequeue() = %d, want %d", n, tt.n-i-1)
				}
			}
			if v, ok := q.Dequeue(); ok {
				t.Fatalf("Dequeue() on the emptied Queue = %d, true; want false", v)
			}
		})
	}
}

// TestQueueWrapAroundGrowth keeps a few values queued while many pass
// through, so the ring's start moves round and round, and it grows while the
// values wrap past the end of the slice. Growing must keep them in order: a
// queue that only extends its slice leaves the wrapped values in the wrong
// place. A plain slice records what the queue should hold.
func TestQueueWrapAroundGrowth(t *testing.T) {
	var q Queue[int]
	var want []int
	next := 0
	for round := 1; round <= 40; round++ {
		// Enqueue round values and dequeue round-1, so the queue holds one
		// more value after each round and grows at every size.
		for range round {
			q.Enqueue(next)
			want = append(want, next)
			next++
		}
		for range round - 1 {
			got, ok := q.Dequeue()
			if !ok || got != want[0] {
				t.Fatalf("round %d: Dequeue() = %d, %v; want %d, true", round, got, ok, want[0])
			}
			want = want[1:]
		}
		if n := q.Len(); n != len(want) {
			t.Fatalf("round %d: Len() = %d, want %d", round, n, len(want))
		}
	}
	for len(want) > 0 {
		got, ok := q.Dequeue()
		if !ok || got != want[0] {
			t.Fatalf("draining: Dequeue() = %d, %v; want %d, true", got, ok, want[0])
		}
		want = want[1:]
	}
	if q.Len() != 0 {
		t.Fatalf("Len() after draining = %d, want 0", q.Len())
	}
}

// TestQueueRefill empties the queue and fills it again, so Enqueue must work
// with the ring's start anywhere in the slice.
func TestQueueRefill(t *testing.T) {
	var q Queue[string]
	for pass := range 5 {
		in := []string{"a", "b", "c", "d", "e", "f", "g"}[:pass+3]
		for _, v := range in {
			q.Enqueue(v)
		}
		for _, v := range in {
			if got, ok := q.Dequeue(); !ok || got != v {
				t.Fatalf("pass %d: Dequeue() = %q, %v; want %q, true", pass, got, ok, v)
			}
		}
		if v, ok := q.Dequeue(); ok {
			t.Fatalf("pass %d: Dequeue() on the emptied Queue = %q, true; want false", pass, v)
		}
	}
}

type blob struct{ data [1024]byte }

// TestQueueDequeueReleasesValue dequeues a pointer and drops it while the
// queue lives on. If Dequeue leaves the pointer in its slot, the garbage
// collector can still reach it and never frees it.
func TestQueueDequeueReleasesValue(t *testing.T) {
	var q Queue[*blob]
	dequeued := &blob{}
	freed := make(chan struct{})
	runtime.AddCleanup(dequeued, func(ch chan struct{}) { close(ch) }, freed)
	q.Enqueue(dequeued)
	q.Enqueue(&blob{})
	dequeued = nil
	if _, ok := q.Dequeue(); !ok {
		t.Fatal("Dequeue() = _, false on a Queue holding two values")
	}
	if !collected(freed) {
		t.Fatal("a dequeued value was never freed: Dequeue must set the vacated slot to the zero value")
	}
	runtime.KeepAlive(&q)
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
