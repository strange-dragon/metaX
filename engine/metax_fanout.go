package engine

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envAggregateFanout     = "METAX_AGGREGATE_FANOUT"
	defaultAggregateFanout = 16

	// EnvSearchBudget bounds how long a category search waits for its slowest
	// provider before answering with what arrived. 0/off/none restores waiting
	// for every provider.
	EnvSearchBudget     = "METAX_SEARCH_BUDGET"
	defaultSearchBudget = 10 * time.Second
)

// fanoutLimiter bounds concurrent provider calls started by category search and
// detail aggregation. A nil limiter disables the bound.
type fanoutLimiter chan struct{}

var aggregateFanoutLimiter = newFanoutLimiter(parseFanoutLimit(os.Getenv(envAggregateFanout)))

func parseFanoutLimit(raw string) int {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return defaultAggregateFanout
	}
	switch raw {
	case "0", "off", "disabled", "none":
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return defaultAggregateFanout
	}
	return limit
}

func newFanoutLimiter(limit int) fanoutLimiter {
	if limit <= 0 {
		return nil
	}
	return make(fanoutLimiter, limit)
}

// searchBudget resolves EnvSearchBudget. A disabled budget returns 0, which
// callers read as "wait for every provider".
func searchBudget() time.Duration {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(EnvSearchBudget)))
	if raw == "" {
		return defaultSearchBudget
	}
	switch raw {
	case "0", "off", "disabled", "none":
		return 0
	}
	budget, err := time.ParseDuration(raw)
	if err != nil || budget <= 0 {
		return defaultSearchBudget
	}
	return budget
}

func (l fanoutLimiter) acquire() func() {
	if l == nil {
		return func() {}
	}
	l <- struct{}{}
	return func() { <-l }
}

// setAggregateFanoutLimit is a test hook. The production limiter is fixed at
// package initialization.
func setAggregateFanoutLimit(limit int) func() {
	previous := aggregateFanoutLimiter
	aggregateFanoutLimiter = newFanoutLimiter(limit)
	return func() { aggregateFanoutLimiter = previous }
}
