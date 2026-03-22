package httpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/WJQSERVER-STUDIO/go-utils/iox"
)

// 错误定义
var (
	ErrRequestTimeout     = errors.New("httpc: request timeout")
	ErrMaxRetriesExceeded = errors.New("httpc: max retries exceeded")
	ErrDecodeResponse     = errors.New("httpc: failed to decode response body")
	ErrInvalidURL         = errors.New("httpc: invalid URL")
	ErrInvalidSSEStream   = errors.New("httpc: invalid SSE stream")
	ErrNoResponse         = errors.New("httpc: no response")
)

var ErrShortWrite = errors.New("short write")
var EOF = io.EOF

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

	if resp == nil {
		return ErrNoResponse
	}

	// 定义为错误预览读取的最大字节数
	const maxErrorBodyRead = 1 * 1024 // 读取最多 1KB

	buf := c.bufferPool.Get()
	defer c.bufferPool.Put(buf)

	limitedReader := io.LimitReader(resp.Body, maxErrorBodyRead)
	readErr := func() error { // 使用匿名函数捕获读取错误
		_, err := iox.Copy(buf, limitedReader)
		return err
	}() // 立即执行

	// *** 关键: 丢弃剩余的响应体 ***
	const maxDiscardSize = 64 * 1024
	discardErr := func() error { // 使用匿名函数捕获丢弃错误
		_, err := iox.CopyN(io.Discard, resp.Body, maxDiscardSize)
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
