package splitter

import (
	"testing"
	"time"
)

// TestStopIdempotent 验证 Stop 幂等，可多次安全调用。
func TestStopIdempotent(t *testing.T) {
	src, err := Open("COM192", 115200)
	if err != nil {
		t.Skipf("COM192 不可用: %v", err)
	}
	target, err := Open("COM193", 115200)
	if err != nil {
		src.Close()
		t.Skipf("COM193 不可用: %v", err)
	}
	sp := NewSplitter(src, "COM192", []*Serial{target})
	go sp.Run()
	time.Sleep(200 * time.Millisecond)

	// 多次调用 Stop 不应 panic
	for i := 0; i < 3; i++ {
		sp.Stop()
	}
	sp.Close()
	t.Log("Stop 幂等测试通过")
}

// TestStopBeforeRun 验证 Run 前调用 Stop 应立即返回，不阻塞。
func TestStopBeforeRun(t *testing.T) {
	src, err := Open("COM192", 115200)
	if err != nil {
		t.Skipf("COM192 不可用: %v", err)
	}
	target, err := Open("COM193", 115200)
	if err != nil {
		src.Close()
		t.Skipf("COM193 不可用: %v", err)
	}
	sp := NewSplitter(src, "COM192", []*Serial{target})
	done := make(chan struct{})
	go func() {
		sp.Stop()
		close(done)
	}()
	select {
	case <-done:
		// Stop 立即返回，符合预期
	case <-time.After(1 * time.Second):
		t.Fatal("Stop before Run 阻塞超时")
	}
	sp.Close()
	t.Log("Stop before Run 立即返回测试通过")
}
