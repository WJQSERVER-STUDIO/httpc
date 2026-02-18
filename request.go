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

// RequestBuilder 用于构建 HTTP 请求
type RequestBuilder struct {
	client  *Client
	method  string
	rawURL  string
	header  http.Header
	params  url.Values
	body    io.Reader
	ctx     context.Context
	built   bool
	lastReq *http.Request
}

func (c *Client) NewRequestBuilder(method, urlStr string) *RequestBuilder {
	return &RequestBuilder{
		client: c,
		method: method,
		rawURL: urlStr,
		header: make(http.Header),
		params: make(url.Values),
		ctx:    context.Background(),
	}
}

func (c *Client) GET(urlStr string) *RequestBuilder     { return c.NewRequestBuilder("GET", urlStr) }
func (c *Client) POST(urlStr string) *RequestBuilder    { return c.NewRequestBuilder("POST", urlStr) }
func (c *Client) PUT(urlStr string) *RequestBuilder     { return c.NewRequestBuilder("PUT", urlStr) }
func (c *Client) DELETE(urlStr string) *RequestBuilder  { return c.NewRequestBuilder("DELETE", urlStr) }
func (c *Client) PATCH(urlStr string) *RequestBuilder   { return c.NewRequestBuilder("PATCH", urlStr) }
func (c *Client) HEAD(urlStr string) *RequestBuilder    { return c.NewRequestBuilder("HEAD", urlStr) }
func (rb *RequestBuilder) OPTIONS(urlStr string) *RequestBuilder { return rbMethod(rb, "OPTIONS", urlStr) }

func rbMethod(rb *RequestBuilder, method, urlStr string) *RequestBuilder {
	rb.method = method
	rb.rawURL = urlStr
	return rb
}

func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	if ctx == nil {
		ctx = context.Background()
	}
	rb.ctx = ctx
	return rb
}

func (rb *RequestBuilder) SetHeader(key, value string) *RequestBuilder {
	rb.header.Set(key, value)
	return rb
}

func (rb *RequestBuilder) AddHeader(key, value string) *RequestBuilder {
	rb.header.Add(key, value)
	return rb
}

func (rb *RequestBuilder) SetHeaders(headers map[string]string) *RequestBuilder {
	for k, v := range headers {
		rb.header.Set(k, v)
	}
	return rb
}

func (rb *RequestBuilder) SetQueryParam(key, value string) *RequestBuilder {
	rb.params.Set(key, value)
	return rb
}

func (rb *RequestBuilder) AddQueryParam(key, value string) *RequestBuilder {
	rb.params.Add(key, value)
	return rb
}

func (rb *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	for k, v := range params {
		rb.params.Set(k, v)
	}
	return rb
}

func (rb *RequestBuilder) SetBody(body io.Reader) *RequestBuilder {
	rb.body = body
	return rb
}

func (rb *RequestBuilder) SetJSONBody(v any) (*RequestBuilder, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	rb.body = bytes.NewReader(data)
	rb.SetHeader("Content-Type", "application/json")
	return rb, nil
}

func (rb *RequestBuilder) SetXMLBody(v any) (*RequestBuilder, error) {
	var buf bytes.Buffer
	if err := xml.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.SetHeader("Content-Type", "application/xml")
	return rb, nil
}

func (rb *RequestBuilder) SetGOBBody(v any) (*RequestBuilder, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.SetHeader("Content-Type", "application/octet-stream")
	return rb, nil
}

func (rb *RequestBuilder) Build() (*http.Request, error) {
	u, err := url.Parse(rb.rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	if len(rb.params) > 0 {
		q := u.Query()
		for k, v := range rb.params {
			for _, vv := range v {
				q.Add(k, vv)
			}
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(rb.ctx, rb.method, u.String(), rb.body)
	if err != nil {
		return nil, err
	}

	maps.Copy(req.Header, rb.header)
	rb.lastReq = req
	rb.built = true
	return req, nil
}

func (rb *RequestBuilder) Execute() (*http.Response, error) {
	req, err := rb.Build()
	if err != nil {
		return nil, err
	}
	return rb.client.Do(req)
}

func (rb *RequestBuilder) DecodeJSON(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeJSONResponse(resp, v)
}

func (rb *RequestBuilder) DecodeXML(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeXMLResponse(resp, v)
}

func (rb *RequestBuilder) DecodeGOB(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeGOBResponse(resp, v)
}

func (rb *RequestBuilder) Text() (string, error) {
	resp, err := rb.Execute()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return rb.client.decodeTextResponse(resp)
}

func (rb *RequestBuilder) Bytes() ([]byte, error) {
	resp, err := rb.Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return rb.client.decodeBytesResponse(resp)
}
