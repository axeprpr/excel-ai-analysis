package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

const localEmbeddingDimensions = 64

type embeddingProvider string

const (
	embeddingProviderLocal            embeddingProvider = "local"
	embeddingProviderOpenAICompatible embeddingProvider = "openai-compatible"
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

func resolveEmbeddingProvider(settings modelSettings) embeddingProvider {
	if offlineModeEnabled() {
		return embeddingProviderLocal
	}
	provider := strings.ToLower(strings.TrimSpace(settings.EmbeddingProvider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(settings.Provider))
	}
	switch provider {
	case "openai", "openai-compatible":
		if strings.TrimSpace(resolveEmbeddingModel(settings)) != "" &&
			strings.TrimSpace(resolveEmbeddingBaseURL(settings)) != "" {
			return embeddingProviderOpenAICompatible
		}
	}
	return embeddingProviderLocal
}

func resolveEmbeddingModel(settings modelSettings) string {
	if strings.TrimSpace(settings.EmbeddingModel) != "" {
		return strings.TrimSpace(settings.EmbeddingModel)
	}
	return ""
}

func resolveEmbeddingBaseURL(settings modelSettings) string {
	if strings.TrimSpace(settings.EmbeddingBaseURL) != "" {
		return strings.TrimSpace(settings.EmbeddingBaseURL)
	}
	return strings.TrimSpace(settings.BaseURL)
}

func resolveEmbeddingAPIKey(settings modelSettings) string {
	if strings.TrimSpace(settings.EmbeddingAPIKey) != "" {
		return strings.TrimSpace(settings.EmbeddingAPIKey)
	}
	return strings.TrimSpace(settings.APIKey)
}

func embeddingEnabled(settings modelSettings) bool {
	return resolveEmbeddingProvider(settings) == embeddingProviderOpenAICompatible
}

func embedTexts(settings modelSettings, texts []string) ([][]float64, embeddingProvider, error) {
	provider := resolveEmbeddingProvider(settings)
	switch provider {
	case embeddingProviderOpenAICompatible:
		vectors, err := embedTextsOpenAICompatible(settings, texts)
		if err == nil {
			return vectors, provider, nil
		}
		return embedTextsLocally(texts), embeddingProviderLocal, err
	default:
		return embedTextsLocally(texts), embeddingProviderLocal, nil
	}
}

func (h *Handler) generateEmbeddings(settings modelSettings, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, errors.New("no embedding inputs provided")
	}
	vectors, _, err := embedTexts(settings, inputs)
	return vectors, err
}

func (h *Handler) generateOpenAICompatibleEmbeddings(settings modelSettings, inputs []string) ([][]float64, error) {
	return embedTextsOpenAICompatible(settings, inputs)
}

func embedTextsOpenAICompatible(settings modelSettings, inputs []string) ([][]float64, error) {
	body, err := json.Marshal(embeddingRequest{
		Model: resolveEmbeddingModel(settings),
		Input: inputs,
	})
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(resolveEmbeddingBaseURL(settings), "/") + "/embeddings"
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := resolveEmbeddingAPIKey(settings); key != "" {
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
		out = append(out, normalizeEmbedding(item.Embedding))
	}
	return out, nil
}

func embedTextsLocally(texts []string) [][]float64 {
	vectors := make([][]float64, 0, len(texts))
	for _, text := range texts {
		vector := make([]float64, localEmbeddingDimensions)
		for _, token := range localEmbeddingTokens(text) {
			hash := fnv.New64a()
			_, _ = hash.Write([]byte(token))
			index := int(hash.Sum64() % uint64(localEmbeddingDimensions))
			vector[index] += 1
		}
		vectors = append(vectors, normalizeEmbedding(vector))
	}
	return vectors
}

func localEmbeddingTokens(text string) []string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ", ",", " ", ":", " ", ";", " ", "\n", " ", "\t", " ")
	parts := strings.Fields(replacer.Replace(normalized))
	tokens := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		if part == "" {
			continue
		}
		tokens = append(tokens, part)
		runes := []rune(part)
		if len(runes) <= 2 {
			continue
		}
		for i := 0; i < len(runes)-1; i++ {
			tokens = append(tokens, string(runes[i:i+2]))
		}
	}
	if len(tokens) == 0 {
		return []string{"empty"}
	}
	return tokens
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
	slices.SortStableFunc(candidates, func(a, b schemaEmbeddingCandidate) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		default:
			return 0
		}
	})
	return candidates
}

func normalizeEmbedding(vector []float64) []float64 {
	sumSquares := 0.0
	for _, value := range vector {
		sumSquares += value * value
	}
	if sumSquares == 0 {
		return vector
	}
	norm := math.Sqrt(sumSquares)
	out := make([]float64, len(vector))
	for i, value := range vector {
		out[i] = value / norm
	}
	return out
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for index := range a {
		sum += a[index] * b[index]
	}
	return sum
}
