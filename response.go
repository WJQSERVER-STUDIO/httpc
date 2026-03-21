package httpc

import (
	"encoding/gob"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-json-experiment/json"

	"github.com/WJQSERVER-STUDIO/go-utils/iox"
)

// --- 响应处理方法 (使用 RequestBuilder 重构) ---

// DecodeJSON 解析 JSON 响应
func (rb *RequestBuilder) DecodeJSON(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()
	err = rb.client.decodeJSONResponse(resp, v)
	if err != nil {
		return err
	}
	return nil
}

// DecodeXML 解析 XML 响应
func (rb *RequestBuilder) DecodeXML(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeXMLResponse(resp, v)
}

// DecodeGOB 解析 GOB 响应
func (rb *RequestBuilder) DecodeGOB(v any) error {
	resp, err := rb.Execute()
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return rb.client.decodeGOBResponse(resp, v)
}

// Text 获取 Text 响应
func (rb *RequestBuilder) Text() (string, error) {
	resp, err := rb.Execute()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return rb.client.decodeTextResponse(resp)
}

// Bytes 获取 Bytes 响应
func (rb *RequestBuilder) Bytes() ([]byte, error) {
	resp, err := rb.Execute()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return rb.client.decodeBytesResponse(resp)
}

// decodeJSONResponse 内部 JSON 响应解码
func (c *Client) decodeJSONResponse(resp *http.Response, obj any) error {
	if resp.StatusCode >= 400 {
		return c.errorResponse(resp)
	}

	/*
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("%w: %v", ErrDecodeResponse, err)
		}
	*/

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
