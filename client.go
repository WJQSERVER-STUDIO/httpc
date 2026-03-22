package httpc

import (
	"math/rand/v2"
	"net"
	"net/http"
	"runtime"
	"time"
)

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
		randomFloat64: rand.Float64,
		bufferPool:    newDefaultPool(defaultBufferSize),
		userAgent:     defaultUserAgent,
		dumpLog:       nil, // 默认不启用日志
		maxIdleConns:  defaultMaxIdleConns,
		bufferSize:    defaultBufferSize,
		maxBufferPool: defaultMaxBufferPool,
		timeout:       0, // 默认不设置全局超时
		middlewares:   []MiddlewareFunc{},
		dialer:        dialer,
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
