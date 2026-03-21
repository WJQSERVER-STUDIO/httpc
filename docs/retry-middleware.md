# 重试与中间件

## 重试机制

### 基本配置

```go
client := httpc.New(httpc.WithRetryOptions(httpc.RetryOptions{
    MaxAttempts:   3,
    BaseDelay:     100 * time.Millisecond,
    MaxDelay:      1 * time.Second,
    RetryStatuses: []int{429, 500, 502, 503, 504},
}))
```

- `MaxAttempts`: 最大重试次数 (不含首次请求)
- `BaseDelay`: 基础延迟，用于计算退避时间
- `MaxDelay`: 最大延迟上限
- `RetryStatuses`: 触发重试的 HTTP 状态码列表
- `Jitter`: 是否添加抖动 (当前基于 attempt 固定比例，非随机)

### 重试触发条件

重试在以下情况触发：
1. **网络错误**: 返回的 error 是 `net.Error` 类型
2. **指定状态码**: 响应状态码在 `RetryStatuses` 列表中

### 退避策略

使用指数退避，延迟计算为：

```
delay = min(BaseDelay * 2^attempt, MaxDelay)
```

当 `Jitter` 为 true 时，乘以 `0.8 + 0.4 * attempt` 作为抖动因子。

### Retry-After 支持

如果响应包含 `Retry-After` 头部，会优先使用该值作为延迟：
- 支持秒数格式 (如 `60`)
- 支持 HTTP 日期格式 (如 `Wed, 21 Oct 2015 07:28:00 GMT`)
- 仅在状态码为 429 时生效

### Body 重试限制

**重要：** 重试依赖请求的 `GetBody` 方法来重放 body。

- `SetRawBody()`: 使用 `bytes.Reader`，自动设置 `GetBody`，支持重试
- `SetXMLBody()` / `SetGOBBody()`: 使用 `bytes.Reader`，支持重试
- `SetJSONBody()`: 使用 `io.Pipe()`，**不支持重试** (body 不可重读)
- `SetBody(io.Reader)`: 取决于 Reader 是否支持 `GetBody`

## 日志

### 启用日志

```go
// 默认日志 (fmt.Println)
client := httpc.New(httpc.WithDumpLog())

// 自定义日志函数
client := httpc.New(httpc.WithDumpLogFunc(func(ctx context.Context, log string) {
    log.Info("http request", "log", log)
}))
```

### 日志内容

日志记录在请求发送前输出，包含：
- 时间
- 方法
- URL / Host / Protocol
- Transport 详情 (MaxIdleConns, IdleConnTimeout, Protocols 等)
- 请求头

**注意：** 当前日志仅记录请求信息，不记录响应状态码和耗时。

### 动态启用

```go
client.SetDumpLogFunc(myLogger)
```

## 中间件

### 添加中间件

```go
client := httpc.New(httpc.WithMiddleware(
    loggingMiddleware,
    metricsMiddleware,
))
```

中间件类型：

```go
type MiddlewareFunc func(next http.RoundTripper) http.RoundTripper
```

### 执行顺序

`Do()` 中的包装顺序 (从外到内)：

```
retryRoundTripper (重试)
  └→ logRoundTripper (日志)
       └→ middleware[n-1]
            └→ middleware[...]
                 └→ middleware[0]
                      └→ transport (底层 HTTP Transport)
```

- 中间件按添加顺序应用，第一个中间件在最外层
- 日志在中间件之后、重试之前
- 重试是最外层包装器

### 示例：耗时统计

```go
func timingMiddleware(next http.RoundTripper) http.RoundTripper {
    return httpc.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        start := time.Now()
        resp, err := next.RoundTrip(req)
        elapsed := time.Since(start)
        fmt.Printf("[%s] %s %s -> %d (%v)\n",
            req.Method, req.URL.Host, req.URL.Path,
            resp.StatusCode, elapsed)
        return resp, err
    })
}
```

### 示例：请求头注入

```go
func authMiddleware(token string) httpc.MiddlewareFunc {
    return func(next http.RoundTripper) http.RoundTripper {
        return httpc.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
            req.Header.Set("Authorization", "Bearer "+token)
            return next.RoundTrip(req)
        })
    }
}
```

### RoundTripperFunc

`RoundTripperFunc` 是一个适配器，允许普通函数作为 `http.RoundTripper`：

```go
type RoundTripperFunc func(req *http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
    return f(req)
}
```
