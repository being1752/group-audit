package config

// Config 是整个服务的统一配置结构体。
// 所有字段均有默认值（通过 config.yaml 提供），
// 加载方式由 load.go 决定，与此结构体无关。
type Config struct {
	API     APIConfig     `yaml:"api"`
	AI      AIConfig      `yaml:"ai"`
	Worker  WorkerConfig  `yaml:"worker"`
	Cron    CronConfig    `yaml:"cron"`
	Mock    MockConfig    `yaml:"mock"`
	Redline RedlineConfig `yaml:"redline"`
	Log     LogConfig     `yaml:"log"`
}

// LogConfig 日志输出配置。
type LogConfig struct {
	// File 日志文件路径。为空时只输出到 stdout；非空时同时写入文件和 stdout。
	File string `yaml:"file"`
	// Level 日志级别：debug / info / warn / error，默认 info。
	Level string `yaml:"level"`
}

// RedlineConfig 服务红线文档配置。
type RedlineConfig struct {
	// Path 服务红线文档路径，支持相对路径（相对于程序工作目录）或绝对路径。
	// 文件内容更新后，重启服务即可生效，无需修改任何代码。
	// 设为空字符串时禁用红线检测功能。
	Path string `yaml:"path"`
}

// MockConfig 控制是否使用本地 Mock 数据替代真实后端和 AI 服务。
type MockConfig struct {
	// Enabled 若为 true，GroupClient/ComplaintClient/AIProvider 均使用 Mock 实现，
	// 不发起任何真实 HTTP 请求，适合本地调试和 CI 环境。
	Enabled bool `yaml:"enabled"`
}

// APIConfig 后端接口相关配置。
type APIConfig struct {
	// BaseURL 后端接口基础地址，不含路径（例如 http://api.example.com/go/api/app）。
	BaseURL string `yaml:"base_url"`
	// Endpoints 后端接口路径配置。支持 {date}、{group_id}、{page} 占位符。
	Endpoints APIEndpointsConfig `yaml:"endpoints"`
}

// APIEndpointsConfig 后端接口路径配置。
type APIEndpointsConfig struct {
	// GroupsByDate 拉取指定日期群聊 ID 列表的接口。
	GroupsByDate string `yaml:"groups_by_date"`
	// GroupMessages 分页拉取群聊消息的接口。
	GroupMessages string `yaml:"group_messages"`
	// SubmitComplaint 提交投诉/表扬结果的接口。
	SubmitComplaint string `yaml:"submit_complaint"`
}

// AIConfig 大模型相关配置。
// BaseURL 兼容任何 OpenAI Chat Completions 协议（DeepSeek、Qwen、Ollama 等）。
type AIConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	// RPS 全局 LLM 请求速率上限（令牌/秒），防止打穿 API 配额。
	RPS int `yaml:"rps"`
}

// WorkerConfig Worker Pool 相关配置。
type WorkerConfig struct {
	// Count 并发 Worker（Goroutine）数量。
	Count int `yaml:"count"`
	// BufferSize 任务 Channel 缓冲大小，建议 >= 日均群聊数量。
	BufferSize int `yaml:"buffer_size"`
	// ResponseTimeoutMinutes 服务人员响应超时阈值（分钟），超过则生成警告。
	ResponseTimeoutMinutes float64 `yaml:"response_timeout_minutes"`
	// MaxMessages LLM 单次分析最多传入的消息条数，超出时截断取首尾，防止超出 Token 限制。
	MaxMessages int `yaml:"max_messages"`
}

// CronConfig 定时调度相关配置。
type CronConfig struct {
	// Spec 标准 5 段 Cron 表达式（例如 "0 1 * * *" 表示每天凌晨 1:00）。
	Spec string `yaml:"spec"`
	// RunNow 若为 true，服务启动时立即触发一次质检任务（用于调试或补跑）。
	RunNow bool `yaml:"run_now"`
}
