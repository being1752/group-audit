package ai

import "context"

// Provider 是 AI 平台的统一接口。
//
// 设计原则：
//   - 质检场景只需结构化 JSON 输出，因此只暴露 ChatJSON。
//   - 任何实现了 OpenAI Chat Completions 协议的平台（DeepSeek、Qwen、Ollama 本地模型等）
//     均可通过实现此接口接入，无需修改上层调用代码。
type Provider interface {
	// ChatJSON 向大模型发送 Prompt，并将模型返回的 JSON 直接反序列化到 out（必须为指针）。
	// systemPrompt 为空字符串时不发送 system 消息。
	ChatJSON(ctx context.Context, systemPrompt, userPrompt string, out any) error

	ProviderName() string
	ModelName() string
}
