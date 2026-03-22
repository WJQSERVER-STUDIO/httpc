# httpc 文档

## 目录

- [概述](#概述)
- [快速开始](#快速开始)
- [SSE 支持](#sse-支持)
- [架构说明](#架构说明)
- [完整文档](#完整文档)

## 概述

httpc 是一个基于 Go 标准库 `net/http` 的增强型 HTTP 客户端库。它封装了请求构建、Transport 配置、自动重试、响应解码、日志记录、中间件等常见能力，提供更顺手的 API。

**核心特性：**
- Fluent Builder 链式构建请求
- 自动重试与指数退避
- JSON/XML/GOB 响应解码
- 自定义中间件 (RoundTripper 模式)
- 结构化 HTTP 错误 (HTTPError)
- SOCKS5/HTTP 代理支持
- 自定义 DNS 解析
- HTTP/1.1 + HTTP/2 协议配置
- 标准库 `http.Client` 兼容方法

**依赖：**
- `go 1.26`
- `github.com/go-json-experiment/json` (JSON 编解码)
- `github.com/WJQSERVER-STUDIO/go-utils/iox` (IO 工具)
- `golang.org/x/net/proxy` (SOCKS5 代理)

## 快速开始

```go
package main

import (
    "fmt"
    httpc "github.com/WJQSERVER-STUDIO/httpc"
)

func main() {
    client := httpc.New()

    var resp struct {
        Name string `json:"name"`
    }
    if err := client.GET("https://api.example.com/user/1").DecodeJSON(&resp); err != nil {
        fmt.Println("error:", err)
        return
    }
    fmt.Println("user:", resp.Name)
}
```

## SSE 支持

httpc 提供了原生 SSE 客户端支持，可直接连接 `text/event-stream` 端点并逐帧解析事件。

```go
import (
    "errors"
    "fmt"
    "io"
)

stream, err := client.GET("https://api.example.com/events").SSE()
if err != nil {
    return
}
defer stream.Close()

for {
    event, err := stream.Next()
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return
    }
    fmt.Printf("event=%s id=%s data=%q retry=%s\n",
        event.Event, event.Id, event.Data, event.Retry)
}
```

SSE 线格式与 touka 框架的服务端 `Event.Render` 输出兼容，支持：
- `event:`
- `data:` 多行聚合
- `id:`
- `retry:`
- 注释行 `:` 和空 keepalive 块忽略

## 架构说明

httpc 采用分层设计，按职责拆分为多个源码文件：

```
请求构建层        → request_builder.go  (RequestBuilder)
执行管线层        → transport.go        (Do / RoundTripper 包装)
响应处理层        → response.go         (Decode / Text / Bytes)
错误处理层        → errors.go           (HTTPError / errorResponse)
客户端配置层      → client.go           (New / 超时设置)
Option 配置层     → options.go          (WithTransport / WithProtocols / ...)
DNS 解析层        → resolver.go         (customDialer)
类型与常量层      → types.go            (Client / ProtocolsConfig / ...)
标准库兼容层      → stdlib.go           (Get / Post / Put / ...)
```

**调用流程：**
```
用户调用 client.GET(url)
  → 创建 RequestBuilder
  → 链式配置 Header / Query / Body / Context
  → Build() → http.Request
  → Do() → RoundTripper 包装链
    → transport (底层)
    → middlewares (用户中间件，逆序)
    → logRoundTripper (日志)
    → retryRoundTripper (重试)
  → Execute() → *http.Response
  → DecodeJSON / Text / Bytes → 解码或错误
```

## 完整文档

- [API 参考](api.md) — 导出类型、函数和方法
- [客户端配置](client.md) — 创建客户端、Option 配置详解
- [请求构建](builder.md) — RequestBuilder 用法
- [响应处理](response.md) — 响应解码与错误处理
- [SSE 流处理](response.md#sse-流处理) — SSE 连接建立、事件解析与关闭
- [重试与中间件](retry-middleware.md) — 重试策略、日志、中间件
- [Transport 与协议](transport.md) — Transport 配置、HTTP/2、代理、DNS
