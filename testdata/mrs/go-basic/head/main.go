package main

import "fmt"

func greet(name string) string {
	return "hello " + name
}

func divide(a, b int) int {
	return a / b
}

func printAll(items []string) {
	for i := 0; i <= len(items); i++ {
		fmt.Println(items[i])
	}
}

func untouched() {}
