"""串口分线器（多分线器版）：一个窗口管理多个独立分线器，每个可把任意串口分出多个终端。

用法：
  1. 点「＋ 新建分线器」添加一个分线器（默认已有一个）
  2. 每个分线器：选「源串口」→ 加「终端」（点「＋ 新建端口对」自动建，或手动选已有端口对）
  3. 点该分线器的「▶ 启动」，终端用 SecureCRT/MobaXterm 连对应的「终端连接端」
  4. 各分线器独立启停、互不影响

「＋ 新建端口对」自动创建一对 com0com 标准端口并自动接管 B 端，
界面只显示「终端连接端」，用户只需知道 SecureCRT 连哪个端口。

依赖：pyserial，com0com（驱动已装）。
"""

import ctypes
import os
import re
import sys
import subprocess
import tempfile
import threading
import time
import tkinter as tk
from tkinter import ttk, messagebox

import serial
import serial.tools.list_ports as lp

BAUD_RATES = ["9600", "19200", "38400", "57600", "115200",
              "230400", "460800", "921600", "1500000"]

COM0COM_SETUPC = [
    r"C:\Program Files (x86)\com0com\setupc.exe",
    r"C:\Program Files\com0com\setupc.exe",
]


def find_setupc():
    for p in COM0COM_SETUPC:
        if os.path.exists(p):
            return p
    return None


def is_admin():
    try:
        return bool(ctypes.windll.shell32.IsUserAnAdmin())
    except Exception:
        return False


def parse_pair(desc):
    """从 com0com 描述提取 (kind, idx)：CNCA5 -> ('A',5)，CNCB3 -> ('B',3)。"""
    m = re.search(r"(CNCA|CNCB)(\d+)", desc)
    if not m:
        return None
    return m.group(1), int(m.group(2))


def src_label(device, desc):
    """源串口下拉标签：任何端口都能当源。"""
    p = parse_pair(desc)
    if p:
        kind, idx = p
        role = "终端连接端" if kind == "CNCA" else "接管端"
        return f"{device}  [{kind}{idx} · {role}]"
    return f"{device}  ({desc[:28]})"


def takeover_label(device, desc, cnca_of):
    """接管端下拉标签：只列 com0com 的 CNCB 接管端，标注对应终端端。"""
    return f"{device}  [接管端 → 终端连 {cnca_of}]"


class SplitterUnit:
    """一个独立分线器：源串口 + 多个接管端口。"""

    def __init__(self, console, frame, unit_id):
        self.console = console
        self.frame = frame
        self.unit_id = unit_id
        self.pairs = []        # [(接管端口str, 终端连接端口str)]
        self.serials = {}      # 接管端口 -> serial 对象
        self.source = None
        self.source_name = ""
        self.running = False
        self.stop_event = threading.Event()
        self.threads = []

        self._build_ui()

    def _build_ui(self):
        self.box = ttk.LabelFrame(self.frame, text=f" 分线器 {self.unit_id} ", padding=8)
        self.box.pack(fill="x", padx=4, pady=4)

        # 顶部：源串口 + 波特率 + 删除
        top = ttk.Frame(self.box)
        top.pack(fill="x")
        ttk.Label(top, text="源串口").pack(side="left", padx=(0, 4))
        self.cmb_src = ttk.Combobox(top, width=42)
        self.cmb_src.pack(side="left", padx=(0, 12))
        ttk.Label(top, text="波特率").pack(side="left", padx=(0, 4))
        self.cmb_baud = ttk.Combobox(top, width=8, values=BAUD_RATES)
        self.cmb_baud.current(4)
        self.cmb_baud.pack(side="left")
        ttk.Button(top, text="删除本分线器", command=self.remove).pack(side="right")

        # 终端列表
        head = ttk.Frame(self.box)
        head.pack(fill="x", pady=(6, 0))
        ttk.Label(head, text="终端连接（SecureCRT/MobaXterm 连对应端口）",
                  foreground="#1a6a1a").pack(side="left")
        self.term_wrap = ttk.Frame(self.box)
        self.term_wrap.pack(fill="x", pady=(2, 0))

        # 操作按钮
        btns = ttk.Frame(self.box)
        btns.pack(fill="x", pady=(6, 0))
        ttk.Button(btns, text="＋ 新建端口对", command=self.new_pair).pack(side="left")
        ttk.Button(btns, text="＋ 选已有端口", command=self.add_manual).pack(side="left", padx=(6, 0))
        ttk.Button(btns, text="刷新", command=self.console.scan_ports).pack(side="left", padx=(6, 0))
        ttk.Separator(btns, orient="vertical").pack(side="left", fill="y", padx=(12, 8))
        self.btn_start = ttk.Button(btns, text="▶ 启动", command=self.start)
        self.btn_start.pack(side="left")
        self.btn_stop = ttk.Button(btns, text="■ 停止", command=self.stop, state="disabled")
        self.btn_stop.pack(side="left", padx=(6, 0))

        self.status = ttk.Label(self.box, text="○ 未启动", foreground="#888888")
        self.status.pack(fill="x", pady=(6, 0))
    # ---------- 终端行 ----------
    def add_terminal_row(self, takeover, cnca_of):
        row = ttk.Frame(self.term_wrap)
        row.pack(fill="x", pady=1)
        idx = len(self.pairs) + 1
        ttk.Label(row, text=f"终端 {idx}", width=6).pack(side="left")
        ttk.Label(row, text=f"请连 {cnca_of}",
                  foreground="#0a6a0a", font=("TkDefaultFont", 9, "bold")).pack(side="left")
        ttk.Button(row, text="移除", width=4,
                   command=lambda: self.remove_terminal(row, takeover)).pack(side="right")
        self.pairs.append((takeover, cnca_of))

    def remove_terminal(self, row, takeover):
        if self.running:
            return
        row.destroy()
        self.pairs = [(t, a) for (t, a) in self.pairs if t != takeover]
        self.renumber()

    def renumber(self):
        for i, w in enumerate(self.term_wrap.winfo_children(), 1):
            for ch in w.winfo_children():
                if isinstance(ch, ttk.Label) and ch.cget("text").startswith("终端"):
                    ch.config(text=f"终端{i}:")
                    break

    # ---------- 端口操作 ----------
    def new_pair(self):
        if self.running:
            messagebox.showinfo("提示", "请先停止本分线器再新建端口对。")
            return
        a_port, b_port = self.console.create_com0com_pair()
        if not a_port:
            return
        time.sleep(1.0)
        self.console.scan_ports()
        self.add_terminal_row(b_port, a_port)
        messagebox.showinfo("已新建端口对",
                            f"终端连接端：{a_port}（SecureCRT/MobaXterm 连这个）\n"
                            f"分线器接管端：{b_port}（已自动接管，无需操作）")

    def add_manual(self):
        if self.running:
            messagebox.showinfo("提示", "请先停止本分线器再添加端口。")
            return
        takeover_vals = self.console.takeover_options()
        if not takeover_vals:
            messagebox.showinfo("提示", "没有可用的 com0com 端口对。\n先点「＋ 新建端口对(自动)」创建一个。")
            return
        dlg = tk.Toplevel(self.frame)
        dlg.title("选择接管端口")
        dlg.transient(self.frame)
        dlg.grab_set()
        ttk.Label(dlg, text="选择「接管端」（对应终端连另一个端口）：").pack(padx=10, pady=(10, 4))
        cmb = ttk.Combobox(dlg, values=takeover_vals, width=44)
        cmb.current(0)
        cmb.pack(padx=10, pady=4)

        def ok():
            sel = cmb.get()
            m = re.match(r"(COM\d+)", sel)
            dlg.destroy()
            if not m:
                return
            takeover = m.group(1)
            cnca = self.console.takeover_to_cnca(takeover)
            self.add_terminal_row(takeover, cnca or "?")
            self.console.scan_ports()
        ttk.Button(dlg, text="确定", command=ok).pack(pady=(4, 10))

    # ---------- 启动 / 停止 ----------
    def start(self):
        if self.running:
            return
        src_name = self.console.resolve(self.cmb_src.get())
        try:
            baud = int(self.cmb_baud.get())
        except ValueError:
            messagebox.showerror("错误", "波特率必须是数字")
            return
        if not src_name:
            messagebox.showerror("错误", "请选择源串口")
            return
        if not self.pairs:
            messagebox.showerror("错误", "请先添加终端端口（点「＋ 新建端口对」）")
            return
        if src_name in [t for t, _ in self.pairs]:
            messagebox.showerror("错误", "接管端口不能与源串口相同")
            return
        # 防止两个分线器用同一源
        for u in self.console.units:
            if u is not self and u.running and u.source_name == src_name:
                messagebox.showerror("错误", f"源串口 {src_name} 已被分线器 {u.unit_id} 占用")
                return

        try:
            self.source = serial.Serial(src_name, baud, timeout=0.05)
        except serial.SerialException as e:
            messagebox.showerror("错误", f"打开源串口 {src_name} 失败：\n{e}\n"
                                        f"请确认端口存在、未被其他程序占用。")
            return

        self.serials = {}
        for takeover, _ in self.pairs:
            try:
                self.serials[takeover] = serial.Serial(takeover, baud, timeout=0.05)
            except serial.SerialException as e:
                self.close_serial()
                messagebox.showerror("错误", f"打开接管端口 {takeover} 失败：\n{e}")
                return

        self.source_name = src_name
        self.stop_event.clear()
        self.running = True
        self.threads = [threading.Thread(target=self._read_source, daemon=True)]
        for takeover in self.serials:
            self.threads.append(threading.Thread(
                target=self._read_vport, args=(takeover,), daemon=True))
        for t in self.threads:
            t.start()

        self.btn_start.config(state="disabled")
        self.btn_stop.config(state="normal")
        self.cmb_src.config(state="disabled")
        term_ports = " / ".join(f"{a}" for _, a in self.pairs)
        self.status.config(
            text=f"● 运行中  {src_name} @ {baud}  → 终端端口：{term_ports}",
            foreground="#0a6a0a")
        self.console.update_status()

    def stop(self):
        if not self.running:
            return
        self.running = False
        self.stop_event.set()
        self.close_serial()
        self.btn_start.config(state="normal")
        self.btn_stop.config(state="disabled")
        self.cmb_src.config(state="normal")
        self.status.config(text="○ 已停止", foreground="#888888")
        self.console.update_status()

    def close_serial(self):
        if self.source is not None and self.source.is_open:
            try:
                self.source.close()
            except Exception:
                pass
        self.source = None
        for s in self.serials.values():
            try:
                s.close()
            except Exception:
                pass
        self.serials = {}

    def remove(self):
        if self.running:
            messagebox.showinfo("提示", "请先停止本分线器再删除。")
            return
        self.console.remove_unit(self)

    # ---------- 数据线程 ----------
    def _read_source(self):
        while not self.stop_event.is_set():
            try:
                n = self.source.in_waiting
                if n:
                    data = self.source.read(n)
                    for s in self.serials.values():
                        try:
                            s.write(data)
                        except Exception:
                            pass
                else:
                    time.sleep(0.001)
            except Exception:
                break

    def _read_vport(self, takeover):
        vp = self.serials[takeover]
        while not self.stop_event.is_set():
            try:
                n = vp.in_waiting
                if n:
                    data = vp.read(n)
                    try:
                        self.source.write(data)
                    except Exception:
                        pass
                else:
                    time.sleep(0.001)
            except Exception:
                break

    def refresh_cmbs(self):
        self.cmb_src["values"] = getattr(self.console, "port_sorted",
                                         list(self.console.port_map))


class SplitterConsole:
    def __init__(self, root):
        self.root = root
        root.title("串口分线器 — 多分线器管理")
        root.minsize(700, 380)

        self.port_map = {}
        self.port_sorted = []
        self.cncb_of = {}     # 接管端口 -> 终端连接端口
        self.cnca_of = {}     # 终端连接端口 -> 接管端口
        self.units = []

        self._build_ui()
        self.scan_ports()
        self.add_unit()
        root.protocol("WM_DELETE_WINDOW", self._on_close)

    # ---------- UI ----------
    def _build_ui(self):
        tip = ttk.Label(
            self.root,
            text=("① 选「源串口」 ② 点「＋新建端口对」 ③ 点「▶启动」  "
                  "④ 用 SecureCRT/MobaXterm 连下方绿色标出的端口"),
            foreground="#555555", font=("TkDefaultFont", 9))
        tip.pack(fill="x", padx=10, pady=(8, 2))

        self.unit_frame = ttk.Frame(self.root)
        self.unit_frame.pack(fill="both", expand=True, padx=4, pady=2)

        addbar = ttk.Frame(self.root)
        addbar.pack(fill="x", padx=10, pady=(2, 6))
        ttk.Button(addbar, text="＋ 新建分线器", command=self.add_unit).pack(side="left")
        ttk.Button(addbar, text="⟳ 重新扫描端口", command=self.scan_ports).pack(side="left", padx=(8, 0))

        self.status = ttk.Label(self.root, text="就绪", anchor="w", relief="sunken", padding=4)
        self.status.pack(fill="x", side="bottom")

    def update_status(self):
        running = [u for u in self.units if u.running]
        if running:
            self.status.config(
                text=f"● {len(running)} 个分线器运行中（共 {len(self.units)} 个）",
                foreground="#0a6a0a")
        else:
            self.status.config(
                text=f"○ {len(self.units)} 个分线器，均未启动",
                foreground="#888888")

    def add_unit(self):
        unit = SplitterUnit(self, self.unit_frame, len(self.units) + 1)
        unit.refresh_cmbs()
        self.units.append(unit)
        self.preselect_unit(unit)
        self.update_status()

    def remove_unit(self, unit):
        if unit in self.units:
            unit.frame.destroy()
            self.units.remove(unit)
            for i, u in enumerate(self.units, 1):
                u.box.config(text=f" 分线器 {i} ")
            self.update_status()

    # ---------- 端口扫描 ----------
    def scan_ports(self):
        self.port_map = {}
        self.cncb_of = {}
        self.cnca_of = {}
        cnca_by_idx = {}
        cncb_by_idx = {}
        try:
            ports = lp.comports()
        except Exception:
            ports = []
        for p in ports:
            desc = p.description or ""
            self.port_map[src_label(p.device, desc)] = p.device
            pr = parse_pair(desc)
            if not pr:
                continue
            kind, idx = pr
            if kind == "CNCA":
                cnca_by_idx[idx] = p.device
            else:
                cncb_by_idx[idx] = p.device
        for idx, cb in cncb_by_idx.items():
            ca = cnca_by_idx.get(idx)
            self.cncb_of[cb] = ca
            if ca:
                self.cnca_of[ca] = cb
        # 重新排序下拉：真实串口(USB)在前，com0com 虚拟串口在后
        real = [k for k in self.port_map if "com0com" not in k]
        virt = [k for k in self.port_map if "com0com" in k]
        sorted_vals = real + virt
        self.port_sorted = sorted_vals
        for u in self.units:
            u.refresh_cmbs()
        self.update_status()

    def takeover_options(self):
        """手动选接管端口的下拉选项。"""
        return [takeover_label(t, "", a) for t, a in self.cncb_of.items() if a]

    def takeover_to_cnca(self, takeover):
        return self.cncb_of.get(takeover)

    def resolve(self, text):
        text = text.strip()
        return self.port_map.get(text, text)

    def preselect_unit(self, unit):
        vals = list(self.port_map)
        if not vals:
            return
        # 源：优先真实串口（非 com0com），取第一个；否则任意第一个
        real = [v for v in vals if "com0com" not in v]
        if real:
            # 若系统里有常见开发板串口优先
            for dev in ("COM143", "COM84", "COM1", "COM2"):
                for v in real:
                    if v.startswith(dev):
                        unit.cmb_src.set(v)
                        break
                if unit.cmb_src.get():
                    break
            if not unit.cmb_src.get():
                unit.cmb_src.set(real[0])
        else:
            unit.cmb_src.set(vals[0])
        # 自动带上前两个可用的接管端口（仅第一个分线器）
        if len(self.units) <= 1:
            for t, a in list(self.cncb_of.items())[:2]:
                if a:
                    unit.add_terminal_row(t, a)

    # ---------- 新建 com0com 端口对 ----------
    @staticmethod
    def _setupc_list_map():
        """用 setupc list 解析准确的端口对（不依赖 FriendlyName，可靠）。
        返回 {(CNCA|CNCB, idx): 'COMxxx'}。"""
        result = {}
        setupc = find_setupc()
        if not setupc:
            return result
        try:
            r = subprocess.run([setupc, "list"], cwd=os.path.dirname(setupc),
                               capture_output=True, timeout=20, text=True)
            for line in r.stdout.splitlines():
                # 格式1: CNCA0 PortName=COM#,RealPortName=COM146
                # 格式2: CNCA2 PortName=COM150
                m = re.match(r"\s*(CNCA|CNCB)(\d+)\s+PortName=(?:COM#(?:,RealPortName=)?|)(COM\d+)", line)
                if not m:
                    continue
                kind, idx, port = m.group(1), int(m.group(2)), m.group(3)
                if port:
                    result[(kind, idx)] = port
        except Exception:
            pass
        return result

    @staticmethod
    def _com0com_map():
        """枚举当前所有 com0com 端口：{(CNCA|CNCB, idx): 'COMxxx'}。"""
        result = {}
        for p in lp.comports():
            pr = parse_pair(p.description or "")
            if pr:
                result[pr] = p.device
        return result

    def create_com0com_pair(self):
        setupc = find_setupc()
        if not setupc:
            messagebox.showerror("未找到 com0com",
                                 "未找到 com0com 驱动。\n请先安装 com0com 后再试。")
            return None, None
        if not is_admin():
            messagebox.showwarning(
                "需要管理员权限",
                "请以管理员身份运行本程序（右键 → 以管理员身份运行），\n"
                "否则无法创建虚拟串口。")
            return None, None

        before = self._setupc_list_map()
        try:
            # 用 CREATE_NO_WINDOW 隐藏命令行窗口，直接调 setupc，不弹 cmd/UAC
            flags = subprocess.CREATE_NO_WINDOW if hasattr(subprocess, "CREATE_NO_WINDOW") else 0
            # setupc 需要以 com0com 安装目录为工作目录（读 com0com.inf）
            com0dir = os.path.dirname(setupc)
            # 抑制硬件向导
            subprocess.run(
                ["reg", "add", r"HKLM\Software\Policies\Microsoft\Windows\DeviceInstall\Settings",
                 "/v", "SuppressNewHWUI", "/t", "REG_DWORD", "/d", "1", "/f"],
                creationflags=flags, capture_output=True, timeout=30)
            proc = subprocess.Popen(
                [setupc, "--silent", "install", "PortName=COM#", "PortName=COM#"],
                cwd=com0dir, creationflags=flags,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            proc.wait(timeout=60)
            # 更新友好名称
            subprocess.run([setupc, "--silent", "updatefnames"], cwd=com0dir,
                           creationflags=flags, capture_output=True, timeout=30)
            subprocess.run(
                ["reg", "add", r"HKLM\Software\Policies\Microsoft\Windows\DeviceInstall\Settings",
                 "/v", "SuppressNewHWUI", "/t", "REG_DWORD", "/d", "0", "/f"],
                creationflags=flags, capture_output=True, timeout=30)
        except Exception as e:
            messagebox.showerror("错误", f"执行 com0com 失败：{e}")
            return None, None

        # 轮询检测新端口对（避免时序竞态），最多等 25 秒
        deadline = time.time() + 25
        while time.time() < deadline:
            after = self._setupc_list_map()
            new = {k: v for k, v in after.items() if k not in before}
            a_port = b_port = None
            for (kind, idx), dev in new.items():
                if kind == "CNCA":
                    a_port = dev
                else:
                    b_port = dev
            if a_port and b_port:
                return a_port, b_port
            time.sleep(0.6)

        messagebox.showerror(
            "新建失败",
            "未能检测到新端口对。\n\n"
            "可能原因：\n"
            "1. com0com 驱动未正确安装\n"
            "2. UAC 授权被取消\n"
            "3. 系统端口号耗尽\n\n"
            "详情：\n" + self._read_err(install))
        return None, None

    @staticmethod
    def _read_err(path):
        try:
            with open(path, encoding="utf-8", errors="replace") as f:
                return f.read()[:500]
        except Exception:
            return ""

    def _on_close(self):
        for u in self.units:
            u.stop()
        self.root.destroy()


def _high_res_timer():
    """提高 Windows 定时器精度到 1ms，降低串口轮询/阻塞检测延迟。"""
    if sys.platform == "win32":
        try:
            ctypes.windll.winmm.timeBeginPeriod(1)
        except Exception:
            pass


def main():
    _high_res_timer()
    root = tk.Tk()
    SplitterConsole(root)
    root.mainloop()


if __name__ == "__main__":
    main()
