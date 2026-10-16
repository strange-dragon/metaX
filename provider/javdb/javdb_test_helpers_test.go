package javdb

func selectNumberIndexFromNumbers(numbers []string, query string) int {
	type item struct {
		Number string
	}
	results := make([]*item, 0, len(numbers))
	_ = results
	exact := -1
	var loose []int
	for i, number := range numbers {
		switch numberMatch(number, query) {
		case 2:
			if exact < 0 {
				exact = i
			}
		case 1:
			loose = append(loose, i)
		}
	}
	if exact >= 0 {
		return exact
	}
	if len(loose) == 1 {
		return loose[0]
	}
	return -1
}
