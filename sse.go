package httpc

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// SSEEvent 表示一个服务器发送事件.
// 字段设计与 touka 的 SSE 事件结构保持兼容.
type SSEEvent struct {
	Event string
	Data  string
	Id    string
	Retry string
}

// Render 将事件按 SSE 线格式写入指定 writer.
func (e *SSEEvent) Render(w io.Writer) error {
	var buf bytes.Buffer

	if len(e.Id) > 0 {
		buf.WriteString("id: ")
		buf.WriteString(e.Id)
		buf.WriteByte('\n')
	}
	if len(e.Event) > 0 {
		buf.WriteString("event: ")
		buf.WriteString(e.Event)
		buf.WriteByte('\n')
	}
	if len(e.Data) > 0 {
		for line := range strings.SplitSeq(e.Data, "\n") {
			buf.WriteString("data: ")
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	if len(e.Retry) > 0 {
		buf.WriteString("retry: ")
		buf.WriteString(e.Retry)
		buf.WriteByte('\n')
	}

	buf.WriteByte('\n')
	_, err := buf.WriteTo(w)
	return err
}

// SSEStream 表示一个已经建立的 SSE 流连接.
type SSEStream struct {
	resp   *http.Response
	reader *bufio.Reader
	closed bool
}

// Response 返回建立流时的原始 HTTP 响应.
func (s *SSEStream) Response() *http.Response {
	if s == nil {
		return nil
	}
	return s.resp
}

// Close 关闭底层 SSE 响应体.
func (s *SSEStream) Close() error {
	if s == nil || s.resp == nil || s.resp.Body == nil || s.closed {
		return nil
	}
	s.closed = true
	return s.resp.Body.Close()
}

// Next 读取下一个完整的 SSE 事件帧.
// 它按 SSE 协议逐行解析，并忽略注释行与空的 keepalive 块.
func (s *SSEStream) Next() (*SSEEvent, error) {
	if s == nil || s.reader == nil {
		return nil, io.EOF
	}

	var event SSEEvent
	var dataLines []string
	hasFields := false

	for {
		line, eof, err := readSSELine(s.reader)
		if err != nil {
			return nil, err
		}

		if line == "" {
			if !hasFields {
				if eof {
					return nil, io.EOF
				}
				continue
			}
			event.Data = strings.Join(dataLines, "\n")
			return &event, nil
		}

		if strings.HasPrefix(line, ":") {
			if eof {
				return nil, io.EOF
			}
			continue
		}

		field, value := parseSSEField(line)
		switch field {
		case "event":
			event.Event = value
			hasFields = true
		case "data":
			dataLines = append(dataLines, value)
			hasFields = true
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				event.Id = value
				hasFields = true
			}
		case "retry":
			event.Retry = value
			hasFields = true
		}

		if eof {
			return nil, io.EOF
		}
	}
}

// SSE 建立一个 SSE 流，并返回流式解析器.
func (rb *RequestBuilder) SSE() (*SSEStream, error) {
	if rb.header.Get("Accept") == "" {
		rb.header.Set("Accept", "text/event-stream")
	}

	resp, err := rb.Execute()
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		httpErr := rb.client.errorResponse(resp)
		resp.Body.Close()
		return nil, httpErr
	}

	contentType := resp.Header.Get("Content-Type")
	if !isSSEContentType(contentType) {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: unexpected Content-Type %q", ErrInvalidSSEStream, contentType)
	}

	return &SSEStream{
		resp:   resp,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

// GetSSE 使用 GET 请求建立一个 SSE 流.
func (c *Client) GetSSE(ctx context.Context, url string) (*SSEStream, error) {
	return c.GET(url).WithContext(ctx).SSE()
}

func isSSEContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	return mediaType == "text/event-stream" || strings.HasPrefix(mediaType, "text/event-stream;")
}

func parseSSEField(line string) (field, value string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}

	field = line[:idx]
	value = line[idx+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return field, value
}

func readSSELine(r *bufio.Reader) (line string, eof bool, err error) {
	var buf bytes.Buffer

	for {
		b, readErr := r.ReadByte()
		if readErr != nil {
			if readErr == io.EOF {
				if buf.Len() == 0 {
					return "", true, nil
				}
				return buf.String(), true, nil
			}
			return "", false, readErr
		}

		switch b {
		case '\n':
			return buf.String(), false, nil
		case '\r':
			if next, peekErr := r.Peek(1); peekErr == nil && len(next) == 1 && next[0] == '\n' {
				_, _ = r.ReadByte()
			}
			return buf.String(), false, nil
		default:
			buf.WriteByte(b)
		}
	}
}
