# httpc

[![Go Report Card](https://goreportcard.com/badge/github.com/WJQSERVER-STUDIO/httpc)](https://goreportcard.com/report/github.com/WJQSERVER-STUDIO/httpc)
[![GoDoc](https://godoc.org/github.com/WJQSERVER-STUDIO/httpc?status.svg)](https://godoc.org/github.com/WJQSERVER-STUDIO/httpc)
[![License](https://img.shields.io/badge/license-MPL--2.0%20%2F%20WJQserver--2.0-blue.svg)](LICENSE)

`httpc` 是一个基于 Go 标准库 `net/http` 构建的高级 HTTP 客户端库。它提供了链式调用、自动重试、中间件支持、响应自动解码以及详细的日志记录等功能，旨在简化日常开发中的 HTTP 请求处理。

## 特性

- **流式 API**：支持链式调用，轻松构建复杂的请求。
- **自动重试**：内置指数退避 (Exponential Backoff) 和抖动 (Jitter) 策略。
- **响应解码**：支持 JSON、XML、GOB、字符串及字节流的自动解码。
- **中间件支持**：允许在请求生命周期内注入自定义逻辑。
- **性能优化**：通过 `sync.Pool` 复用字节缓冲区，降低内存分配开销。
- **深度可调**：支持对底层 `Transport` 和 `Dialer` 的精细配置。
- **HTTP/2 & H2C**：完整支持 HTTP/1.1、HTTP/2 以及非加密的 H2C 协议。

## 安装

```bash
go get github.com/WJQSERVER-STUDIO/httpc
```

## 快速开始

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/WJQSERVER-STUDIO/httpc"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	// 创建并配置客户端
	client := httpc.New(
		httpc.WithTimeout(15 * time.Second),
		httpc.WithDumpLog(), // 启用请求日志
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user User
	// 链式调用发送 GET 请求并解码 JSON
	err := client.GET("https://api.example.com/users/1").
		WithContext(ctx).
		SetHeader("Accept", "application/json").
		AddQueryParam("details", "true").
		DecodeJSON(&user)

	if err != nil {
		if httpErr, ok := err.(*httpc.HTTPError); ok {
			log.Printf("HTTP Error: %d, Body: %s", httpErr.StatusCode, string(httpErr.Body))
		} else {
			log.Fatal(err)
		}
		return
	}

	fmt.Printf("User: %+v\n", user)
}
```

## 文档

更多详细指南请参阅以下文档：

- [客户端配置 (Client)](docs/client.md)
- [请求构建 (RequestBuilder)](docs/builder.md)
- [响应处理 (Response)](docs/response.md)
- [重试与中间件 (Retry & Middleware)](docs/retry-middleware.md)
- [底层传输与协议 (Transport & Protocols)](docs/transport.md)
- [API 参考 (API Index)](docs/api.md)

## 核心功能

### 1. 请求构建器 (RequestBuilder)

`RequestBuilder` 提供了丰富的接口来设置请求参数：

```go
rb := client.POST("https://api.example.com/data").
    SetHeaders(map[string]string{"Authorization": "Bearer token"}).
    SetQueryParams(map[string]string{"page": "1", "limit": "20"}).
    SetJSONBody(map[string]interface{}{"foo": "bar"})

// 执行请求并获取原始响应
resp, err := rb.Execute()
if err == nil {
    defer resp.Body.Close()
}
```

### 2. 自动重试策略

默认启用 2 次重试（共执行 3 次），支持针对特定状态码进行指数退避。

```go
client := httpc.New(
    httpc.WithRetryOptions(httpc.RetryOptions{
        MaxAttempts:   3,
        BaseDelay:     200 * time.Millisecond,
        MaxDelay:      2 * time.Second,
        RetryStatuses: []int{429, 500, 503},
        Jitter:        true,
    }),
)
```

### 3. 中间件

中间件签名遵循 `func(next http.Handler) http.Handler`，方便与标准库生态集成。

```go
func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s took %v", r.Method, r.URL.Path, time.Since(start))
    })
}

client := httpc.New(httpc.WithMiddleware(LoggingMiddleware))
```

### 4. 详细日志记录

通过 `WithDumpLog` 可以输出结构化的请求调试日志，包括请求头、协议版本及底层 Transport 配置。

## 详细配置

| 选项 | 说明 |
| :--- | :--- |
| `WithTimeout` | 设置全局请求超时 |
| `WithUserAgent` | 自定义 User-Agent |
| `WithTransport` | 替换或扩展底层 `http.Transport` |
| `WithProtocols` | 配置支持的 HTTP 协议 (HTTP1/2/H2C) |
| `WithMiddleware` | 注册全局中间件 |

## 错误处理

`httpc` 会在状态码 >= 400 时返回 `*httpc.HTTPError`。你可以通过它获取响应头、状态码以及响应体的前几位预览，方便调试。

## 许可证

本项目采用 **MPL-2.0** 与 **WJQserver Studio License 2.0** 双重授权。您可以任选其一使用。所有权利由 satomitouka 保留，WJQserver Studio 代行相关权利。
