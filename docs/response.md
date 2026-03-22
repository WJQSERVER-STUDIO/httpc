# 响应处理

## Decode 快捷方法

`RequestBuilder` 提供了一步完成执行 + 解码的方法，自动关闭响应体：

```go
// JSON
var user User
err := client.GET("https://api.example.com/user/1").DecodeJSON(&user)
if err != nil {
    // 可能是 HTTPError、ErrDecodeResponse 或网络错误
}

// XML
var config Config
err := client.GET(url).DecodeXML(&config)

// GOB
var data MyData
err := client.GET(url).DecodeGOB(&data)

// Text
text, err := client.GET(url).Text()
fmt.Println(text)

// Bytes
body, err := client.GET(url).Bytes()
fmt.Printf("%x\n", body)
```

## 获取原始响应

```go
resp, err := client.GET(url).Execute()
if err != nil {
    // ...
}
defer resp.Body.Close()

// 自己处理 resp
```

## 错误处理

### HTTPError

当状态码 >= 400 时，decode 方法返回 `*http.Error`：

```go
var user User
err := client.GET("https://api.example.com/user/999").DecodeJSON(&user)
if err != nil {
    var httpErr *httpc.HTTPError
    if errors.As(err, &httpErr) {
        fmt.Println("状态码:", httpErr.StatusCode)
        fmt.Println("状态文本:", httpErr.Status)
        fmt.Println("响应头:", httpErr.Header.Get("X-Request-Id"))
        fmt.Println("Body 预览:", string(httpErr.Body))
    }
}
```

**HTTPError 字段：**
- `StatusCode`: HTTP 状态码 (如 404)
- `Status`: 状态文本 (如 "Not Found")
- `Header`: 响应头副本
- `Body`: 响应体前缀 (最多 1KB)，用于错误排查

### 解码错误

解码失败时返回 `ErrDecodeResponse`：

```go
err := client.GET(url).DecodeJSON(&target)
if errors.Is(err, httpc.ErrDecodeResponse) {
    fmt.Println("响应格式不正确")
}
```

### 导出的错误变量

```go
var (
    httpc.ErrRequestTimeout     // 请求超时 (包含 context.DeadlineExceeded 和 net.Error Timeout)
    httpc.ErrMaxRetriesExceeded // 超过最大重试次数
    httpc.ErrDecodeResponse     // 响应解码失败
    httpc.ErrInvalidURL         // 无效 URL
    httpc.ErrNoResponse         // 无响应 (resp == nil)
)
```

### 连接复用

`errorResponse` 在返回 `HTTPError` 时会：
1. 读取最多 1KB 的 body 作为预览
2. 丢弃剩余 body (最多 64KB)，帮助 HTTP 连接复用

这确保即使在错误响应场景下，底层连接也能被正确归还到连接池。

## SSE 流处理

### 建立 SSE 连接

```go
stream, err := client.GET("https://api.example.com/events").SSE()
if err != nil {
    return err
}
defer stream.Close()
```

或者使用标准库兼容入口：

```go
stream, err := client.GetSSE(ctx, "https://api.example.com/events")
```

### 读取事件

```go
for {
    event, err := stream.Next()
    if errors.Is(err, io.EOF) {
        break
    }
    if err != nil {
        return err
    }

    fmt.Println("event:", event.Event)
    fmt.Println("id:", event.Id)
    fmt.Println("retry:", event.Retry)
    fmt.Println("data:", event.Data)
}
```

### 解析规则

SSE 解析行为遵循 WHATWG/MDN 约定，并与 touka 的服务端渲染格式兼容：

- `data:` 多行会按 `\n` 连接为一个 `Data` 字段
- `event:` 解析为 `Event`
- `id:` 解析为 `Id`
- `retry:` 解析为 `Retry`
- 以 `:` 开头的注释行会被忽略
- 空块和 keepalive 块会被忽略
- 如果流在事件结束空行之前断开，但当前事件已经解析到字段，客户端会返回这条最后事件，再在后续读取中返回 `io.EOF`

### SSE 建立时的校验

`SSE()` 在建连时会做以下校验：

1. 自动补 `Accept: text/event-stream`（如果调用方未设置）
2. 如果状态码 `>= 400`，返回 `*HTTPError`
3. 如果 `Content-Type` 不是 `text/event-stream`，返回 `ErrInvalidSSEStream`

### 手动关闭

SSE 属于长连接，请在不再使用时调用 `Close()`：

```go
defer stream.Close()
```
