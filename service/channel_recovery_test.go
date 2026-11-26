package service

import (
	"testing"
	"time"
)

func TestChannelRecoveryBackoffSequence(t *testing.T) {
	id := 990001
	ClearChannelRecovery(id)

	// 无记录（重启/旧状态）视为立即到期
	if !IsChannelRecoveryDue(id) {
		t.Fatal("no state should be due immediately")
	}

	markChannelAutoDisabled(id)
	if IsChannelRecoveryDue(id) {
		t.Fatal("just disabled should not be due within 1 minute")
	}

	// 第一次失败后排 5 分钟
	MarkChannelRecoveryTestFailed(id)
	raw, _ := channelRecoveryStates.Load(id)
	state := raw.(*channelRecoveryState)
	if state.failCount != 1 {
		t.Fatalf("failCount = %d, want 1", state.failCount)
	}
	wantMin := time.Now().Add(4 * time.Minute)
	if state.nextDueAt.Before(wantMin) {
		t.Fatalf("after 1st failure nextDueAt too early: %v", state.nextDueAt)
	}

	// 连续失败 failCount 递增
	MarkChannelRecoveryTestFailed(id)
	raw, _ = channelRecoveryStates.Load(id)
	if raw.(*channelRecoveryState).failCount != 2 {
		t.Fatal("failCount should be 2")
	}

	ClearChannelRecovery(id)
	if !IsChannelRecoveryDue(id) {
		t.Fatal("cleared state should be due immediately")
	}
}
