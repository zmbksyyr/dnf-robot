package network

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func HTTPGet(rawURL string) (string, error) {
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("HTTPGet %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPGet read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTPGet status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPPost(rawURL string, bodyStr string) (string, error) {
	resp, err := httpClient.Post(rawURL, "application/x-www-form-urlencoded", strings.NewReader(bodyStr))
	if err != nil {
		return "", fmt.Errorf("HTTPPost %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPPost read body: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTPPost status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPGetV5(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return HTTPGet(rawURL)
	}

	req, err := http.NewRequest("GET", parsedURL.String(), nil)
	if err != nil {
		return HTTPGet(rawURL)
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Connection", "keep-alive")

	resp, err := httpClient.Do(req)
	if err != nil {
		return HTTPGet(rawURL)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPGetV5 read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTPGetV5 status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func HTTPPostV5(rawURL string, bodyStr string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}

	data := url.Values{}
	data.Set("data", bodyStr)
	encoded := data.Encode()

	req, err := http.NewRequest("POST", parsedURL.String(), strings.NewReader(encoded))
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return HTTPPost(rawURL, bodyStr)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("HTTPPostV5 read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTPPostV5 status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func ParseURL(u string) (host string, file string, port int, err error) {
	raw := strings.TrimSpace(u)
	if raw == "" {
		return "", "", 0, fmt.Errorf("empty url")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", 0, err
	}
	host = parsed.Hostname()
	if host == "" {
		return "", "", 0, fmt.Errorf("missing host")
	}
	switch p := parsed.Port(); {
	case p != "":
		port, err = strconv.Atoi(p)
		if err != nil || port <= 0 || port > 65535 {
			return "", "", 0, fmt.Errorf("invalid port %q", p)
		}
	case parsed.Scheme == "https":
		port = 443
	default:
		port = 80
	}
	file = strings.TrimPrefix(parsed.EscapedPath(), "/")
	if parsed.RawQuery != "" {
		file += "?" + parsed.RawQuery
	}
	return host, file, port, nil
}
