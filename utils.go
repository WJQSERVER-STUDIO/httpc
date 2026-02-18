package httpc

import (
	"errors"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// mergeTransport 合并两个 http.Transport 的非零字段
func mergeTransport(dst, src *http.Transport) {
	if src == nil {
		return
	}
	d := reflect.ValueOf(dst).Elem()
	s := reflect.ValueOf(src).Elem()
	for i := 0; i < s.NumField(); i++ {
		sf := s.Field(i)
		if !sf.IsZero() && d.Field(i).CanSet() {
			d.Field(i).Set(sf)
		}
	}
}

// isNetworkError 辅助函数
func isNetworkError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

// parseRetryAfter 解析 Retry-After
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

// getTransportDetails 获取 Transport 的详细信息
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
	}
}

// formatHeaders 格式化头部信息
func formatHeaders(headers http.Header, sb *strings.Builder) {
	for key, values := range headers {
		sb.WriteString("  ")
		sb.WriteString(key)
		sb.WriteString(": ")
		sb.WriteString(strings.Join(values, ", "))
		sb.WriteByte('\n')
	}
}
