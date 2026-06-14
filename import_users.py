import re
import json
import socket

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
        if b"+OK\r\n" in resp or b"-ERR" in resp or resp.endswith(b"\r\n"):
            break
    return resp.decode('utf-8', errors='ignore').strip()

def main():
    # 读取 AOF 文件 (二进制模式，保留 \r\n)
    with open(AOF_PATH, 'rb') as f:
        content = f.read().decode('utf-8', errors='ignore')

    # 解析 RESP SET 命令
    pattern = r'\*3\r\n\$\d+\r\n(?:set|SET)\r\n\$(\d+)\r\n(wxid_[^\r\n]+)\r\n\$(\d+)\r\n'
    matches = list(re.finditer(pattern, content))

    users = []
    seen = set()
    for m in matches:
        key = m.group(2)
        val_len = int(m.group(3))
        val_start = m.end()
        val = content[val_start:val_start+val_len]

        if key in seen:
            continue

        if 'Sessionkey' in val and len(val) > 1000:
            seen.add(key)
            try:
                data = json.loads(val)
                wxid = data.get('Wxid', key)
                users.append({
                    'wxid': wxid,
                    'nickname': data.get('NickName', ''),
                    'data': val
                })
            except:
                pass

    print(f"从 AOF 中提取到 {len(users)} 个用户")

    # 连接 Redis
    conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    conn.settimeout(5)
    try:
        conn.connect((REDIS_ADDR, REDIS_PORT))
    except Exception as e:
        print(f"连接 Redis 失败: {e}")
        return

    # SELECT DB
    resp = redis_cmd(conn, "SELECT", str(REDIS_DB))
    print(f"SELECT: {resp}")

    # 导入
    imported = 0
    for user in users:
        wxid = user['wxid']
        nickname = user['nickname']

        # 检查是否已存在
        resp = redis_cmd(conn, "EXISTS", wxid)
        exists = ":1" in resp
        if exists:
            print(f"  跳过 {nickname} ({wxid}) - 已存在")
            continue

        resp = redis_cmd(conn, "SET", wxid, user['data'])
        if "+OK" in resp:
            print(f"  导入 {nickname} ({wxid}) - OK")
            imported += 1
        else:
            print(f"  导入 {nickname} ({wxid}) - 失败")

    conn.close()
    print(f"\n完成: 导入 {imported} 个用户")

if __name__ == "__main__":
    main()
