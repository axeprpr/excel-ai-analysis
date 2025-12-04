package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type openAICompatRequest struct {
	Model       string                     `json:"model"`
	Messages    []openAICompatMessage      `json:"messages"`
	Temperature *float64                   `json:"temperature,omitempty"`
	Stream      bool                       `json:"stream,omitempty"`
	SessionID   string                     `json:"session_id,omitempty"`
	Metadata    map[string]any             `json:"metadata,omitempty"`
	Extra       map[string]json.RawMessage `json:"-"`
}

type openAICompatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

func (h *Handler) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req openAICompatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, "messages are required", http.StatusBadRequest)
		return
	}
	if req.Stream {
		http.Error(w, "stream is not supported yet", http.StatusBadRequest)
		return
	}

	settings, err := h.readModelSettings()
	if err != nil {
		http.Error(w, "failed to read model settings", http.StatusInternalServerError)
		return
	}

	question := extractLastUserMessageText(req.Messages)
	if question == "" {
		question = "继续"
	}

	// Analysis-style prompts run through the session-aware data engine.
	if shouldTreatAsAnalysis(question) {
		resp, statusCode, err := h.answerThroughAnalysisPipeline(req, settings, question)
		if err != nil {
			http.Error(w, err.Error(), statusCode)
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Normal chat is transparently proxied to the configured OpenAI-compatible model endpoint.
	respBody, statusCode, err := passthroughChatCompletion(r.Context(), settings, req)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

func shouldTreatAsAnalysis(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	if normalized == "" {
		return false
	}
	return containsAny(normalized, []string{
		"分析", "统计", "报表", "图表", "excel", "csv", "sql", "查询", "分布", "趋势", "dashboard", "report", "analysis",
	})
}

func inferChartModeFromQuestion(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch {
	case strings.Contains(q, "mermaid"):
		return "mermaid"
	case hasAny(q, "图", "chart", "plot", "柱状", "折线", "饼图", "bar", "line", "pie"):
		return "mcp"
	default:
		return "data"
	}
}

func (h *Handler) answerThroughAnalysisPipeline(req openAICompatRequest, settings modelSettings, question string) (map[string]any, int, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil, http.StatusBadRequest, errors.New("analysis mode requires session_id; upload files first via /api/chat/upload")
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, http.StatusNotFound, errors.New("session not found")
		}
		return nil, http.StatusInternalServerError, errors.New("failed to read session")
	}
	if meta.Status != "ready" {
		return nil, http.StatusConflict, errors.New("session is not ready")
	}

	answer, err := h.executeSessionQuery(sessionDir, meta, queryRequest{
		Question:  question,
		ChartMode: inferChartModeFromQuestion(question),
	})
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	content := buildOpenAIContentFromAnswer(answer)
	now := time.Now().UTC().Unix()
	out := map[string]any{
		"id":      "chatcmpl-" + meta.SessionID,
		"object":  "chat.completion",
		"created": now,
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	return out, http.StatusOK, nil
}

func buildOpenAIContentFromAnswer(answer map[string]any) string {
	summary, _ := answer["summary"].(string)
	payload := map[string]any{
		"summary":       summary,
		"session_id":    answer["session_id"],
		"query_plan":    answer["query_plan"],
		"visualization": answer["visualization"],
		"rows":          answer["rows"],
		"columns":       answer["columns"],
		"row_count":     answer["row_count"],
		"chart":         answer["chart"],
		"warnings":      answer["warnings"],
	}
	data, _ := json.Marshal(payload)
	if summary == "" {
		return string(data)
	}
	return summary + "\n\n" + string(data)
}

func passthroughChatCompletion(ctx context.Context, settings modelSettings, req openAICompatRequest) ([]byte, int, error) {
	if !llmEnabled(settings) {
		return nil, http.StatusBadRequest, errors.New("llm settings are incomplete")
	}

	raw := map[string]any{
		"messages": req.Messages,
		"stream":   false,
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(settings.Model)
	}
	if model == "" {
		return nil, http.StatusBadRequest, errors.New("model is required")
	}
	raw["model"] = model
	if req.Temperature != nil {
		raw["temperature"] = *req.Temperature
	}

	body, err := json.Marshal(raw)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to encode request")
	}

	endpoint := strings.TrimRight(settings.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("failed to create upstream request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+settings.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, http.StatusBadGateway, errors.New("failed to call upstream llm endpoint")
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, http.StatusBadGateway, errors.New("failed to read upstream response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, http.StatusBadGateway, errors.New("upstream llm returned non-success")
	}
	return buf.Bytes(), http.StatusOK, nil
}

func extractLastUserMessageText(messages []openAICompatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.ToLower(strings.TrimSpace(messages[i].Role)) != "user" {
			continue
		}
		switch content := messages[i].Content.(type) {
		case string:
			return strings.TrimSpace(content)
		case []interface{}:
			parts := make([]string, 0, len(content))
			for _, item := range content {
				obj, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				partType, _ := obj["type"].(string)
				if partType == "text" {
					if text, ok := obj["text"].(string); ok && strings.TrimSpace(text) != "" {
						parts = append(parts, strings.TrimSpace(text))
					}
				}
			}
			return strings.Join(parts, " ")
		default:
			continue
		}
	}
	return ""
}
