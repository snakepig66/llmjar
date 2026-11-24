package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheShaveKeepRatioFor(t *testing.T) {
	policy := &CacheShavePolicy{
		Mode:             CacheShaveModeOn,
		DefaultKeepRatio: 0.9,
		Channels:         map[string]float64{"52": 0.5},
		Models:           map[string]float64{"deepseek-v4.1-flash": 0.7},
	}

	// 渠道覆盖优先
	assert.Equal(t, 0.5, policy.KeepRatioFor(52, "deepseek-v4.1-flash"))
	// 模型覆盖其次
	assert.Equal(t, 0.7, policy.KeepRatioFor(51, "deepseek-v4.1-flash"))
	// 默认
	assert.Equal(t, 0.9, policy.KeepRatioFor(51, "other-model"))
}

func TestCacheShaveKeepRatioClamp(t *testing.T) {
	policy := &CacheShavePolicy{
		DefaultKeepRatio: 0,
		Channels:         map[string]float64{"1": 0},
	}
	// 非法值一律当 1（不削）
	assert.Equal(t, 1.0, policy.KeepRatioFor(1, "m"))
	policy.Channels["1"] = 1.5
	assert.Equal(t, 1.0, policy.KeepRatioFor(1, "m"))
}
