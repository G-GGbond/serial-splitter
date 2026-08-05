"""串口复用中继：一个真实串口 对 两个虚拟串口，双向收发。

原理：
  用 com0com 创建两对虚拟串口（例如 COM5<->COM6、COM7<->COM8）。
  两个终端分别连 COM5、COM7；本脚本连 COM6、COM8、真实串口 COM3。
  - 终端1 输入 -> COM6 -> 本脚本 -> COM3 -> 开发板
  - 终端2 输入 -> COM8 -> 本脚本 -> COM3 -> 开发板
  - 开发板打印 -> COM3 -> 本脚本 -> COM6+COM8 -> 两个终端都能看到

用法：
  python serial_splitter.py --real COM3 --vport1 COM6 --vport2 COM8 --baud 115200
"""

import argparse
import sys
import threading
import time

try:
    import serial
except ImportError:
    sys.exit("缺少 pyserial，请先运行：pip install pyserial")

if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass


def pipe(src, dst, lock, name, stop):
    """持续把 src 读到的数据转发到 dst（保护 dst 的写锁）。"""
    while not stop.is_set():
        try:
            waiting = src.in_waiting
            if waiting:
                data = src.read(waiting)
                if data:
                    with lock:
                        dst.write(data)
            else:
                time.sleep(0.001)
        except serial.SerialException as e:
            print(f"[{name}] 串口异常，该线路停止转发: {e}", flush=True)
            return


def broadcast(real, vports, names, stop):
    """把真实串口的数据同时写给所有虚拟串口（终端）。"""
    while not stop.is_set():
        try:
            waiting = real.in_waiting
            if waiting:
                data = real.read(waiting)
                for vp, nm in zip(vports, names):
                    try:
                        vp.write(data)
                    except serial.SerialException:
                        # 某个终端未打开时不阻塞其他终端
                        pass
            else:
                time.sleep(0.001)
        except serial.SerialException as e:
            print(f"[真实串口] 异常，转发停止: {e}", flush=True)
            return


def main():
    ap = argparse.ArgumentParser(description="串口复用中继：一个真实串口对两个虚拟串口")
    ap.add_argument("--real", required=True, help="开发板的真实串口，如 COM3")
    ap.add_argument("--vport1", required=True, help="虚拟串口对1的中继端，如 COM6（终端1连 COM5）")
    ap.add_argument("--vport2", required=True, help="虚拟串口对2的中继端，如 COM8（终端2连 COM7）")
    ap.add_argument("--baud", type=int, default=115200, help="波特率，默认 115200")
    args = ap.parse_args()

    try:
        real = serial.Serial(args.real, args.baud, timeout=0.1)
        v1 = serial.Serial(args.vport1, args.baud, timeout=0.1)
        v2 = serial.Serial(args.vport2, args.baud, timeout=0.1)
    except serial.SerialException as e:
        sys.exit(f"打开串口失败: {e}\n请确认串口号和 com0com 虚拟串口已创建。")

    print("串口复用中继已启动", flush=True)
    print(f"  真实串口 : {args.real} @ {args.baud}", flush=True)
    print(f"  终端1 连 : {args.vport1}", flush=True)
    print(f"  终端2 连 : {args.vport2}", flush=True)
    print("按 Ctrl+C 退出。", flush=True)

    lock = threading.Lock()
    stop = threading.Event()

    threads = [
        threading.Thread(target=pipe, args=(v1, real, lock, "终端1", stop), daemon=True),
        threading.Thread(target=pipe, args=(v2, real, lock, "终端2", stop), daemon=True),
        threading.Thread(
            target=broadcast, args=(real, [v1, v2], ["终端1", "终端2"], stop), daemon=True
        ),
    ]
    for t in threads:
        t.start()

    try:
        while True:
            time.sleep(0.5)
    except KeyboardInterrupt:
        print("\n正在退出…", flush=True)
        stop.set()
        time.sleep(0.2)


if __name__ == "__main__":
    main()
