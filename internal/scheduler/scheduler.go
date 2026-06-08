package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"group-audit/internal/client"
	"group-audit/internal/model"
)

// Scheduler 封装定时任务，负责在触发时间拉取当日群聘 ID 并将任务写入 Channel。
type Scheduler struct {
	c           *cron.Cron
	groupClient client.GroupFetcher
	taskCh      chan<- model.AuditTask
}

// NewScheduler 创建 Scheduler。
//   - cronSpec: cron 表达式，例如 "0 1 * * *"（每天凌晨 1:00）。
//   - taskCh: 任务写入 Channel（只写，由 main 控制生命周期）。
func NewScheduler(cronSpec string, groupClient client.GroupFetcher, taskCh chan<- model.AuditTask) (*Scheduler, error) {
	s := &Scheduler{
		c:           cron.New(),
		groupClient: groupClient,
		taskCh:      taskCh,
	}

	_, err := s.c.AddFunc(cronSpec, s.run)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Start 启动定时调度器（非阻塞）。
func (s *Scheduler) Start() {
	s.c.Start()
	slog.Info("Scheduler 已启动，等待定时触发")
}

// Stop 优雅停止调度器（等待当前 Job 执行完毕）。
func (s *Scheduler) Stop() {
	ctx := s.c.Stop()
	<-ctx.Done()
	slog.Warn("Scheduler 已停止")
}

// RunNow 立即触发一次质检任务（用于手动调试或启动时立即执行）。
func (s *Scheduler) RunNow() {
	go s.run()
}

// run 是 cron Job 的实际执行函数。
func (s *Scheduler) run() {
	date := time.Now().Format("20060102")
	log := slog.With("date", date)
	log.Info("Scheduler 触发，开始拉取群聊列表")

	// 使用带超时的 Context，防止接口长时间挂起阻塞下一个 Job
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ids, err := s.groupClient.FetchGroupIDsByDate(ctx, date)
	if err != nil {
		log.Error("拉取群聊列表失败", "error", err)
		return
	}
	log.Info("群聊列表拉取完成，开始入队", "count", len(ids))

	enqueued := 0
	for _, id := range ids {
		task := model.AuditTask{GroupID: id, Date: date}
		select {
		case s.taskCh <- task:
			enqueued++
		case <-ctx.Done():
			log.Warn("入队超时，剩余任务已丢弃", "enqueued", enqueued, "total", len(ids))
			return
		}
	}
	log.Info("所有任务已入队", "enqueued", enqueued)
}
