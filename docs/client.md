# 客户端配置

## 创建客户端

```go
import httpc "github.com/WJQSERVER-STUDIO/httpc"

// 默认配置
client := httpc.New()

// 带配置
client := httpc.New(
    httpc.WithTimeout(30 * time.Second),
    httpc.WithUserAgent("my-app/1.0"),
)
```

`New()` 会按顺序应用所有 Option，每次应用后重新绑定 Transport 和 Timeout。

## 默认配置

| 配置项 | 默认值 |
|--------|--------|
| User-Agent | `Touka HTTP Client/v0` |
| Timeout | 0 (不超时，由 Context 控制) |
| MaxIdleConns | 根据 GOMAXPROCS 智能设置 (32/24*CPUs/128) |
| MaxIdleConnsPerHost | MaxIdleConns / 2 |
| IdleConnTimeout | 90s |
| DialTimeout | 10s |
| KeepAliveTimeout | 30s |
| TLSHandshakeTimeout | 10s |
| ExpectContinueTimeout | 1s |
| WriteBufferSize | 32KB |
| ReadBufferSize | 32KB |
| Protocols | HTTP/1.1 + HTTP/2 |
| ForceAttemptHTTP2 | true |
| Proxy | http.ProxyFromEnvironment |

## Option 详解

### 超时

```go
// 全局超时
httpc.WithTimeout(30 * time.Second)

// Dial 超时
httpc.WithDialTimeout(5 * time.Second)

// KeepAlive 超时
httpc.WithKeepAliveTimeout(60 * time.Second)

// TLS 握手超时
httpc.WithTLSHandshakeTimeout(10 * time.Second)

// Expect Continue 超时
httpc.WithExpectContinueTimeout(2 * time.Second)

// 空闲连接超时
httpc.WithIdleConnTimeout(120 * time.Second)
```

### 连接池

```go
// 最大空闲连接数
httpc.WithMaxIdleConns(256)

// 自定义 Buffer 池大小
httpc.WithBufferSize(64 << 10)

// 自定义最大 Buffer 池数量 (实际未严格限制，当前仅记录)
httpc.WithMaxBufferPoolSize(200)
```

### User-Agent

```go
httpc.WithUserAgent("my-app/1.0")
```

### Transport 合并

```go
httpc.WithTransport(&http.Transport{
    DisableCompression: true,
    MaxResponseHeaderBytes: 1 << 20,
})
```

使用反射式非零字段合并：只覆盖 src 中非零的字段，保留 dst 其他字段。

### 协议配置

```go
// 仅 HTTP/2
httpc.WithProtocols(httpc.ProtocolsConfig{
    Http1: false,
    Http2: true,
})

// 启用 H2C (非加密 HTTP/2)
httpc.WithProtocols(httpc.ProtocolsConfig{
    Http1:           true,
    Http2_Cleartext: true,
})

// 强制 H2C
httpc.WithProtocols(httpc.ProtocolsConfig{
    ForceH2C: true,
})
```

**注意事项：**
- `ForceH2C` 会禁用 HTTP/1.1 和加密 HTTP/2，仅启用非加密 HTTP/2 (Prior Knowledge)
- 当 `Http1 + Http2_Cleartext` 同时开启时，客户端对 `http://` URL 使用 HTTP/1，不会自动降级到 H2C
- 这是 Go 1.24+ 标准库的原生行为

### 代理

```go
// HTTP/HTTPS 代理
httpc.WithHTTPProxy("http://user:pass@proxy:8080")

// SOCKS5 代理
httpc.WithSocks5Proxy("socks5://user:pass@proxy:1080")
```

SOCKS5 代理依赖 `golang.org/x/net/proxy`。

### 自定义 DNS

```go
httpc.WithDNSResolver(
    []string{"8.8.8.8:53", "1.1.1.1:53"},
    5 * time.Second,
)
```

- DNS 服务器地址格式为 `ip:port`
- 超时为 0 时使用默认 5 秒
- 自定义解析失败时自动回退到系统默认 DNS

### 重试

```go
httpc.WithRetryOptions(httpc.RetryOptions{
    MaxAttempts:   3,
    BaseDelay:     200 * time.Millisecond,
    MaxDelay:      5 * time.Second,
    RetryStatuses: []int{429, 500, 502, 503, 504},
    Jitter:        true,
})
```

### 日志

```go
// 启用默认日志 (fmt.Println)
httpc.WithDumpLog()

// 自定义日志函数
httpc.WithDumpLogFunc(func(ctx context.Context, log string) {
    slog.InfoContext(ctx, log)
})
```

### 中间件

```go
httpc.WithMiddleware(
    func(next http.RoundTripper) http.RoundTripper {
        return httpc.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
            start := time.Now()
            resp, err := next.RoundTrip(req)
            fmt.Printf("%s %s %v\n", req.Method, req.URL, time.Since(start))
            return resp, err
        })
    },
)
```

多个中间件按添加顺序应用，第一个在最外层。

### 缓冲池

```go
httpc.WithBufferPool(myPool)
```

## 动态修改

创建客户端后可以动态修改部分配置：

```go
client.SetTimeout(10 * time.Second)
client.SetDumpLogFunc(myLogger)
client.SetRetryOptions(httpc.RetryOptions{
    MaxAttempts: 5,
})
```
