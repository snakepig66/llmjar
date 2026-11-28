package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type channelTokenStatsRow struct {
	ChannelId        int     `json:"channel_id"`
	ChannelName      string  `json:"channel_name"`
	Requests         int64   `json:"requests"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	TokenShare       float64 `json:"token_share"`
	RequestShare     float64 `json:"request_share"`
}

// GetChannelTokenStats 按渠道聚合最近 N 小时的消费 token 量与占比，
// 供运营看板展示「各渠道跑了多少、占多少」。
func GetChannelTokenStats(c *gin.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			hours = min(parsed, 24*30)
		}
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()

	type row struct {
		ChannelId        int   `gorm:"column:channel_id"`
		Requests         int64 `gorm:"column:requests"`
		PromptTokens     int64 `gorm:"column:prompt_tokens"`
		CompletionTokens int64 `gorm:"column:completion_tokens"`
	}
	var rows []row
	err := model.LOG_DB.Table("logs").
		Select("channel_id, COUNT(*) as requests, COALESCE(SUM(prompt_tokens),0) as prompt_tokens, COALESCE(SUM(completion_tokens),0) as completion_tokens").
		Where("type = ? AND created_at > ?", model.LogTypeConsume, since).
		Group("channel_id").
		Scan(&rows).Error
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, err)
		return
	}

	channelNames := map[int]string{}
	var channels []model.Channel
	if err := model.DB.Select("id", "name").Find(&channels).Error; err == nil {
		for _, ch := range channels {
			channelNames[ch.Id] = ch.Name
		}
	}

	var totalRequests, totalTokens int64
	stats := make([]channelTokenStatsRow, 0, len(rows))
	for _, r := range rows {
		tokens := r.PromptTokens + r.CompletionTokens
		totalRequests += r.Requests
		totalTokens += tokens
		stats = append(stats, channelTokenStatsRow{
			ChannelId:        r.ChannelId,
			ChannelName:      channelNames[r.ChannelId],
			Requests:         r.Requests,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalTokens:      tokens,
		})
	}
	for i := range stats {
		if totalTokens > 0 {
			stats[i].TokenShare = float64(stats[i].TotalTokens) / float64(totalTokens)
		}
		if totalRequests > 0 {
			stats[i].RequestShare = float64(stats[i].Requests) / float64(totalRequests)
		}
	}
	// token 量大的渠道排前面
	for i := 0; i < len(stats); i++ {
		for j := i + 1; j < len(stats); j++ {
			if stats[j].TotalTokens > stats[i].TotalTokens {
				stats[i], stats[j] = stats[j], stats[i]
			}
		}
	}
	if stats == nil {
		stats = []channelTokenStatsRow{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"window_hours": hours,
			"total": gin.H{
				"requests":         totalRequests,
				"prompt_tokens":    sumPromptTokens(stats),
				"completion_tokens": sumCompletionTokens(stats),
				"total_tokens":     totalTokens,
			},
			"channels": stats,
		},
	})
}

func sumPromptTokens(stats []channelTokenStatsRow) int64 {
	var v int64
	for _, s := range stats {
		v += s.PromptTokens
	}
	return v
}

func sumCompletionTokens(stats []channelTokenStatsRow) int64 {
	var v int64
	for _, s := range stats {
		v += s.CompletionTokens
	}
	return v
}
