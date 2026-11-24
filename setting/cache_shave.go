package setting

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

// CacheShavePolicyKey 是缓存削减策略在 OptionMap 里的键，值为 JSON。
// 移植自 cache-shave 独立网关插件（zhongzhuanzhan/src/cache-shave）：
// 上游自报虚高的缓存命中时，把计费与命中率统计口径的缓存读取 tokens 按
// keep_ratio 压回，只减不增。
const CacheShavePolicyKey = "CacheShavePolicy"

const (
	CacheShaveModeOff     = "off"
	CacheShaveModeObserve = "observe"
	CacheShaveModeOn      = "on"
)

type CacheShavePolicy struct {
	// Mode: off = 总开关关闭（纯透传，不记审计）；observe = 只在日志里记
	// 「如果开启会削多少」，不动计费；on = 真正削减。
	Mode string `json:"mode"`
	// DefaultKeepRatio 是没有命中渠道/模型规则时的保留比例，1 = 不削。
	DefaultKeepRatio float64 `json:"default_keep_ratio"`
	// Channels 按渠道 id 覆盖保留比例。
	Channels map[string]float64 `json:"channels"`
	// Models 按请求的原始模型名覆盖保留比例。
	Models map[string]float64 `json:"models"`
}

// GetCacheShavePolicy 从 OptionMap 读取当前策略；未配置或解析失败时返回
// 关闭的默认策略（等价于完全不启用）。
func GetCacheShavePolicy() CacheShavePolicy {
	policy := CacheShavePolicy{
		Mode:             CacheShaveModeOff,
		DefaultKeepRatio: 1,
	}
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[CacheShavePolicyKey]
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		return policy
	}
	if err := common.UnmarshalJsonStr(raw, &policy); err != nil {
		common.SysError("cache shave policy parse failed: " + err.Error())
		return CacheShavePolicy{Mode: CacheShaveModeOff, DefaultKeepRatio: 1}
	}
	if policy.DefaultKeepRatio <= 0 || policy.DefaultKeepRatio > 1 {
		policy.DefaultKeepRatio = 1
	}
	return policy
}

// KeepRatioFor 按「渠道覆盖 > 模型覆盖 > 默认」解析保留比例，结果收敛到 (0,1]。
// 返回的 ratio 为 1 表示该请求不削减。
func (p *CacheShavePolicy) KeepRatioFor(channelId int, modelName string) float64 {
	ratio := p.DefaultKeepRatio
	if p.Models != nil {
		if v, ok := p.Models[modelName]; ok {
			ratio = v
		}
	}
	if p.Channels != nil {
		if v, ok := p.Channels[strconv.Itoa(channelId)]; ok {
			ratio = v
		}
	}
	if ratio <= 0 || ratio > 1 {
		return 1
	}
	return ratio
}
