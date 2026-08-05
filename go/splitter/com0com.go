package splitter

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var setupcPaths = []string{
	`C:\Program Files (x86)\com0com\setupc.exe`,
	`C:\Program Files\com0com\setupc.exe`,
}

// isAdmin 检测当前进程是否以管理员权限运行。
func isAdmin() bool {
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

// CreatePair 调用 com0com 创建一对端口，返回 (CNCA端口, CNCB端口)。
func CreatePair() (string, string, error) {
	setupc := FindSetupc()
	if setupc == "" {
		return "", "", fmt.Errorf("未找到 com0com setupc.exe")
	}

	before := Com0comMap()

	tmp, err := os.MkdirTemp("", "splt")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	installLog := tmp + "\\install.txt"
	bat := tmp + "\\mkpair.bat"

	content := "@echo off\r\n" +
		"reg add HKLM\\Software\\Policies\\Microsoft\\Windows\\DeviceInstall\\Settings /v SuppressNewHWUI /t REG_DWORD /d 1 /f\r\n" +
		fmt.Sprintf(`"%s" --silent install PortName=COM# PortName=COM# > "%s" 2>&1`, setupc, installLog) + "\r\n" +
		"echo INSTALL_EXIT=%ERRORLEVEL% >> \"" + installLog + "\"\r\n" +
		fmt.Sprintf(`"%s" --silent updatefnames >> "%s" 2>&1`, setupc, installLog) + "\r\n" +
		"reg add HKLM\\Software\\Policies\\Microsoft\\Windows\\DeviceInstall\\Settings /v SuppressNewHWUI /t REG_DWORD /d 0 /f\r\n"
	if err := os.WriteFile(bat, []byte(content), 0644); err != nil {
		return "", "", err
	}

	// 需要管理员权限。若已是管理员直接执行；否则用 powershell 提权（会弹 UAC）。
	if isAdmin() {
		cmd := exec.Command("cmd.exe", "/c", bat)
		if err := cmd.Start(); err != nil {
			return "", "", err
		}
		go cmd.Wait()
	} else {
		// 非管理员：后台提权执行，不等待（避免阻塞）
		psCmd := fmt.Sprintf(`Start-Process -FilePath 'cmd.exe' -ArgumentList '/c','%s' -Verb RunAs`, bat)
		cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
		if err := cmd.Start(); err != nil {
			return "", "", err
		}
		go cmd.Wait()
	}

	// 轮询检测新端口对
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		after := Com0comMap()
		var aPort, bPort string
		found := false
		for name, pi := range after {
			if _, existed := before[name]; !existed {
				if pi.Kind == "CNCA" {
					aPort = pi.Device
				} else {
					bPort = pi.Device
				}
				found = true
			}
		}
		if found && aPort != "" && bPort != "" {
			return aPort, bPort, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	log, _ := os.ReadFile(installLog)
	return "", "", fmt.Errorf("创建失败：\n%s", string(log))
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
