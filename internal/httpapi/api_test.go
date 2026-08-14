package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"task003-inventory/internal/inventory"
)

// testClock provides a controllable clock for tests.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newServer(t *testing.T) (*httptest.Server, *testClock) {
	t.Helper()
	clk := &testClock{t: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	api := NewWithClock(clk.now)
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, clk
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func TestAPI_CreateGetList(t *testing.T) {
	srv, _ := newServer(t)
	resp, body := do(t, srv, http.MethodPost, "/api/products", `{"sku":"S1","name":"鼠标","stock":10}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Product inventory.Product `json:"product"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Product.SKU != "S1" || out.Product.Stock != 10 || out.Product.Status != inventory.StatusActive {
		t.Fatalf("unexpected: %+v", out.Product)
	}

	resp, body = do(t, srv, http.MethodGet, "/api/products/S1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", resp.StatusCode)
	}

	resp, body = do(t, srv, http.MethodGet, "/api/products", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var list struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 {
		t.Fatalf("total=%d", list.Total)
	}
}

func TestAPI_ValidationErrors(t *testing.T) {
	srv, _ := newServer(t)
	// create SKU=D first for duplicate check below.
	if resp, _ := do(t, srv, http.MethodPost, "/api/products", `{"sku":"D","name":"x","stock":1}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup create status=%d", resp.StatusCode)
	}
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"duplicate", `{"sku":"D","name":"x","stock":1}`, http.StatusConflict},
		{"empty sku", `{"sku":"","name":"x","stock":1}`, http.StatusBadRequest},
		{"empty name", `{"sku":"E","name":"","stock":1}`, http.StatusBadRequest},
		{"neg stock", `{"sku":"N","name":"x","stock":-1}`, http.StatusBadRequest},
		{"bad json", `not-json`, http.StatusBadRequest},
		{"multi json", `{"sku":"M","name":"x","stock":1} {}`, http.StatusBadRequest},
		{"unknown field", `{"sku":"U","name":"x","stock":1,"x":1}`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, _ := do(t, srv, http.MethodPost, "/api/products", c.body)
			if resp.StatusCode != c.status {
				t.Fatalf("got %d, want %d", resp.StatusCode, c.status)
			}
		})
	}
}

func TestAPI_StockFlow(t *testing.T) {
	srv, _ := newServer(t)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"S","name":"x","stock":10}`)

	resp, body := do(t, srv, http.MethodPost, "/api/products/S/stock-in", `{"amount":5}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stock-in status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Product inventory.Product `json:"product"`
	}
	json.Unmarshal(body, &out)
	if out.Product.Stock != 15 || out.Product.Status != inventory.StatusActive || out.Product.LastInAt == nil {
		t.Fatalf("stock=%+v", out.Product)
	}

	// stock-out to zero should mark out_of_stock.
	resp, body = do(t, srv, http.MethodPost, "/api/products/S/stock-out", `{"amount":15}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stock-out status=%d body=%s", resp.StatusCode, body)
	}
	json.Unmarshal(body, &out)
	if out.Product.Stock != 0 || out.Product.Status != inventory.StatusOutOfStock || out.Product.LastOutAt == nil {
		t.Fatalf("expected out_of_stock: %+v", out.Product)
	}

	// out-of-stock product rejects further stock-out.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/S/stock-out", `{"amount":1}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("insufficient got %d", resp.StatusCode)
	}

	// stock-out exceeding stock returns conflict.
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"T","name":"x","stock":2}`)
	resp, _ = do(t, srv, http.MethodPost, "/api/products/T/stock-out", `{"amount":100}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("exceeding got %d", resp.StatusCode)
	}

	// invalid amount returns 400.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/T/stock-in", `{"amount":0}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid amount got %d", resp.StatusCode)
	}

	// stock-in on missing product returns 404.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/NOPE/stock-in", `{"amount":1}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("notfound got %d", resp.StatusCode)
	}
}

func TestAPI_ThresholdAndDiscontinue(t *testing.T) {
	srv, _ := newServer(t)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"S","name":"x","stock":5}`)

	// set threshold and verify low_stock flag.
	resp, body := do(t, srv, http.MethodPost, "/api/products/S/threshold", `{"threshold":10}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("threshold status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Product inventory.Product `json:"product"`
	}
	json.Unmarshal(body, &out)
	if out.Product.Threshold != 10 || !out.Product.LowStock {
		t.Fatalf("expected low stock: %+v", out.Product)
	}

	// invalid threshold returns 400.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/S/threshold", `{"threshold":0}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid threshold got %d", resp.StatusCode)
	}

	// discontinue the product.
	resp, body = do(t, srv, http.MethodPost, "/api/products/S/discontinue", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discontinue status=%d body=%s", resp.StatusCode, body)
	}
	json.Unmarshal(body, &out)
	if out.Product.Status != inventory.StatusDiscontinued || out.Product.DiscontinuedAt == nil {
		t.Fatalf("expected discontinued: %+v", out.Product)
	}

	// discontinued product rejects stock-in/out with 409.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/S/stock-in", `{"amount":1}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stock-in on discontinued got %d", resp.StatusCode)
	}
	resp, _ = do(t, srv, http.MethodPost, "/api/products/S/stock-out", `{"amount":1}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stock-out on discontinued got %d", resp.StatusCode)
	}
	// re-discontinue returns conflict.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/S/discontinue", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-discontinue got %d", resp.StatusCode)
	}
	// discontinue on missing product returns 404.
	resp, _ = do(t, srv, http.MethodPost, "/api/products/NOPE/discontinue", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("discontinue notfound got %d", resp.StatusCode)
	}
}

func TestAPI_ListOrder(t *testing.T) {
	srv, clk := newServer(t)
	// verify list order: low-stock first, then by created_at ascending.
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"LOW1","name":"低1","stock":2}`)
	clk.t = clk.t.Add(time.Hour)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"NORM1","name":"普1","stock":100}`)
	clk.t = clk.t.Add(time.Hour)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"LOW2","name":"低2","stock":1}`)
	clk.t = clk.t.Add(time.Hour)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"NORM2","name":"普2","stock":50}`)
	do(t, srv, http.MethodPost, "/api/products/LOW1/threshold", `{"threshold":10}`)
	do(t, srv, http.MethodPost, "/api/products/LOW2/threshold", `{"threshold":5}`)

	resp, body := do(t, srv, http.MethodGet, "/api/products", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var list struct {
		Products []inventory.Product `json:"products"`
		Total    int                 `json:"total"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 4 || len(list.Products) != 4 {
		t.Fatalf("total=%d len=%d", list.Total, len(list.Products))
	}
	got := []string{list.Products[0].SKU, list.Products[1].SKU, list.Products[2].SKU, list.Products[3].SKU}
	want := []string{"LOW1", "LOW2", "NORM1", "NORM2"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("order=%v want=%v", got, want)
		}
	}
	if !list.Products[0].LowStock || !list.Products[1].LowStock || list.Products[2].LowStock || list.Products[3].LowStock {
		t.Fatalf("low_stock flags wrong: %+v", list.Products)
	}
}

func TestAPI_Delete(t *testing.T) {
	srv, _ := newServer(t)
	do(t, srv, http.MethodPost, "/api/products", `{"sku":"S","name":"x","stock":1}`)
	resp, _ := do(t, srv, http.MethodDelete, "/api/products/S", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	resp, _ = do(t, srv, http.MethodDelete, "/api/products/S", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("re-delete status=%d", resp.StatusCode)
	}
	resp, _ = do(t, srv, http.MethodGet, "/api/products/S", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted status=%d", resp.StatusCode)
	}
}

func TestAPI_Health(t *testing.T) {
	srv, _ := newServer(t)
	resp, body := do(t, srv, http.MethodGet, "/healthz", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", resp.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
	}
	json.Unmarshal(body, &out)
	if out.Status != "ok" {
		t.Fatalf("status=%q", out.Status)
	}
}

func TestAPI_NotFound(t *testing.T) {
	srv, _ := newServer(t)
	resp, _ := do(t, srv, http.MethodGet, "/api/products/NOPE", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
