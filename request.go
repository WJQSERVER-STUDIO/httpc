package httpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-json-experiment/json"
)

// RequestBuilder 用于构建 HTTP 请求
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

func (c *Client) NewRequestBuilder(method, urlStr string) *RequestBuilder {
	return &RequestBuilder{
		client:  c,
		method:  method,
		url:     urlStr,
		header:  make(http.Header),
		query:   make(url.Values),
		context: context.Background(),
	}
}

func (c *Client) GET(urlStr string) *RequestBuilder    { return c.NewRequestBuilder("GET", urlStr) }
func (c *Client) POST(urlStr string) *RequestBuilder   { return c.NewRequestBuilder("POST", urlStr) }
func (c *Client) PUT(urlStr string) *RequestBuilder    { return c.NewRequestBuilder("PUT", urlStr) }
func (c *Client) DELETE(urlStr string) *RequestBuilder { return c.NewRequestBuilder("DELETE", urlStr) }
func (c *Client) PATCH(urlStr string) *RequestBuilder  { return c.NewRequestBuilder("PATCH", urlStr) }
func (c *Client) HEAD(urlStr string) *RequestBuilder   { return c.NewRequestBuilder("HEAD", urlStr) }
func (c *Client) OPTIONS(urlStr string) *RequestBuilder { return c.NewRequestBuilder("OPTIONS", urlStr) }

func (rb *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	if ctx == nil {
		ctx = context.Background()
	}
	rb.context = ctx
	return rb
}

func (rb *RequestBuilder) NoDefaultHeaders() *RequestBuilder {
	rb.noDefaultHeaders = true
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
	rb.query.Set(key, value)
	return rb
}

func (rb *RequestBuilder) AddQueryParam(key, value string) *RequestBuilder {
	rb.query.Add(key, value)
	return rb
}

func (rb *RequestBuilder) SetQueryParams(params map[string]string) *RequestBuilder {
	for k, v := range params {
		rb.query.Set(k, v)
	}
	return rb
}

func (rb *RequestBuilder) SetBody(body io.Reader) *RequestBuilder {
	rb.body = body
	return rb
}

func (rb *RequestBuilder) SetRawBody(body []byte) *RequestBuilder {
	rb.body = bytes.NewReader(body)
	return rb
}

func (rb *RequestBuilder) SetJSONBody(body interface{}) (*RequestBuilder, error) {
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

func (rb *RequestBuilder) SetXMLBody(body interface{}) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	if err := xml.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode xml body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/xml")
	return rb, nil
}

func (rb *RequestBuilder) SetGOBBody(body interface{}) (*RequestBuilder, error) {
	buf := rb.client.bufferPool.Get()
	defer rb.client.bufferPool.Put(buf)

	if err := gob.NewEncoder(buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode gob body error: %w", err)
	}
	rb.body = bytes.NewReader(buf.Bytes())
	rb.header.Set("Content-Type", "application/octet-stream")
	return rb, nil
}

func (rb *RequestBuilder) Build() (*http.Request, error) {
	u, err := url.Parse(rb.url)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	if len(rb.query) > 0 {
		q := u.Query()
		for k, v := range rb.query {
			for _, vv := range v {
				q.Add(k, vv)
			}
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(rb.context, rb.method, u.String(), rb.body)
	if err != nil {
		return nil, err
	}

	for k, v := range rb.header {
		req.Header[k] = v
	}
	if !rb.noDefaultHeaders && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", rb.client.userAgent)
	}
	return req, nil
}

func (rb *RequestBuilder) Execute() (*http.Response, error) {
	req, err := rb.Build()
	if err != nil {
		return nil, err
	}
	return rb.client.Do(req)
}

func (rb *RequestBuilder) DecodeJSON(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	return rb.client.decodeJSONResponse(resp, v)
}

func (rb *RequestBuilder) DecodeXML(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	return rb.client.decodeXMLResponse(resp, v)
}

func (rb *RequestBuilder) DecodeGOB(v interface{}) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	return rb.client.decodeGOBResponse(resp, v)
}

func (rb *RequestBuilder) Text() (string, error) {
	resp, err := rb.Execute()
	if err != nil {
		return "", err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	return rb.client.decodeTextResponse(resp)
}

func (rb *RequestBuilder) Bytes() ([]byte, error) {
	resp, err := rb.Execute()
	if err != nil {
		return nil, err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	return rb.client.decodeBytesResponse(resp)
}
