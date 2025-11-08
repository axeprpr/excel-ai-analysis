package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type llmSQLRequest struct {
	Question       string         `json:"question"`
	Schema         schemaSnapshot `json:"schema"`
	FailedSQL      string         `json:"failed_sql,omitempty"`
	ExecutionError string         `json:"execution_error,omitempty"`
	PlanningHints  []string       `json:"planning_hints,omitempty"`
}

type llmSQLResponse struct {
	SQL    string `json:"sql"`
	Mode   string `json:"mode"`
	Refuse bool   `json:"refuse,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

func llmEnabled(settings modelSettings) bool {
	return strings.TrimSpace(settings.Model) != "" &&
		strings.TrimSpace(settings.BaseURL) != "" &&
		strings.TrimSpace(settings.APIKey) != ""
}

func (h *Handler) generateSQLWithLLM(settings modelSettings, req llmSQLRequest) (llmSQLResponse, error) {
	if !llmEnabled(settings) {
		return llmSQLResponse{}, errors.New("llm settings are incomplete")
	}

	switch resolveLLMProvider(settings) {
	case "openai", "openai-compatible":
		return h.generateSQLWithOpenAICompatible(settings, req)
	default:
		return llmSQLResponse{}, errors.New("unsupported llm provider")
	}
}

func resolveLLMProvider(settings modelSettings) string {
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	switch provider {
	case "", "openai", "openai-compatible":
		if provider == "" {
			return "openai-compatible"
		}
		return provider
	default:
		if strings.HasPrefix(provider, "http://") || strings.HasPrefix(provider, "https://") {
			return "openai-compatible"
		}
		return provider
	}
}

func (h *Handler) generateSQLWithOpenAICompatible(settings modelSettings, req llmSQLRequest) (llmSQLResponse, error) {
	body, err := json.Marshal(openAIChatRequest{
		Model: settings.Model,
		Messages: []openAIChatMessage{
			{
				Role: "system",
				Content: "You generate read-only SQLite SQL for the user's question. " +
					"Return strict JSON with keys sql, mode, refuse, and reason. " +
					"Allowed modes are detail, aggregate, topn, trend, count, share, compare, refuse. " +
					"Only generate a single read-only SELECT or WITH statement for SQLite. " +
					"Prefer aggregation over raw detail when the user asks for analysis, distribution, summary, counts, or charts. " +
					"If a detail query could return many rows, add a LIMIT no greater than 200. " +
					"If the request is ambiguous, unsupported by the schema, or asks for a chart without enough evidence for a valid chart, set refuse=true, provide a short reason, and leave sql empty.",
			},
			{
				Role:    "user",
				Content: buildLLMSQLPrompt(req),
			},
		},
		Temperature: 0,
	})
	if err != nil {
		return llmSQLResponse{}, err
	}

	endpoint := strings.TrimRight(settings.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return llmSQLResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+settings.APIKey)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return llmSQLResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llmSQLResponse{}, errors.New("llm request failed")
	}

	var parsed openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return llmSQLResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return llmSQLResponse{}, errors.New("llm response did not contain choices")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var out llmSQLResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return llmSQLResponse{}, err
	}
	if strings.TrimSpace(out.SQL) == "" {
		return llmSQLResponse{}, errors.New("llm response did not contain sql")
	}
	return out, nil
}

func buildLLMSQLPrompt(req llmSQLRequest) string {
	schemaJSON, _ := json.Marshal(req.Schema)
	prompt := "Question:\n" + req.Question + "\n\nSchema:\n" + string(schemaJSON)
	if strings.TrimSpace(req.FailedSQL) != "" || strings.TrimSpace(req.ExecutionError) != "" {
		prompt += "\n\nPrevious SQL Attempt:\n" + req.FailedSQL
		prompt += "\n\nExecution Error:\n" + req.ExecutionError
		prompt += "\n\nRepair the SQL for SQLite and keep it read-only."
	}
	if len(req.PlanningHints) > 0 {
		prompt += "\n\nPlanning Hints:\n- " + strings.Join(req.PlanningHints, "\n- ")
	}
	return prompt
}
