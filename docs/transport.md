# Transport 与协议配置

## Transport 概述

httpc 底层使用 `http.Transport` 管理 HTTP 连接。`New()` 创建时会初始化一个合理的默认配置，并支持通过 Option 和 `WithTransport()` 进行自定义。

## 协议配置

### HTTP/1.1 + HTTP/2 (默认)

```go
client := httpc.New()
// 默认启用 HTTP/1.1 和 HTTP/2
// ForceAttemptHTTP2 = true
```

### 仅 HTTP/2

```go
client := httpc.New(httpc.WithProtocols(httpc.ProtocolsConfig{
    Http1: false,
    Http2: true,
}))
```

### H2C (非加密 HTTP/2)

```go
client := httpc.New(httpc.WithProtocols(httpc.ProtocolsConfig{
    Http1:           true,
    Http2_Cleartext: true,
}))
```

启用后对 `http://` URL 使用 HTTP/1.1，对需要 H2C 的场景需配合服务端配置。

### 强制 H2C

```go
client := httpc.New(httpc.WithProtocols(httpc.ProtocolsConfig{
    ForceH2C: true,
}))
```

- 禁用 HTTP/1.1 和加密 HTTP/2
- 仅启用非加密 HTTP/2 (RFC 9113 Prior Knowledge)
- `ForceAttemptHTTP2` 设为 false

### ProtocolsConfig 字段

```go
type ProtocolsConfig struct {
    Http1           bool // HTTP/1.1
    Http2           bool // HTTP/2 over TLS
    Http2_Cleartext bool // HTTP/2 over cleartext (H2C)
    ForceH2C        bool // 强制 H2C，排斥其他协议
}
```

### 与 Go 标准库的关系

httpc 使用 Go 1.24+ 的 `http.Transport.Protocols` 字段进行协议配置。这是标准库原生支持，无需额外的 `golang.org/x/net/http2` 依赖。

Go 1.24 发布说明中的行为约定：
- `Transport.Protocols` 含 `UnencryptedHTTP2` 但不含 `HTTP1`：对 `http://` URL 使用 H2C
- `Transport.Protocols` 同时含 `HTTP1` 和 `UnencryptedHTTP2`：使用 HTTP/1
- H2C 使用 "HTTP/2 with Prior Knowledge"，不支持旧式 `Upgrade: h2c`

## Transport 合并

```go
httpc.WithTransport(&http.Transport{
    DisableCompression: true,
    MaxResponseHeaderBytes: 1 << 20,
})
```

使用反射式非零字段合并：遍历 src 的所有导出字段，只覆盖非零值到 dst。

```go
func mergeTransport(dst, src *http.Transport) {
    // 遍历 src 的导出字段
    // 如果 src 字段非零 → 覆盖 dst 的同名字段
    // 如果 src 字段是零值 → 保留 dst 原值
}
```

**注意事项：**
- `WithTransport()` 不会覆盖整个 Transport，只合并非零字段
- 多个 Option 按添加顺序执行，后续 Option 的非零字段会覆盖前面的
- Protocols 字段也是通过此机制合并

## 代理

### HTTP/HTTPS 代理

```go
client := httpc.New(httpc.WithHTTPProxy("http://user:pass@proxy.example.com:8080"))
```

使用 `http.ProxyURL()` 设置代理。

### SOCKS5 代理

```go
client := httpc.New(httpc.WithSocks5Proxy("socks5://user:pass@proxy.example.com:1080"))
```

- 依赖 `golang.org/x/net/proxy`
- 支持用户名密码认证
- 不需要认证时可省略 `user:password`

### 环境变量代理

默认使用 `http.ProxyFromEnvironment`，自动读取 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` 等环境变量。

## 自定义 DNS

```go
client := httpc.New(httpc.WithDNSResolver(
    []string{"8.8.8.8:53", "1.1.1.1:53"},
    5 * time.Second,
))
```

### 工作原理

1. 从目标地址中分离 host 和 port
2. 使用指定的 DNS 服务器列表解析 host
3. 按顺序尝试连接解析出的 IP
4. 如果自定义解析失败，回退到系统默认 DNS

### 自定义 DNS 服务器格式

`ip:port`，例如：
- `"8.8.8.8:53"` (Google DNS)
- `"1.1.1.1:53"` (Cloudflare DNS)
- `"223.5.5.5:53"` (阿里 DNS)

### 回退机制

自定义 DNS 解析失败时不会导致请求失败，而是回退到系统默认的 DNS 解析和拨号流程，保证兼容性。

## 连接池配置

```go
client := httpc.New(
    httpc.WithMaxIdleConns(256),
    httpc.WithIdleConnTimeout(120 * time.Second),
)
```

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| MaxIdleConns | GOMAXPROCS 相关 (32/24*CPU/128) | 所有 host 的最大空闲连接数 |
| MaxIdleConnsPerHost | MaxIdleConns / 2 | 单 host 最大空闲连接数 |
| IdleConnTimeout | 90s | 空闲连接超时 |
| MaxConnsPerHost | 0 (无限制) | 单 host 最大连接数 |

## 超时配置

| 超时项 | 默认值 | 说明 |
|--------|--------|------|
| Client Timeout | 0 | 全局超时 (0 = 由 Context 控制) |
| DialTimeout | 10s | TCP 连接超时 |
| KeepAliveTimeout | 30s | TCP KeepAlive 间隔 |
| TLSHandshakeTimeout | 10s | TLS 握手超时 |
| ExpectContinueTimeout | 1s | 100-Continue 超时 |

## Buffer 池

httpc 使用 `sync.Pool` 管理缓冲区，用于 XML/GOB 编解码和错误 body 预览：

```go
// 自定义缓冲区大小
client := httpc.New(httpc.WithBufferSize(64 << 10))

// 自定义 BufferPool 实现
client := httpc.New(httpc.WithBufferPool(myPool))
```

`BufferPool` 接口：

```go
type BufferPool interface {
    Get() *bytes.Buffer
    Put(*bytes.Buffer)
}
```

默认实现会在 `Put()` 时检查 buffer 容量，超过 `bufferSize * 2` 的 buffer 不放回池中。
