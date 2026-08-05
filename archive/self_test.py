"""闭环自测：验证中继脚本的双向转发。

场景（脚本以 --real COM151 --vport1 COM147 --vport2 COM149 运行）：
  COM146 <-> COM147  终端1用 COM146
  COM148 <-> COM149  终端2用 COM148
  COM150 <-> COM151  假开发板用 COM150，脚本连 COM151
"""

import sys
import time

import serial

BAUD = 115200


def read_all(port, timeout_ms=2000):
    buf = b""
    deadline = time.time() + timeout_ms / 1000.0
    while time.time() < deadline:
        n = port.in_waiting
        if n:
            buf += port.read(n)
        else:
            time.sleep(0.02)
    return buf


def main():
    t1 = serial.Serial("COM146", BAUD, timeout=0.2)
    t2 = serial.Serial("COM148", BAUD, timeout=0.2)
    board = serial.Serial("COM150", BAUD, timeout=0.2)
    time.sleep(0.3)

    # 1) 终端1 -> 板子
    t1.write(b"TERM1_HELLO\n")
    got = read_all(board)
    print("板子收到终端1:", got)
    assert b"TERM1_HELLO" in got, "终端1->板子 转发失败!"

    # 2) 终端2 -> 板子
    t2.write(b"TERM2_HELLO\n")
    got = read_all(board)
    print("板子收到终端2:", got)
    assert b"TERM2_HELLO" in got, "终端2->板子 转发失败!"

    # 3) 板子 -> 两个终端（广播）
    board.write(b"BOARD_REPLY\n")
    g1 = read_all(t1)
    g2 = read_all(t2)
    print("终端1收到板子:", g1)
    print("终端2收到板子:", g2)
    assert b"BOARD_REPLY" in g1, "终端1 收不到板子广播!"
    assert b"BOARD_REPLY" in g2, "终端2 收不到板子广播!"

    t1.close()
    t2.close()
    board.close()
    print("=== 全部通过：双向转发与广播正常 ===")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print("测试失败:", e)
        sys.exit(1)
