package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOllamaPresetUsesCloudOpenAICompatibility(t *testing.T) {
	preset, ok := presetByID("ollama")
	if !ok {
		t.Fatal("Ollama preset is missing")
	}
	if preset.Name != "Ollama Cloud" ||
		preset.Protocol != protocolOpenAIChat ||
		preset.BaseURL != "https://ollama.com/v1" ||
		preset.AuthMode != authBearer ||
		!preset.DiscoverModels ||
		!preset.AutoImportModels {
		t.Fatalf("unexpected Ollama preset: %+v", preset)
	}
}

func TestOllamaProviderCatalogImportsModelsWithoutKey(t *testing.T) {
	var request *http.Request
	detector := newDetector()
	detector.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			request = req
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"object":"list","data":[{"id":"gemma4:31b"},{"id":"gpt-oss:120b"}]}`,
				)),
				Request: req,
			}, nil
		}),
	}
	result, err := detector.discoverProviderCatalog(context.Background(), Provider{
		PresetID: "ollama",
		Protocol: protocolOpenAIChat,
		BaseURL:  "https://ollama.com/v1",
		AuthMode: authBearer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if request == nil || request.URL.String() != "https://ollama.com/v1/models" {
		t.Fatalf("request URL = %v", request)
	}
	if request.Header.Get("Authorization") != "" {
		t.Fatal("public Ollama catalog request unexpectedly included a key")
	}
	if len(result.Models) != 2 ||
		result.Models[0].ID != "gemma4:31b" ||
		result.Models[1].ID != "gpt-oss:120b" {
		t.Fatalf("models = %+v", result.Models)
	}
}
