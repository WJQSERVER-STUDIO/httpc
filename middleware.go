package httpc

import "net/http"

// RoundTripperFunc 是一个适配器，允许使用普通函数作为 HTTP RoundTripper
type RoundTripperFunc func(req *http.Request) (*http.Response, error)

// RoundTrip 实现了 http.RoundTripper 接口
func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// MiddlewareFunc 是客户端中间件的类型
// 它接收一个 http.RoundTripper (代表下一个处理器) 并返回一个新的 http.RoundTripper
type MiddlewareFunc func(next http.RoundTripper) http.RoundTripper
