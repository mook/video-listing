package utils_test

import (
	"testing"

	"github.com/mook/video-listing/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCutLastString(t *testing.T) {
	for _, testCase := range [][3]string{
		{"", "", ""},
		{"word", "", "word"},
		{"hello/world", "hello", "world"},
		{"one/two/three", "one/two", "three"},
	} {
		testCase := testCase // capture loop variable
		t.Run(testCase[0], func(t *testing.T) {
			pre, post := utils.CutLastString(testCase[0], "/")
			assert.Equal(t, testCase[1], pre)
			assert.Equal(t, testCase[2], post)
		})
	}
}
