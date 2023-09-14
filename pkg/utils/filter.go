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
