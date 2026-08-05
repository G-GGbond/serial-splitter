// Package splitter 实现串口分线核心逻辑：
// 一个源串口分出多个终端，双向透明转发。
package splitter

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Serial 封装一个串口读写。
// go.bug.st/serial 的 Port 内部已线程安全，此处直接透传。
type Serial struct {
	port serial.Port
	mu   sync.Mutex // 仅保护 Close 与 Read/Write 的竞争
}

// Open 打开串口。
func Open(name string, baud int) (*Serial, error) {
	c := &serial.Mode{
		BaudRate: baud,
	}
	p, err := serial.Open(name, c)
	if err != nil {
		return nil, err
	}
	// 短超时：有数据立即返回，无数据最多等 5ms，保证转发低延迟
	p.SetReadTimeout(5 * time.Millisecond)
	return &Serial{port: p}, nil
}

// Close 关闭串口。
func (s *Serial) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.port != nil {
		s.port.Close()
		s.port = nil
	}
}

// Read 读串口数据（阻塞，数据到达立即返回）。
func (s *Serial) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.port == nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("port closed")
	}
	// 读取期间释放锁，避免阻塞读锁住 Write
	s.mu.Unlock()
	return s.port.Read(p)
}

// Write 写串口数据。
func (s *Serial) Write(p []byte) (int, error) {
	s.mu.Lock()
	if s.port == nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("port closed")
	}
	s.mu.Unlock()
	return s.port.Write(p)
}

// Splitter 一个分线器实例。
type Splitter struct {
	source     *Serial
	targets    []*Serial
	stop       chan struct{}
	done       chan struct{}
	mu         sync.Mutex
	running    bool
	sourceName string
}

// NewSplitter 创建分线器。
func NewSplitter(source *Serial, sourceName string, targets []*Serial) *Splitter {
	return &Splitter{
		source:     source,
		targets:    targets,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		sourceName: sourceName,
	}
}

// Run 启动分线，阻塞直到 Stop 被调用。
func (sp *Splitter) Run() {
	defer close(sp.done)
	sp.mu.Lock()
	sp.running = true
	sp.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(1 + len(sp.targets))

	// 源 -> 所有目标（广播）
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-sp.stop:
				return
			default:
			}
			n, err := sp.source.Read(buf)
			if err != nil || n == 0 {
				continue
			}
			for _, t := range sp.targets {
				t.Write(buf[:n])
			}
		}
	}()

	// 每个目标 -> 源
	for _, t := range sp.targets {
		go func(t *Serial) {
			defer wg.Done()
			buf := make([]byte, 4096)
			for {
				select {
				case <-sp.stop:
					return
				default:
				}
				n, err := t.Read(buf)
				if err != nil || n == 0 {
					continue
				}
				sp.source.Write(buf[:n])
			}
		}(t)
	}

	wg.Wait()
	sp.mu.Lock()
	sp.running = false
	sp.mu.Unlock()
}

// Stop 停止分线。
func (sp *Splitter) Stop() {
	close(sp.stop)
	<-sp.done
}

// IsRunning 是否运行中。
func (sp *Splitter) IsRunning() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.running
}

// SourceName 返回源串口名。
func (sp *Splitter) SourceName() string {
	return sp.sourceName
}

// Close 关闭所有串口。
func (sp *Splitter) Close() {
	sp.source.Close()
	for _, t := range sp.targets {
		t.Close()
	}
}
