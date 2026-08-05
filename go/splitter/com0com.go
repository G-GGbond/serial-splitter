package splitter

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var setupcPaths = []string{
	`C:\Program Files (x86)\com0com\setupc.exe`,
	`C:\Program Files\com0com\setupc.exe`,
}

// isAdmin 检测当前进程是否以管理员权限运行。
func isAdmin() bool {
	return IsAdmin()
}

// IsAdmin 公开检测当前进程是否以管理员权限运行。
func IsAdmin() bool {
	err := exec.Command("net", "session").Run()
	return err == nil
}

// FindSetupc 定位 setupc.exe。
func FindSetupc() string {
	for _, p := range setupcPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// PortInfo 描述一个串口。
type PortInfo struct {
	Device      string // COM 端口名
	Kind        string // "CNCA" 或 "CNCB"（com0com 端口才有）
	Idx         int    // 编号
	Description string // 端口描述
}

// FriendlyName 形如 "com0com - serial port emulator CNCA10 (COM166)"
var fnamePairRe = regexp.MustCompile(`(CNCA|CNCB)(\d+)\s*\(COM\d+\)`)
var fnameComRe = regexp.MustCompile(`\(COM(\d+)\)`)

// Com0comMap 枚举当前所有 com0com 端口，返回 key=设备名(COMxx)。
func Com0comMap() map[string]PortInfo {
	result := map[string]PortInfo{}
	for _, p := range ListPorts() {
		pi, ok := parseCom0com(p.Description)
		if !ok {
			continue
		}
		result[p.Device] = pi
	}
	return result
}

func parseCom0com(desc string) (PortInfo, bool) {
	m := fnamePairRe.FindStringSubmatch(desc)
	if m == nil {
		return PortInfo{}, false
	}
	kind := m[1]
	idx := 0
	fmt.Sscanf(m[2], "%d", &idx)
	mc := fnameComRe.FindStringSubmatch(desc)
	if mc == nil {
		return PortInfo{}, false
	}
	device := "COM" + mc[1]
	return PortInfo{Device: device, Kind: kind, Idx: idx}, true
}

// setupcList 用 `setupc list` 解析准确的端口对（不依赖 FriendlyName）。
// 返回 map[(kind,idx)] -> COM 端口名。
func setupcList() map[[2]interface{}]string {
	result := map[[2]interface{}]string{}
	setupc := FindSetupc()
	if setupc == "" {
		return result
	}
	hide := &syscall.SysProcAttr{HideWindow: true}
	cmd := exec.Command(setupc, "list")
	cmd.Dir = filepath.Dir(setupc)
	cmd.SysProcAttr = hide
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	// 格式1: CNCA0 PortName=COM#,RealPortName=COM146
	// 格式2: CNCA2 PortName=COM150
	re := regexp.MustCompile(`(CNCA|CNCB)(\d+)\s+PortName=(?:COM#(?:,RealPortName=)?|)(COM\d+)`)
	for _, line := range strings.Split(string(out), "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		kind := m[1]
		idx := 0
		fmt.Sscanf(m[2], "%d", &idx)
		port := m[3]
		if port == "" {
			continue
		}
		result[[2]interface{}{kind, idx}] = port
	}
	return result
}

// CreatePair 调用 com0com 创建一对端口，返回 (CNCA端口, CNCB端口)。
func CreatePair() (string, string, error) {
	setupc := FindSetupc()
	if setupc == "" {
		return "", "", fmt.Errorf("未找到 com0com setupc.exe")
	}

	before := setupcList()

	// 需要管理员权限。若已是管理员直接执行（隐藏窗口）；否则返回错误提示。
	if !isAdmin() {
		return "", "", fmt.Errorf("需要管理员权限才能创建虚拟串口。请右键「以管理员身份运行」本程序。")
	}

	// 隐藏控制台窗口直接调 setupc（不闪 cmd 黑窗）。
	// 关键：必须把工作目录设为 com0com 安装目录，setupc 需要读 com0com.inf。
	dir := filepath.Dir(setupc)
	hide := &syscall.SysProcAttr{HideWindow: true}

	// 抑制硬件向导
	regCmd := exec.Command("reg", "add", `HKLM\Software\Policies\Microsoft\Windows\DeviceInstall\Settings`,
		"/v", "SuppressNewHWUI", "/t", "REG_DWORD", "/d", "1", "/f")
	regCmd.SysProcAttr = hide
	_ = regCmd.Run()

	cmd := exec.Command(setupc, "--silent", "install", "PortName=COM#", "PortName=COM#")
	cmd.Dir = dir
	cmd.SysProcAttr = hide
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("setupc install 失败: %v", err)
	}

	// 同步执行 updatefnames，确保 FriendlyName 更新
	upd := exec.Command(setupc, "--silent", "updatefnames")
	upd.Dir = dir
	upd.SysProcAttr = hide
	_ = upd.Run()

	regOff := exec.Command("reg", "add", `HKLM\Software\Policies\Microsoft\Windows\DeviceInstall\Settings`,
		"/v", "SuppressNewHWUI", "/t", "REG_DWORD", "/d", "0", "/f")
	regOff.SysProcAttr = hide
	_ = regOff.Run()

	// 轮询检测新端口对（用 setupc list 解析，不依赖 FriendlyName）
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		after := setupcList()
		var aPort, bPort string
		for key, dev := range after {
			if _, existed := before[key]; !existed {
				kind := key[0].(string)
				if kind == "CNCA" {
					aPort = dev
				} else {
					bPort = dev
				}
			}
		}
		if aPort != "" && bPort != "" {
			return aPort, bPort, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return "", "", fmt.Errorf("创建端口对超时（30秒内未检测到新端口）")
}

// ListPorts 列出系统所有串口（含描述），用于区分真实串口和 com0com 虚拟串口。
func ListPorts() []PortInfo {
	var result []PortInfo
	// Windows 下用 pnputil 或 powershell 获取带描述的端口列表
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-PnpDevice -Class Ports | Where-Object { $_.Status -eq 'OK' } | ForEach-Object { "$($_.FriendlyName)" }`)
	out, err := cmd.Output()
	if err != nil {
		// 回退到注册表
		return ListPortsRegistry()
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// FriendlyName 形如 "USB-SERIAL CH340 (COM3)"
		re := regexp.MustCompile(`\(COM\d+\)`)
		m := re.FindString(line)
		if m == "" {
			continue
		}
		device := strings.Trim(m, "()")
		result = append(result, PortInfo{Device: device, Description: line})
	}
	return result
}

// ListPortsRegistry 从注册表读取 COM 端口名（无描述，作为回退）。
func ListPortsRegistry() []PortInfo {
	var result []PortInfo
	cmd := exec.Command("reg", "query", `HKLM\HARDWARE\DEVICEMAP\SERIALCOMM`)
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	re := regexp.MustCompile(`\sCOM\d+\s*$`)
	for _, line := range strings.Split(string(out), "\n") {
		m := re.FindString(line)
		if m != "" {
			result = append(result, PortInfo{Device: strings.TrimSpace(m)})
		}
	}
	return result
}
