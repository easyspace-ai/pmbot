package client

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient HTTP 客户端接口
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpClient HTTP 客户端封装
type httpClient struct {
	client     *http.Client
	host       string
	authConfig *AuthConfig
	useProxy   bool
	proxyURL   *url.URL
}

// newHTTPClient 创建新的 HTTP 客户端
func newHTTPClient(host string, authConfig *AuthConfig, useProxy bool, proxyURL *url.URL) *httpClient {
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	// 如果启用代理且提供了代理URL，则使用代理
	if useProxy && proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &httpClient{
		client:     client,
		host:       strings.TrimSuffix(host, "/"),
		authConfig: authConfig,
		useProxy:   useProxy,
		proxyURL:   proxyURL,
	}
}

// get 执行 GET 请求
func (h *httpClient) get(endpoint string, headers map[string]string, params map[string]string) (*http.Response, error) {
	reqURL := h.host + endpoint

	if len(params) > 0 {
		u, err := url.Parse(reqURL)
		if err != nil {
			return nil, fmt.Errorf("解析 URL 失败: %w", err)
		}
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置默认头
	h.setDefaultHeaders(req)

	// 为 balance-allowance 端点添加浏览器样式的 headers
	if strings.Contains(endpoint, "balance-allowance") {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Origin", "https://polymarket.com")
		req.Header.Set("Referer", "https://polymarket.com/")
	}

	// 设置自定义头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return h.client.Do(req)
}

// post 执行 POST 请求
func (h *httpClient) post(endpoint string, headers map[string]string, body interface{}) (*http.Response, error) {
	reqURL := h.host + endpoint

	var bodyReader io.Reader
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(http.MethodPost, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置默认头
	h.setDefaultHeaders(req)

	// 设置自定义头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return h.client.Do(req)
}

// delete 执行 DELETE 请求
func (h *httpClient) delete(endpoint string, headers map[string]string, params map[string]string) (*http.Response, error) {
	reqURL := h.host + endpoint
	if len(params) > 0 {
		u, err := url.Parse(reqURL)
		if err != nil {
			return nil, fmt.Errorf("解析 URL 失败: %w", err)
		}
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}

	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置默认头
	h.setDefaultHeaders(req)

	// 设置自定义头
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return h.client.Do(req)
}

// setDefaultHeaders 设置默认请求头
func (h *httpClient) setDefaultHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "polymarket-btc-bot/0.1")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/json")

	if req.Method == http.MethodGet {
		req.Header.Set("Accept-Encoding", "gzip")
	}
}

// parseResponse 解析响应
func parseResponse(resp *http.Response, result interface{}) error {
	defer resp.Body.Close()

	// 处理 gzip 压缩的响应
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("创建 gzip 读取器失败: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(reader)
		errorMsg := fmt.Sprintf("HTTP 错误 %d: %s", resp.StatusCode, string(bodyBytes))
		// go vet: fmt.Errorf expects a format string; wrap as "%s".
		return fmt.Errorf("%s", errorMsg)
	}

	if result != nil {
		bodyBytes, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("读取响应体失败: %w", err)
		}

		if err := json.Unmarshal(bodyBytes, result); err != nil {
			return fmt.Errorf("解析响应失败: %w, 响应体: %s", err, string(bodyBytes))
		}
	}

	return nil
}

