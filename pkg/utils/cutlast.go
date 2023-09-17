package utils

import "fmt"

func CutLast[S ~[]E, E comparable](s S, sep E) (S, S) {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == sep {
			return s[:i], s[i+1:]
		}
	}
	return []E{}, s
}

func CutLastString(s, sep string) (string, string) {
	if len(sep) != 1 {
		panic(fmt.Sprintf("Invalid separator %s", sep))
	}
	pre, post := CutLast([]byte(s), byte(sep[0]))
	return string(pre), string(post)
}
