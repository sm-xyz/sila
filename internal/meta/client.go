package meta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const GraphAPIBase = "https://graph.facebook.com/v21.0"

type Client struct {
	httpClient *http.Client
	appID      string
	appSecret  string
}

func NewClient(appID, appSecret string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		appID:      appID,
		appSecret:  appSecret,
	}
}

func (c *Client) Get(endpoint string, params url.Values, accessToken string) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	if accessToken != "" {
		params.Set("access_token", accessToken)
	}

	fullURL := fmt.Sprintf("%s%s?%s", GraphAPIBase, endpoint, params.Encode())
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("Meta Graph API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) Post(endpoint string, payload interface{}, accessToken string) ([]byte, error) {
	var bodyReader io.Reader
	var contentType string

	switch p := payload.(type) {
	case url.Values:
		if accessToken != "" {
			p.Set("access_token", accessToken)
		}
		bodyReader = stringsReader(p.Encode())
		contentType = "application/x-www-form-urlencoded"
	default:
		jsonBytes, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBytes)
		contentType = "application/json"
	}

	fullURL := fmt.Sprintf("%s%s", GraphAPIBase, endpoint)
	if contentType == "application/json" && accessToken != "" {
		fullURL = fmt.Sprintf("%s?access_token=%s", fullURL, url.QueryEscape(accessToken))
	}

	req, err := http.NewRequest(http.MethodPost, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("Meta Graph API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func stringsReader(s string) io.Reader {
	return bytes.NewReader([]byte(s))
}
