"""多分线器闭环自测（全用空闲端口，避开被占用的 COM146/148/152）：
  分线器1: 源=COM150(假板1=COM151), 接管=COM155 -> 终端端=COM154
  分线器2: 源=COM156(假板2=COM157), 接管=COM159 -> 终端端=COM158
"""

import time
import tkinter as tk

import serial

import serial_splitter_gui as sg

root = tk.Tk()
root.withdraw()
app = sg.SplitterConsole(root)


def label_of(port):
    for lbl, dev in app.port_map.items():
        if dev == port:
            return lbl
    return port


def clear_terminals(unit):
    for child in unit.term_wrap.winfo_children():
        child.destroy()
    unit.pairs = []


# ---- 分线器1 ----
u1 = app.units[0]
clear_terminals(u1)
u1.cmb_src.set(label_of("COM150"))
u1.cmb_baud.set("115200")
u1.add_terminal_row("COM155", "COM154")
u1.start()

# ---- 分线器2 ----
app.add_unit()
u2 = app.units[-1]
clear_terminals(u2)
u2.cmb_src.set(label_of("COM156"))
u2.cmb_baud.set("115200")
u2.add_terminal_row("COM159", "COM158")
u2.start()

board1 = serial.Serial("COM151", 115200, timeout=0.5)  # 假板1
t1 = serial.Serial("COM154", 115200, timeout=0.5)      # 终端1
board2 = serial.Serial("COM157", 115200, timeout=0.5)  # 假板2
t2 = serial.Serial("COM158", 115200, timeout=0.5)      # 终端2
time.sleep(0.5)


def read_all(port, ms=1500):
    buf = b""
    end = time.time() + ms / 1000
    while time.time() < end:
        n = port.in_waiting
        if n:
            buf += port.read(n)
        else:
            time.sleep(0.02)
    return buf


# 分线器1：板子打印 -> 终端
board1.write(b"BOARD_MSG_1\n")
g = read_all(t1)
print("分线器1 终端收到板子:", g)
assert b"BOARD_MSG_1" in g, "分线器1 广播失败!"

# 分线器1：终端输入 -> 板子
t1.write(b"T1_CMD\r\n")
got = read_all(board1)
print("分线器1 板子收到终端:", got)
assert b"T1_CMD" in got, "分线器1 回传失败!"

# 分线器2：板子打印 -> 终端（独立互不干扰）
board2.write(b"BOARD_MSG_2\n")
g2 = read_all(t2)
print("分线器2 终端收到板子:", g2)
assert b"BOARD_MSG_2" in g2, "分线器2 广播失败!"

# 分线器2：终端输入 -> 板子
t2.write(b"T2_CMD\r\n")
got2 = read_all(board2)
print("分线器2 板子收到终端:", got2)
assert b"T2_CMD" in got2, "分线器2 回传失败!"

# 确认分线器1 未收到分线器2 的数据（隔离性）
time.sleep(0.3)
g1_extra = read_all(t1, 300)
assert b"BOARD_MSG_2" not in g1_extra, "分线器之间串扰!"
print("隔离性验证：分线器1 未收到分线器2 数据 OK")

u1.stop(); u2.stop()
board1.close(); t1.close(); board2.close(); t2.close()
root.destroy()
print("=== 多分线器闭环自测全部通过 ===")
