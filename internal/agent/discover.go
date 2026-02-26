package agent

import (
	"sync"

	"github.com/Eric-Song-Nop/agentstat/internal/model"
)

// ConcurrentProbe runs probe concurrently on each item and collects non-nil results.
func ConcurrentProbe[T any](items []T, probe func(T) *model.AgentSession) []model.AgentSession {
	var mu sync.Mutex
	var results []model.AgentSession
	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)
		go func(it T) {
			defer wg.Done()
			if session := probe(it); session != nil {
				mu.Lock()
				results = append(results, *session)
				mu.Unlock()
			}
		}(item)
	}

	wg.Wait()
	return results
}
