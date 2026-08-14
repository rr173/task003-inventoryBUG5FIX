# task003-inventory

轻量库存管理服务，使用进程内存保存数据，不依赖数据库、第三方包和外部服务。

## 本地运行

```bash
go run . server --addr :8080
go run . --smoke-test
```

主要接口：`POST /api/products`、`GET /api/products`、`GET /api/products/{sku}`、`POST /api/products/{sku}/stock-in`、`POST /api/products/{sku}/stock-out`、`POST /api/products/{sku}/threshold`、`POST /api/products/{sku}/discontinue`、`GET /healthz`。

## Docker

镜像使用国内 DaoCloud Go 1.26.3 Bookworm builder 和 Alpine 3.20 runtime；支持 `linux/amd64` 与 `linux/arm64`。
