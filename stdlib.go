package httpc

import (
	"context"
	"io"
	"net/http"
)

// --- 标准库兼容方法 (使用 RequestBuilder 重构) ---

// NewRequest 创建请求，支持与 http.NewRequest 兼容
func (c *Client) NewRequest(method, urlStr string, body io.Reader) (*http.Request, error) {
	builder := c.NewRequestBuilder(method, urlStr).SetBody(body)
	return builder.Build()
}

// Get 发送 GET 请求
func (c *Client) Get(url string) (*http.Response, error) {
	return c.GET(url).Execute()
}

// GetContext 发送带 Context 的 GET 请求
func (c *Client) GetContext(ctx context.Context, url string) (*http.Response, error) {
	return c.GET(url).WithContext(ctx).Execute()
}

// PostJSON 发送 JSON POST 请求
func (c *Client) PostJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PostXML 发送 XML POST 请求
func (c *Client) PostXML(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PostGOB 发送 GOB POST 请求
func (c *Client) PostGOB(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.POST(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutJSON 发送 JSON PUT 请求
func (c *Client) PutJSON(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetJSONBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutXML 发送 XML PUT 请求
func (c *Client) PutXML(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetXMLBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// PutGOB 发送 GOB PUT 请求
func (c *Client) PutGOB(ctx context.Context, url string, body any) (*http.Response, error) {
	builder := c.PUT(url)
	_, err := builder.SetGOBBody(body)
	if err != nil {
		return nil, err
	}
	return builder.WithContext(ctx).Execute()
}

// Post 发送 POST 请求
func (c *Client) Post(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.POST(url).SetBody(body).WithContext(ctx).Execute()
}

// Put 发送 PUT 请求
func (c *Client) Put(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.PUT(url).SetBody(body).WithContext(ctx).Execute()
}

// Delete 发送 DELETE 请求
func (c *Client) Delete(ctx context.Context, url string) (*http.Response, error) {
	return c.DELETE(url).WithContext(ctx).Execute()
}
