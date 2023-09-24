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
