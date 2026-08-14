package inventory

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentGet_PointerIsolation verifies that multiple concurrent Get
// calls return independent Product instances whose time pointers do not alias.
func TestConcurrentGet_PointerIsolation(t *testing.T) {
	s := New()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	if _, err := s.Create(CreateInput{SKU: "CG", Name: "concurrent", Stock: 5}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StockIn("CG", AmountInput{Amount: 1}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]*Product, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p, _ := s.Get("CG")
			results[idx] = p
		}(i)
	}
	wg.Wait()

	if results[0].LastInAt == nil || results[1].LastInAt == nil {
		t.Fatal("LastInAt should not be nil after stock-in")
	}

	// mutate one result's pointer value
	*results[0].LastInAt = time.Time{}

	// other results must not be affected
	if results[1].LastInAt.IsZero() {
		t.Fatal("concurrent Get() returns Products sharing the same *time.Time pointer - mutation of one affects another")
	}
}
