package httpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"

	"github.com/go-json-experiment/json"
)

// NewRequestBuilder 创建 RequestBuilder 实例
func (c *Client) NewRequestBuilder(method, urlStr string) *RequestBuilder {
	return &RequestBuilder{
		client:  c,
		method:  method,
		url:     urlStr,
		header:  make(http.Header),
		query:   make(url.Values),
		context: context.Background(), // 默认使用 Background Context
	}
}

// GET, POST, PUT, DELETE 等快捷方法
func (c *Client) GET(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodGet, urlStr)
}

func (c *Client) POST(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPost, urlStr)
}

func (c *Client) PUT(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPut, urlStr)
}

func (c *Client) DELETE(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodDelete, urlStr)
}

func (c *Client) PATCH(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodPatch, urlStr)
}

func (c *Client) HEAD(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodHead, urlStr)
}

func (c *Client) OPTIONS(urlStr string) *RequestBuilder {
	return c.NewRequestBuilder(http.MethodOptions, urlStr)
}

// WithContext 设置 Context
func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	rb.context = ctx
	return rb
}

// NoDefaultHeaders 设置请求不添加默认 Header
func (rb *RequestBuilder) NoDefaultHeaders() *RequestBuilder {
	rb.noDefaultHeaders = true
	return rb
}

// SetHeader 设置 Header
func (rb *RequestBuilder) SetHeader(key, value string) *RequestBuilder {
	rb.header.Set(key, value)
	return rb
}

// AddHeader 添加 Header
func (rb *RequestBuilder) AddHeader(key, value string) *RequestBuilder {
	rb.header.Add(key, value)
	return rb
}

// SetHeaders 批量设置 Headers
func (rb *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder {
	for key, value := range headers {
		rb.header.Set(key, value)
	}
	return rb
}

// SetQueryParam 设置 Query 参数
func (rb *RequestBuilder) SetQueryParam(key, value string) *RequestBuilder {
	rb.query.Set(key, value)
	return rb
}

// AddQueryParam 添加 Query 参数
func (rb *RequestBuilder) AddQueryParam(key, value string) *RequestBuilder {
	rb.query.Add(key, value)
	return rb
}

// SetQueryParams 批量设置 Query 参数
func (rb *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	for key, value := range params {
		rb.query.Set(key, value)
	}
	return rb
}

// SetBody 设置 Body (io.Reader)
func (rb *RequestBuilder) SetBody(body io.Reader) *RequestBuilder {
	rb.body = body
	return rb
}

// SetRawBody 设置 Body ([]byte)
func (rb *RequestBuilder) SetRawBody(body []byte) *RequestBuilder {
	rb.body = bytes.NewReader(body)
	return rb
}

// SetJSONBody 设置 JSON Body
func (rb *RequestBuilder) SetJSONBody(body any) (*RequestBuilder, error) {
	pr, pw := io.Pipe()
	rb.body = pr
	rb.header.Set("Content-Type", "application/json")

	go func() {
		var err error
		defer func() {
			pw.CloseWithError(err)
		}()

		err = json.MarshalWrite(pw, body)
	}()
	return rb, nil
}

// SetXMLBody 设置 XML Body
func (rb *RequestBuilder) SetXMLBody(body any) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	if err := xml.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode xml body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/xml")
	return rb, nil
}

// SetGOBBody 设置GOB Body
func (rb *RequestBuilder) SetGOBBody(body any) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	// 使用 gob 编码
	if err := gob.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode gob body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/octet-stream") // 设置合适的 Content-Type
	return rb, nil
}

// Build 构建 http.Request
func (rb *RequestBuilder) Build() (*http.Request, error) {

	reqURL, err := url.Parse(rb.url)
	if err != nil {
		return nil, fmt.Errorf("%w: %s, error: %v", ErrInvalidURL, rb.url, err)
	}
	if len(rb.query) > 0 {
		q := reqURL.Query()
		for k, v := range rb.query {
			for _, val := range v {
				q.Add(k, val)
			}
		}
		reqURL.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(rb.context, rb.method, reqURL.String(), rb.body)
	if err != nil {
		return nil, err
	}
	maps.Copy(req.Header, rb.header)
	if !rb.noDefaultHeaders && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", rb.client.userAgent)
	}
	return req, nil
}

// Execute 执行请求并返回 http.Response
func (rb *RequestBuilder) Execute() (*http.Response, error) {
	req, err := rb.Build()
	if err != nil {
		return nil, err
	}
	return rb.client.Do(req)
}
