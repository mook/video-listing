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
