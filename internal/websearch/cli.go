package websearch

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

func RunCLI(args []string) error {
	fs := flag.NewFlagSet("web-search", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", "", "path to workspace .search.json")
	count := fs.Int("count", 5, "number of results")

	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("query is required")
	}
	if strings.TrimSpace(*configPath) == "" {
		return fmt.Errorf("--config is required")
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		return err
	}

	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	resp, err := Search(context.Background(), cfg, query, *count)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}
