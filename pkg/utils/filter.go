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

func Filter[S ~[]E, E any](x S, pred func(e E) bool) S {
	var results S
	for _, e := range x {
		if pred(e) {
			results = append(results, e)
		}
	}
	return results
}

func Map[S ~[]T, T any, U any](x S, pred func(e T) U) []U {
	var results []U
	for _, e := range x {
		results = append(results, pred(e))
	}
	return results
}
