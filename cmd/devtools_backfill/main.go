package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type database struct {
	Runs  []any  `json:"runs"`
	Steps []step `json:"steps"`
}

type step struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id"`
	StepNumber     int     `json:"step_number"`
	Type           string  `json:"type"`
	ModelID        string  `json:"model_id"`
	Provider       *string `json:"provider"`
	StartedAt      string  `json:"started_at"`
	DurationMS     *int64  `json:"duration_ms"`
	Input          string  `json:"input"`
	Output         *string `json:"output"`
	Usage          *string `json:"usage"`
	Error          *string `json:"error"`
	RawRequest     *string `json:"raw_request"`
	RawResponse    *string `json:"raw_response"`
	RawChunks      *string `json:"raw_chunks"`
	ProviderOption *string `json:"provider_options"`
}

func main() {
	var path string
	flag.StringVar(&path, "path", ".devtools/generations.json", "path to generations.json")
	flag.Parse()

	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	var db database
	if err := json.Unmarshal(b, &db); err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	var providerOptionsStr *string
	if baseURL != "" {
		s, _ := json.Marshal(map[string]any{"baseURL": baseURL})
		ss := string(s)
		providerOptionsStr = &ss
	}

	changed := 0
	for i := range db.Steps {
		s := &db.Steps[i]
		if s.RawRequest == nil || *s.RawRequest == "" {
			rr := s.Input
			s.RawRequest = &rr
			changed++
		}
		if (s.RawResponse == nil || *s.RawResponse == "") && s.Output != nil && *s.Output != "" {
			rs := *s.Output
			s.RawResponse = &rs
			changed++
		}
		if s.ProviderOption == nil && providerOptionsStr != nil {
			s.ProviderOption = providerOptionsStr
			changed++
		}
	}

	out, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}

	fmt.Printf("Backfilled %d fields into %s\n", changed, path)
}
