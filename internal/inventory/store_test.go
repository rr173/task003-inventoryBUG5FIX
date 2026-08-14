package inventory

import (
	"errors"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) }

func TestCreateValidation(t *testing.T) {
	cases := []struct {
		name string
		in   CreateInput
		want error
	}{
		{"空 SKU", CreateInput{SKU: "   ", Name: "鼠标", Stock: 1}, ErrEmptySKU},
		{"空名称", CreateInput{SKU: "S1", Name: "   ", Stock: 1}, ErrEmptyName},
		{"负初始库存", CreateInput{SKU: "S1", Name: "鼠标", Stock: -1}, ErrInvalidStock},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			_, err := s.Create(c.in, fixedNow())
			if !errors.Is(err, c.want) {
				t.Fatalf("got err=%v, want %v", err, c.want)
			}
		})
	}
}

func TestCreateTrimsFields(t *testing.T) {
	s := New()
	p, err := s.Create(CreateInput{SKU: "  S1  ", Name: "  无线鼠标  ", Stock: 5}, fixedNow())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.SKU != "S1" || p.Name != "无线鼠标" {
		t.Fatalf("not trimmed: %+v", p)
	}
	if p.Stock != 5 || p.Status != StatusActive {
		t.Fatalf("unexpected state: %+v", p)
	}
	if p.LowStock {
		t.Fatalf("无阈值不应标记低库存: %+v", p)
	}
}

func TestCreateZeroStockIsOutOfStock(t *testing.T) {
	s := New()
	p, err := s.Create(CreateInput{SKU: "S1", Name: "键盘", Stock: 0}, fixedNow())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.Stock != 0 || p.Status != StatusOutOfStock {
		t.Fatalf("expected out_of_stock, got %+v", p)
	}
}

func TestDuplicateSKURejected(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 1}, fixedNow()); err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err := s.Create(CreateInput{SKU: "S1", Name: "重复", Stock: 1}, fixedNow())
	if !errors.Is(err, ErrDuplicateSKU) {
		t.Fatalf("got err=%v, want ErrDuplicateSKU", err)
	}
}

func TestStockInIncreasesStockAndRecordsTime(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	later := fixedNow().Add(time.Hour)
	p, err := s.StockIn("S1", AmountInput{Amount: 3}, later)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.Stock != 8 || p.Status != StatusActive {
		t.Fatalf("unexpected state: %+v", p)
	}
	if p.LastInAt == nil || !p.LastInAt.Equal(later) {
		t.Fatalf("last_in_at not recorded: %+v", p)
	}
	if !p.UpdatedAt.Equal(later) {
		t.Fatalf("updated_at not recorded: got %v want %v", p.UpdatedAt, later)
	}
}

func TestStockInNonPositiveRejected(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	for _, amt := range []int64{0, -2} {
		if _, err := s.StockIn("S1", AmountInput{Amount: amt}, fixedNow()); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("amount=%d got err=%v, want ErrInvalidAmount", amt, err)
		}
	}
}

func TestStockOutDecreasesStockAndRecordsTime(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 10}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	later := fixedNow().Add(time.Hour)
	p, err := s.StockOut("S1", AmountInput{Amount: 4}, later)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.Stock != 6 || p.Status != StatusActive {
		t.Fatalf("unexpected state: %+v", p)
	}
	if p.LastOutAt == nil || !p.LastOutAt.Equal(later) {
		t.Fatalf("last_out_at not recorded: %+v", p)
	}
}

func TestStockOutToZeroMarksOutOfStock(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 3}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	p, err := s.StockOut("S1", AmountInput{Amount: 3}, fixedNow())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.Stock != 0 || p.Status != StatusOutOfStock {
		t.Fatalf("expected out_of_stock at zero, got %+v", p)
	}
}

func TestStockOutExceedingStockRejected(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 2}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	_, err := s.StockOut("S1", AmountInput{Amount: 3}, fixedNow())
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("got err=%v, want ErrInsufficientStock", err)
	}
}

func TestStockOutOnZeroStockRejected(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 0}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	_, err := s.StockOut("S1", AmountInput{Amount: 1}, fixedNow())
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("got err=%v, want ErrInsufficientStock", err)
	}
}

func TestSetThresholdAndLowStock(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	p, err := s.SetThreshold("S1", ThresholdInput{Threshold: 10}, fixedNow())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if p.Threshold != 10 || !p.LowStock {
		t.Fatalf("expected low stock: %+v", p)
	}
	// after restocking above threshold, low_stock should clear.
	p, err = s.StockIn("S1", AmountInput{Amount: 6}, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	if p.Stock != 11 || p.LowStock {
		t.Fatalf("should not be low stock: %+v", p)
	}
}

func TestSetThresholdInvalidRejected(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	for _, th := range []int64{0, -1} {
		if _, err := s.SetThreshold("S1", ThresholdInput{Threshold: th}, fixedNow()); !errors.Is(err, ErrInvalidThreshold) {
			t.Fatalf("threshold=%d got err=%v, want ErrInvalidThreshold", th, err)
		}
	}
}

func TestDiscontinueRecordsTimeAndBlocksOps(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	later := fixedNow().Add(time.Hour)
	p, err := s.Discontinue("S1", later)
	if err != nil {
		t.Fatalf("discontinue failed: %v", err)
	}
	if p.Status != StatusDiscontinued || p.DiscontinuedAt == nil || !p.DiscontinuedAt.Equal(later) {
		t.Fatalf("unexpected state: %+v", p)
	}
	// discontinued product rejects stock-in/out.
	if _, err := s.StockIn("S1", AmountInput{Amount: 1}, fixedNow()); !errors.Is(err, ErrDiscontinued) {
		t.Fatalf("stock-in on discontinued got err=%v, want ErrDiscontinued", err)
	}
	if _, err := s.StockOut("S1", AmountInput{Amount: 1}, fixedNow()); !errors.Is(err, ErrDiscontinued) {
		t.Fatalf("stock-out on discontinued got err=%v, want ErrDiscontinued", err)
	}
	// re-discontinue is rejected.
	if _, err := s.Discontinue("S1", fixedNow()); !errors.Is(err, ErrDiscontinued) {
		t.Fatalf("re-discontinue got err=%v, want ErrDiscontinued", err)
	}
}

func TestOperationsOnMissingProduct(t *testing.T) {
	s := New()
	for _, fn := range []struct {
		name string
		call func() error
	}{
		{"StockIn", func() error { _, e := s.StockIn("NOPE", AmountInput{Amount: 1}, fixedNow()); return e }},
		{"StockOut", func() error { _, e := s.StockOut("NOPE", AmountInput{Amount: 1}, fixedNow()); return e }},
		{"SetThreshold", func() error { _, e := s.SetThreshold("NOPE", ThresholdInput{Threshold: 1}, fixedNow()); return e }},
		{"Discontinue", func() error { _, e := s.Discontinue("NOPE", fixedNow()); return e }},
		{"Get", func() error { _, e := s.Get("NOPE"); return e }},
		{"Delete", func() error { return s.Delete("NOPE") }},
	} {
		t.Run(fn.name, func(t *testing.T) {
			if !errors.Is(fn.call(), ErrNotFound) {
				t.Fatalf("expected ErrNotFound for %s", fn.name)
			}
		})
	}
}

func TestDeleteRemovesProduct(t *testing.T) {
	s := New()
	if _, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 1}, fixedNow()); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("S1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := s.Get("S1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete, Get got err=%v, want ErrNotFound", err)
	}
	if _, err := s.StockIn("S1", AmountInput{Amount: 1}, fixedNow()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stock-in on deleted got err=%v, want ErrNotFound", err)
	}
}

// List order: low-stock first, then by created_at asc, SKU asc tiebreak.
func TestListLowStockFirstThenCreatedAsc(t *testing.T) {
	s := New()
	t0 := fixedNow()
	// LOW1: threshold=10, stock=2, low-stock, created at t0
	if _, err := s.Create(CreateInput{SKU: "LOW1", Name: "低1", Stock: 2}, t0); err != nil {
		t.Fatal(err)
	}
	// NORM1: no threshold, stock=100, created at t0+1h
	if _, err := s.Create(CreateInput{SKU: "NORM1", Name: "普1", Stock: 100}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// LOW2: threshold=5, stock=1, low-stock, created at t0+2h
	if _, err := s.Create(CreateInput{SKU: "LOW2", Name: "低2", Stock: 1}, t0.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	// NORM2: no threshold, stock=50, created at t0+3h
	if _, err := s.Create(CreateInput{SKU: "NORM2", Name: "普2", Stock: 50}, t0.Add(3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetThreshold("LOW1", ThresholdInput{Threshold: 10}, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetThreshold("LOW2", ThresholdInput{Threshold: 5}, t0); err != nil {
		t.Fatal(err)
	}

	got := s.List()
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	order := []string{got[0].SKU, got[1].SKU, got[2].SKU, got[3].SKU}
	// low-stock group by created_at asc: LOW1 before LOW2; normal group: NORM1 before NORM2.
	want := []string{"LOW1", "LOW2", "NORM1", "NORM2"}
	for i, w := range want {
		if order[i] != w {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
	if !got[0].LowStock || !got[1].LowStock || got[2].LowStock || got[3].LowStock {
		t.Fatalf("low_stock flags wrong: %+v", got)
	}
}

func TestListStableOnEqualCreatedAt(t *testing.T) {
	s := New()
	t0 := fixedNow()
	for _, sku := range []string{"Z", "A", "M"} {
		if _, err := s.Create(CreateInput{SKU: sku, Name: "同时间", Stock: 1}, t0); err != nil {
			t.Fatal(err)
		}
	}
	got := s.List()
	order := []string{got[0].SKU, got[1].SKU, got[2].SKU}
	want := []string{"A", "M", "Z"} // 同时间按 SKU 升序兜底
	for i, w := range want {
		if order[i] != w {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}

func TestViewIsolatesMutation(t *testing.T) {
	s := New()
	p, err := s.Create(CreateInput{SKU: "S1", Name: "鼠标", Stock: 5}, fixedNow())
	if err != nil {
		t.Fatal(err)
	}
	p.Stock = 9999
	p.Status = "tampered"
	again, err := s.Get("S1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Stock != 5 || again.Status != StatusActive {
		t.Fatalf("internal state leaked via returned pointer: %+v", again)
	}
}
