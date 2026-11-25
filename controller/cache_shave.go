package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// cacheShaveLogRecord 对应 service 写入管理侧 admin_info.cache_shave 的结构。
type cacheShaveLogRecord struct {
	Mode      string  `json:"mode"`
	KeepRatio float64 `json:"keep_ratio"`
	Original  int     `json:"original"`
	Effective int     `json:"effective"`
	Applied   bool    `json:"applied"`
}

type cacheShaveChannelStats struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Requests    int    `json:"requests"`
	Applied     int    `json:"applied"`
	Observed    int    `json:"observed"`
	Original    int64  `json:"original"`
	Effective   int64  `json:"effective"`
	Shaved      int64  `json:"shaved"`
}

// GetCacheShaveStats 聚合最近 N 小时消费日志里的缓存削减记录，
// 给运营设置页展示「各渠道原报/削后缓存 tokens」。
func GetCacheShaveStats(c *gin.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			hours = min(parsed, 24*30)
		}
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()

	var logs []model.Log
	err := model.LOG_DB.
		Select("channel_id", "other").
		Where("type = ? AND created_at > ? AND other LIKE ?", model.LogTypeConsume, since, "%cache_shave%").
		Limit(20000).
		Find(&logs).Error
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

	type agg struct {
		requests  int
		applied   int
		observed  int
		original  int64
		effective int64
	}
	byChannel := map[int]*agg{}
	total := &agg{}
	for _, log := range logs {
		var other struct {
			AdminInfo struct {
				CacheShave *cacheShaveLogRecord `json:"cache_shave"`
			} `json:"admin_info"`
		}
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			continue
		}
		record := other.AdminInfo.CacheShave
		if record == nil {
			continue
		}
		entry := byChannel[log.ChannelId]
		if entry == nil {
			entry = &agg{}
			byChannel[log.ChannelId] = entry
		}
		for _, target := range []*agg{entry, total} {
			target.requests++
			target.original += int64(record.Original)
			target.effective += int64(record.Effective)
			if record.Applied {
				target.applied++
			} else {
				target.observed++
			}
		}
	}

	channelStats := make([]cacheShaveChannelStats, 0, len(byChannel))
	for channelId, entry := range byChannel {
		channelStats = append(channelStats, cacheShaveChannelStats{
			ChannelId:   channelId,
			ChannelName: channelNames[channelId],
			Requests:    entry.requests,
			Applied:     entry.applied,
			Observed:    entry.observed,
			Original:    entry.original,
			Effective:   entry.effective,
			Shaved:      entry.original - entry.effective,
		})
	}
	// 按削掉的量降序
	for i := range channelStats {
		for j := i + 1; j < len(channelStats); j++ {
			if channelStats[j].Shaved > channelStats[i].Shaved {
				channelStats[i], channelStats[j] = channelStats[j], channelStats[i]
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"window_hours": hours,
			"total": gin.H{
				"requests":  total.requests,
				"applied":   total.applied,
				"observed":  total.observed,
				"original":  total.original,
				"effective": total.effective,
				"shaved":    total.original - total.effective,
			},
			"channels": channelStats,
		},
	})
}
