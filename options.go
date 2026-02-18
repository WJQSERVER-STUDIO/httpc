package httpc

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
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

// RetryOptions 重试配置
type RetryOptions struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	RetryStatuses []int
	Jitter        bool // 是否启用 Jitter 抖动
}

// ProtocolsConfig 协议配置
type ProtocolsConfig struct {
	Http1           bool
	Http2           bool
	Http2_Cleartext bool
	ForceH2C        bool
}

// DumpLogFunc 定义日志记录函数
type DumpLogFunc func(ctx context.Context, log string)

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
		c.dialer.Timeout = dialTimeout
		c.transport.DialContext = c.dialer.DialContext
	}
}

// WithKeepAliveTimeout 设置 KeepAlive 超时时间
func WithKeepAliveTimeout(keepAliveTimeout time.Duration) Option {
	return func(c *Client) {
		c.dialer.KeepAlive = keepAliveTimeout
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

// WithMaxBufferPoolSize 设置最大缓冲池大小 (虽然目前仅用于回收检查)
func WithMaxBufferPoolSize(maxPoolSize int) Option {
	return func(c *Client) {
		c.maxBufferPool = maxPoolSize
	}
}

// WithTimeout 设置默认超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
		c.client.Timeout = timeout
	}
}

// WithBufferPool 提供自定义缓冲池实现
func WithBufferPool(pool BufferPool) Option {
	return func(c *Client) {
		if pool != nil {
			c.bufferPool = pool
		}
	}
}

// WithRetryOptions 设置自定义重试策略
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

// WithDumpLog 启用默认日志记录
func WithDumpLog() Option {
	return func(c *Client) {
		c.dumpLog = func(ctx context.Context, log string) {
			println(log)
		}
	}
}

// WithDumpLogFunc 提供自定义日志记录函数
func WithDumpLogFunc(f DumpLogFunc) Option {
	return func(c *Client) {
		c.dumpLog = f
	}
}

// WithMiddleware 添加中间件
func WithMiddleware(m ...MiddlewareFunc) Option {
	return func(c *Client) {
		c.middlewares = append(c.middlewares, m...)
	}
}

// WithProtocols 配置 HTTP 协议版本
func WithProtocols(config ProtocolsConfig) Option {
	return func(c *Client) {
		c.SetProtocols(config)
	}
}

// WithHTTPProxy 设置 HTTP 代理
func WithHTTPProxy(proxyURL string) Option {
	return func(c *Client) {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return
		}
		c.transport.Proxy = http.ProxyURL(u)
	}
}

// WithSocks5Proxy 设置 SOCKS5 代理
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
		if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
			c.transport.DialContext = contextDialer.DialContext
		}
	}
}

// WithDNSResolver 设置自定义 DNS 解析器
func WithDNSResolver(servers []string, timeout time.Duration) Option {
	return func(c *Client) {
		if len(servers) == 0 {
			return
		}
		if timeout <= 0 {
			timeout = defaultResolverTimeout
		}

		customDialer := &customDialer{
			defaultDialer: c.dialer,
			dnsServers:    servers,
			dnsTimeout:    timeout,
		}
		c.transport.DialContext = customDialer.DialContext
	}
}
