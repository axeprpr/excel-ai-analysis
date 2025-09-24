package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type mcpRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      string         `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Meta map[string]any `json:"_meta"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func executeChartMCP(endpoint string, visualization map[string]any, columns []string, rows []map[string]any) (map[string]any, error) {
	toolName, arguments, err := buildMCPToolRequest(visualization, columns, rows)
	if err != nil {
		return nil, err
	}

	callResult, err := callMCPTool(endpoint, toolName, arguments)
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"tool_name": toolName,
		"arguments": arguments,
		"content":   callResult.Result.Content,
		"meta":      callResult.Result.Meta,
	}
	if len(callResult.Result.Content) > 0 {
		response["url"] = callResult.Result.Content[0].Text
	}
	return response, nil
}

func buildMCPToolRequest(visualization map[string]any, columns []string, rows []map[string]any) (string, map[string]any, error) {
	if len(rows) == 0 {
		return "", nil, errors.New("no rows available for mcp chart execution")
	}

	chartType, _ := visualization["type"].(string)
	title, _ := visualization["title"].(string)
	x, _ := visualization["x"].(string)
	y, _ := visualization["y"].(string)

	switch chartType {
	case "line":
		return "generate_line_chart", map[string]any{
			"title": title,
			"data":  buildLineChartData(rows, x, y),
		}, nil
	case "pie":
		return "generate_pie_chart", map[string]any{
			"title": title,
			"data":  buildCategoryValueData(rows, x, y),
		}, nil
	case "bar":
		return "generate_bar_chart", map[string]any{
			"title": title,
			"data":  buildCategoryValueData(rows, x, y),
		}, nil
	default:
		return "generate_spreadsheet", map[string]any{
			"data":    rows,
			"columns": columns,
		}, nil
	}
}

func buildCategoryValueData(rows []map[string]any, xKey, yKey string) []map[string]any {
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"category": fmt.Sprint(row[xKey]),
			"value":    asChartNumber(row[yKey]),
		})
	}
	return data
}

func buildLineChartData(rows []map[string]any, xKey, yKey string) []map[string]any {
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"time":  fmt.Sprint(row[xKey]),
			"value": asChartNumber(row[yKey]),
		})
	}
	return data
}

func callMCPTool(endpoint, toolName string, arguments map[string]any) (mcpRPCResponse, error) {
	requestBody, err := json.Marshal(mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      "tools-call-1",
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": arguments,
		},
	})
	if err != nil {
		return mcpRPCResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimSpace(endpoint), bytes.NewReader(requestBody))
	if err != nil {
		return mcpRPCResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return mcpRPCResponse{}, err
	}
	defer resp.Body.Close()

	var decoded mcpRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return mcpRPCResponse{}, err
	}
	if decoded.Error != nil {
		return mcpRPCResponse{}, errors.New(decoded.Error.Message)
	}
	return decoded, nil
}
