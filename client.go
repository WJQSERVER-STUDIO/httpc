package httpc

import (
	"context"
	"encoding/gob"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/WJQSERVER-STUDIO/go-utils/iox"
	"github.com/go-json-experiment/json"
)

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

// New 创建一个新的 httpc 客户端
func New(opts ...Option) *Client {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAliveTimeout,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          defaultMaxIdleConns,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: defaultExpectContinueTimeout,
		ForceAttemptHTTP2:     true,
	}

	c := &Client{
		client: &http.Client{
			Transport: transport,
		},
		transport:     transport,
		dialer:        dialer,
		userAgent:     defaultUserAgent,
		maxIdleConns:  defaultMaxIdleConns,
		bufferSize:    defaultBufferSize,
		maxBufferPool: defaultMaxBufferPool,
		retryOpts:     defaultRetryOptions(),
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.bufferPool == nil {
		c.bufferPool = newDefaultPool(c.bufferSize)
	}

	c.transport.MaxIdleConns = c.maxIdleConns
	c.transport.MaxIdleConnsPerHost = c.maxIdleConns / 2

	return c
}

func defaultRetryOptions() RetryOptions {
	return RetryOptions{
		MaxAttempts:   2,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		RetryStatuses: []int{429, 500, 502, 503, 504},
		Jitter:        false,
	}
}

// SetRetryOptions 动态修改重试选项
func (c *Client) SetRetryOptions(opts RetryOptions) {
	c.retryOpts = opts
}

// SetDumpLogFunc 动态设置日志记录函数
func (c *Client) SetDumpLogFunc(f DumpLogFunc) {
	c.dumpLog = f
}

// SetTimeout 动态设置超时时间
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
	c.client.Timeout = timeout
}

// SetProtocols 动态配置协议
func (c *Client) SetProtocols(config ProtocolsConfig) {
	if c.transport.Protocols == nil {
		c.transport.Protocols = new(http.Protocols)
	}

	if config.ForceH2C {
		c.transport.Protocols.SetHTTP1(false)
		c.transport.Protocols.SetHTTP2(false)
		c.transport.Protocols.SetUnencryptedHTTP2(true)
		c.transport.ForceAttemptHTTP2 = false
		return
	}

	c.transport.Protocols.SetHTTP1(config.Http1)
	c.transport.Protocols.SetHTTP2(config.Http2)
	c.transport.Protocols.SetUnencryptedHTTP2(config.Http2_Cleartext)
	c.transport.ForceAttemptHTTP2 = config.Http2 || config.Http2_Cleartext
}

// Do 执行请求
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	var finalRT http.RoundTripper = c.transport

	for i := len(c.middlewares) - 1; i >= 0; i-- {
		finalRT = c.middlewares[i](finalRT)
	}

	if c.dumpLog != nil {
		finalRT = c.logRoundTripper(finalRT)
	}

	if c.retryOpts.MaxAttempts > 0 {
		finalRT = c.retryRoundTripper(finalRT)
	}

	return finalRT.RoundTrip(req)
}

func (c *Client) logRoundTripper(next http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		c.logRequest(req)
		return next.RoundTrip(req)
	})
}

func (c *Client) retryRoundTripper(next http.RoundTripper) http.RoundTripper {
	return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var bodyReaderFunc func() (io.ReadCloser, error)
		if req.GetBody != nil {
			bodyReaderFunc = req.GetBody
		}

		var lastResp *http.Response
		var lastErr error

		for attempt := 0; attempt <= c.retryOpts.MaxAttempts; attempt++ {
			if attempt > 0 {
				if bodyReaderFunc != nil {
					newBody, err := bodyReaderFunc()
					if err != nil {
						if lastResp != nil {
							lastResp.Body.Close()
						}
						return nil, fmt.Errorf("httpc: failed to get request body for retry attempt %d: %w", attempt, err)
					}
					req.Body = newBody
				} else if req.Body != nil && req.Body != http.NoBody {
					break
				}
			}

			select {
			case <-req.Context().Done():
				if lastResp != nil {
					lastResp.Body.Close()
				}
				return nil, c.wrapError(req.Context().Err())
			default:
			}

			resp, err := next.RoundTrip(req)
			lastResp, lastErr = resp, err

			if !c.shouldRetry(resp, err) {
				break
			}

			if attempt >= c.retryOpts.MaxAttempts {
				lastErr = ErrMaxRetriesExceeded
				break
			}

			delay := c.calculateRetryAfter(resp)
			if delay <= 0 {
				delay = c.calculateExponentialBackoff(attempt, c.retryOpts.Jitter)
			}

			if resp != nil && resp.Body != nil {
				iox.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}

			select {
			case <-req.Context().Done():
				return nil, c.wrapError(req.Context().Err())
			case <-time.After(delay):
			}
		}

		if lastErr != nil {
			return lastResp, c.wrapError(lastErr)
		}
		return lastResp, nil
	})
}

func (c *Client) logRequest(req *http.Request) {
	if c.dumpLog == nil {
		return
	}

	sb := stringsBuilderPool.Get().(*strings.Builder)
	defer func() {
		sb.Reset()
		stringsBuilderPool.Put(sb)
	}()

	sb.WriteString("\n[HTTP Request Log]\n")
	sb.WriteString("-------------------------------\n")
	sb.WriteString("Time       : ")
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05\n"))
	sb.WriteString("Method     : ")
	sb.WriteString(req.Method)
	sb.WriteByte('\n')
	sb.WriteString("URL        : ")
	sb.WriteString(req.URL.String())
	sb.WriteByte('\n')
	sb.WriteString("Host       : ")
	sb.WriteString(req.URL.Host)
	sb.WriteByte('\n')
	sb.WriteString("Protocol   : ")
	sb.WriteString(req.Proto)
	sb.WriteByte('\n')
	sb.WriteString("Transport  :\n")
	getTransportDetails(c.transport, sb)
	sb.WriteString("Headers    :\n")
	formatHeaders(req.Header, sb)
	sb.WriteString("-------------------------------\n")

	c.dumpLog(req.Context(), sb.String())
}

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

func (c *Client) calculateExponentialBackoff(attempt int, jitter bool) time.Duration {
	delay := c.retryOpts.BaseDelay * time.Duration(1<<uint(attempt))
	if delay > c.retryOpts.MaxDelay {
		delay = c.retryOpts.MaxDelay
	}

	if jitter {
		randomFactor := 0.8 + 0.4*float64(attempt)
		delay = time.Duration(float64(delay) * randomFactor)
	}
	return delay
}

func (c *Client) shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return isNetworkError(err)
	}
	for _, status := range c.retryOpts.RetryStatuses {
		if resp != nil && resp.StatusCode == status {
			return true
		}
	}
	return false
}

func (c *Client) decodeJSONResponse(resp *http.Response, obj any) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}
	err := json.UnmarshalRead(resp.Body, obj)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return nil
}

func (c *Client) decodeXMLResponse(resp *http.Response, v any) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}
	if err := xml.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
	}
	return nil
}

func (c *Client) decodeGOBResponse(resp *http.Response, v any) error {
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
	bodyBytes, err := iox.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, ErrDecodeResponse)
	}
	return string(bodyBytes), nil
}

func (c *Client) decodeBytesResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode >= 400 {
		return nil, c.errorResponse(resp)
	}
	bodyBytes, err := iox.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, ErrDecodeResponse)
	}
	return bodyBytes, nil
}

// --- 标准库兼容方法 ---

func (c *Client) NewRequest(method, urlStr string, body io.Reader) (*http.Request, error) {
	builder := c.NewRequestBuilder(method, urlStr).SetBody(body)
	return builder.Build()
}

func (c *Client) Get(url string) (*http.Response, error) {
	return c.GET(url).Execute()
}

func (c *Client) GetContext(ctx context.Context, url string) (*http.Response, error) {
	return c.GET(url).WithContext(ctx).Execute()
}

func (c *Client) PostJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) PostXML(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) PostGOB(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) PutJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) PutXML(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) PutGOB(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

func (c *Client) Post(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.POST(url).SetBody(body).WithContext(ctx).Execute()
}

func (c *Client) Put(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.PUT(url).SetBody(body).WithContext(ctx).Execute()
}

func (c *Client) Delete(ctx context.Context, url string) (*http.Response, error) {
	return c.DELETE(url).WithContext(ctx).Execute()
}
