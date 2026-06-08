package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/time/rate"

	"group-audit/internal/ai"
	"group-audit/internal/audit"
	"group-audit/internal/client"
	"group-audit/internal/model"
)

// Pool 是 Worker Pool 的核心结构，持有任务 Channel 和所有依赖。
type Pool struct {
	taskCh          chan model.AuditTask      // 任务队列 Channel，由 Scheduler 写入，Worker 消费；Channel 关闭后 Worker 自动退出
	workerCount     int                       // 并发 Worker（Goroutine）数量
	timeoutMin      float64                   // 服务人员响应超时阈值（分钟），超过此值触发定量警告
	maxMessages     int                       // LLM 单次分析传入的最大消息条数，超出时截断
	redlineContent  string                    // 服务红线文档全文，启动时从文件加载后注入 LLM Prompt；为空时跳过红线违规检测
	groupClient     client.GroupFetcher       // 群聊数据拉取接口，支持真实 HTTP 实现和 Mock 实现
	complaintClient client.ComplaintSubmitter // 投诉/表扬提交接口，支持真实 HTTP 实现和 Mock 实现
	aiProvider      ai.Provider               // 大模型调用接口，支持 DeepSeek、本地 Ollama 等任何 OpenAI 协议兼容实现
	limiter         *rate.Limiter             // 全局 LLM 令牌桶限流器，防止并发 Worker 瞬间打穿 AI API 配额
	wg              sync.WaitGroup            // 等待所有 Worker Goroutine 退出，用于优雅关闭
}

// NewPool 创建 Worker Pool。
//
//   - taskCh: 外部传入的任务 Channel（由 Scheduler 写入）。
//   - workerCount: 并发 Goroutine 数量。
//   - timeoutMin: 响应超时阈值（分钟）。
//   - limiter: LLM 全局速率限制器。
func NewPool(
	taskCh chan model.AuditTask,
	workerCount int,
	timeoutMin float64,
	maxMessages int,
	redlineContent string,
	groupClient client.GroupFetcher,
	complaintClient client.ComplaintSubmitter,
	aiProvider ai.Provider,
	limiter *rate.Limiter,
) *Pool {
	return &Pool{
		taskCh:          taskCh,
		workerCount:     workerCount,
		timeoutMin:      timeoutMin,
		maxMessages:     maxMessages,
		redlineContent:  redlineContent,
		groupClient:     groupClient,
		complaintClient: complaintClient,
		aiProvider:      aiProvider,
		limiter:         limiter,
	}
}

// Start 启动所有 Worker Goroutine。调用方应在程序退出前调用 Wait。
func (p *Pool) Start(ctx context.Context) {
	for i := range p.workerCount {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.runWorker(ctx, id)
		}(i + 1)
	}
	slog.Info("Worker Pool 已启动", "count", p.workerCount)
}

// Wait 阻塞直到所有 Worker 退出（Channel 关闭后自然退出）。
func (p *Pool) Wait() {
	p.wg.Wait()
	slog.Info("Worker Pool 已全部退出")
}

// runWorker 是单个 Worker 的主循环：从 taskCh 中抢占任务并执行。
// Channel 关闭后循环自动结束。
func (p *Pool) runWorker(ctx context.Context, id int) {
	for task := range p.taskCh {
		log := slog.With("worker", id, "group_id", task.GroupID, "date", task.Date)
		log.Info("开始处理任务")

		if err := p.processTask(ctx, task, log); err != nil {
			log.Error("任务处理失败", "error", err)
		} else {
			log.Info("任务处理完成")
		}
	}
}

// processTask 执行单个任务的完整流程：
// Pull → Fetch → Quantitative → Qualitative(LLM) → Submit
func (p *Pool) processTask(ctx context.Context, task model.AuditTask, log *slog.Logger) error {
	// ── Step 1: Fetch ─────────────────────────────────────
	messages, err := p.groupClient.FetchAllMessages(ctx, task.GroupID)
	if err != nil {
		return err
	}
	log.Info("消息拉取完成", "count", len(messages))

	// 打印消息摘要（Debug）
	log.Debug("━━━ 消息记录 ━━━")
	for i, m := range messages {
		roleName := roleLabel(m.Role)
		log.Debug(fmt.Sprintf("  [%02d] %s %s(%d): %s", i+1, m.CreatedTime, roleName, m.UserID, m.Content))
	}
	log.Debug("━━━━━━━━━━━━━")

	// ── Step 2: Quantitative（纯规则，无网络调用） ─────────
	quantResult := audit.Quantitative(messages, p.timeoutMin)
	log.Info("定量分析完成", "warnings", len(quantResult.Complaints))
	for i, req := range quantResult.Complaints {
		log.Warn(fmt.Sprintf("  ⚠ 定量警告[%d] 原因=%s", i+1, req.Reason))
	}

	// ── Step 3: Qualitative（LLM，受速率限制） ────────────
	qualResult, err := audit.Qualitative(ctx, p.aiProvider, p.limiter, messages, p.redlineContent, p.maxMessages)
	if err != nil {
		log.Warn("定性分析失败，跳过本次 LLM 判断", "error", err)
	} else {
		log.Info("定性分析完成", "complaints", len(qualResult.Complaints))
		for i, req := range qualResult.Complaints {
			typeLabel := "正向激励(表扬/经验)"
			if req.Type == model.ComplaintWarning {
				typeLabel = "改进提醒(警告)"
			}
			log.Info(fmt.Sprintf("  ★ 定性结果[%d] %s | 原因=%s", i+1, typeLabel, req.Reason))
		}
	}

	// ── Step 4: Submit ────────────────────────────────────
	allComplaints := append(quantResult.Complaints, qualResult.Complaints...)
	log.Info("开始提交结果", "total", len(allComplaints))
	for i, req := range allComplaints {
		if err := p.complaintClient.Submit(ctx, task.GroupID, req); err != nil {
			log.Error(fmt.Sprintf("  ✗ 提交[%d] 失败", i+1), "error", err)
		} else {
			log.Debug(fmt.Sprintf("  ✓ 提交[%d] 成功", i+1))
		}
	}
	log.Info("结果提交完成", "submitted", len(allComplaints))

	return nil
}

// roleLabel 将角色数值转为可读的中文标签。
func roleLabel(role int) string {
	switch role {
	case model.RoleUser:
		return "用户"
	case model.RoleHeadCoach:
		return "主教练"
	case model.RoleAssistantCoach:
		return "副教练"
	case model.RoleCustomerService:
		return "客服"
	default:
		return fmt.Sprintf("未知(%d)", role)
	}
}
