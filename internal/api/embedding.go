package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

type schemaEmbeddingCandidate struct {
	TableName string
	Score     float64
}

func embeddingEnabled(settings modelSettings) bool {
	return strings.TrimSpace(settings.EmbeddingProvider) != "" &&
		strings.TrimSpace(settings.EmbeddingModel) != "" &&
		strings.TrimSpace(settings.EmbeddingBaseURL) != ""
}

func (h *Handler) generateEmbeddings(settings modelSettings, inputs []string) ([][]float64, error) {
	if !embeddingEnabled(settings) {
		return nil, errors.New("embedding settings are incomplete")
	}
	if len(inputs) == 0 {
		return nil, errors.New("no embedding inputs provided")
	}

	switch strings.ToLower(strings.TrimSpace(settings.EmbeddingProvider)) {
	case "openai", "openai-compatible":
		return h.generateOpenAICompatibleEmbeddings(settings, inputs)
	default:
		return nil, errors.New("unsupported embedding provider")
	}
}

func (h *Handler) generateOpenAICompatibleEmbeddings(settings modelSettings, inputs []string) ([][]float64, error) {
	body, err := json.Marshal(embeddingRequest{
		Model: settings.EmbeddingModel,
		Input: inputs,
	})
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(settings.EmbeddingBaseURL, "/") + "/embeddings"
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(settings.EmbeddingAPIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("embedding request failed")
	}

	var parsed embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) != len(inputs) {
		return nil, errors.New("embedding response count mismatch")
	}

	out := make([][]float64, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if len(item.Embedding) == 0 {
			return nil, errors.New("embedding response contained an empty vector")
		}
		out = append(out, item.Embedding)
	}
	return out, nil
}

func buildSchemaEmbeddingDocuments(snapshot schemaSnapshot) []string {
	docs := make([]string, 0, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		parts := []string{
			"table " + table.TableName,
			"file " + table.SourceFile,
			"sheet " + table.SourceSheet,
		}
		for _, column := range table.Columns {
			parts = append(parts, column.Name+" "+column.Type+" "+column.Semantic)
		}
		docs = append(docs, strings.Join(parts, " | "))
	}
	return docs
}

func rankTablesByEmbedding(snapshot schemaSnapshot, queryVector []float64, tableVectors [][]float64) []schemaEmbeddingCandidate {
	candidates := make([]schemaEmbeddingCandidate, 0, len(snapshot.Tables))
	for index, table := range snapshot.Tables {
		if index >= len(tableVectors) {
			break
		}
		candidates = append(candidates, schemaEmbeddingCandidate{
			TableName: table.TableName,
			Score:     cosineSimilarity(queryVector, tableVectors[index]),
		})
	}
	return candidates
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	var dot float64
	var normA float64
	var normB float64
	for index := range a {
		dot += a[index] * b[index]
		normA += a[index] * a[index]
		normB += b[index] * b[index]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
