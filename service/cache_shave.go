package service

import (
	"math"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"
)

// 缓存削减（移植自 cache-shave 插件）：上游自报虚高的缓存命中时，把计费与
// 命中率统计口径的缓存读取 tokens 按策略 keep_ratio 压回，只减不增。
// 作用点在 effectiveBillingUsage 之后，下游所有口径（配额计算、cache_tokens
// 统计、渠道亲和度、分层计费）看到的都是削后的值，与插件改写响应的效果一致。
// observe 模式只记录「如果开启会削多少」进管理侧日志，不动计费。

type cacheShaveRecord struct {
	Mode      string  `json:"mode"`
	KeepRatio float64 `json:"keep_ratio"`
	Original  int     `json:"original"`
	Effective int     `json:"effective"`
	Applied   bool    `json:"applied"`
}

// applyCacheShave 按当前策略削减 usage 里的缓存读取 tokens，返回记录；
// 无需处理（开关关闭、没有缓存 tokens、策略比例为 1）时返回 nil。
func applyCacheShave(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) *cacheShaveRecord {
	if relayInfo == nil || usage == nil {
		return nil
	}
	policy := setting.GetCacheShavePolicy()
	if policy.Mode == setting.CacheShaveModeOff {
		return nil
	}
	original := usage.PromptTokensDetails.CachedTokens
	if original <= 0 {
		return nil
	}
	keepRatio := policy.KeepRatioFor(relayInfo.GetChannelID(), relayInfo.GetOriginModelName())
	if keepRatio >= 1 {
		return nil
	}
	effective := int(math.Floor(float64(original) * keepRatio))
	record := &cacheShaveRecord{
		Mode:      policy.Mode,
		KeepRatio: keepRatio,
		Original:  original,
		Effective: effective,
		Applied:   policy.Mode == setting.CacheShaveModeOn,
	}
	if record.Applied {
		usage.PromptTokensDetails.CachedTokens = effective
	}
	return record
}

func appendCacheShaveLogInfo(other *model.LogOther, record *cacheShaveRecord) {
	if other == nil || record == nil {
		return
	}
	other.SetAdmin("cache_shave", record)
}
