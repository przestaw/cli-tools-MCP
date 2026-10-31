package httpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func Transport(verifyTLS bool, caFile string) (*http.Transport, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verifyTLS}
	if caFile != "" {
		pem, err := os.ReadFile(os.ExpandEnv(caFile))
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file %s contains no usable PEM certificates", caFile)
		}
		cfg.RootCAs = pool
	}
	return &http.Transport{TLSClientConfig: cfg}, nil
}

func NewClient(timeout time.Duration, verifyTLS bool) *http.Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	tr, _ := Transport(verifyTLS, "")
	return &http.Client{Timeout: timeout, Transport: tr}
}

type ErrorParser func(status int, url string, body []byte) error

func Do(client *http.Client, req *http.Request, parseError ErrorParser) ([]byte, http.Header, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, parseError(resp.StatusCode, req.URL.String(), data)
	}
	return data, resp.Header, nil
}

func JSON(ctx context.Context, client *http.Client, method, url string, headers http.Header, body, out any, parseError ErrorParser, validate func([]byte) error) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		var data []byte
		switch value := body.(type) {
		case json.RawMessage:
			data = value
		case []byte:
			data = value
		default:
			var err error
			data, err = json.Marshal(body)
			if err != nil {
				return nil, err
			}
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	data, hdr, err := Do(client, req, parseError)
	if err != nil {
		return hdr, err
	}
	if validate != nil {
		if err := validate(data); err != nil {
			return hdr, err
		}
	}
	return hdr, Decode(url, data, out)
}

func Decode(url string, data []byte, out any) error {
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s response: %w", url, err)
	}
	return nil
}

func ErrorMessage(provider string, status int, message, url string) string {
	if message == "" {
		return fmt.Sprintf("%s %d for %s", provider, status, url)
	}
	return fmt.Sprintf("%s %d: %s", provider, status, message)
}

func RawError(method, url string, status int, body string) string {
	body = strings.TrimSpace(body)
	if len(body) > 1200 {
		body = body[:1200] + "..."
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", method, url, status, body)
}
