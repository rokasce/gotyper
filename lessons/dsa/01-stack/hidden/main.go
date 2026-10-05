package main

import "fmt"

func main() {
	var s Stack[string]
	for _, w := range []string{"one", "two", "three"} {
		s.Push(w)
	}
	for s.Len() > 0 {
		w, _ := s.Pop()
		fmt.Println(w)
	}
}
