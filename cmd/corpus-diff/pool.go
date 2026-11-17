package main

import (
	"context"
	"strings"
	"sync"
)

// forEach calls fn for 0..n-1 on up to workers goroutines, and stops early
// when ctx is done.
func forEach(ctx context.Context, n, workers int, fn func(i int)) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(max(1, workers), max(1, n)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				fn(i)
			}
		}()
	}
	for i := range n {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
}

func indent(s string) string {
	return strings.ReplaceAll(s, "\n", "\n  ")
}
