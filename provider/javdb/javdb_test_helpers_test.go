package javdb

import "github.com/strange-dragon/metaX/model"

func selectNumberIndexFromNumbers(numbers []string, query string) int {
	results := make([]*model.MovieSearchResult, 0, len(numbers))
	for _, number := range numbers {
		results = append(results, &model.MovieSearchResult{Number: number})
	}
	return selectNumberIndex(results, query)
}
