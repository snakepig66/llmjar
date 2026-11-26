// Package channelrpm 统计每个渠道的实时请求速率（RPM）并学习上游限流上限，
// 供渠道选择逻辑做软加权：接近打满的渠道权重自动衰减，但不归零，
// 从而在不限死并发的前提下把流量自然导向有余量的渠道。
package channelrpm

import (
	"math"
	"sync"
	"time"
)

// headroomFloor 是权重因子的下限：即使打满也保留 5% 权重，
// 保证唯一可用渠道仍能被选中、以及重试时有机会探测恢复。
const headroomFloor = 0.05

type channelState struct {
	mu          sync.Mutex
	curMinute   int64
	curCount    float64
	prevCount   float64
	cap         float64 // 0 = 未知/不限
	lastUpdated int64
}

var states sync.Map // map[int]*channelState

func getState(channelId int) *channelState {
	raw, _ := states.LoadOrStore(channelId, &channelState{})
	return raw.(*channelState)
}

func minuteNow() int64 {
	return time.Now().Unix() / 60
}

// RecordHit 记录一次对该渠道的上游请求尝试（含亲和与重试路径）。
func RecordHit(channelId int) {
	if channelId <= 0 {
		return
	}
	st := getState(channelId)
	st.mu.Lock()
	defer st.mu.Unlock()
	m := minuteNow()
	if st.curMinute != m {
		if m-st.curMinute == 1 {
			st.prevCount = st.curCount
		} else {
			st.prevCount = 0
		}
		st.curMinute = m
		st.curCount = 0
	}
	st.curCount++
	st.lastUpdated = time.Now().Unix()
}

// currentRPM 用两分钟滑动窗口估算当前 RPM。
func (st *channelState) currentRPMLocked() float64 {
	m := minuteNow()
	if st.curMinute != m {
		if m-st.curMinute == 1 {
			// 上一分钟的计数按已经流逝的比例衰减
			elapsed := float64(time.Now().Unix() % 60)
			return st.prevCount * (60 - elapsed) / 60
		}
		return 0
	}
	elapsed := float64(time.Now().Unix()%60) + 1
	return st.curCount + st.prevCount*(60-elapsed)/60
}

// CurrentRPM 导出当前速率估算，供调试和测试。
func CurrentRPM(channelId int) float64 {
	st := getState(channelId)
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.currentRPMLocked()
}

// LearnCapFromRateLimit 在渠道触发 429/限流时调用：
// 把当时的速率估算记为该渠道的 RPM 上限。重复触发时会向下修正，
// 上限不会自动调高，除非渠道长期在更高速率下无错运行。
func LearnCapFromRateLimit(channelId int) {
	if channelId <= 0 {
		return
	}
	st := getState(channelId)
	st.mu.Lock()
	defer st.mu.Unlock()
	rpm := st.currentRPMLocked()
	learned := math.Max(1, math.Floor(rpm))
	if st.cap == 0 || learned < st.cap {
		st.cap = learned
	}
}

// maybeRaiseCapLocked 慢速恢复：速率持续超过 cap 而没有限流，说明上限学低了。
func (st *channelState) maybeRaiseCapLocked(rpm float64) {
	if st.cap > 0 && rpm > st.cap {
		st.cap = math.Ceil(rpm)
	}
}

// HeadroomFactor 返回该渠道的权重因子 (0,1]：
// 无上限信息时恒为 1（不影响选择）；有上限时按余量线性衰减，下限 5%。
func HeadroomFactor(channelId int) float64 {
	st := getState(channelId)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cap <= 0 {
		return 1
	}
	rpm := st.currentRPMLocked()
	st.maybeRaiseCapLocked(rpm)
	headroom := (st.cap - rpm) / st.cap
	if headroom < headroomFloor {
		return headroomFloor
	}
	if headroom > 1 {
		return 1
	}
	return headroom
}

// Reset 清除全部状态（仅测试用）。
func Reset() {
	states.Range(func(k, _ any) bool {
		states.Delete(k)
		return true
	})
}
