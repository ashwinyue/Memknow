package websearch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSearch_PrefersTavilyWhenConfigured(t *testing.T) {
	calls := []string{}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls = append(calls, req.URL.String())
			if strings.Contains(req.URL.String(), "tavily.test") {
				if got := req.Header.Get("Authorization"); got != "Bearer tvly-secret" {
					t.Fatalf("authorization = %q, want Bearer tvly-secret", got)
				}
				return jsonResponse(http.StatusOK, `{"results":[{"title":"Tavily Result","url":"https://example.com","content":"hello"}]}`), nil
			}
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}),
	}

	resp, err := searchWithClient(context.Background(), client, RuntimeConfig{
		Providers:      []string{"tavily", "duckduckgo"},
		TimeoutSeconds: 3,
		Tavily: TavilyConfig{
			APIKey:  "tvly-secret",
			BaseURL: "https://tavily.test/search",
		},
	}, "memknow", 5)
	if err != nil {
		t.Fatalf("searchWithClient() error = %v", err)
	}
	if resp.Provider != "tavily" {
		t.Fatalf("provider = %q, want tavily", resp.Provider)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "tavily.test") {
		t.Fatalf("calls = %v, want only tavily", calls)
	}
}

func TestSearch_FallsBackToDDGWhenTavilyMissing(t *testing.T) {
	oldDDGURL := duckDuckGoHTMLURL
	duckDuckGoHTMLURL = "https://ddg.test/html/"
	defer func() { duckDuckGoHTMLURL = oldDDGURL }()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "ddg.test") {
				return htmlResponse(http.StatusOK, `<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com">Example</a><a class="result__snippet">Snippet text</a>`), nil
			}
			t.Fatalf("unexpected request: %s", req.URL.String())
			return nil, nil
		}),
	}

	resp, err := searchWithClient(context.Background(), client, RuntimeConfig{
		Providers:      []string{"tavily", "duckduckgo"},
		TimeoutSeconds: 3,
	}, "memknow", 5)
	if err != nil {
		t.Fatalf("searchWithClient() error = %v", err)
	}
	if resp.Provider != "duckduckgo" {
		t.Fatalf("provider = %q, want duckduckgo", resp.Provider)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://example.com" {
		t.Fatalf("results = %+v", resp.Results)
	}
}

func TestSearch_ReturnsJoinedErrorsWhenProvidersFail(t *testing.T) {
	oldDDGURL := duckDuckGoHTMLURL
	duckDuckGoHTMLURL = "https://ddg.test/html/"
	defer func() { duckDuckGoHTMLURL = oldDDGURL }()

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.String(), "tavily.test"):
				return jsonResponse(http.StatusInternalServerError, `{"message":"tavily failed"}`), nil
			case strings.Contains(req.URL.String(), "ddg.test"):
				return htmlResponse(http.StatusBadGateway, `ddg failed`), nil
			default:
				t.Fatalf("unexpected request: %s", req.URL.String())
				return nil, nil
			}
		}),
	}

	_, err := searchWithClient(context.Background(), client, RuntimeConfig{
		Providers:      []string{"tavily", "duckduckgo"},
		TimeoutSeconds: 3,
		Tavily: TavilyConfig{
			APIKey:  "tvly-secret",
			BaseURL: "https://tavily.test/search",
		},
	}, "memknow", 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tavily:") || !strings.Contains(err.Error(), "duckduckgo:") {
		t.Fatalf("error = %q, want both provider errors", err.Error())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body string) *http.Response {
	resp := htmlResponse(status, body)
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func htmlResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
