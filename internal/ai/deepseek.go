package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
)

type deepseekProvider struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewDeepSeekProvider 创建 DeepSeek Provider。
// baseURL 兼容任何 OpenAI 协议兼容的服务（如本地 Ollama: http://localhost:11434/v1）。
func NewDeepSeekProvider(baseURL, apiKey, model string) Provider {
	return &deepseekProvider{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *deepseekProvider) ProviderName() string { return "deepseek" }
func (p *deepseekProvider) ModelName() string    { return p.model }

func (p *deepseekProvider) ChatJSON(ctx context.Context, systemPrompt, userPrompt string, out any) error {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type responseFormat struct {
		Type string `json:"type"`
	}
	type reqBody struct {
		Model          string         `json:"model"`
		Messages       []message      `json:"messages"`
		ResponseFormat responseFormat `json:"response_format"`
	}
	type respMessage struct {
		Content string `json:"content"`
	}
	type choice struct {
		Message respMessage `json:"message"`
	}
	type respBody struct {
		Choices []choice `json:"choices"`
	}

	msgs := make([]message, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, message{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, message{Role: "user", Content: userPrompt})

	bodyBytes, err := json.Marshal(reqBody{
		Model:          p.model,
		Messages:       msgs,
		ResponseFormat: responseFormat{Type: "json_object"},
	})
	if err != nil {
		return fmt.Errorf("序列化 AI 请求体失败: %w", err)
	}

	var rr respBody

	op := func() error {
		// 每次重试需要重新创建 Reader
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			p.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
		if err != nil {
			// 请求构建失败属于代码 bug，无需重试
			return backoff.Permanent(fmt.Errorf("构建 HTTP 请求失败: %w", err))
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("HTTP 请求失败: %w", err)
		}
		defer resp.Body.Close()

		// 429（限流）或 5xx（服务端错误）→ 可重试
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return fmt.Errorf("AI API 返回 %d，等待重试", resp.StatusCode)
		}
		// 其他非 200 → 永久失败，不再重试
		if resp.StatusCode != http.StatusOK {
			return backoff.Permanent(fmt.Errorf("AI API 返回非预期状态码 %d", resp.StatusCode))
		}

		if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
			return backoff.Permanent(fmt.Errorf("解析 AI 响应失败: %w", err))
		}
		return nil
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = time.Second     // 首次等待 1s
	bo.Multiplier = 2.0                  // 每次翻倍：1s → 2s → 4s → 8s
	bo.MaxElapsedTime = 60 * time.Second // 最长重试 60s

	if err := backoff.Retry(op, backoff.WithContext(bo, ctx)); err != nil {
		return err
	}

	if len(rr.Choices) == 0 {
		return fmt.Errorf("AI 返回了空的 choices")
	}

	content := rr.Choices[0].Message.Content
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("反序列化 AI JSON 输出失败: %w（原始内容: %s）", err, content)
	}
	return nil
}
