# API 参考

## 类型

### `Client`

主客户端结构，封装 `http.Client`、`http.Transport`、重试、日志、中间件等。

```go
type Client struct { ... }
```

不可直接构造，使用 `httpc.New(opts ...Option)` 创建。

---

### `RequestBuilder`

请求构建器，通过 `client.GET(url)` 等方法创建，支持链式配置。

```go
type RequestBuilder struct { ... }
```

---

### `Option`

配置选项函数类型：

```go
type Option func(*Client)
```

---

### `RetryOptions`

重试配置：

```go
type RetryOptions struct {
    MaxAttempts   int           // 最大重试次数 (不含首次)
    BaseDelay     time.Duration // 基础延迟
    MaxDelay      time.Duration // 最大延迟
    RetryStatuses []int         // 触发重试的 HTTP 状态码
    Jitter        bool          // 是否启用抖动
}
```

**默认值：**
- `MaxAttempts`: 2
- `BaseDelay`: 100ms
- `MaxDelay`: 1s
- `RetryStatuses`: `[429, 500, 502, 503, 504]`
- `Jitter`: false

---

### `ProtocolsConfig`

HTTP 协议版本配置：

```go
type ProtocolsConfig struct {
    Http1           bool // 启用 HTTP/1.1
    Http2           bool // 启用 HTTP/2 (TLS)
    Http2_Cleartext bool // 启用 H2C (非加密 HTTP/2)
    ForceH2C        bool // 强制 H2C (排斥其他协议)
}
```

**注意：** 当 `ForceH2C` 为 true 时，会禁用 HTTP/1.1 和加密 HTTP/2，仅启用非加密 HTTP/2。这是基于 Go 1.24+ 标准库 `Transport.Protocols` 的行为。

---

### `BufferPool`

缓冲池接口：

```go
type BufferPool interface {
    Get() *bytes.Buffer
    Put(*bytes.Buffer)
}
```

---

### `RoundTripperFunc`

函数适配器，允许普通函数作为 `http.RoundTripper`：

```go
type RoundTripperFunc func(req *http.Request) (*http.Response, error)
```

---

### `MiddlewareFunc`

中间件函数类型：

```go
type MiddlewareFunc func(next http.RoundTripper) http.RoundTripper
```

---

### `DumpLogFunc`

日志记录函数类型：

```go
type DumpLogFunc func(ctx context.Context, log string)
```

---

### `HTTPError`

结构化 HTTP 错误，当状态码 >= 400 时返回：

```go
type HTTPError struct {
    StatusCode int         // HTTP 状态码
    Status     string      // 状态文本 (如 "Not Found")
    Header     http.Header // 响应头 (副本)
    Body       []byte      // 响应体前缀 (最多 1KB)
}
```

实现 `error` 接口，body 预览最多 200 字符。

---

## 导出变量

```go
var (
    ErrRequestTimeout     // 请求超时
    ErrMaxRetriesExceeded // 超过最大重试次数
    ErrDecodeResponse     // 响应解码失败
    ErrInvalidURL         // 无效 URL
    ErrNoResponse         // 无响应
)
```

---

## 函数

### `New(opts ...Option) *Client`

创建客户端实例。应用所有 Option 后返回。

见 [客户端配置](client.md)。

---

## Client 方法

### 请求快捷方法

```go
func (c *Client) GET(urlStr string) *RequestBuilder
func (c *Client) POST(urlStr string) *RequestBuilder
func (c *Client) PUT(urlStr string) *RequestBuilder
func (c *Client) DELETE(urlStr string) *RequestBuilder
func (c *Client) PATCH(urlStr string) *RequestBuilder
func (c *Client) HEAD(urlStr string) *RequestBuilder
func (c *Client) OPTIONS(urlStr string) *RequestBuilder
```

### 请求构建

```go
func (c *Client) NewRequestBuilder(method, urlStr string) *RequestBuilder
func (c *Client) NewRequest(method, urlStr string, body io.Reader) (*http.Request, error)
```

### 执行

```go
func (c *Client) Do(req *http.Request) (*http.Response, error)
```

### 标准库兼容

```go
func (c *Client) Get(url string) (*http.Response, error)
func (c *Client) GetContext(ctx context.Context, url string) (*http.Response, error)
func (c *Client) Post(ctx context.Context, url string, body io.Reader) (*http.Response, error)
func (c *Client) PostJSON(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) PostXML(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) PostGOB(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) Put(ctx context.Context, url string, body io.Reader) (*http.Response, error)
func (c *Client) PutJSON(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) PutXML(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) PutGOB(ctx context.Context, url string, body any) (*http.Response, error)
func (c *Client) Delete(ctx context.Context, url string) (*http.Response, error)
```

### 动态设置

```go
func (c *Client) SetRetryOptions(opts RetryOptions)
func (c *Client) SetDumpLogFunc(dumpLog DumpLogFunc)
func (c *Client) SetTimeout(timeout time.Duration)
```

---

## RequestBuilder 方法

### 上下文与配置

```go
func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder
func (rb *RequestBuilder) NoDefaultHeaders() *RequestBuilder
```

### Header

```go
func (rb *RequestBuilder) SetHeader(key, value string) *RequestBuilder
func (rb *RequestBuilder) AddHeader(key, value string) *RequestBuilder
func (rb *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder
```

### Query

```go
func (rb *RequestBuilder) SetQueryParam(key, value string) *RequestBuilder
func (rb *RequestBuilder) AddQueryParam(key, value string) *RequestBuilder
func (rb *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder
```

### Body

```go
func (rb *RequestBuilder) SetBody(body io.Reader) *RequestBuilder
func (rb *RequestBuilder) SetRawBody(body []byte) *RequestBuilder
func (rb *RequestBuilder) SetJSONBody(body any) (*RequestBuilder, error)
func (rb *RequestBuilder) SetXMLBody(body any) (*RequestBuilder, error)
func (rb *RequestBuilder) SetGOBBody(body any) (*RequestBuilder, error)
```

### 执行与解码

```go
func (rb *RequestBuilder) Build() (*http.Request, error)
func (rb *RequestBuilder) Execute() (*http.Response, error)
func (rb *RequestBuilder) DecodeJSON(v any) error
func (rb *RequestBuilder) DecodeXML(v any) error
func (rb *RequestBuilder) DecodeGOB(v any) error
func (rb *RequestBuilder) Text() (string, error)
func (rb *RequestBuilder) Bytes() ([]byte, error)
```
