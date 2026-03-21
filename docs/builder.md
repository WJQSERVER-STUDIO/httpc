# 请求构建 (RequestBuilder)

## 基本用法

通过 `client.GET(url)` 等方法创建 `RequestBuilder`，然后链式配置：

```go
resp, err := client.GET("https://api.example.com/users").
    SetHeader("Accept", "application/json").
    SetQueryParam("page", "1").
    Execute()
```

## 设置 Context

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

resp, err := client.GET("https://api.example.com/data").
    WithContext(ctx).
    Execute()
```

默认使用 `context.Background()`。

## Header 操作

```go
// 设置单个 Header (覆盖已有的)
client.GET(url).SetHeader("Authorization", "Bearer xxx")

// 添加 Header (保留已有值)
client.GET(url).AddHeader("X-Trace", "id1").AddHeader("X-Trace", "id2")

// 批量设置
client.GET(url).SetHeaders(map[string]string{
    "Accept":     "application/json",
    "User-Agent": "my-app/1.0",
})
```

## Query 参数

```go
// 设置 (覆盖)
client.GET(url).SetQueryParam("lang", "zh")

// 添加 (多值)
client.GET(url).AddQueryParam("tag", "go").AddQueryParam("tag", "net/http")

// 批量设置
client.GET(url).SetQueryParams(map[string]string{
    "page": "1",
    "size": "20",
})
```

Query 参数会与 URL 中已有的 query 合并，`AddQueryParam` 的值会追加。

## Body

### 原始 Body

```go
// io.Reader
client.POST(url).SetBody(strings.NewReader("raw data"))

// []byte
client.POST(url).SetRawBody([]byte(`{"key":"value"}`))
```

### JSON Body

```go
body := map[string]string{"name": "touka"}
builder, err := client.POST(url).SetJSONBody(body)
```

- 自动设置 `Content-Type: application/json`
- 使用 `io.Pipe()` 流式写入

### XML Body

```go
body := MyStruct{Field: "value"}
builder, err := client.POST(url).SetXMLBody(body)
```

- 自动设置 `Content-Type: application/xml`
- 使用缓冲池编码

### GOB Body

```go
builder, err := client.POST(url).SetGOBBody(myData)
```

- 自动设置 `Content-Type: application/octet-stream`
- 使用缓冲池编码

## 构建与执行

### Build

仅构建 `*http.Request`，不执行：

```go
req, err := client.GET(url).Build()
```

合并逻辑：
1. 解析 URL
2. 合并 Query 参数 (URL 原有 + builder 添加)
3. 合并 Header (builder 覆盖)
4. 如果未设置 `User-Agent` 且未调用 `NoDefaultHeaders()`，注入默认 UA

### Execute

构建并执行，返回 `*http.Response`：

```go
resp, err := client.GET(url).Execute()
```

### Decode 快捷方法

一步完成执行 + 解码，自动关闭响应体：

```go
// JSON
var user User
err := client.GET(url).DecodeJSON(&user)

// XML
var config Config
err := client.GET(url).DecodeXML(&config)

// GOB
var data MyData
err := client.GET(url).DecodeGOB(&data)

// Text
text, err := client.GET(url).Text()

// Bytes
body, err := client.GET(url).Bytes()
```

## NoDefaultHeaders

禁用默认 Header (如 User-Agent)：

```go
client.GET(url).NoDefaultHeaders().Build()
```

## 注意事项

- `SetJSONBody()` 使用 `io.Pipe()`，body 不可重读，会影响重试行为
- `SetXMLBody()` 和 `SetGOBBody()` 使用缓冲池编码，body 可重读，支持重试
- `SetRawBody()` 和 `SetBody()` 使用 `bytes.Reader` 或自定义 Reader，重试取决于 Reader 是否支持 `GetBody`
- `Build()` 返回的 `*http.Request` 是一个独立副本，可以单独使用或传给 `Do()`
