package main

import "fmt"

func main() {
	p := point{X: 1, Y: 2}
	fmt.Println(p)
}

func (p point) String() string {
	return fmt.Sprintf("(%d, %d)", p.X, p.Y)
}

type point struct {
	X, Y int
}
