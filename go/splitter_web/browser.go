package main

import (
	"os/exec"
	"runtime"
)

// openBrowser 用系统默认浏览器打开 URL。
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		println("无法自动打开浏览器，请手动访问:", url)
	}
}
