package httpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/WJQSERVER-STUDIO/go-utils/copyb"
)

// 错误定义
var (
	ErrRequestTimeout     = errors.New("httpc: request timeout")
	ErrMaxRetriesExceeded = errors.New("httpc: max retries exceeded")
	ErrDecodeResponse     = errors.New("httpc: failed to decode response body")
	ErrInvalidURL         = errors.New("httpc: invalid URL")
)

// 默认配置常量
const (
	defaultBufferSize            = 32 << 10 // 32KB
	defaultMaxBufferPool         = 100
	defaultUserAgent             = "Touka HTTP Client/v0"
	defaultMaxIdleConns          = 128
	defaultIdleConnTimeout       = 90 * time.Second
	defaultDialTimeout           = 10 * time.Second
	defaultKeepAliveTimeout      = 30 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultResolverTimeout       = 5 * time.Second
)

// RoundTripperFunc 是一个适配器，允许使用普通函数作为 HTTP RoundTripper
type RoundTripperFunc func(req *http.Request) (*http.Response, error)

// RoundTrip 实现了 http.RoundTripper 接口
func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// MiddlewareFunc 是客户端中间件的类型
// 它接收一个 http.RoundTripper (代表下一个处理器) 并返回一个新的 http.RoundTripper
type MiddlewareFunc func(next http.RoundTripper) http.RoundTripper

var bufferPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
	},
}

var ErrShortWrite = errors.New("short write")
var EOF = io.EOF

// DumpLogFunc 定义日志记录函数
type DumpLogFunc func(ctx context.Context, log string)

// Client 主客户端结构
type Client struct {
	client        *http.Client
	transport     *http.Transport
	retryOpts     RetryOptions
	bufferPool    BufferPool
	userAgent     string
	dumpLog       DumpLogFunc      // 日志记录函数
	maxIdleConns  int              // 最大空闲连接数
	bufferSize    int              // 缓冲池 buffer 大小
	maxBufferPool int              // 最大缓冲池数量
	timeout       time.Duration    // 默认请求超时时间 (可选)
	middlewares   []MiddlewareFunc // 中间件链
	dialer        *net.Dialer      // dialer实例
}

// RetryOptions 重试配置
type RetryOptions struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	RetryStatuses []int
	Jitter        bool // 是否启用 Jitter 抖动
}

// BufferPool 缓冲池接口
type BufferPool interface {
	Get() *bytes.Buffer
	Put(*bytes.Buffer)
}

// 默认缓冲池实现
type defaultPool struct {
	bufferSize int
}

func newDefaultPool(bufferSize int) *defaultPool {
	return &defaultPool{bufferSize: bufferSize}
}

func (p *defaultPool) Get() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func (p *defaultPool) Put(buf *bytes.Buffer) {
	if buf.Cap() > p.bufferSize*2 { // 防止内存泄漏，基于配置的 bufferSize
		return
	}
	bufferPool.Put(buf)
}

// Option 配置选项类型
type Option func(*Client)

// WithTransport 自定义 Transport，将非零字段合并到默认 Transport 中
func WithTransport(t *http.Transport) Option {
	return func(c *Client) {
		defaultTransport := c.transport
		mergeTransport(defaultTransport, t)
		c.transport = defaultTransport
		c.client.Transport = defaultTransport
	}
}

// WithMaxIdleConns 设置最大空闲连接数
func WithMaxIdleConns(maxIdleConns int) Option {
	return func(c *Client) {
		c.maxIdleConns = maxIdleConns
	}
}

// WithIdleConnTimeout 设置空闲连接超时时间
func WithIdleConnTimeout(idleConnTimeout time.Duration) Option {
	return func(c *Client) {
		c.transport.IdleConnTimeout = idleConnTimeout
	}
}

// WithDialTimeout 设置 DialContext 的超时时间
func WithDialTimeout(dialTimeout time.Duration) Option {
	return func(c *Client) {
		// 直接修改 c.dialer.Timeout
		c.dialer.Timeout = dialTimeout
		// 重新将 dialer.DialContext 赋值给 transport.DialContext
		c.transport.DialContext = c.dialer.DialContext
	}
}

// WithKeepAliveTimeout 设置 KeepAlive 超时时间
func WithKeepAliveTimeout(keepAliveTimeout time.Duration) Option {
	return func(c *Client) {
		// 直接修改 c.dialer.KeepAlive
		c.dialer.KeepAlive = keepAliveTimeout
		// 重新将 dialer.DialContext 赋值给 transport.DialContext
		c.transport.DialContext = c.dialer.DialContext
	}
}

// WithTLSHandshakeTimeout 设置 TLS 握手超时时间
func WithTLSHandshakeTimeout(tlsHandshakeTimeout time.Duration) Option {
	return func(c *Client) {
		c.transport.TLSHandshakeTimeout = tlsHandshakeTimeout
	}
}

// WithExpectContinueTimeout 设置 ExpectContinue 超时时间
func WithExpectContinueTimeout(expectContinueTimeout time.Duration) Option {
	return func(c *Client) {
		c.transport.ExpectContinueTimeout = expectContinueTimeout
	}
}

// WithBufferSize 自定义缓冲池 Buffer 大小
func WithBufferSize(bufferSize int) Option {
	return func(c *Client) {
		c.bufferSize = bufferSize
	}
}

// WithMaxBufferPoolSize 自定义最大缓冲池数量
func WithMaxBufferPoolSize(maxBufferPool int) Option {
	return func(c *Client) {
		c.maxBufferPool = maxBufferPool
	}
}

// WithTimeout 设置默认请求超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithDNSResolver 设置自定义DNS解析器
// servers: 一个或多个DNS服务器地址, 格式为 "ip:port" (例如, "8.8.8.8:53")
// timeout: DNS查询的超时时间如果为0, 将使用默认超时 (5秒)
// 此选项会覆盖系统默认的DNS解析行为
func WithDNSResolver(servers []string, timeout time.Duration) Option {
	return func(c *Client) {
		if len(servers) == 0 {
			return // 如果未提供服务器, 则不进行任何操作
		}
		if timeout == 0 {
			timeout = defaultResolverTimeout
		}
		// 调用 resolver.go 中的函数创建自定义解析器
		dialer := &customDialer{
			defaultDialer: c.dialer, // 传入原始的拨号器用于回退和实际连接
			dnsServers:    servers,  // 设置DNS服务器列表
			dnsTimeout:    timeout,  // 设置DNS查询超时
		}
		// 将自定义解析器附加到客户端的拨号器(dialer)上
		//c.dialer.Resolver = resolver

		c.transport.DialContext = dialer.DialContext
	}

}

// mergeTransport 将 src 的非零字段合并到 dst 中 (保持原函数不变)
func mergeTransport(dst, src *http.Transport) {
	dstVal := reflect.ValueOf(dst).Elem()
	srcVal := reflect.ValueOf(src).Elem()

	for i := 0; i < srcVal.NumField(); i++ {
		srcField := srcVal.Field(i)
		srcType := srcVal.Type().Field(i)
		if srcType.PkgPath != "" {
			continue
		}
		dstField := dstVal.FieldByName(srcType.Name)
		if !dstField.IsValid() || !dstField.CanSet() {
			continue
		}
		if !isZero(srcField) {
			dstField.Set(srcField)
		}
	}
}

// isZero 检查反射值是否为对应类型的零值 (保持原函数不变)
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		z := reflect.Zero(v.Type())
		return v.Interface() == z.Interface()
	}
}

// WithBufferPool 自定义缓冲池
func WithBufferPool(pool BufferPool) Option {
	return func(c *Client) {
		c.bufferPool = pool
	}
}

// WithRetryOptions 自定义重试策略
func WithRetryOptions(opts RetryOptions) Option {
	return func(c *Client) {
		c.retryOpts = opts
	}
}

// WithUserAgent 设置自定义 User-Agent
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithDumpLog 启用默认日志记录功能
func WithDumpLog() Option {
	return func(c *Client) {
		c.dumpLog = func(ctx context.Context, log string) {
			fmt.Println(log)
		}
	}
}

// WithDumpLogFunc 自定义日志记录功能
func WithDumpLogFunc(dumpLog DumpLogFunc) Option {
	return func(c *Client) {
		c.dumpLog = dumpLog
	}
}

// WithMiddleware 添加中间件
func WithMiddleware(middleware ...MiddlewareFunc) Option {
	return func(c *Client) {
		c.middlewares = append(c.middlewares, middleware...)
	}
}

// WithProtocols 配置客户端支持的 HTTP 协议版本
func WithProtocols(config ProtocolsConfig) Option {
	return func(c *Client) {
		// 直接修改当前 Client 实例的 transport 的 Protocols 字段
		if c.transport == nil {
			// 如果 transport 还未初始化 (理论上 New 函数会先初始化)，
			// 可以在 Client 结构体中暂存配置，待 transport 初始化后再应用
			// 但更好的方式是确保 transport 在应用此 Option 前已初始化
			// 这里假设 transport 已存在
			c.transport = &http.Transport{}
			c.client.Transport = c.transport

			return
		}
		if c.transport.Protocols == nil {
			c.transport.Protocols = new(http.Protocols) // Ensure Protocols field is initialized
		}

		// 优先应用 ForceH2C (因为它排斥其他协议)
		if config.ForceH2C {
			c.transport.Protocols.SetHTTP1(false)
			c.transport.Protocols.SetHTTP2(false)
			c.transport.Protocols.SetUnencryptedHTTP2(true)
			// 如果 ForceH2C，也应该设置 Transport 的 ForceAttemptHTTP2 为 false
			// 因为 H2C 是非加密的，不需要强制尝试加密的 HTTP/2
			c.transport.ForceAttemptHTTP2 = false
		} else {
			c.transport.Protocols.SetHTTP1(config.Http1)
			c.transport.Protocols.SetHTTP2(config.Http2)
			c.transport.Protocols.SetUnencryptedHTTP2(config.Http2_Cleartext)
			// 根据是否启用 HTTP/2 来决定是否尝试
			c.transport.ForceAttemptHTTP2 = config.Http2 || config.Http2_Cleartext
		}
	}
}

// ProtocolsConfig 协议版本配置结构体
type ProtocolsConfig struct {
	Http1           bool // 是否启用 HTTP/1.1
	Http2           bool // 是否启用 HTTP/2
	Http2_Cleartext bool // 是否启用 H2C
	ForceH2C        bool // 是否强制启用 H2C
}

// New 创建客户端实例
func New(opts ...Option) *Client {
	// 智能MaxIdleConns 设置 (保持不变)
	var maxIdleConns = defaultMaxIdleConns
	if runtime.GOMAXPROCS(0) > 4 {
		maxIdleConns = 128
	} else if runtime.GOMAXPROCS(0) != 1 {
		maxIdleConns = runtime.GOMAXPROCS(0) * 24
	} else {
		maxIdleConns = 32
	}

	// 初始化 net.Dialer 实例并存储到 Client 结构体中
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAliveTimeout,
	}

	var proTolcols = new(http.Protocols)
	proTolcols.SetHTTP1(true)
	proTolcols.SetHTTP2(true)

	c := &Client{
		client: &http.Client{
			//Transport: transport,
			Timeout: 0, // 默认 Client Timeout 为 0，表示不超时，由 Request Context 控制
		},
		//transport:     transport,
		retryOpts:     defaultRetryOptions(),
		bufferPool:    newDefaultPool(defaultBufferSize),
		userAgent:     defaultUserAgent,
		dumpLog:       nil, // 默认不启用日志
		maxIdleConns:  defaultMaxIdleConns,
		bufferSize:    defaultBufferSize,
		maxBufferPool: defaultMaxBufferPool,
		timeout:       0, // 默认不设置全局超时
		middlewares:   []MiddlewareFunc{},
	}

	// 默认 Transport 配置
	transport := &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            dialer.DialContext,
		MaxIdleConns:           maxIdleConns,
		MaxIdleConnsPerHost:    maxIdleConns / 2,
		MaxConnsPerHost:        0, // 默认为 0，表示无限制
		IdleConnTimeout:        defaultIdleConnTimeout,
		TLSHandshakeTimeout:    defaultTLSHandshakeTimeout,
		ExpectContinueTimeout:  defaultExpectContinueTimeout,
		WriteBufferSize:        32 * 1024, // 默认为 32KB
		ReadBufferSize:         32 * 1024, // 默认为 32KB
		DisableKeepAlives:      false,
		DisableCompression:     false,
		MaxResponseHeaderBytes: 0, // 默认为 0，表示无限制
		ForceAttemptHTTP2:      true,
		Protocols:              proTolcols,
	}

	c.transport = transport
	c.client.Transport = transport
	if c.timeout != 0 { // 如果设置了全局超时，则更新 Client 的 Timeout
		c.client.Timeout = c.timeout
	}

	for _, opt := range opts {
		opt(c)
		// 应用 Option 后，需要重新设置 Transport 到 Client，确保配置生效
		c.client.Transport = c.transport
		if c.timeout != 0 { // 如果设置了全局超时，则更新 Client 的 Timeout
			c.client.Timeout = c.timeout
		}
	}

	return c
}

// defaultRetryOptions 返回默认的重试策略
func defaultRetryOptions() RetryOptions {
	return RetryOptions{
		MaxAttempts:   2,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		RetryStatuses: []int{429, 500, 502, 503, 504},
		Jitter:        false, // 默认不启用 Jitter
	}
}

// SetRetryOptions 动态设置重试选项
func (c *Client) SetRetryOptions(opts RetryOptions) {
	c.retryOpts = opts
}

// SetDumpLogFunc 动态设置日志记录函数
func (c *Client) SetDumpLogFunc(dumpLog DumpLogFunc) {
	c.dumpLog = dumpLog
}

// SetTimeout 动态设置客户端超时
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
	c.client.Timeout = timeout // 同时更新 http.Client 的 Timeout
}

// RequestBuilder 用于构建请求的结构体
type RequestBuilder struct {
	client           *Client
	method           string
	url              string
	header           http.Header
	query            url.Values
	body             io.Reader
	context          context.Context
	noDefaultHeaders bool
}

// NewRequestBuilder 创建 RequestBuilder 实例
func (c *Client) NewRequestBuilder(method, urlStr string) *RequestBuilder {
	return &RequestBuilder{
		client:  c,
		method:  method,
		url:     urlStr,
		header:  make(http.Header),
		query:   make(url.Values),
		context: context.Background(), // 默认使用 Background Context
	}
}

// GET, POST, PUT, DELETE 等快捷方法
func (c *Client) GET(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodGet, urlStr)
}

func (c *Client) POST(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPost, urlStr)
}

func (c *Client) PUT(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPut, urlStr)
}

func (c *Client) DELETE(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodDelete, urlStr)
}

func (c *Client) PATCH(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPatch, urlStr)
}

func (c *Client) HEAD(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodHead, urlStr)
}

func (c *Client) OPTIONS(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodOptions, urlStr)
}

// WithContext 设置 Context
func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	rb.context = ctx
	return rb
}

// NoDefaultHeaders 设置请求不添加默认 Header
func (rb *RequestBuilder) NoDefaultHeaders() *RequestBuilder {
	rb.noDefaultHeaders = true
	return rb
}

// SetHeader 设置 Header
func (rb *RequestBuilder) SetHeader(key, value string) *RequestBuilder {
	rb.header.Set(key, value)
	return rb
}

// AddHeader 添加 Header
func (rb *RequestBuilder) AddHeader(key, value string) *RequestBuilder {
	rb.header.Add(key, value)
	return rb
}

// SetHeaders 批量设置 Headers
func (rb *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder {
	for key, value := range headers {
		rb.header.Set(key, value)
	}
	return rb
}

// SetQueryParam 设置 Query 参数
func (rb *RequestBuilder) SetQueryParam(key, value string) *RequestBuilder {
	rb.query.Set(key, value)
	return rb
}

// AddQueryParam 添加 Query 参数
func (rb *RequestBuilder) AddQueryParam(key, value string) *RequestBuilder {
	rb.query.Add(key, value)
	return rb
}

// SetQueryParams 批量设置 Query 参数
func (rb *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	for key, value := range params {
		rb.query.Set(key, value)
	}
	return rb
}

// SetBody 设置 Body (io.Reader)
func (rb *RequestBuilder) SetBody(body io.Reader) *RequestBuilder {
	rb.body = body
	return rb
}

// SetJSONBody 设置 JSON Body
func (rb *RequestBuilder) SetJSONBody(body interface{}) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	if err := json.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode json body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/json")
	return rb, nil
}

// SetXMLBody 设置 XML Body
func (rb *RequestBuilder) SetXMLBody(body interface{}) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	if err := xml.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode xml body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/xml")
	return rb, nil
}

// SetGOBBody 设置GOB Body
func (rb *RequestBuilder) SetGOBBody(body interface{}) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	// 使用 gob 编码
	if err := gob.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode gob body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/octet-stream") // 设置合适的 Content-Type
	return rb, nil
}

// Build 构建 http.Request
func (rb *RequestBuilder) Build() (*http.Request, error) {
	/*
		// 构建带 Query 参数的 URL
		reqURL, err := url.Parse(rb.url)
		if err != nil {
			return nil, fmt.Errorf("%w: %s, error: %v", ErrInvalidURL, rb.url, err)
		}
		if len(rb.query) > 0 {
			query := reqURL.Query()
			for k, v := range rb.query {
				for _, val := range v {
					query.Add(k, val)
				}
			}
			reqURL.RawQuery = query.Encode()
		}

		req, err := http.NewRequestWithContext(rb.context, rb.method, reqURL.String(), rb.body)
		if err != nil {
			return nil, err
		}

		// 合并 Header，RequestBuilder 中的 Header 优先级更高
		req.Header = rb.header

		// 若没有设置 NoDefaultHeaders，则添加默认 UA Header
		if !rb.noDefaultHeaders {
			req.Header.Set("User-Agent", rb.client.userAgent) // 确保 User-Agent 被设置
		}
		return req, nil
	*/

	reqURL, err := url.Parse(rb.url)
	if err != nil {
		return nil, fmt.Errorf("%w: %s, error: %v", ErrInvalidURL, rb.url, err)
	}
	if len(rb.query) > 0 {
		q := reqURL.Query()
		for k, v := range rb.query {
			for _, val := range v {
				q.Add(k, val)
			}
		}
		reqURL.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(rb.context, rb.method, reqURL.String(), rb.body)
	if err != nil {
		return nil, err
	}
	for k, v := range rb.header {
		req.Header[k] = v
	}
	if !rb.noDefaultHeaders && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", rb.client.userAgent)
	}
	return req, nil
}

// Execute 执行请求并返回 http.Response
func (rb *RequestBuilder) Execute() (*http.Response, error) {
	/*
		req, err := rb.Build()
		if err != nil {
			return nil, err
		}

		// 应用中间件
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp, err := rb.client.Do(r) // 调用 Client.Do 执行请求
			rb.responseWrapper(w, resp, err)
		})

		// 构建中间件链
		middlewareHandler := applyMiddlewares(handler, rb.client.middlewares...)

		// 创建 ResponseWriter 和 Request，并调用中间件链
		rw := newResponseWriter()
		middlewareHandler.ServeHTTP(rw, req)

		return rw.getResponse(), rw.getError()
	*/
	req, err := rb.Build()
	if err != nil {
		return nil, err
	}
	return rb.client.Do(req)
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	/*
		if req.ProtoMajor == 2 {
			if req.Header.Get("Connection") == "Upgrade" && req.Header.Get("Upgrade") != "" {
				req.Header.Del("Connection")
				req.Header.Del("Upgrade")
			}
		}

		// 记录日志
		c.logRequest(req)

		// 执行中间件链和重试逻辑
		return c.doWithRetry(req)
	*/
	var finalRT http.RoundTripper = c.transport

	// 逆序应用，使得第一个中间件在最外层
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		finalRT = c.middlewares[i](finalRT)
	}

	if c.dumpLog != nil {
		finalRT = c.logRoundTripper(finalRT)
	}

	// 只有在配置了重试次数时才应用
	if c.retryOpts.MaxAttempts > 0 {
		finalRT = c.retryRoundTripper(finalRT)
	}

	return finalRT.RoundTrip(req)
}

// logRoundTripper 是一个内部中间件，用于在请求发送前记录日志
func (c *Client) logRoundTripper(next http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		c.logRequest(req) // 在请求发送前记录
		return next.RoundTrip(req)
	})
}

// retryRoundTripper 是一个内部中间件，用于实现请求的重试逻辑
func (c *Client) retryRoundTripper(next http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var bodyReaderFunc func() (io.ReadCloser, error) // 用于缓存和重置 Body

		// 如果请求已经有 GetBody，我们直接使用它
		if req.GetBody != nil {
			bodyReaderFunc = req.GetBody
		}

		var lastResp *http.Response
		var lastErr error

		for attempt := 0; attempt <= c.retryOpts.MaxAttempts; attempt++ {

			if attempt > 0 {
				if bodyReaderFunc == nil {
					// 如果没有 bodyReaderFunc，意味着原始 Body 不可重读，
					// 且已在第一次尝试中被消耗，所以无法重试带 Body 的请求
					// 在这种情况下，我们应该在第一次失败后立即停止
					// shouldRetry 逻辑应该考虑到这一点
					// 这里我们直接中断重试
					break
				}

				// 从 bodyReaderFunc 创建一个新的 Body
				newBody, err := bodyReaderFunc()
				if err != nil {
					if lastResp != nil {
						lastResp.Body.Close()
					}
					return nil, fmt.Errorf("httpc: failed to get request body for retry attempt %d: %w", attempt, err) // 英文错误
				}
				req.Body = newBody
			}

			// 检查上下文是否已取消
			select {
			case <-req.Context().Done():
				// 如果之前的响应体已关闭，则返回上下文错误
				if lastResp != nil {
					lastResp.Body.Close()
				}
				return nil, c.wrapError(req.Context().Err())
			default:
			}

			// 调用链中的下一个 RoundTripper (可能是日志、Padding或其他中间件)
			resp, err := next.RoundTrip(req)
			lastResp, lastErr = resp, err

			// 判断是否需要重试
			if !c.shouldRetry(resp, err) {
				break // 不需要重试，跳出循环
			}

			// 如果是最后一次尝试，则不再重试，直接返回结果
			if attempt >= c.retryOpts.MaxAttempts {
				lastErr = ErrMaxRetriesExceeded
				break
			}

			// 计算重试延迟
			delay := c.calculateRetryAfter(resp)
			if delay <= 0 {
				delay = c.calculateExponentialBackoff(attempt, c.retryOpts.Jitter)
			}

			// 在重试前，确保关闭当前失败的响应体以复用连接
			if resp != nil && resp.Body != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}

			// 等待延迟，同时监听上下文取消
			select {
			case <-req.Context().Done():
				return nil, c.wrapError(req.Context().Err())
			case <-time.After(delay):
				// 继续下一次循环
			}
		}

		if lastErr != nil {
			return lastResp, c.wrapError(lastErr)
		}
		return lastResp, nil
	})
}

// 记录请求日志 (保持原函数不变)
func (c *Client) logRequest(req *http.Request) {
	if c.dumpLog == nil {
		return
	}

	transportDetails := getTransportDetails(c.transport)

	logContent := fmt.Sprintf(`
[HTTP Request Log]
-------------------------------
Time       : %s
Method     : %s
URL        : %s
Host       : %s
Protocol   : %s
Transport  :
%v
Headers    :
%v
-------------------------------
`,
		time.Now().Format("2006-01-02 15:04:05"),
		req.Method,
		req.URL.String(),
		req.URL.Host,
		req.Proto,
		transportDetails,
		formatHeaders(req.Header),
	)

	c.dumpLog(req.Context(), logContent)
}

// 获取 Transport 的详细信息 (保持原函数不变)
func getTransportDetails(transport http.RoundTripper) string {
	if t, ok := transport.(*http.Transport); ok {
		return fmt.Sprintf(`  Type                 : *http.Transport
  MaxIdleConns         : %d
  MaxIdleConnsPerHost  : %d
  MaxConnsPerHost      : %d
  IdleConnTimeout      : %s
  TLSHandshakeTimeout  : %s
  DisableKeepAlives    : %v
  WriteBufferSize      : %d
  ReadBufferSize       : %d
  Protocol             : %v
`,
			t.MaxIdleConns,
			t.MaxIdleConnsPerHost,
			t.MaxConnsPerHost,
			t.IdleConnTimeout,
			t.TLSHandshakeTimeout,
			t.DisableKeepAlives,
			t.WriteBufferSize,
			t.ReadBufferSize,
			t.Protocols,
		)
	}

	if transport != nil {
		return fmt.Sprintf("  Type                 : %T", transport)
	}

	return "  Type                 : nil"
}

// 格式化请求头为多行字符串
func formatHeaders(headers http.Header) string {
	var builder strings.Builder
	for key, values := range headers {
		builder.WriteString(fmt.Sprintf("  %s: %s\n", key, strings.Join(values, ", ")))
	}
	return builder.String()
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var (
		resp    *http.Response
		err     error
		lastErr error
	)

	for attempt := 0; attempt <= c.retryOpts.MaxAttempts; attempt++ {

		// 检查ctx状态
		select {
		case <-req.Context().Done():
			return nil, c.wrapError(req.Context().Err())
		default:
		}

		resp, err = c.client.Do(req) // 注意这里调用的是 http.Client.Do
		lastErr = err

		if c.shouldRetry(resp, lastErr) {
			if attempt < c.retryOpts.MaxAttempts {
				var delay time.Duration
				if resp != nil && resp.StatusCode == 429 {
					delay = c.calculateRetryAfter(resp)
				} else {
					delay = c.calculateExponentialBackoff(attempt, c.retryOpts.Jitter) // 传递 Jitter 参数
				}

				if resp != nil && resp.Body != nil {
					_, _ = copyb.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					resp = nil
				}

				// 重试前检查ctx
				select {
				case <-req.Context().Done():
					return nil, c.wrapError(req.Context().Err())
				case <-time.After(delay):
					continue
				}
			} else {
				return resp, ErrMaxRetriesExceeded
			}
		} else {
			break
		}
	}

	if lastErr != nil {
		return resp, c.wrapError(lastErr)
	}

	return resp, nil
}

// 解析 Retry-After 头部，仅在状态码为 429 时调用 (保持原函数不变)
func (c *Client) calculateRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		if delay, err := parseRetryAfter(retryAfter); err == nil {
			return delay
		}
	}
	return c.retryOpts.BaseDelay
}

// 解析 Retry-After 的具体实现 (保持原函数不变)
func parseRetryAfter(retryAfter string) (time.Duration, error) {
	if seconds, err := time.ParseDuration(retryAfter + "s"); err == nil {
		return seconds, nil
	}

	if retryTime, err := http.ParseTime(retryAfter); err == nil {
		delay := time.Until(retryTime)
		if delay > 0 {
			return delay, nil
		}
	}

	return 0, errors.New("invalid Retry-After value")
}

// 指数退避计算 (修改为支持 Jitter)
func (c *Client) calculateExponentialBackoff(attempt int, jitter bool) time.Duration {
	delay := c.retryOpts.BaseDelay * time.Duration(1<<uint(attempt))
	if delay > c.retryOpts.MaxDelay {
		delay = c.retryOpts.MaxDelay
	}

	if jitter {
		// 添加 Jitter 抖动，防止 thundering herd 问题
		randomFactor := 0.8 + 0.4*float64(attempt) // 随着重试次数增加，抖动范围略微扩大
		delay = time.Duration(float64(delay) * randomFactor)
	}
	return delay
}

// 错误包装 (保持原函数不变)
func (c *Client) wrapError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ErrRequestTimeout, err)
	default:
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("%w: %v", ErrRequestTimeout, err)
		}
		return err
	}
}

// 重试条件判断 (保持原函数不变)
func (c *Client) shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return isNetworkError(err)
	}

	for _, status := range c.retryOpts.RetryStatuses {
		if resp != nil && resp.StatusCode == status { // 增加 resp != nil 判断
			return true
		}
	}
	return false
}

// 辅助函数 (保持原函数不变)
func isNetworkError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

// --- 响应处理方法 (使用 RequestBuilder 重构) ---

// DecodeJSON 解析 JSON 响应
func (rb *RequestBuilder) DecodeJSON(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	err = rb.client.decodeJSONResponse(resp, v)
	if err != nil {
		return err
	}
	return nil
}

// DecodeXML 解析 XML 响应
func (rb *RequestBuilder) DecodeXML(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeXMLResponse(resp, v)
}

// DecodeGOB 解析 GOB 响应
func (rb *RequestBuilder) DecodeGOB(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeGOBResponse(resp, v)
}

// Text 获取 Text 响应
func (rb *RequestBuilder) Text() (string, error) {
	resp, err := rb.Execute()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return rb.client.decodeTextResponse(resp)
}

// Bytes 获取 Bytes 响应
func (rb *RequestBuilder) Bytes() ([]byte, error) {
	resp, err := rb.Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return rb.client.decodeBytesResponse(resp)
}

// decodeJSONResponse 内部 JSON 响应解码
func (c *Client) decodeJSONResponse(resp *http.Response, v interface{}) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return nil
}

func (c *Client) decodeXMLResponse(resp *http.Response, v interface{}) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}
	if err := xml.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return nil
}

func (c *Client) decodeGOBResponse(resp *http.Response, v interface{}) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}
	if err := gob.NewDecoder(resp.Body).Decode(v); err != nil {
		if errors.Is(err, io.EOF) && v != nil {

			return fmt.Errorf("%w: unexpected end of data: %v", ErrDecodeResponse, err)
		}
		return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return nil
}

func (c *Client) decodeTextResponse(resp *http.Response) (string, error) {
	if resp.StatusCode >= 400 {
		return "", c.errorResponse(resp)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return string(bodyBytes), nil
}

func (c *Client) decodeBytesResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode >= 400 {
		return nil, c.errorResponse(resp)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return bodyBytes, nil
}

// HTTPError 表示一个 HTTP 错误响应 (状态码 >= 400).
// 它实现了 error 接口.
type HTTPError struct {
	StatusCode int         // HTTP 状态码
	Status     string      // HTTP 状态文本 (e.g., "Not Found")
	Header     http.Header // 响应头 (副本)
	Body       []byte      // 响应体的前缀 (用于预览)
}

func (e *HTTPError) Error() string {
	bodyPreview := string(e.Body)
	const maxPreviewLen = 200
	if len(bodyPreview) > maxPreviewLen {
		bodyPreview = bodyPreview[:maxPreviewLen] + "..."
	}
	bodyPreview = strings.TrimSpace(bodyPreview)
	return fmt.Sprintf("httpc: unexpected status %d (%s); body preview: %q",
		e.StatusCode, e.Status, bodyPreview)
}

// errorResponse 读取响应体的一小部分并返回结构化的 HTTPError.
// 它还会尝试丢弃剩余的响应体以帮助连接复用.
func (c *Client) errorResponse(resp *http.Response) error {

	// 定义为错误预览读取的最大字节数
	const maxErrorBodyRead = 1 * 1024 // 读取最多 1KB

	buf := c.bufferPool.Get()
	defer c.bufferPool.Put(buf)

	limitedReader := io.LimitReader(resp.Body, maxErrorBodyRead)
	readErr := func() error { // 使用匿名函数捕获读取错误
		_, err := io.Copy(buf, limitedReader)
		return err
	}() // 立即执行

	// *** 关键: 丢弃剩余的响应体 ***
	const maxDiscardSize = 64 * 1024
	discardErr := func() error { // 使用匿名函数捕获丢弃错误
		_, err := io.CopyN(io.Discard, resp.Body, maxDiscardSize)
		// 如果错误是 EOF，说明我们已经读完了或者超出了 maxDiscardSize，这不是一个需要报告的错误
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}() // 立即执行

	var reqCtx context.Context = context.Background()
	if resp.Request != nil {
		reqCtx = resp.Request.Context()
	}

	// 记录丢弃时发生的错误 (检查 c.dumpLog 是否为 nil)
	if discardErr != nil && c.dumpLog != nil {
		logMsg := fmt.Sprintf("httpc: warning - error discarding response body for %v", discardErr)
		c.dumpLog(reqCtx, logMsg) // 使用获取到的或默认的 Context
	}

	// 复制 Body 预览
	bodyBytes := make([]byte, buf.Len())
	copy(bodyBytes, buf.Bytes()) // 从 buf 复制，buf 会被回收

	// 复制 Header
	headerCopy := make(http.Header)
	if resp.Header != nil {
		for k, v := range resp.Header {
			headerCopy[k] = append([]string(nil), v...)
		}
	}

	// 创建结构化错误
	httpErr := &HTTPError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     headerCopy,
		Body:       bodyBytes,
	}

	// 记录读取预览时发生的错误 (检查 c.dumpLog 是否为 nil)
	// 仅在非 EOF 错误时记录
	if readErr != nil && !errors.Is(readErr, io.EOF) && c.dumpLog != nil {
		logMsg := fmt.Sprintf("httpc: warning - error reading error response body preview for %v", readErr)
		c.dumpLog(reqCtx, logMsg) // 使用获取到的或默认的 Context
	}

	return httpErr
}

// --- 标准库兼容方法 (使用 RequestBuilder 重构) ---

// NewRequest 创建请求，支持与 http.NewRequest 兼容
func (c *Client) NewRequest(method, urlStr string, body io.Reader) (*http.Request, error) {
	builder := c.NewRequestBuilder(method, urlStr).SetBody(body)
	return builder.Build()
}

// Get 发送 GET 请求
func (c *Client) Get(url string) (*http.Response, error) {
	return c.GET(url).Execute()
}

// GetContext 发送带 Context 的 GET 请求
func (c *Client) GetContext(ctx context.Context, url string) (*http.Response, error) {
	return c.GET(url).WithContext(ctx).Execute()
}

// PostJSON 发送 JSON POST 请求
func (c *Client) PostJSON(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PostXML 发送 XML POST 请求
func (c *Client) PostXML(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PostGOB 发送 GOB POST 请求
func (c *Client) PostGOB(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutJSON 发送 JSON PUT 请求
func (c *Client) PutJSON(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutXML 发送 XML PUT 请求
func (c *Client) PutXML(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutGOB 发送 GOB PUT 请求
func (c *Client) PutGOB(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// Post 发送 POST 请求
func (c *Client) Post(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.POST(url).SetBody(body).WithContext(ctx).Execute()
}

// Put 发送 PUT 请求
func (c *Client) Put(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.PUT(url).SetBody(body).WithContext(ctx).Execute()
}

// Delete 发送 DELETE 请求
func (c *Client) Delete(ctx context.Context, url string) (*http.Response, error) {
	return c.DELETE(url).WithContext(ctx).Execute()
}
