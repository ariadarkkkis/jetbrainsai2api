package main

import "time"

// Data structures
type RequestStats struct {
	TotalRequests      int64           `json:"total_requests"`
	SuccessfulRequests int64           `json:"successful_requests"`
	FailedRequests     int64           `json:"failed_requests"`
	TotalResponseTime  int64           `json:"total_response_time"`
	LastRequestTime    time.Time       `json:"last_request_time"`
	RequestHistory     []RequestRecord `json:"request_history"`
}

type RequestRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	Success      bool      `json:"success"`
	ResponseTime int64     `json:"response_time"`
	Model        string    `json:"model"`
	Account      string    `json:"account"`
}

type PeriodStats struct {
	Requests        int64   `json:"requests"`
	SuccessRate     float64 `json:"successRate"`
	AvgResponseTime int64   `json:"avgResponseTime"`
	QPS             float64 `json:"qps"`
}

type TokenInfo struct {
	Name       string    `json:"name"`
	License    string    `json:"license"`
	Used       float64   `json:"used"`
	Total      float64   `json:"total"`
	UsageRate  float64   `json:"usage_rate"`
	ExpiryDate time.Time `json:"expiry_date"`
	Status     string    `json:"status"`
	HasQuota   bool      `json:"has_quota"`
}

type JetbrainsAccount struct {
	LicenseID      string    `json:"licenseId,omitempty"`
	Authorization  string    `json:"authorization,omitempty"`
	JWT            string    `json:"jwt,omitempty"`
	LastUpdated    float64   `json:"last_updated"`
	HasQuota       bool      `json:"has_quota"`
	LastQuotaCheck float64   `json:"last_quota_check"`
	ExpiryTime     time.Time `json:"expiry_time"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type ModelsData struct {
	Data []ModelInfo `json:"data"`
}

type ModelList struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelsConfig struct {
	Models                 map[string]string `json:"models"`
	AnthropicModelMappings map[string]string `json:"anthropic_model_mappings"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Tools       []Tool        `json:"tools,omitempty"`
	Stop        any           `json:"stop,omitempty"`
	ServiceTier string        `json:"service_tier,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type ChatCompletionChoice struct {
	Message      ChatMessage `json:"message"`
	Index        int         `json:"index"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   map[string]int         `json:"usage"`
}

type StreamChoice struct {
	Delta        map[string]any `json:"delta"`
	Index        int            `json:"index"`
	FinishReason *string        `json:"finish_reason"`
}

type StreamResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// Anthropic compatible structures
type AnthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type AnthropicMessageRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        any                `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Stream        bool               `json:"stream"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicResponseContent struct {
	Type  string         `json:"type"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	Text  string         `json:"text,omitempty"`
}

type AnthropicResponseMessage struct {
	ID           string                     `json:"id"`
	Type         string                     `json:"type"`
	Role         string                     `json:"role"`
	Model        string                     `json:"model"`
	Content      []AnthropicResponseContent `json:"content"`
	StopReason   *string                    `json:"stop_reason"`
	StopSequence *string                    `json:"stop_sequence"`
	Usage        AnthropicUsage             `json:"usage"`
}

type JetbrainsMessage struct {
	Type         string                 `json:"type"`
	Content      string                 `json:"content"`
	FunctionCall *JetbrainsFunctionCall `json:"functionCall,omitempty"`
	FunctionName string                 `json:"functionName,omitempty"`
}

type JetbrainsFunctionCall struct {
	FunctionName string `json:"functionName"`
	Content      string `json:"content"`
}

type JetbrainsPayload struct {
	Prompt     string              `json:"prompt"`
	Profile    string              `json:"profile"`
	Chat       JetbrainsChat       `json:"chat"`
	Parameters JetbrainsParameters `json:"parameters"`
}

type JetbrainsChat struct {
	Messages []JetbrainsMessage `json:"messages"`
}

type JetbrainsParameters struct {
	Data []JetbrainsData `json:"data"`
}

type JetbrainsData struct {
	Type  string `json:"type"`
	FQDN  string `json:"fqdn,omitempty"`
	Value string `json:"value,omitempty"`
}
