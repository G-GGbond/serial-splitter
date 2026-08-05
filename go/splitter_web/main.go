// 串口分线器 - 本地 Web 版
// Go 后端：串口分线管理 + HTTP API + SSE 状态推送
// 前端：内嵌 HTML/CSS/JS，用系统浏览器打开
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"serialsplitter/splitter"
)

//go:embed web
var webFS embed.FS

// -------- 状态模型 --------

// Pair 端口对
type Pair struct {
	Takeover string `json:"takeover"` // 分线器接管端口 (CNCB)
	Terminal string `json:"terminal"` // 终端连接端口 (CNCA)
}

// Unit 分线器单元
type Unit struct {
	mu        sync.Mutex
	ID        int      `json:"id"`
	Source    string   `json:"source"`
	Baud      int      `json:"baud"`
	Pairs     []Pair   `json:"pairs"`
	Running   bool     `json:"running"`
	LastError string   `json:"lastError"`
	splitter  *splitter.Splitter
}

// App 应用状态
type App struct {
	mu     sync.Mutex
	units  []*Unit
	nextID int
	hub    *Hub
}

func (u *Unit) snapshot() map[string]interface{} {
	u.mu.Lock()
	defer u.mu.Unlock()
	pairs := u.Pairs
	if pairs == nil {
		pairs = []Pair{}
	}
	return map[string]interface{}{
		"id":        u.ID,
		"source":    u.Source,
		"baud":      u.Baud,
		"pairs":     pairs,
		"running":   u.Running,
		"lastError": u.LastError,
	}
}

func (a *App) snapshotAll() []map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]map[string]interface{}, 0, len(a.units))
	for _, u := range a.units {
		result = append(result, u.snapshot())
	}
	return result
}

func (a *App) findUnit(id int) *Unit {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, u := range a.units {
		if u.ID == id {
			return u
		}
	}
	return nil
}

// -------- SSE Hub --------

// Hub SSE 客户端管理
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: map[chan []byte]struct{}{}}
}

func (h *Hub) Add(ch chan []byte) {
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Remove(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(state []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- state:
		default:
		}
	}
}

// -------- 端口枚举 --------

func portsResponse() map[string]interface{} {
	real := []string{}
	virt := []string{}
	for _, p := range splitter.ListPorts() {
		if strings.Contains(p.Description, "com0com") {
			virt = append(virt, p.Device)
		} else {
			real = append(real, p.Device)
		}
	}
	sort.Strings(real)
	sort.Strings(virt)

	// com0com 端口对
	com0 := splitter.Com0comMap()
	caByIdx := map[int]string{}
	cbByIdx := map[int]string{}
	for _, pi := range com0 {
		if pi.Kind == "CNCA" {
			caByIdx[pi.Idx] = pi.Device
		} else {
			cbByIdx[pi.Idx] = pi.Device
		}
	}
	pairs := []Pair{}
	for idx, cb := range cbByIdx {
		if ca, ok := caByIdx[idx]; ok {
			pairs = append(pairs, Pair{Takeover: cb, Terminal: ca})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Terminal < pairs[j].Terminal
	})

	return map[string]interface{}{
		"real":  real,
		"virt":  virt,
		"pairs": pairs,
	}
}

// -------- main --------

func main() {
	app := &App{nextID: 1, hub: NewHub()}

	mux := http.NewServeMux()

	// 静态资源（内嵌前端）
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	// API
	mux.HandleFunc("/api/ports", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, portsResponse())
	})

	// 全部分线器
	mux.HandleFunc("/api/units/all", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, app.snapshotAll())
	})

	// 新建分线器
	mux.HandleFunc("/api/units/new", func(w http.ResponseWriter, r *http.Request) {
		app.mu.Lock()
		u := &Unit{ID: app.nextID, Pairs: []Pair{}}
		app.nextID++
		app.units = append(app.units, u)
		app.mu.Unlock()
		app.broadcast()
		writeJSON(w, u.snapshot())
	})

	// 删除分线器
	mux.HandleFunc("/api/units/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/units/")
		parts := strings.SplitN(path, "/", 2)
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			http.Error(w, "bad id", 400)
			return
		}
		u := app.findUnit(id)
		if u == nil {
			http.Error(w, "not found", 404)
			return
		}
		if len(parts) > 1 {
			// 子操作
			switch parts[1] {
			case "start":
				handleStart(w, r, app, u)
			case "stop":
				handleStop(w, r, app, u)
			case "addpair":
				handleAddPair(w, r, app, u)
			case "addmanual":
				handleAddManual(w, r, app, u)
			case "delpair":
				handleDelPair(w, r, app, u)
			default:
				http.Error(w, "unknown action", 400)
			}
			return
		}
		// 删除
		u.mu.Lock()
		var sp *splitter.Splitter
		if u.Running && u.splitter != nil {
			sp = u.splitter
			u.splitter = nil
			u.Running = false
		} else if u.Running {
			// 启动中：标记停止，启动 goroutine 会检测到并释放端口
			u.Running = false
		}
		u.mu.Unlock()
		if sp != nil {
			sp.Stop()
			sp.Close()
		}
		app.mu.Lock()
		for i, v := range app.units {
			if v == u {
				app.units = append(app.units[:i], app.units[i+1:]...)
				break
			}
		}
		app.mu.Unlock()
		app.broadcast()
		writeJSON(w, map[string]bool{"ok": true})
	})

	// SSE 状态推送
	mux.HandleFunc("/api/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)

		ch := make(chan []byte, 16)
		app.hub.Add(ch)
		defer app.hub.Remove(ch)

		// 立即推送一次当前状态
		data, _ := json.Marshal(app.snapshotAll())
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case state := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", state)
				flusher.Flush()
			case <-heartbeat.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	// 自动打开浏览器
	go openBrowser("http://127.0.0.1:18768")

	log.Println("串口分线器已启动: http://127.0.0.1:18768")
	if err := http.ListenAndServe("127.0.0.1:18768", mux); err != nil {
		log.Fatal(err)
	}
}

func (a *App) broadcast() {
	data, _ := json.Marshal(a.snapshotAll())
	a.hub.Broadcast(data)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// -------- 单元操作 --------

func handleStart(w http.ResponseWriter, r *http.Request, app *App, u *Unit) {
	u.mu.Lock()
	if u.Running {
		u.mu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	var req struct {
		Source string `json:"source"`
		Baud   int    `json:"baud"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Source == "" || req.Baud <= 0 {
		u.mu.Unlock()
		http.Error(w, "bad request", 400)
		return
	}
	if len(u.Pairs) == 0 {
		u.mu.Unlock()
		http.Error(w, "no pairs", 400)
		return
	}
	// 复制端口对（避免锁外读取数据竞争）
	pairs := make([]Pair, len(u.Pairs))
	copy(pairs, u.Pairs)
	u.Source = req.Source
	u.Baud = req.Baud
	u.Running = true
	u.mu.Unlock()

	// 后台异步打开串口并启动转发
	go func() {
		src, err := splitter.Open(req.Source, req.Baud)
		if err != nil {
			u.mu.Lock()
			u.Running = false
			u.mu.Unlock()
			log.Printf("分线器 %d 打开源串口 %s 失败: %v", u.ID, req.Source, err)
			app.broadcast()
			return
		}
		targets := make([]*splitter.Serial, 0, len(pairs))
		for _, p := range pairs {
			t, err := splitter.Open(p.Takeover, req.Baud)
			if err != nil {
				src.Close()
				for _, tt := range targets {
					tt.Close()
				}
				u.mu.Lock()
				u.Running = false
				u.mu.Unlock()
				log.Printf("分线器 %d 打开接管端口 %s 失败: %v", u.ID, p.Takeover, err)
				app.broadcast()
				return
			}
			targets = append(targets, t)
		}
		sp := splitter.NewSplitter(src, req.Source, targets)
		u.mu.Lock()
		if !u.Running {
			// 启动过程中被停止，立即释放
			u.mu.Unlock()
			sp.Close()
			return
		}
		u.splitter = sp
		u.mu.Unlock()
		go sp.Run()
		app.broadcast()
	}()

	app.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}

func handleStop(w http.ResponseWriter, r *http.Request, app *App, u *Unit) {
	u.mu.Lock()
	if u.Running && u.splitter != nil {
		sp := u.splitter
		u.splitter = nil
		u.Running = false
		u.mu.Unlock()
		sp.Stop()
		sp.Close()
		app.broadcast()
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	u.mu.Unlock()
	app.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}

func handleAddPair(w http.ResponseWriter, r *http.Request, app *App, u *Unit) {
	u.mu.Lock()
	if u.Running {
		u.mu.Unlock()
		http.Error(w, "分线器运行中，请先停止", 400)
		return
	}
	u.mu.Unlock()

	// 同步检查管理员权限：非管理员立即返回明确错误，避免前端盲目轮询超时
	if !splitter.IsAdmin() {
		http.Error(w, "需要管理员权限才能创建虚拟串口。\n请关闭本程序，右键「以管理员身份运行」启动后重试。", 403)
		return
	}

	// 纯异步：立即返回，CreatePair 在后台创建端口对，
	// 前端通过 SSE 收到状态更新后刷新，或轮询 /api/units/all。
	go func() {
		aPort, bPort, err := splitter.CreatePair()
		if err != nil {
			u.mu.Lock()
			u.LastError = "新建端口对失败: " + err.Error()
			u.mu.Unlock()
			log.Printf("分线器 %d 新建端口对失败: %v", u.ID, err)
			app.broadcast()
			return
		}
		u.mu.Lock()
		u.Pairs = append(u.Pairs, Pair{Takeover: bPort, Terminal: aPort})
		u.LastError = ""
		u.mu.Unlock()
		app.broadcast()
	}()

	writeJSON(w, map[string]interface{}{"ok": true, "pending": true})
}

func handleAddManual(w http.ResponseWriter, r *http.Request, app *App, u *Unit) {
	var req struct {
		Takeover string `json:"takeover"`
		Terminal string `json:"terminal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Takeover == "" || req.Terminal == "" {
		http.Error(w, "bad request", 400)
		return
	}
	u.mu.Lock()
	if u.Running {
		u.mu.Unlock()
		http.Error(w, "分线器运行中，请先停止", 400)
		return
	}
	u.Pairs = append(u.Pairs, Pair{Takeover: req.Takeover, Terminal: req.Terminal})
	u.mu.Unlock()
	app.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}

func handleDelPair(w http.ResponseWriter, r *http.Request, app *App, u *Unit) {
	var req struct {
		Terminal string `json:"terminal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	u.mu.Lock()
	if u.Running {
		u.mu.Unlock()
		http.Error(w, "分线器运行中，请先停止", 400)
		return
	}
	for i, p := range u.Pairs {
		if p.Terminal == req.Terminal {
			u.Pairs = append(u.Pairs[:i], u.Pairs[i+1:]...)
			break
		}
	}
	u.mu.Unlock()
	app.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}
