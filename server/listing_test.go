package server

import (
	"strings"
	"testing"
)

func TestCommonLength(t *testing.T) {
	testCases := []struct {
		inputs []string
		prefix int
		suffix int
	}{
		{[]string{}, 0, 0},
		{[]string{"single element"}, 0, 0},
		{[]string{"nothing", "common"}, 0, 0},
		{[]string{"prefix matches", "prefix is the same"}, 7, 0},
		{[]string{"common suffix", "shared suffix"}, 0, 7},
		{[]string{"prefix plus suffix", "prefix and suffix"}, 7, 7},
		{[]string{"same string", "same string"}, 0, 0},
	}

	for _, testCase := range testCases {
		t.Run(strings.Join(testCase.inputs, "/"), func(t *testing.T) {
			t.Parallel()
			prefixActual := commonLength(testCase.inputs, true)
			if testCase.prefix != prefixActual {
				t.Error(testCase.inputs, testCase.prefix, prefixActual)
			}
			suffixActual := commonLength(testCase.inputs, false)
			if testCase.suffix != suffixActual {
				t.Error(testCase.inputs, testCase.suffix, suffixActual)
			}
		})
	}
}
