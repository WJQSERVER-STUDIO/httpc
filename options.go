package httpc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"golang.org/x/net/proxy"
)

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

// WithSocks5Proxy 设置 SOCKS5 代理
// proxyURL: SOCKS5 代理地址, 例如 "socks5://user:password@host:port"
// 如果代理不需要认证, 可以省略 user:password, 例如 "socks5://host:port"
func WithSocks5Proxy(proxyURL string) Option {
	return func(c *Client) {
		proxyURI, err := url.Parse(proxyURL)
		if err != nil {
			return
		}

		dialer, err := proxy.FromURL(proxyURI, c.dialer)
		if err != nil {
			return
		}

		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return
		}

		c.transport.DialContext = contextDialer.DialContext
	}
}

// WithHTTPProxy 设置 HTTP/HTTPS 代理
// proxyURL: HTTP/HTTPS 代理地址, 例如 "http://user:password@host:port"
func WithHTTPProxy(proxyURL string) Option {
	return func(c *Client) {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return
		}
		c.transport.Proxy = http.ProxyURL(proxy)
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
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
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
