package httpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WJQSERVER-STUDIO/go-utils/iox"
)

func (c *Client) Do(req *http.Request) (*http.Response, error) {
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
				iox.Copy(io.Discard, resp.Body)
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

// 记录请求日志, 使用 strings.Builder 和 sync.Pool 优化性能
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

// 获取 Transport 的详细信息
func getTransportDetails(transport http.RoundTripper, sb *strings.Builder) {
	if t, ok := transport.(*http.Transport); ok {
		sb.WriteString("  Type                 : *http.Transport\n")
		sb.WriteString("  MaxIdleConns         : ")
		sb.WriteString(strconv.Itoa(t.MaxIdleConns))
		sb.WriteByte('\n')
		sb.WriteString("  MaxIdleConnsPerHost  : ")
		sb.WriteString(strconv.Itoa(t.MaxIdleConnsPerHost))
		sb.WriteByte('\n')
		sb.WriteString("  MaxConnsPerHost      : ")
		sb.WriteString(strconv.Itoa(t.MaxConnsPerHost))
		sb.WriteByte('\n')
		sb.WriteString("  IdleConnTimeout      : ")
		sb.WriteString(t.IdleConnTimeout.String())
		sb.WriteByte('\n')
		sb.WriteString("  TLSHandshakeTimeout  : ")
		sb.WriteString(t.TLSHandshakeTimeout.String())
		sb.WriteByte('\n')
		sb.WriteString("  DisableKeepAlives    : ")
		sb.WriteString(strconv.FormatBool(t.DisableKeepAlives))
		sb.WriteByte('\n')
		sb.WriteString("  WriteBufferSize      : ")
		sb.WriteString(strconv.Itoa(t.WriteBufferSize))
		sb.WriteByte('\n')
		sb.WriteString("  ReadBufferSize       : ")
		sb.WriteString(strconv.Itoa(t.ReadBufferSize))
		sb.WriteByte('\n')
		sb.WriteString("  Protocol             : ")
		fmt.Fprintf(sb, "%v\n", t.Protocols) // 协议部分结构复杂, 暂时保留 Fprintf
		return
	}

	if transport != nil {
		sb.WriteString("  Type                 : ")
		fmt.Fprintf(sb, "%T\n", transport)
		return
	}

	sb.WriteString("  Type                 : nil\n")
}

// 格式化请求头为多行字符串
func formatHeaders(headers http.Header, sb *strings.Builder) {
	for key, values := range headers {
		sb.WriteString("  ")
		sb.WriteString(key)
		sb.WriteString(": ")
		sb.WriteString(strings.Join(values, ", "))
		sb.WriteByte('\n')
	}
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

// 指数退避计算，启用 jitter 时在 [0.5, 1.5) 区间内随机扰动。
func (c *Client) calculateExponentialBackoff(attempt int, jitter bool) time.Duration {
	delay := min(c.retryOpts.BaseDelay*time.Duration(1<<uint(attempt)), c.retryOpts.MaxDelay)

	if jitter {
		randomFactor := 0.5 + c.randomFloat64()
		delay = time.Duration(float64(delay) * randomFactor)
		if delay > c.retryOpts.MaxDelay {
			return c.retryOpts.MaxDelay
		}
		if delay < 0 {
			return 0
		}
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
