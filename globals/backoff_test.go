package globals

import (
	"testing"
	"time"
)

func resetBackoffForTest() {
	ResetFeedBackoff()
}

func TestBackoffStartsAfterThreshold(t *testing.T) {
	resetBackoffForTest()
	now := time.Now()
	interval := 5 * time.Minute

	RecordFetchFailure("https://example.com/a", interval, now)
	RecordFetchFailure("https://example.com/a", interval, now)
	if !ShouldFetch("https://example.com/a", now) {
		t.Fatal("两次失败不应进入退避")
	}

	RecordFetchFailure("https://example.com/a", interval, now)
	if ShouldFetch("https://example.com/a", now) {
		t.Fatal("第三次连续失败后应立即进入退避")
	}
	if got := BackoffCount(now); got != 1 {
		t.Fatalf("退避源数量 = %d, 期望 1", got)
	}
}

func TestBackoffRecoversAfterDelay(t *testing.T) {
	resetBackoffForTest()
	now := time.Now()
	interval := 5 * time.Minute

	for i := 0; i < 3; i++ {
		RecordFetchFailure("https://example.com/b", interval, now)
	}
	if ShouldFetch("https://example.com/b", now) {
		t.Fatal("应处于退避期")
	}
	// 第三次失败退避一个刷新周期。
	if !ShouldFetch("https://example.com/b", now.Add(interval+time.Second)) {
		t.Fatal("退避期结束后应恢复抓取")
	}
}

func TestBackoffSuccessResets(t *testing.T) {
	resetBackoffForTest()
	now := time.Now()

	for i := 0; i < 4; i++ {
		RecordFetchFailure("https://example.com/c", 5*time.Minute, now)
	}
	RecordFetchSuccess("https://example.com/c")
	if !ShouldFetch("https://example.com/c", now) {
		t.Fatal("成功后应清除退避状态")
	}
	if got := BackoffCount(now); got != 0 {
		t.Fatalf("退避源数量 = %d, 期望 0", got)
	}
}

func TestBackoffCapped(t *testing.T) {
	resetBackoffForTest()
	now := time.Now()

	for i := 0; i < 40; i++ {
		RecordFetchFailure("https://example.com/d", 30*time.Minute, now)
	}
	if ShouldFetch("https://example.com/d", now.Add(59*time.Minute)) {
		t.Fatal("退避期内不应抓取")
	}
	if !ShouldFetch("https://example.com/d", now.Add(61*time.Minute)) {
		t.Fatal("退避封顶 60 分钟，之后应恢复抓取")
	}
}

func TestResetFeedBackoffClearsAll(t *testing.T) {
	resetBackoffForTest()
	now := time.Now()

	for i := 0; i < 3; i++ {
		RecordFetchFailure("https://example.com/e", 5*time.Minute, now)
	}
	ResetFeedBackoff()
	if !ShouldFetch("https://example.com/e", now) {
		t.Fatal("重置后应恢复抓取")
	}
}
