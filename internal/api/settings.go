package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type modelSettings struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	DefaultChartMode string `json:"default_chart_mode"`
	MCPServerURL     string `json:"mcp_server_url"`
}

func (h *Handler) handleModelSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := h.readModelSettings()
		if err != nil {
			http.Error(w, "failed to read model settings", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var input modelSettings
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		input.Provider = strings.TrimSpace(input.Provider)
		input.Model = strings.TrimSpace(input.Model)
		input.BaseURL = strings.TrimSpace(input.BaseURL)
		input.DefaultChartMode = normalizeChartMode(input.DefaultChartMode)
		input.MCPServerURL = strings.TrimSpace(input.MCPServerURL)
		if input.DefaultChartMode == "" {
			input.DefaultChartMode = "data"
		}
		if err := h.writeModelSettings(input); err != nil {
			http.Error(w, "failed to persist model settings", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, input)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) readModelSettings() (modelSettings, error) {
	path := filepath.Join(h.dataDir, "settings", "model.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultModelSettings(), nil
		}
		return modelSettings{}, err
	}

	var settings modelSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return modelSettings{}, err
	}
	if settings.DefaultChartMode == "" {
		settings.DefaultChartMode = "data"
	}
	return settings, nil
}

func (h *Handler) writeModelSettings(settings modelSettings) error {
	settingsDir := filepath.Join(h.dataDir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(settingsDir, "model.json"), data, 0o600)
}

func defaultModelSettings() modelSettings {
	return modelSettings{
		Provider:         "",
		Model:            "",
		BaseURL:          "",
		APIKey:           "",
		DefaultChartMode: "data",
		MCPServerURL:     "http://chart-mcp:1122/mcp",
	}
}

func normalizeChartMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "data", "mermaid", "mcp":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ""
	}
}
