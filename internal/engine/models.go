package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	mlog "menace/internal/log"
)

// FetchModels queries the provider's model list API and returns available models.
// Falls back to hardcoded defaults on failure.
func FetchModels(provider, apiKey string) (architect []ModelOption, worker []ModelOption) {
	models, err := fetchModelList(provider, apiKey)
	if err != nil {
		mlog.Error("FetchModels", slog.String("provider", provider), slog.String("err", err.Error()))
		return ArchitectModels(provider), WorkerModels(provider)
	}
	if len(models) == 0 {
		return ArchitectModels(provider), WorkerModels(provider)
	}

	// Split into tiers
	var strong, mid, cheap []ModelOption
	for _, m := range models {
		tier := classifyModel(provider, m.ID)
		switch tier {
		case "strong":
			strong = append(strong, m)
		case "mid":
			mid = append(mid, m)
		case "cheap":
			cheap = append(cheap, m)
		}
	}

	// Architect: strong first, then mid
	architect = append(strong, mid...)
	if len(architect) == 0 {
		architect = models
	}

	// Worker: cheap first, then mid
	worker = append(cheap, mid...)
	if len(worker) == 0 {
		worker = models
	}

	if len(architect) == 0 {
		architect = ArchitectModels(provider)
	}
	if len(worker) == 0 {
		worker = WorkerModels(provider)
	}
	return
}

func fetchModelList(provider, apiKey string) ([]ModelOption, error) {
	const apiTimeout = 10 * time.Second
	client := &http.Client{Timeout: apiTimeout}

	var req *http.Request
	var err error

	switch provider {
	case "anthropic":
		req, err = http.NewRequest("GET", "https://api.anthropic.com/v1/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

	case "google":
		req, err = http.NewRequest("GET", "https://generativelanguage.googleapis.com/v1beta/models?pageSize=100", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-goog-api-key", apiKey)

	case "openai":
		req, err = http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)

	case "ollama":
		req, err = http.NewRequest("GET", "http://localhost:11434/api/tags", nil)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	const maxResponseBody = 2 << 20 // 2 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, preview)
	}

	switch provider {
	case "anthropic":
		return parseAnthropicModels(body)
	case "google":
		return parseGoogleModels(body)
	case "openai":
		return parseOpenAIModels(body)
	case "ollama":
		return parseOllamaModels(body)
	}
	return nil, nil
}

func parseAnthropicModels(body []byte) ([]ModelOption, error) {
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var models []ModelOption
	for _, m := range resp.Data {
		desc := m.DisplayName
		if desc == "" {
			desc = m.ID
		}
		models = append(models, ModelOption{ID: m.ID, Desc: desc})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func parseGoogleModels(body []byte) ([]ModelOption, error) {
	var resp struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var models []ModelOption
	for _, m := range resp.Models {
		// Only include models that support generateContent
		supportsGenerate := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsGenerate = true
				break
			}
		}
		if !supportsGenerate {
			continue
		}

		id := strings.TrimPrefix(m.Name, "models/")

		// Skip non-LLM models (video, image, audio-only, robotics, etc.)
		skip := false
		for _, prefix := range []string{"veo", "lyria", "nano-banana", "imagen"} {
			if strings.HasPrefix(id, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// Skip robotics, deep-research, computer-use, TTS, audio-only, image-only
		for _, substr := range []string{"robotics", "deep-research", "computer-use", "-tts", "-image", "native-audio"} {
			if strings.Contains(id, substr) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		desc := m.DisplayName
		if desc == "" {
			desc = id
		}
		models = append(models, ModelOption{ID: id, Desc: desc})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func parseOpenAIModels(body []byte) ([]ModelOption, error) {
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var models []ModelOption
	for _, m := range resp.Data {
		// Filter to chat models (skip dall-e, whisper, tts, embeddings, etc)
		id := m.ID
		if strings.HasPrefix(id, "dall-e") || strings.HasPrefix(id, "whisper") ||
			strings.HasPrefix(id, "tts") || strings.Contains(id, "embedding") ||
			strings.HasPrefix(id, "text-") || strings.HasPrefix(id, "davinci") ||
			strings.HasPrefix(id, "babbage") || strings.HasPrefix(id, "curie") ||
			strings.Contains(id, "moderation") || strings.Contains(id, "realtime") ||
			strings.Contains(id, "audio") || strings.Contains(id, "transcribe") {
			continue
		}
		models = append(models, ModelOption{ID: id, Desc: id})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func parseOllamaModels(body []byte) ([]ModelOption, error) {
	var resp struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	var models []ModelOption
	for _, m := range resp.Models {
		// Size in bytes, convert to human readable for description
		sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
		desc := fmt.Sprintf("%.1fGB", sizeGB)
		models = append(models, ModelOption{ID: m.Name, Desc: desc})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

// classifyModel puts a model into a tier for the architect/worker split.
func classifyModel(provider, id string) string {
	low := strings.ToLower(id)
	switch provider {
	case "anthropic":
		if strings.Contains(low, "opus") {
			return "strong"
		}
		if strings.Contains(low, "sonnet") {
			return "mid"
		}
		if strings.Contains(low, "haiku") {
			return "cheap"
		}
	case "google":
		if strings.Contains(low, "gemma") {
			return "cheap"
		}
		if strings.Contains(low, "pro") {
			return "strong"
		}
		if strings.Contains(low, "flash-lite") || strings.Contains(low, "flash-8b") {
			return "cheap"
		}
		if strings.Contains(low, "flash") {
			return "mid"
		}
	case "openai":
		if strings.Contains(low, "o4-mini") || strings.Contains(low, "o3") || strings.Contains(low, "o1") {
			return "strong"
		}
		if strings.Contains(low, "gpt-4") && !strings.Contains(low, "mini") && !strings.Contains(low, "nano") {
			return "strong"
		}
		if strings.Contains(low, "mini") {
			return "mid"
		}
		if strings.Contains(low, "nano") {
			return "cheap"
		}
	case "ollama":
		// Ollama models — classify by size hints in the name
		if strings.Contains(low, "70b") || strings.Contains(low, "72b") || strings.Contains(low, "34b") {
			return "strong"
		}
		if strings.Contains(low, "1b") || strings.Contains(low, "3b") || strings.Contains(low, "1.7b") {
			return "cheap"
		}
	}
	return "mid" // default
}
