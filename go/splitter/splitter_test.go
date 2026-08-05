package splitter

import (
	"testing"
	"time"
)

// TestSplitterForward 闭环测试：用 com0com 临时创建两对端口
// 对1: 源/假板，对2: 接管/终端
func TestSplitterForward(t *testing.T) {
	// 创建两对端口
	a1, b1, err := CreatePair() // 源=CNCA, 假板=CNCB
	if err != nil {
		t.Skipf("创建端口对1失败: %v", err)
	}
	a2, b2, err := CreatePair() // 接管=CNCB, 终端=CNCA
	if err != nil {
		t.Skipf("创建端口对2失败: %v", err)
	}
	t.Logf("端口对1: %s/%s, 端口对2: %s/%s", a1, b1, a2, b2)

	// 源取 a1(CNCA)，假板连 b1(CNCB)
	src, err := Open(a1, 115200)
	if err != nil {
		t.Fatalf("打开源 %s: %v", a1, err)
	}
	target, err := Open(b2, 115200) // 接管=CNCB
	if err != nil {
		src.Close()
		t.Fatalf("打开接管 %s: %v", b2, err)
	}
	term, err := Open(a2, 115200) // 终端=CNCA
	if err != nil {
		src.Close()
		target.Close()
		t.Fatalf("打开终端 %s: %v", a2, err)
	}
	board, err := Open(b1, 115200) // 假板
	if err != nil {
		src.Close()
		target.Close()
		term.Close()
		t.Fatalf("打开假板 %s: %v", b1, err)
	}

	sp := NewSplitter(src, a1, []*Serial{target})
	go sp.Run()
	time.Sleep(300 * time.Millisecond)

	// 1) 板子 -> 终端
	board.Write([]byte("BOARD_GO\n"))
	time.Sleep(400 * time.Millisecond)
	buf := make([]byte, 64)
	n, _ := term.Read(buf)
	got := string(buf[:n])
	t.Logf("终端收到: %q", got)
	if got == "" {
		t.Error("终端未收到板子广播")
	}

	// 2) 终端 -> 板子
	term.Write([]byte("TERM_GO\n"))
	time.Sleep(400 * time.Millisecond)
	n, _ = board.Read(buf)
	got = string(buf[:n])
	t.Logf("板子收到: %q", got)
	if got == "" {
		t.Error("板子未收到终端输入")
	}

	sp.Stop()
	sp.Close()
	term.Close()
	board.Close()
}
