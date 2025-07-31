package main

import (
	"fmt"
)

const (
	epochNum = 10000000
)

func main() {
	stk := make([]byte, 0)
	stk2 := make([]byte, 0)

	fmt.Println(cap(stk))

	for i := 0; i < 100; i++ {
		stk = append(stk, byte(i))
		//stk2 = append(stk2, byte(i))
		//stk2 = append(stk2, byte(i))
		//stk2 = append(stk2, byte(i))
	}

	fmt.Println(cap(stk))

	stk = stk2

	fmt.Println(cap(stk))

	stk = stk[:0]

	fmt.Println(cap(stk))
}
