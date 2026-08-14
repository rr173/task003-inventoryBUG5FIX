// Package inventory 实现库存商品的领域模型与内存存储。
//
// 核心不变量：商品库存始终为非负整数。出库数量超过当前库存时被拒绝，
// 已停售的商品拒绝任何入库或出库操作。
package inventory

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StatusActive       = "active"
	StatusDiscontinued  = "discontinued"
	StatusOutOfStock    = "out_of_stock"
)

var (
	ErrNotFound          = errors.New("商品不存在")
	ErrEmptySKU          = errors.New("商品 SKU 不能为空")
	ErrDuplicateSKU      = errors.New("商品 SKU 已存在")
	ErrEmptyName         = errors.New("商品名称不能为空")
	ErrInvalidStock      = errors.New("初始库存必须为零或正数")
	ErrInvalidAmount     = errors.New("入库或出库数量必须是正数")
	ErrInsufficientStock = errors.New("库存不足，无法出库")
	ErrInvalidThreshold  = errors.New("库存预警阈值必须是正数")
	ErrDiscontinued      = errors.New("商品已停售，不能再进行该操作")
)

// Product 表示一个商品。Status 与 LowStock 由库存与阈值派生，不作为持久字段。
type Product struct {
	SKU            string     `json:"sku"`
	Name           string     `json:"name"`
	Stock          int64      `json:"stock"`
	Status         string     `json:"status"`
	Threshold      int64      `json:"threshold"`
	LowStock       bool       `json:"low_stock"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastInAt       *time.Time `json:"last_in_at,omitempty"`
	LastOutAt      *time.Time `json:"last_out_at,omitempty"`
	DiscontinuedAt *time.Time `json:"discontinued_at,omitempty"`
}

// CreateInput 用于录入新商品。
type CreateInput struct {
	SKU   string `json:"sku"`
	Name  string `json:"name"`
	Stock int64  `json:"stock"`
}

// AmountInput 用于入库/出库的变动数量。
type AmountInput struct {
	Amount int64 `json:"amount"`
}

// ThresholdInput 用于设置库存预警阈值。
type ThresholdInput struct {
	Threshold int64 `json:"threshold"`
}

// Store 是并发安全的商品内存存储。
type Store struct {
	mu       sync.RWMutex
	products map[string]*Product
}

// New 创建一个空存储。
func New() *Store {
	return &Store{products: make(map[string]*Product)}
}

func trim(s string) string { return strings.TrimSpace(s) }

// lowStockOf 判断商品是否处于低库存：阈值已设置且库存不超过阈值。
func lowStockOf(p *Product) bool {
	return p.Threshold > 0 && p.Stock <= p.Threshold
}

// statusOf 依据停售状态与库存派生商品状态。
func statusOf(p *Product) string {
	if p.DiscontinuedAt != nil {
		return StatusDiscontinued
	}
	if p.Stock > 0 {
		return StatusActive
	}
	return StatusOutOfStock
}

// view 返回商品的只读副本，并派生状态与低库存标记。
func (p *Product) view() *Product {
	c := *p
	c.LowStock = lowStockOf(&c)
	c.Status = statusOf(&c)
	return &c
}

// Create 录入一个新商品，校验 SKU、名称与初始库存，并去除首尾空白。
func (s *Store) Create(in CreateInput, now time.Time) (*Product, error) {
	in.SKU = trim(in.SKU)
	if in.SKU == "" {
		return nil, ErrEmptySKU
	}
	in.Name = trim(in.Name)
	if in.Name == "" {
		return nil, ErrEmptyName
	}
	if in.Stock < 0 {
		return nil, ErrInvalidStock
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.products[in.SKU]; exists {
		return nil, ErrDuplicateSKU
	}
	p := &Product{
		SKU:       in.SKU,
		Name:      in.Name,
		Stock:     in.Stock,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.products[in.SKU] = p
	return p.view(), nil
}

// StockIn 对指定商品执行入库，库存增加并记录入库时间。
func (s *Store) StockIn(sku string, in AmountInput, now time.Time) (*Product, error) {
	if in.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[sku]
	if !ok {
		return nil, ErrNotFound
	}
	if p.DiscontinuedAt != nil {
		return nil, ErrDiscontinued
	}
	p.Stock += in.Amount
	p.LastInAt = &now
	p.UpdatedAt = now
	return p.view(), nil
}

// StockOut 对指定商品执行出库，库存减少并记录出库时间；变动数量超过当前库存则拒绝。
func (s *Store) StockOut(sku string, in AmountInput, now time.Time) (*Product, error) {
	if in.Amount <= 0 {
		return nil, ErrInvalidAmount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[sku]
	if !ok {
		return nil, ErrNotFound
	}
	if p.DiscontinuedAt != nil {
		return nil, ErrDiscontinued
	}
	if in.Amount > p.Stock {
		return nil, ErrInsufficientStock
	}
	p.Stock -= in.Amount
	p.LastOutAt = &now
	p.UpdatedAt = now
	return p.view(), nil
}

// SetThreshold 设置商品的库存预警阈值，阈值必须为正数。
func (s *Store) SetThreshold(sku string, in ThresholdInput, now time.Time) (*Product, error) {
	if in.Threshold <= 0 {
		return nil, ErrInvalidThreshold
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[sku]
	if !ok {
		return nil, ErrNotFound
	}
	p.Threshold = in.Threshold
	p.UpdatedAt = now
	return p.view(), nil
}

// Discontinue 停售商品并记录停售时间；已停售商品不能再次停售。
func (s *Store) Discontinue(sku string, now time.Time) (*Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[sku]
	if !ok {
		return nil, ErrNotFound
	}
	if p.DiscontinuedAt != nil {
		return nil, ErrDiscontinued
	}
	p.DiscontinuedAt = &now
	p.UpdatedAt = now
	return p.view(), nil
}

// Get 返回单个商品的只读视图。
func (s *Store) Get(sku string) (*Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.products[sku]
	if !ok {
		return nil, ErrNotFound
	}
	return p.view(), nil
}

// List 返回全部商品：低库存商品优先，组内按创建时间升序，同时间按 SKU 升序兜底。
func (s *Store) List() []*Product {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Product, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := lowStockOf(out[i]), lowStockOf(out[j])
		if li != lj {
			return li // 低库存优先
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt) // 创建时间升序
		}
		return out[i].SKU < out[j].SKU
	})
	views := make([]*Product, len(out))
	for i, p := range out {
		views[i] = p.view()
	}
	return views
}

// Delete 删除指定商品。
func (s *Store) Delete(sku string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.products[sku]; !ok {
		return ErrNotFound
	}
	delete(s.products, sku)
	return nil
}
