package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"golang.org/x/time/rate"

	"group-audit/internal/ai"
	"group-audit/internal/client"
	"group-audit/internal/config"
	"group-audit/internal/mock"
	"group-audit/internal/model"
	"group-audit/internal/scheduler"
	"group-audit/internal/worker"
)

func main() {
	// ── 临时日志（配置加载前）：只输出到 stdout ──────────────
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Local().Format("2006-01-02 15:04:05.000"))
			}
			return a
		},
	})))

	// ── 启动 Banner ──────────────────────────────────────────
	fmt.Println(`╔═══════════════════════════════════════════════╗
║         Group Audit · 群聊质检服务          ║
║           系统正在运行...                    ║
╚═══════════════════════════════════════════════╝`)

	// ── 加载配置文件 ─────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	// ── 初始化正式日志（stdout + 文件双写） ───────────────────
	if err := setupLogger(cfg.Log.File, cfg.Log.Level); err != nil {
		slog.Warn("日志文件初始化失败，将只输出到 stdout", "error", err)
	}

	// ═══ 日志文件分隔标记（使用 Warn 级别，即使在 warn/error 模式下也能看到） ═══
	slog.Warn("══════════════════════════════════════════════")
	slog.Warn("  Group Audit · 群聊质检服务启动")
	slog.Warn(fmt.Sprintf("  模式: %s | AI: %s | Worker: %d",
		map[bool]string{true: "Mock", false: "生产"}[cfg.Mock.Enabled],
		cfg.AI.Model,
		cfg.Worker.Count,
	))
	slog.Warn("══════════════════════════════════════════════")

	slog.Info("配置加载完成",
		"api_base", cfg.API.BaseURL,
		"ai_model", cfg.AI.Model,
		"workers", cfg.Worker.Count,
		"mock", cfg.Mock.Enabled,
		"log_file", cfg.Log.File,
	)

	// ── 加载服务红线文档 ───────────────────────────────
	redlineContent, err := config.LoadRedline(cfg.Redline.Path)
	if err != nil {
		slog.Warn("加载服务红线文档失败，将跳过红线检测", "path", cfg.Redline.Path, "error", err)
		redlineContent = ""
	} else if redlineContent != "" {
		slog.Warn("服务红线文档加载完成", "path", cfg.Redline.Path, "size", len(redlineContent))
	} else {
		slog.Warn("红线文档路径为空，已禁用红线违规检测")
	}

	// ── 依赖初始化（根据 mock.enabled 切换真实/Mock 实现） ───
	// AI Provider 始终使用真实实现，Mock 模式仅替换数据拉取和结果提交
	var (
		groupClient     client.GroupFetcher
		complaintClient client.ComplaintSubmitter
	)

	if cfg.Mock.Enabled {
		slog.Warn("⚠️  Mock 模式已开启，群聊数据和结果提交使用本地 Mock，AI 仍使用真实接口")
		groupClient = &mock.GroupClient{}
		complaintClient = &mock.ComplaintClient{}
	} else {
		groupClient = client.NewGroupClient(cfg.API.BaseURL, cfg.API.Endpoints)
		complaintClient = client.NewComplaintClient(cfg.API.BaseURL, cfg.API.Endpoints)
	}

	aiProvider := ai.NewDeepSeekProvider(
		cfg.AI.BaseURL,
		cfg.AI.APIKey,
		cfg.AI.Model,
	)

	// 全局 LLM 令牌桶限流器（mock 模式下同样生效，但不会阻塞）
	limiter := rate.NewLimiter(rate.Limit(cfg.AI.RPS), cfg.AI.RPS)

	// ── 任务 Channel（原生队列） ───────────────────────────────
	taskCh := make(chan model.AuditTask, cfg.Worker.BufferSize)

	// ── Worker Pool ──────────────────────────────────────────
	pool := worker.NewPool(
		taskCh,
		cfg.Worker.Count,
		cfg.Worker.ResponseTimeoutMinutes,
		cfg.Worker.MaxMessages,
		redlineContent,
		groupClient,
		complaintClient,
		aiProvider,
		limiter,
	)

	// Worker 使用 background context；退出时通过关闭 Channel 通知 Worker 停止
	pool.Start(context.Background())

	// ── Scheduler（Producer） ────────────────────────────────
	sched, err := scheduler.NewScheduler(cfg.Cron.Spec, groupClient, taskCh)
	if err != nil {
		slog.Error("Scheduler 初始化失败", "error", err)
		os.Exit(1)
	}
	sched.Start()

	// 若配置了立即执行，启动时触发一次（调试 / 补跑场景）
	if cfg.Cron.RunNow {
		slog.Info("run_now=true，立即触发一次质检")
		sched.RunNow()
	}

	// ── 优雅退出 ─────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Warn("收到退出信号，开始优雅关闭...")

	// 1. 停止 Scheduler，不再生产新任务
	sched.Stop()

	// 2. 关闭 Channel，Worker 处理完剩余任务后自动退出
	close(taskCh)

	// 3. 等待所有 Worker 退出
	pool.Wait()

	slog.Warn("Group Audit 服务已停止")
}

// setupLogger 初始化结构化日志，同时输出到 stdout 和日志文件（双写）。
// logFile 为空时只写 stdout。
func setupLogger(logFile, level string) error {
	logLevel := slog.LevelInfo
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				// 本地时区 + 自定义格式：2006-01-02 15:04:05.000
				a.Value = slog.StringValue(a.Value.Time().Local().Format("2006-01-02 15:04:05.000"))
			}
			return a
		},
	}

	if logFile == "" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
		return nil
	}

	// 自动创建日志目录
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	// stdout + 文件双写
	w := io.MultiWriter(os.Stdout, f)
	slog.SetDefault(slog.New(slog.NewTextHandler(w, opts)))
	slog.Warn("日志文件已开启", "path", logFile)
	return nil
}
