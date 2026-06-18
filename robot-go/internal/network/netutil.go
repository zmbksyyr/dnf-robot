package network

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	return string(body), nil
}

func ParseURL(u string) (host string, file string, port int, err error) {
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")

	slashIdx := strings.Index(u, "/")
	if slashIdx >= 0 {
		hostPart := u[:slashIdx]
		file = u[slashIdx+1:]
		if colonIdx := strings.LastIndex(hostPart, ":"); colonIdx >= 0 {
			host = hostPart[:colonIdx]
			fmt.Sscanf(hostPart[colonIdx+1:], "%d", &port)
		} else {
			host = hostPart
			port = 80
		}
	} else {
		if colonIdx := strings.LastIndex(u, ":"); colonIdx >= 0 {
			host = u[:colonIdx]
			fmt.Sscanf(u[colonIdx+1:], "%d", &port)
		} else {
			host = u
			port = 80
		}
		file = ""
	}

	return host, file, port, nil
}
