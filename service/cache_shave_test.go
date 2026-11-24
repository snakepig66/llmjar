package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/assert"
)

func setCacheShaveOption(t *testing.T, value string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[setting.CacheShavePolicyKey] = value
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, setting.CacheShavePolicyKey)
		common.OptionMapRWMutex.Unlock()
	})
}

func TestApplyCacheShave(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 52}, OriginModelName: "deepseek-v4.1-flash"}
	usage := &dto.Usage{}
	usage.PromptTokensDetails.CachedTokens = 1000

	// off：不动也不记
	setCacheShaveOption(t, `{"mode":"off","default_keep_ratio":0.5}`)
	assert.Nil(t, applyCacheShave(info, usage))
	assert.Equal(t, 1000, usage.PromptTokensDetails.CachedTokens)

	// on：按渠道覆盖削减
	setCacheShaveOption(t, `{"mode":"on","default_keep_ratio":1,"channels":{"52":0.5}}`)
	record := applyCacheShave(info, usage)
	assert.NotNil(t, record)
	assert.True(t, record.Applied)
	assert.Equal(t, 1000, record.Original)
	assert.Equal(t, 500, record.Effective)
	assert.Equal(t, 500, usage.PromptTokensDetails.CachedTokens)

	// observe：只记不动
	usage.PromptTokensDetails.CachedTokens = 1000
	setCacheShaveOption(t, `{"mode":"observe","default_keep_ratio":0.5}`)
	record = applyCacheShave(info, usage)
	assert.NotNil(t, record)
	assert.False(t, record.Applied)
	assert.Equal(t, 500, record.Effective)
	assert.Equal(t, 1000, usage.PromptTokensDetails.CachedTokens)
}

func TestApplyCacheShaveSkips(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 52}, OriginModelName: "m"}
	usage := &dto.Usage{}

	// 无缓存 tokens
	setCacheShaveOption(t, `{"mode":"on","default_keep_ratio":0.5}`)
	assert.Nil(t, applyCacheShave(info, usage))

	// 比例为 1：不削也不记
	usage.PromptTokensDetails.CachedTokens = 100
	setCacheShaveOption(t, `{"mode":"on","default_keep_ratio":1}`)
	assert.Nil(t, applyCacheShave(info, usage))
	assert.Equal(t, 100, usage.PromptTokensDetails.CachedTokens)

	// nil usage
	assert.Nil(t, applyCacheShave(info, nil))
}
