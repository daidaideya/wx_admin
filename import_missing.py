import re
import json
import socket
import sys
import io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')

AOF_PATH = r"c:\Users\Administrator\Downloads\微信授权逆向\牛子的微信授权\appendonlydir\appendonly.aof.114.incr.aof"
REDIS_ADDR = "127.0.0.1"
REDIS_PORT = 6379
REDIS_DB = 0

def redis_cmd(conn, *args):
    cmd = f"*{len(args)}\r\n"
    for arg in args:
        cmd += f"${len(arg)}\r\n{arg}\r\n"
    conn.sendall(cmd.encode('utf-8'))
    resp = b""
    while True:
        data = conn.recv(4096)
        if not data:
            break
        resp += data
        if b"+OK\r\n" in resp or b"-ERR" in resp or b":1\r\n" in resp or b":0\r\n" in resp:
            break
    return resp.decode('utf-8', errors='ignore').strip()

def main():
    with open(AOF_PATH, 'rb') as f:
        raw = f.read()

    # 找所有 SET wxid_* 的位置
    users = []
    i = 0
    while i < len(raw):
        # 找 "set\r\n$19\r\nwxid_" 模式
        idx = raw.find(b'set\r\n', i)
        if idx == -1:
            idx = raw.find(b'SET\r\n', i)
        if idx == -1:
            break

        # 检查后面是不是 wxid_
        after = raw[idx+5:idx+50]
        if b'wxid_' not in after:
            i = idx + 4
            continue

        # 解析 key
        key_start = raw.find(b'\r\n', idx) + 2
        key_end = raw.find(b'\r\n', key_start)
        key_len_str = raw[key_start+1:key_end].decode()  # 跳过 $
        key_len = int(key_len_str)

        val_len_start = key_end + 2 + key_len + 2  # key data + \r\n
        val_len_end = raw.find(b'\r\n', val_len_start)
        val_len_str = raw[val_len_start+1:val_len_end].decode()  # 跳过 $
        val_len = int(val_len_str)

        val_start = val_len_end + 2
        val = raw[val_start:val_start+val_len].decode('utf-8', errors='ignore')

        key = raw[key_end+2:key_end+2+key_len].decode()

        if key.startswith('wxid_') and len(val) > 500:
            try:
                data = json.loads(val)
                wxid = data.get('Wxid', key)
                nickname = data.get('NickName', '')
                users.append({'wxid': wxid, 'nickname': nickname, 'data': val})
            except:
                pass

        i = val_start + val_len

    # 去重
    seen = {}
    for u in users:
        if u['wxid'] not in seen:
            seen[u['wxid']] = u

    users = list(seen.values())
    print(f"提取到 {len(users)} 个用户")

    # 连接 Redis
    conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    conn.settimeout(5)
    try:
        conn.connect((REDIS_ADDR, REDIS_PORT))
    except Exception as e:
        print(f"连接失败: {e}")
        return

    redis_cmd(conn, "SELECT", str(REDIS_DB))

    imported = 0
    for user in users:
        wxid = user['wxid']
        resp = redis_cmd(conn, "EXISTS", wxid)
        if ":1" in resp:
            print(f"  跳过 {user['nickname']} ({wxid})")
            continue
        resp = redis_cmd(conn, "SET", wxid, user['data'])
        if "+OK" in resp:
            print(f"  导入 {user['nickname']} ({wxid})")
            imported += 1

    conn.close()
    print(f"\n完成: 新增 {imported} 个用户")

if __name__ == "__main__":
    main()
