package routing

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestSearchAllKeepsSuccessfulResultsAndSkipsEmptyOrFailedProviders(t *testing.T) {
	providers := []string{"A", "B", "C"}
	got := SearchAll(context.Background(), providers, func(_ context.Context, provider string) ([]string, error) {
		switch provider {
		case "A":
			return []string{"one"}, nil
		case "B":
			return nil, nil
		default:
			return nil, errors.New("unavailable")
		}
	})
	want := []ProviderResult[string]{{Provider: "A", Items: []string{"one"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchAll() = %#v, want %#v", got, want)
	}
}

func TestSearchAllRunsConcurrentlyAndPreservesInputOrder(t *testing.T) {
	providers := []string{"A", "B", "C"}
	var active atomic.Int32
	var maxActive atomic.Int32
	got := SearchAll(context.Background(), providers, func(_ context.Context, provider string) ([]string, error) {
		current := active.Add(1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		active.Add(-1)
		return []string{provider}, nil
	})
	if maxActive.Load() < 2 {
		t.Fatalf("expected concurrent calls, maximum concurrency = %d", maxActive.Load())
	}
	want := []ProviderResult[string]{
		{Provider: "A", Items: []string{"A"}},
		{Provider: "B", Items: []string{"B"}},
		{Provider: "C", Items: []string{"C"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchAll() = %#v, want %#v", got, want)
	}
}

func TestSearchAllReturnsEmptyWhenNoProviderHasData(t *testing.T) {
	got := SearchAll(context.Background(), []string{"A", "B"}, func(_ context.Context, _ string) ([]int, error) {
		return nil, errors.New("unavailable")
	})
	if len(got) != 0 {
		t.Fatalf("SearchAll() = %#v, want empty result", got)
	}
}

func TestSearchAllEmptyProviderList(t *testing.T) {
	called := false
	got := SearchAll(context.Background(), nil, func(context.Context, string) ([]int, error) {
		called = true
		return nil, nil
	})
	if called || len(got) != 0 {
		t.Fatalf("SearchAll() = %#v, called = %v, want empty result and no calls", got, called)
	}
}
