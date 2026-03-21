package httpc

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
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
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
	},
}

var stringsBuilderPool = sync.Pool{
	New: func() any {
		return &strings.Builder{}
	},
}

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

// ProtocolsConfig 协议版本配置结构体
type ProtocolsConfig struct {
	Http1           bool // 是否启用 HTTP/1.1
	Http2           bool // 是否启用 HTTP/2
	Http2_Cleartext bool // 是否启用 H2C
	ForceH2C        bool // 是否强制启用 H2C
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
