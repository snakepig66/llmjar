package controller

import (
	"net/http"
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

// beijingNow 用固定 +8 时区求北京日历边界，与服务器本地时区无关。
var beijingLocation = time.FixedZone("Asia/Shanghai", 8*3600)

// channelTokenStatsSince 按 period 求统计起点：今日/本周（周一起）/本月，均按北京日历；
// 缺省或未知值回退到最近 24 小时。
func channelTokenStatsSince(period string) (since int64, label string) {
	now := time.Now().In(beijingLocation)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, beijingLocation)
	switch period {
	case "today":
		return midnight.Unix(), "今日"
	case "week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return midnight.AddDate(0, 0, -(weekday - 1)).Unix(), "本周"
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, beijingLocation).Unix(), "本月"
	}
	return time.Now().Add(-24 * time.Hour).Unix(), "最近 24 小时"
}

// GetChannelTokenStats 按渠道聚合一个日历周期内的消费 token 量与占比，
// 供运营看板展示「各渠道跑了多少、占多少」。
func GetChannelTokenStats(c *gin.Context) {
	period := c.DefaultQuery("period", "")
	since, periodLabel := channelTokenStatsSince(period)
	hours := int(time.Since(time.Unix(since, 0)).Hours()) + 1

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
			"period":       periodLabel,
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
