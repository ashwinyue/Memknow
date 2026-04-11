package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type RuntimeConfig struct {
	Providers      []string     `json:"providers"`
	TimeoutSeconds int          `json:"timeout_seconds"`
	Tavily         TavilyConfig `json:"tavily"`
}

type TavilyConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type Response struct {
	Query    string   `json:"query"`
	Provider string   `json:"provider"`
	Results  []Result `json:"results"`
}

func Search(ctx context.Context, cfg RuntimeConfig, query string, count int) (*Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	return searchWithClient(ctx, client, cfg, query, count)
}

func searchWithClient(ctx context.Context, client *http.Client, cfg RuntimeConfig, query string, count int) (*Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}

	providers := cfg.Providers
	if len(providers) == 0 {
		providers = []string{"tavily", "duckduckgo"}
	}

	var errs []string
	for _, provider := range providers {
		switch strings.TrimSpace(provider) {
		case "tavily":
			resp, err := tavilySearch(ctx, client, cfg.Tavily, query, count)
			if err == nil {
				return resp, nil
			}
			errs = append(errs, "tavily: "+err.Error())
		case "duckduckgo":
			resp, err := duckDuckGoSearch(ctx, client, query, count)
			if err == nil {
				return resp, nil
			}
			errs = append(errs, "duckduckgo: "+err.Error())
		}
	}

	if len(errs) == 0 {
		return nil, errors.New("no search providers configured")
	}
	return nil, errors.New(strings.Join(errs, "; "))
}

func tavilySearch(ctx context.Context, client *http.Client, cfg TavilyConfig, query string, count int) (*Response, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("tavily api key is not configured")
	}
	endpoint := strings.TrimSpace(cfg.BaseURL)
	if endpoint == "" {
		endpoint = "https://api.tavily.com/search"
	}

	payload, err := json.Marshal(map[string]any{
		"query":       query,
		"max_results": count,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildHTTPError(resp.StatusCode, body)
	}

	var raw struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("invalid tavily response")
	}

	results := make([]Result, 0, len(raw.Results))
	for _, item := range raw.Results {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		results = append(results, Result{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Snippet: strings.TrimSpace(item.Content),
		})
	}

	return &Response{Query: query, Provider: "tavily", Results: results}, nil
}

func duckDuckGoSearch(ctx context.Context, client *http.Client, query string, count int) (*Response, error) {
	form := url.Values{}
	form.Set("q", query)
	form.Set("b", "")
	form.Set("kl", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, duckDuckGoHTMLURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildHTTPError(resp.StatusCode, body)
	}

	htmlStr := string(body)
	links := ddgResultLinkRe.FindAllStringSubmatch(htmlStr, -1)
	titles := ddgResultTitleRe.FindAllStringSubmatch(htmlStr, -1)
	snippets := ddgResultSnippetRe.FindAllStringSubmatch(htmlStr, -1)

	n := min(len(links), len(titles))
	if count < n {
		n = count
	}

	results := make([]Result, 0, n)
	for i := 0; i < n; i++ {
		rawURL := html.UnescapeString(links[i][1])
		realURL := extractDDGURL(rawURL)
		if strings.TrimSpace(realURL) == "" {
			continue
		}
		snippet := ""
		if i < len(snippets) {
			snippet = html.UnescapeString(strings.TrimSpace(ddgHTMLTagRe.ReplaceAllString(snippets[i][1], "")))
		}
		results = append(results, Result{
			Title:   html.UnescapeString(strings.TrimSpace(titles[i][1])),
			URL:     strings.TrimSpace(realURL),
			Snippet: snippet,
		})
	}

	return &Response{Query: query, Provider: "duckduckgo", Results: results}, nil
}

func buildHTTPError(statusCode int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 200 {
		detail = detail[:200] + "..."
	}
	if detail == "" {
		return fmt.Errorf("search request failed (HTTP %d)", statusCode)
	}
	return fmt.Errorf("search request failed (HTTP %d): %s", statusCode, detail)
}

func extractDDGURL(rawURL string) string {
	if strings.Contains(rawURL, "uddg=") {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			if uddg := parsed.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}
	if strings.HasPrefix(rawURL, "//") {
		return "https:" + rawURL
	}
	return rawURL
}

var (
	duckDuckGoHTMLURL  = "https://html.duckduckgo.com/html/"
	ddgResultLinkRe    = regexp.MustCompile(`class="result__a"[^>]*href="([^"]+)"`)
	ddgResultTitleRe   = regexp.MustCompile(`class="result__a"[^>]*>([^<]+)<`)
	ddgResultSnippetRe = regexp.MustCompile(`class="result__snippet"[^>]*>([\s\S]*?)</a>`)
	ddgHTMLTagRe       = regexp.MustCompile(`<[^>]*>`)
)
