package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"group-audit/internal/config"
	"group-audit/internal/model"

	"github.com/cenkalti/backoff/v4"
)

// GroupClient 封装对群聊相关后端接口的调用。
type GroupClient struct {
	baseURL       string
	groupsByDate  string
	groupMessages string
	httpClient    *http.Client
}

// NewGroupClient 创建 GroupClient。
func NewGroupClient(baseURL string, endpoints config.APIEndpointsConfig) *GroupClient {
	return &GroupClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		groupsByDate:  endpoints.GroupsByDate,
		groupMessages: endpoints.GroupMessages,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchGroupIDsByDate 调用 GET /ai/groups/by/date?date=YYYYMMDD，返回当日群聊 ID 列表。
func (c *GroupClient) FetchGroupIDsByDate(ctx context.Context, date string) ([]int, error) {
	type respBody struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []int  `json:"data"`
	}

	var result respBody
	op := func() error {
		requestURL := c.buildURL(c.groupsByDate, map[string]string{
			"date": url.QueryEscape(date),
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return backoff.Permanent(err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			return fmt.Errorf("服务端错误 %d，将重试", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return backoff.Permanent(fmt.Errorf("获取群聊列表返回 %d", resp.StatusCode))
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return backoff.Permanent(fmt.Errorf("解析群聊列表响应失败: %w", err))
		}
		if result.Code != 0 {
			return backoff.Permanent(fmt.Errorf("获取群聊列表业务错误: %s", result.Msg))
		}
		return nil
	}

	bo := newDefaultBackOff()
	if err := backoff.Retry(op, backoff.WithContext(bo, ctx)); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// FetchAllMessages 分页拉取群聊所有消息，自动翻页直至全量拉取完毕。
func (c *GroupClient) FetchAllMessages(ctx context.Context, groupID int) ([]model.GroupMessage, error) {
	var all []model.GroupMessage
	page := 1

	for {
		page_data, totalPages, err := c.fetchPage(ctx, groupID, page)
		if err != nil {
			return nil, err
		}
		all = append(all, page_data...)
		if page >= totalPages {
			break
		}
		page++
	}
	return all, nil
}

// fetchPage 拉取单页消息，带指数退避重试。
func (c *GroupClient) fetchPage(ctx context.Context, groupID, page int) ([]model.GroupMessage, int, error) {
	type outerResp struct {
		Code    int                     `json:"code"`
		Message string                  `json:"message"`
		Data    model.GroupMessagesPage `json:"data"`
	}

	var result outerResp
	op := func() error {
		requestURL := c.buildURL(c.groupMessages, map[string]string{
			"group_id": url.PathEscape(fmt.Sprint(groupID)),
			"page":     url.QueryEscape(fmt.Sprint(page)),
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return backoff.Permanent(err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 500 {
			return fmt.Errorf("服务端错误 %d，将重试", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return backoff.Permanent(fmt.Errorf("获取群聊消息返回 %d", resp.StatusCode))
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return backoff.Permanent(fmt.Errorf("解析群聊消息响应失败: %w", err))
		}
		if result.Code != 0 {
			return backoff.Permanent(fmt.Errorf("获取群聊消息业务错误: %s", result.Message))
		}
		return nil
	}

	bo := newDefaultBackOff()
	if err := backoff.Retry(op, backoff.WithContext(bo, ctx)); err != nil {
		return nil, 0, err
	}

	totalPages := result.Data.TotalPages
	if totalPages == 0 {
		totalPages = 1
	}
	return result.Data.Data, totalPages, nil
}

func (c *GroupClient) buildURL(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	if strings.HasPrefix(result, "http://") || strings.HasPrefix(result, "https://") {
		return result
	}
	return c.baseURL + "/" + strings.TrimLeft(result, "/")
}

// newDefaultBackOff 创建统一的指数退避策略：1s→2s→4s→8s，最长 30s。
func newDefaultBackOff() *backoff.ExponentialBackOff {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = time.Second
	bo.Multiplier = 2.0
	bo.MaxElapsedTime = 30 * time.Second
	return bo
}
