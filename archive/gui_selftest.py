"""GUI 闭环自测：GUI(real=COM150) + 假板(COM151)，验证面板显示与输入发送。"""

import time
import tkinter as tk

import serial

import serial_splitter_gui as sg

root = tk.Tk()
root.withdraw()  # 隐藏窗口，只做逻辑验证
app = sg.SplitterGUI(root)

app.cmb_real.set("COM150")
app.cmb_v1.set("COM147")
app.cmb_v2.set("COM149")
app.cmb_baud.set("115200")
app.var_newline.set(True)
app.start()

board = serial.Serial("COM151", 115200, timeout=0.5)
time.sleep(0.4)


def pump(sec):
    deadline = time.time() + sec
    while time.time() < deadline:
        root.update()
        time.sleep(0.02)


# 1) 板子打印 -> 两个面板都应显示
board.write(b"BOARD_PRINT_123\n")
pump(1.5)
text1 = app.txt1.get("1.0", "end")
text2 = app.txt2.get("1.0", "end")
print("面板1 内容:", repr(text1))
print("面板2 内容:", repr(text2))
assert "BOARD_PRINT_123" in text1, "面板1 未显示板子打印!"
assert "BOARD_PRINT_123" in text2, "面板2 未显示板子打印!"

# 2) 终端1 输入 -> 假板收到
app.entry1.insert(0, "FROM_TERM1")
app.send(0)
time.sleep(0.5)
got = board.read(200)
print("假板收到(终端1):", repr(got))
assert b"FROM_TERM1" in got, "终端1 输入未到板子!"

# 3) 终端2 输入 -> 假板收到
app.entry2.insert(0, "FROM_TERM2")
app.send(1)
time.sleep(0.5)
got = board.read(200)
print("假板收到(终端2):", repr(got))
assert b"FROM_TERM2" in got, "终端2 输入未到板子!"

app.stop()
board.close()
root.destroy()
print("=== GUI 闭环自测全部通过 ===")
