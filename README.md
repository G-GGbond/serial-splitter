# 串口分线器

把一个真实串口分出多个虚拟串口，多个终端可同时收发。基于 com0com 虚拟串口驱动 + 用户态双向透明转发。

## 功能

- 一个真实串口（如 COM143）分出多个虚拟端口，各终端可同时输入和查看打印
- 多分线器：一个窗口管理多个独立分线器，各自可把任意串口分出多个终端
- 自动创建/清理虚拟端口对：删除分线器、移除终端、退出时自动清理本工具创建的端口对
- 两个版本：
  - **Web 版**（Go + 浏览器界面）：暗色主题，启动自动打开浏览器，单 exe ~8MB
  - **Python 版**（tkinter）：传统桌面窗口

## 架构

```
终端1(SecureCRT) → COM146 ─┐
                            ├─ com0com 端口对 ─→ 分线器 ─→ 真实串口(开发板)
终端2(MobaXterm) → COM148 ─┘                            │
                                                       └→ 广播给所有终端
```

分线器（Go/Python 用户态进程）读取真实串口数据，复制广播到所有虚拟端口；任一终端的输入转发到真实串口。com0com 提供虚拟串口对，驱动级实现。

## 环境依赖

- **Windows 10/11**
- **com0com** 虚拟串口驱动（免费开源）：https://sourceforge.net/projects/com0com/files/ 下载 `com0com-3.0.0.0-i386-and-x64-signed.zip`，运行 `Setup_com0com_v3.0.0.0_W7_x64_signed.exe` 安装
- **Web 版**：无需额外依赖（单 exe）
- **Python 版**：Python 3.8+，`pip install pyserial`

## 使用

### Web 版

```
go/splitter_web/serial-splitter.exe   # 或双击 启动串口分线器Web版.bat
```

双击启动器 → 弹一次 UAC 授权（管理员权限，创建虚拟端口需要）→ 浏览器自动打开界面。

1. 点「＋ 新建分线器」
2. 选源串口（开发板所在端口）和波特率
3. 点「＋ 新建端口对」自动创建虚拟端口对
4. 点「▶ 启动」
5. 终端软件（SecureCRT/MobaXterm）连面板绿色标注的端口

> 注意：创建虚拟端口需要管理员权限，请通过启动器启动（会自动提权一次）。

### Python 版

```
python serial_splitter_gui.py   # 或双击 启动串口分线器.bat
```

### 重新编译

```
cd go
go build -o splitter_web/serial-splitter.exe ./splitter_web/
```

## 目录结构

```
├── go/
│   ├── splitter/            # Go 核心分线引擎（串口转发、com0com 管理）
│   └── splitter_web/        # Web 版（后端 HTTP API + SSE，前端内嵌）
├── serial_splitter_gui.py   # Python 版（tkinter）
├── console_selftest.py      # Python 版自测
└── archive/                 # 早期版本归档
```

## 常见问题

- **新建端口对失败/超时**：请以管理员身份运行（用启动器启动）
- **提示未找到 com0com**：先安装 com0com 驱动
- **串口慢**：确保用最新版本（已修复转发锁竞争导致的延迟）

## 许可证

本项目代码遵循 MIT 许可证。com0com 是其各自作者的独立项目。
