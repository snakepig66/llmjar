package service

import (
	"sync"
	"time"
)

// 渠道熔断恢复退避：首次复测在禁用 1 分钟后，之后每次失败按 5→10→20→40 分钟翻倍，
// 避免对着长期宕掉的渠道高频干烧测速请求。状态只存内存，进程重启后视为到期立即复测，
// 代价最多是多测一轮，可接受。

var channelRecoveryBackoffMinutes = []float64{1, 5, 10, 20, 40}

type channelRecoveryState struct {
	failCount int
	nextDueAt time.Time
}

var channelRecoveryStates sync.Map // map[int]*channelRecoveryState

func recoveryBackoffDuration(failCount int) time.Duration {
	idx := failCount
	if idx >= len(channelRecoveryBackoffMinutes) {
		idx = len(channelRecoveryBackoffMinutes) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return time.Duration(channelRecoveryBackoffMinutes[idx] * float64(time.Minute))
}

// markChannelAutoDisabled 在渠道被自动禁用时记录首轮复测到期时间（1 分钟后）。
func markChannelAutoDisabled(channelId int) {
	channelRecoveryStates.Store(channelId, &channelRecoveryState{
		failCount: 0,
		nextDueAt: time.Now().Add(recoveryBackoffDuration(0)),
	})
}

// IsChannelRecoveryDue 判断一个自动禁用的渠道是否到达复测时间。
// 没有记录（进程重启、或禁用发生在旧版本运行时）视为立即到期。
func IsChannelRecoveryDue(channelId int) bool {
	raw, ok := channelRecoveryStates.Load(channelId)
	if !ok {
		return true
	}
	state := raw.(*channelRecoveryState)
	return !time.Now().Before(state.nextDueAt)
}

// MarkChannelRecoveryTestFailed 复测仍失败：加大退避，下一次复测时间翻倍。
func MarkChannelRecoveryTestFailed(channelId int) {
	raw, ok := channelRecoveryStates.Load(channelId)
	failCount := 0
	if ok {
		failCount = raw.(*channelRecoveryState).failCount + 1
	}
	channelRecoveryStates.Store(channelId, &channelRecoveryState{
		failCount: failCount,
		nextDueAt: time.Now().Add(recoveryBackoffDuration(failCount)),
	})
}

// ClearChannelRecovery 渠道恢复启用（自动或手动）后清除退避状态。
func ClearChannelRecovery(channelId int) {
	channelRecoveryStates.Delete(channelId)
}
