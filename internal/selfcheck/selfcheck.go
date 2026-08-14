package selfcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"task003-inventory/internal/httpapi"
	"task003-inventory/internal/inventory"
)

type clock struct {
	mu sync.RWMutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.RLock(); defer c.mu.RUnlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.t = t }

func Run() int {
	passed, failed := 0, 0
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed++
			fmt.Printf("FAIL %-36s %v\n", name, err)
		} else {
			passed++
			fmt.Printf("PASS %s\n", name)
		}
	}

	clk := &clock{t: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)}
	api := httpapi.NewWithClock(clk.now)
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()

	post := func(path, body string) (*http.Response, []byte, error) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, data, readErr
	}

	createBody := func(sku, name string, stock int) string {
		type req struct {
			SKU   string `json:"sku"`
			Name  string `json:"name"`
			Stock int    `json:"stock"`
		}
		b, _ := json.Marshal(req{SKU: sku, Name: name, Stock: stock})
		return string(b)
	}
	amountBody := func(amount int) string {
		b, _ := json.Marshal(map[string]int{"amount": amount})
		return string(b)
	}
	thresholdBody := func(threshold int) string {
		b, _ := json.Marshal(map[string]int{"threshold": threshold})
		return string(b)
	}

	check("健康检查", func() error {
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	const sku = "SKU-001"
	var createdAt time.Time
	check("录入商品返回在售状态", func() error {
		resp, body, err := post("/api/products", createBody(sku, "商品A", 10))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.SKU != sku || out.Product.Stock != 10 ||
			out.Product.Status != inventory.StatusActive || out.Product.LowStock {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		createdAt = out.Product.CreatedAt
		return nil
	})

	check("重复 SKU 被拒绝", func() error {
		resp, _, err := post("/api/products", createBody(sku, "重复", 1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("空 SKU 被拒绝", func() error {
		resp, _, err := post("/api/products", createBody("  ", "x", 1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("空名称被拒绝", func() error {
		resp, _, err := post("/api/products", createBody("SKU-EMPTY-NAME", "  ", 1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("非法初始库存被拒绝", func() error {
		resp, _, err := post("/api/products", createBody("SKU-BAD-STOCK", "x", -1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	clk.set(clk.now().Add(time.Hour))

	check("入库增加库存并记录时间", func() error {
		resp, body, err := post("/api/products/"+sku+"/stock-in", amountBody(5))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.Stock != 15 || out.Product.LastInAt == nil || out.Product.LastInAt.Equal(createdAt) {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		return nil
	})

	check("非法入库数量被拒绝", func() error {
		resp, _, err := post("/api/products/"+sku+"/stock-in", amountBody(0))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	clk.set(clk.now().Add(time.Hour))

	check("出库减少库存并记录时间", func() error {
		resp, body, err := post("/api/products/"+sku+"/stock-out", amountBody(3))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.Stock != 12 || out.Product.LastOutAt == nil {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		return nil
	})

	check("库存不足的出库被拒绝", func() error {
		resp, _, err := post("/api/products/"+sku+"/stock-out", amountBody(100))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("非法出库数量被拒绝", func() error {
		resp, _, err := post("/api/products/"+sku+"/stock-out", amountBody(-1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("不存在商品入库返回 404", func() error {
		resp, _, err := post("/api/products/NOPE/stock-in", amountBody(1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("设置预警阈值并标记低库存", func() error {
		resp, body, err := post("/api/products/"+sku+"/threshold", thresholdBody(20))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.Threshold != 20 || !out.Product.LowStock {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		return nil
	})

	check("非法阈值被拒绝", func() error {
		resp, _, err := post("/api/products/"+sku+"/threshold", thresholdBody(0))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("查询单个商品信息", func() error {
		resp, err := http.Get(srv.URL + "/api/products/" + sku)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.Stock != 12 || out.Product.Threshold != 20 || !out.Product.LowStock {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		return nil
	})

	check("不存在商品查询返回 404", func() error {
		resp, err := http.Get(srv.URL + "/api/products/NOPE")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	clk.set(clk.now().Add(time.Hour))

	check("停售商品并记录时间", func() error {
		resp, body, err := post("/api/products/"+sku+"/discontinue", "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Product inventory.Product `json:"product"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return err
		}
		if out.Product.Status != inventory.StatusDiscontinued || out.Product.DiscontinuedAt == nil {
			return fmt.Errorf("unexpected product: %+v", out.Product)
		}
		return nil
	})

	check("停售商品不能再入库", func() error {
		resp, _, err := post("/api/products/"+sku+"/stock-in", amountBody(1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("停售商品不能再出库", func() error {
		resp, _, err := post("/api/products/"+sku+"/stock-out", amountBody(1))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("不能再次停售", func() error {
		resp, _, err := post("/api/products/"+sku+"/discontinue", "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("非法 JSON 被拒绝", func() error {
		resp, _, err := post("/api/products", "not-json")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("多段 JSON 被拒绝", func() error {
		resp, _, err := post("/api/products", createBody("SKU-MULTI", "x", 1)+" {}")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	// 列表排序：在独立的干净服务上验证，避免被前面流程遗留的商品干扰。
	// 用受控时钟创建四个商品，低库存优先、组内按创建时间升序稳定排序。
	listClk := &clock{t: time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC)}
	listAPI := httpapi.NewWithClock(listClk.now)
	listSrv := httptest.NewServer(listAPI.Handler())
	defer listSrv.Close()
	listPost := func(path, body string) error {
		req, err := http.NewRequest(http.MethodPost, listSrv.URL+path, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
	must := func(err error) {
		if err != nil {
			fmt.Println("列表前置失败:", err)
			failed++
		}
	}
	must(listPost("/api/products", createBody("LOW1", "低库存1", 2)))
	listClk.set(listClk.now().Add(time.Hour))
	must(listPost("/api/products", createBody("NORM1", "普通1", 100)))
	listClk.set(listClk.now().Add(time.Hour))
	must(listPost("/api/products", createBody("LOW2", "低库存2", 1)))
	listClk.set(listClk.now().Add(time.Hour))
	must(listPost("/api/products", createBody("NORM2", "普通2", 50)))
	must(listPost("/api/products/LOW1/threshold", thresholdBody(10)))
	must(listPost("/api/products/LOW2/threshold", thresholdBody(5)))

	check("列表优先低库存并按创建时间稳定排序", func() error {
		resp, err := http.Get(listSrv.URL + "/api/products")
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var list struct {
			Products []inventory.Product `json:"products"`
			Total    int                 `json:"total"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return err
		}
		if list.Total != 4 || len(list.Products) != 4 {
			return fmt.Errorf("total=%d len=%d", list.Total, len(list.Products))
		}
		want := []string{"LOW1", "LOW2", "NORM1", "NORM2"}
		got := make([]string, 0, len(list.Products))
		for _, p := range list.Products {
			got = append(got, p.SKU)
		}
		for i, w := range want {
			if got[i] != w {
				return fmt.Errorf("order=%v want=%v", got, want)
			}
		}
		// 校验低库存标记只落在 LOW1、LOW2 上。
		if !list.Products[0].LowStock || !list.Products[1].LowStock {
			return fmt.Errorf("low_stock flags wrong: %+v", list.Products)
		}
		if list.Products[2].LowStock || list.Products[3].LowStock {
			return fmt.Errorf("normal group should not be low_stock: %+v", list.Products)
		}
		return nil
	})

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}
