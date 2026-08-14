// Package httpapi 提供库存管理服务的 HTTP 接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"task003-inventory/internal/inventory"
)

// ErrBadJSON 表示请求体不是合法的单个 JSON 对象。
var ErrBadJSON = errors.New("请求体不是合法的单个 JSON 对象")

// API 暴露库存服务的 HTTP 处理器。
type API struct {
	store *inventory.Store
	now   func() time.Time
}

// New 创建使用系统时钟的 API。
func New() *API { return &API{store: inventory.New(), now: time.Now} }

// NewWithClock 创建使用自定义时钟的 API，用于自检。
func NewWithClock(now func() time.Time) *API {
	return &API{store: inventory.New(), now: now}
}

// Handler 返回路由复用器。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/products", a.create)
	mux.HandleFunc("GET /api/products", a.list)
	mux.HandleFunc("GET /api/products/{sku}", a.get)
	mux.HandleFunc("DELETE /api/products/{sku}", a.delete)
	mux.HandleFunc("POST /api/products/{sku}/stock-in", a.stockIn)
	mux.HandleFunc("POST /api/products/{sku}/stock-out", a.stockOut)
	mux.HandleFunc("POST /api/products/{sku}/threshold", a.threshold)
	mux.HandleFunc("POST /api/products/{sku}/discontinue", a.discontinue)
	return mux
}

// decodeJSON 解码单个 JSON 对象，拒绝空请求体、未知字段与多段 JSON。
func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return ErrBadJSON
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrBadJSON
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, inventory.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, inventory.ErrDuplicateSKU),
		errors.Is(err, inventory.ErrInsufficientStock),
		errors.Is(err, inventory.ErrDiscontinued):
		status = http.StatusConflict
	case errors.Is(err, inventory.ErrEmptySKU),
		errors.Is(err, inventory.ErrEmptyName),
		errors.Is(err, inventory.ErrInvalidStock),
		errors.Is(err, inventory.ErrInvalidAmount),
		errors.Is(err, inventory.ErrInvalidThreshold),
		errors.Is(err, ErrBadJSON):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": err.Error(), "status": status})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req inventory.CreateInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p, err := a.store.Create(req, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"product": p})
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	products := a.store.List()
	writeJSON(w, http.StatusOK, map[string]any{"products": products, "total": len(products)})
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	p, err := a.store.Get(r.PathValue("sku"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Delete(r.PathValue("sku")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) stockIn(w http.ResponseWriter, r *http.Request) {
	var req inventory.AmountInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p, err := a.store.StockIn(r.PathValue("sku"), req, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

func (a *API) stockOut(w http.ResponseWriter, r *http.Request) {
	var req inventory.AmountInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p, err := a.store.StockOut(r.PathValue("sku"), req, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

func (a *API) threshold(w http.ResponseWriter, r *http.Request) {
	var req inventory.ThresholdInput
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	p, err := a.store.SetThreshold(r.PathValue("sku"), req, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

// discontinue 不需要请求体。
func (a *API) discontinue(w http.ResponseWriter, r *http.Request) {
	p, err := a.store.Discontinue(r.PathValue("sku"), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}
