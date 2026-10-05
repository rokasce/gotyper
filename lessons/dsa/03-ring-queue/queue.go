package main

type Queue[T any] struct {
	items []T
	head  int
	count int
}

func (q *Queue[T]) Enqueue(v T) {
	if q.count == len(q.items) {
		q.grow()
	}
	q.items[(q.head+q.count)%len(q.items)] = v
	q.count++
}

func (q *Queue[T]) Dequeue() (T, bool) {
	var zero T
	if q.count == 0 {
		return zero, false
	}
	v := q.items[q.head]
	q.items[q.head] = zero
	q.head = (q.head + 1) % len(q.items)
	q.count--
	return v, true
}

func (q *Queue[T]) Len() int {
	return q.count
}

func (q *Queue[T]) grow() {
	items := make([]T, max(4, 2*len(q.items)))
	for i := range q.count {
		items[i] = q.items[(q.head+i)%len(q.items)]
	}
	q.items = items
	q.head = 0
}
