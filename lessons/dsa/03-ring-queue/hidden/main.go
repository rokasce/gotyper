package main

import "fmt"

func main() {
	var q Queue[string]
	for _, w := range []string{"one", "two", "three"} {
		q.Enqueue(w)
	}
	for q.Len() > 0 {
		w, _ := q.Dequeue()
		fmt.Println(w)
	}
}
