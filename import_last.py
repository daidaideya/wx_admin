import re
import socket

AOF_PATH = r"c:\Users\Administrator\Downloads\微信授权逆向\牛子的微信授权\appendonlydir\appendonly.aof.114.incr.aof"
TARGET = "wxid_z99degqjmc6w22"

with open(AOF_PATH, 'rb') as f:
    raw = f.read()

m = re.search(rb'\*3\r\n\$\d+\r\n(?:set|SET)\r\n\$\d+\r\n' + TARGET.encode() + rb'\r\n\$(\d+)\r\n', raw)
if not m:
    print("未找到")
    exit()

val_len = int(m.group(1))
val_start = m.end()
val = raw[val_start:val_start+val_len]

print(f"找到 {TARGET}, 数据长度 {val_len}")

# 写入文件，用 redis-cli 导入
with open(r'c:\Users\Administrator\Downloads\微信授权逆向\wx-admin\last_user.txt', 'wb') as f:
    f.write(val)

print("已导出到 last_user.txt")
