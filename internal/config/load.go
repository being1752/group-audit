package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// defaultConfigPath 默认配置文件路径，可通过环境变量 AUDIT_CONFIG 覆盖。
const defaultConfigPath = "config.yaml"

// Load 从 YAML 文件中读取配置并返回 *Config。
//
// 文件路径优先级：
//  1. 环境变量 AUDIT_CONFIG 指定的路径
//  2. 当前工作目录下的 config.yaml
//
// 如需切换配置来源（远程配置中心、数据库、纯环境变量等），只需修改此函数即可，
// Config 结构体及所有调用方无需改动。
func Load() (*Config, error) {
	path := os.Getenv("AUDIT_CONFIG")
	if path == "" {
		path = defaultConfigPath
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开配置文件 %q 失败: %w", path, err)
	}
	defer f.Close()

	cfg := defaultConfig()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %q 失败: %w", path, err)
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// defaultConfig 返回所有字段填充了合理默认值的 Config。
// 配置文件中未填写的字段将保留这里的默认值。
func defaultConfig() *Config {
	return &Config{
		API: APIConfig{
			BaseURL: "http://api.ufutx.net/mock/2673/go/api/app",
			Endpoints: APIEndpointsConfig{
				GroupsByDate:    "/ai/groups/by/date?date={date}",
				GroupMessages:   "/ai/groups/{group_id}/message?page={page}",
				SubmitComplaint: "/ai/complaint/groups/{group_id}",
			},
		},
		AI: AIConfig{
			BaseURL: "https://api.deepseek.com/v1",
			Model:   "deepseek-chat",
			RPS:     5,
		},
		Worker: WorkerConfig{
			Count:                  20,
			BufferSize:             1000,
			ResponseTimeoutMinutes: 5.0,
			MaxMessages:            60,
		},
		Cron: CronConfig{
			Spec:   "0 1 * * *",
			RunNow: false,
		},
		Redline: RedlineConfig{
			Path: "redline.md",
		},
		Log: LogConfig{
			File:  "logs/group-audit.log",
			Level: "info",
		},
	}
}

// validate 对配置做基本合法性检查。
func validate(cfg *Config) error {
	if cfg.API.BaseURL == "" {
		return fmt.Errorf("config: api.base_url 不能为空")
	}
	if cfg.API.Endpoints.GroupsByDate == "" {
		return fmt.Errorf("config: api.endpoints.groups_by_date 不能为空")
	}
	if cfg.API.Endpoints.GroupMessages == "" {
		return fmt.Errorf("config: api.endpoints.group_messages 不能为空")
	}
	if cfg.API.Endpoints.SubmitComplaint == "" {
		return fmt.Errorf("config: api.endpoints.submit_complaint 不能为空")
	}
	if cfg.AI.BaseURL == "" {
		return fmt.Errorf("config: ai.base_url 不能为空")
	}
	// mock 模式下不需要真实 API Key
	if !cfg.Mock.Enabled && cfg.AI.APIKey == "" {
		return fmt.Errorf("config: ai.api_key 不能为空，请在配置文件中填写或通过 AUDIT_CONFIG 指向正确的文件")
	}
	if cfg.Worker.Count <= 0 {
		return fmt.Errorf("config: worker.count 必须大于 0")
	}
	if cfg.Worker.ResponseTimeoutMinutes <= 0 {
		return fmt.Errorf("config: worker.response_timeout_minutes 必须大于 0")
	}
	if cfg.AI.RPS <= 0 {
		return fmt.Errorf("config: ai.rps 必须大于 0")
	}
	return nil
}

// LoadRedline 从指定路径读取服务红线文档内容。
//
// 设计原则：红线文档与代码解耦，文档更新（新增/修改条款）只需替换文件并重启服务，
// 无需修改任何代码。AI 分析时会将全文注入 Prompt。
//
// 返回规则：
//   - path 为空字符串 → 返回 ("", nil)，上游跳过红线检测
//   - 文件不存在      → 返回 ("", error)，由调用方决定是否降级处理
func LoadRedline(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取服务红线文档 %q 失败: %w", path, err)
	}
	return string(content), nil
}
