/*
 * video-listing Copyright (C) 2023 Mook
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

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
