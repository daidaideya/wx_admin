package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ========== 配置 ==========
type Config struct {
	Port              string
	WxAPI             string
	AdminToken        string
	HeartbeatInterval int // 心跳间隔(秒)
	AutoHeartbeat     bool
	RedisAddr         string
	RedisDB           int
}

var config Config

func loadConfig() {
	config.Port = getEnv("PORT", "8022")
	config.WxAPI = getEnv("WX_API", "http://127.0.0.1:8061")
	config.AdminToken = getEnv("ADMIN_TOKEN", "admin123")
	config.HeartbeatInterval = 150 // 2.5分钟
	config.AutoHeartbeat = true
	config.RedisAddr = getEnv("REDIS_ADDR", "127.0.0.1:6379")
	config.RedisDB = 0
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// ========== 数据模型 ==========
type ProxyInfo struct {
	ProxyIp       string `json:"ProxyIp"`
	ProxyUser     string `json:"ProxyUser"`
	ProxyPassword string `json:"ProxyPassword"`
}

type LoginReq struct {
	DeviceType string    `json:"DeviceType"`
	DeviceID   string    `json:"DeviceID"`
	DeviceName string    `json:"DeviceName"`
	Proxy      ProxyInfo `json:"Proxy"`
}

type UserStatus struct {
	Wxid        string `json:"wxid"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	Device      string `json:"device"`
	Survival    int    `json:"survival"`
	LoginDate   string `json:"loginDate"`
	RefreshDate string `json:"refreshDate"`
}

// ========== 日志系统 ==========
type LogEntry struct {
	ID      int    `json:"id"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type LogConfig struct {
	MaxCount int `json:"maxCount"`
	MaxDays  int `json:"maxDays"`
}

var (
	logBuffer   []LogEntry
	logMu       sync.RWMutex
	logID       int
	logConfig   = LogConfig{MaxCount: 5000, MaxDays: 3}
)

func addLog(level, message string) {
	logMu.Lock()
	defer logMu.Unlock()
	logID++
	logBuffer = append(logBuffer, LogEntry{
		ID:      logID,
		Time:    time.Now().Format("01-02 15:04:05"),
		Level:   level,
		Message: message,
	})
	if len(logBuffer) > logConfig.MaxCount {
		logBuffer = logBuffer[len(logBuffer)-logConfig.MaxCount:]
	}
}

func getLogs() []LogEntry {
	logMu.RLock()
	defer logMu.RUnlock()
	result := make([]LogEntry, len(logBuffer))
	copy(result, logBuffer)
	return result
}

func cleanOldLogs() {
	logMu.Lock()
	defer logMu.Unlock()
	if logConfig.MaxDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -logConfig.MaxDays)
	cutoffStr := cutoff.Format("01-02")
	var filtered []LogEntry
	for _, log := range logBuffer {
		if log.Time >= cutoffStr {
			filtered = append(filtered, log)
		}
	}
	logBuffer = filtered
}

// ========== 用户状态管理 ==========
var (
	userStatusMap = make(map[string]*UserStatus)
	userMapMu     sync.RWMutex
	startTime     = time.Now()
)

// ========== 设备类型 ==========
type DeviceItem struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Endpoint    string `json:"endpoint"`
	DeviceName  string `json:"deviceName"`
}

func getDeviceTypes() map[string][]DeviceItem {
	return map[string][]DeviceItem{
		"android": {
			{Key: "pad", Description: "安卓Pad", Endpoint: "/api/Login/LoginGetQRPad", DeviceName: "HUAWEI MatePad Pro"},
			{Key: "padx", Description: "安卓Pad(绕验证码)", Endpoint: "/api/Login/LoginGetQRPadx", DeviceName: "HUAWEI MatePad Pro"},
		},
		"ipad": {
			{Key: "ipad", Description: "iPad", Endpoint: "/api/Login/LoginGetQR", DeviceName: "iPad"},
			{Key: "ipadx", Description: "iPad(绕验证码)", Endpoint: "/api/Login/LoginGetQRx", DeviceName: "iPad"},
		},
		"mac": {
			{Key: "mac", Description: "Mac", Endpoint: "/api/Login/LoginGetQRMac", DeviceName: "MacBook Pro"},
		},
		"car": {
			{Key: "car", Description: "Car", Endpoint: "/api/Login/LoginGetQRCar", DeviceName: "Xiaomi-M2012K11AC"},
		},
		"windows": {
			{Key: "win", Description: "Windows", Endpoint: "/api/Login/LoginGetQRWin", DeviceName: "DESKTOP-P0QLAW8"},
			{Key: "winuwp", Description: "Windows UWP(绕验证码)", Endpoint: "/api/Login/LoginGetQRWinUwp", DeviceName: "DESKTOP-P0QLAW8"},
			{Key: "winunified", Description: "Windows 统一版", Endpoint: "/api/Login/LoginGetQRWinUnified", DeviceName: "DESKTOP-P0QLAW8"},
		},
	}
}

// ========== 代理请求 ==========
func proxyToWx(c *gin.Context, path string, body interface{}) {
	url := config.WxAPI + path
	addLog("INFO", fmt.Sprintf("代理请求: %s", path))

	var reqBody io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = &byteReader{data: jsonBytes}
	}

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		msg := "请求创建失败: " + err.Error()
		addLog("ERROR", msg)
		c.JSON(500, gin.H{"Code": -1, "Success": false, "Message": msg})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		msg := "后端服务不可达: " + err.Error()
		addLog("ERROR", msg)
		c.JSON(502, gin.H{"Code": -1, "Success": false, "Message": msg})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		msg := "读取响应失败"
		addLog("ERROR", msg)
		c.JSON(500, gin.H{"Code": -1, "Success": false, "Message": msg})
		return
	}

	addLog("INFO", fmt.Sprintf("代理响应: %s [%d]", path, resp.StatusCode))
	c.Header("Content-Type", "application/json")
	c.Status(resp.StatusCode)
	c.Writer.Write(respBody)
}

type byteReader struct {
	data []byte
	off  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

// ========== Redis 客户端 ==========
type RedisClient struct {
	addr string
	db   int
}

func NewRedisClient(addr string, db int) *RedisClient {
	return &RedisClient{addr: addr, db: db}
}

func (r *RedisClient) sendCommand(args ...string) (string, error) {
	conn, err := net.DialTimeout("tcp", r.addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// SELECT db
	if r.db > 0 {
		selectCmd := fmt.Sprintf("*2\r\n$6\r\nSELECT\r\n$%d\r\n%d\r\n", len(strconv.Itoa(r.db)), r.db)
		conn.Write([]byte(selectCmd))
		buf := bufio.NewReader(conn)
		buf.ReadString('\n')
	}

	// 发送命令
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, arg := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg)
	}
	conn.Write([]byte(cmd))

	// 读取响应
	buf := bufio.NewReader(conn)
	return readRESP(buf)
}

func readRESP(buf *bufio.Reader) (string, error) {
	line, err := buf.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return "", fmt.Errorf("empty RESP line")
	}

	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return "", fmt.Errorf(line[1:])
	case ':':
		return line[1:], nil
	case '$':
		length, _ := strconv.Atoi(line[1:])
		if length == -1 {
			return "", nil
		}
		// 读取指定长度的数据 + \r\n
		data := make([]byte, length)
		_, err := io.ReadFull(buf, data)
		if err != nil {
			return "", err
		}
		// 读取末尾的 \r\n
		crlf := make([]byte, 2)
		buf.Read(crlf)
		return string(data), nil
	case '*':
		count, _ := strconv.Atoi(line[1:])
		if count == -1 {
			return "", nil
		}
		var results []string
		for i := 0; i < count; i++ {
			val, err := readRESP(buf)
			if err != nil {
				return "", err
			}
			results = append(results, val)
		}
		return strings.Join(results, "\n"), nil
	}
	return "", fmt.Errorf("unknown RESP type: %c", line[0])
}

// ========== 自动心跳调度 ==========
func autoHeartbeatLoop() {
	// 启动后等 10 秒再开始第一次心跳
	time.Sleep(10 * time.Second)

	for {
		if config.AutoHeartbeat {
			userMapMu.RLock()
			users := make([]string, 0)
			for wxid, u := range userStatusMap {
				if u.Survival == 1 {
					users = append(users, wxid)
				}
			}
			userMapMu.RUnlock()

			if len(users) > 0 {
				addLog("INFO", fmt.Sprintf("自动心跳: 开始, 共 %d 个在线用户", len(users)))
			}

			for _, wxid := range users {
				url := config.WxAPI + "/api/Login/HeartBeat?wxid=" + wxid
				client := &http.Client{Timeout: 30 * time.Second}
				resp, err := client.Post(url, "application/json", nil)
				if err != nil {
					addLog("ERROR", fmt.Sprintf("[%s] 心跳: %s", getUserNickname(wxid), err.Error()))
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				var result map[string]interface{}
				json.Unmarshal(body, &result)
				success, _ := result["Success"].(bool)
				msg := getString(result, "Message")

				if success {
					addLog("INFO", fmt.Sprintf("[%s](%s) 心跳: 成功", getUserNickname(wxid), wxid))
					userMapMu.Lock()
					if u, ok := userStatusMap[wxid]; ok {
						u.Survival = 1
						u.RefreshDate = time.Now().Format("2006-01-02 15:04:05")
					}
					userMapMu.Unlock()
				} else {
					// 心跳失败，判断是否已退出
					offline := strings.Contains(msg, "退出") || strings.Contains(msg, "过期") ||
						strings.Contains(msg, "expired") || strings.Contains(msg, "timeout") ||
						strings.Contains(msg, "异常") || strings.Contains(msg, "失败")
					if offline {
						userMapMu.Lock()
						if u, ok := userStatusMap[wxid]; ok {
							u.Survival = 0
						}
						userMapMu.Unlock()
					}
					addLog("WARN", fmt.Sprintf("[%s](%s) 心跳: %s", getUserNickname(wxid), wxid, msg))
				}

				// 每个用户之间间隔 500ms，避免并发过高
				time.Sleep(500 * time.Millisecond)
			}
		}

		time.Sleep(time.Duration(config.HeartbeatInterval) * time.Second)
	}
}

func getUserNickname(wxid string) string {
	userMapMu.RLock()
	defer userMapMu.RUnlock()
	if u, ok := userStatusMap[wxid]; ok && u.Nickname != "" {
		return u.Nickname
	}
	return wxid
}

// ========== 主函数 ==========
func main() {
	loadConfig()

	// 添加启动日志
	addLog("INFO", "WX Admin 启动成功")
	addLog("INFO", fmt.Sprintf("后端地址: %s", config.WxAPI))

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 静态文件
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.File("./static/index.html")
	})

	// ====== 系统管理 API ======

	r.GET("/system/info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"version":      "1.0.0",
			"author":       "wx-admin",
			"compile_time": time.Now().Format("2006-01-02"),
			"repo":         "https://github.com",
			"description":  "微信授权管理后台 - 基于 docker-wx 协议层",
		})
	})

	r.GET("/system/status", func(c *gin.Context) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		userMapMu.RLock()
		onlineCount := 0
		for _, u := range userStatusMap {
			if u.Survival == 1 {
				onlineCount++
			}
		}
		userMapMu.RUnlock()

		c.JSON(200, gin.H{
			"cpu":         fmt.Sprintf("%d cores", runtime.NumCPU()),
			"memory":      fmt.Sprintf("%.1f MB", float64(m.Alloc)/1024/1024),
			"uptime":      time.Since(startTime).Truncate(time.Second).String(),
			"onlineUsers": onlineCount,
			"goroutines":  runtime.NumGoroutine(),
		})
	})

	// 系统日志接口
	r.GET("/system/logs", func(c *gin.Context) {
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Data": getLogs()})
	})

	// SSE 日志流
	r.GET("/system/logs/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		lastID := 0
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			logs := getLogs()
			var newLogs []LogEntry
			for _, l := range logs {
				if l.ID > lastID {
					newLogs = append(newLogs, l)
					lastID = l.ID
				}
			}
			if len(newLogs) > 0 {
				data, _ := json.Marshal(newLogs)
				fmt.Fprintf(c.Writer, "data: %s\n\n", data)
				c.Writer.Flush()
			}
		}
	})

	r.POST("/system/proxy", func(c *gin.Context) {
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "代理配置已更新"})
	})

	r.POST("/system/push", func(c *gin.Context) {
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "推送配置已更新"})
	})

	r.GET("/system/log/config", func(c *gin.Context) {
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Data": logConfig})
	})

	r.POST("/system/log/config", func(c *gin.Context) {
		var req LogConfig
		c.BindJSON(&req)
		if req.MaxCount > 0 {
			logConfig.MaxCount = req.MaxCount
		}
		if req.MaxDays > 0 {
			logConfig.MaxDays = req.MaxDays
		}
		addLog("INFO", fmt.Sprintf("日志配置已更新: 最大条数=%d, 最大天数=%d", logConfig.MaxCount, logConfig.MaxDays))
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "日志配置已更新"})
	})

	r.POST("/system/activate", func(c *gin.Context) {
		var req struct {
			Token string `json:"token"`
		}
		c.BindJSON(&req)
		if req.Token == config.AdminToken {
			addLog("INFO", "激活码验证成功")
			c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "激活成功"})
		} else {
			addLog("WARN", "激活码验证失败")
			c.JSON(200, gin.H{"Code": -1, "Success": false, "Message": "激活码错误"})
		}
	})

	r.GET("/system/update/check", func(c *gin.Context) {
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "当前已是最新版本", "version": "1.0.0"})
	})

	// ====== 同步已有用户 ======
	r.POST("/api/v1/wx/user/sync", func(c *gin.Context) {
		redis := NewRedisClient(config.RedisAddr, config.RedisDB)
		keys, err := redis.sendCommand("KEYS", "wxid_*")
		if err != nil {
			addLog("ERROR", "Redis 同步失败: "+err.Error())
			c.JSON(500, gin.H{"Code": -1, "Success": false, "Message": "Redis 连接失败: " + err.Error()})
			return
		}

		keyList := strings.Split(keys, "\n")
		synced := 0

		for _, key := range keyList {
			key = strings.TrimSpace(key)
			if key == "" || !strings.HasPrefix(key, "wxid_") {
				continue
			}

			val, err := redis.sendCommand("GET", key)
			if err != nil || val == "" {
				continue
			}

			var userData map[string]interface{}
			json.Unmarshal([]byte(val), &userData)
			if userData == nil {
				continue
			}

			wxid := getString(userData, "Wxid")
			if wxid == "" {
				wxid = key
			}

			userMapMu.Lock()
			if _, exists := userStatusMap[wxid]; !exists {
				userStatusMap[wxid] = &UserStatus{
					Wxid:        wxid,
					Nickname:    getString(userData, "NickName"),
					Avatar:      getString(userData, "HeadUrl"),
					Device:      getString(userData, "DeviceName"),
					Survival:    1,
					LoginDate:   time.Now().Format("2006-01-02 15:04:05"),
					RefreshDate: time.Now().Format("2006-01-02 15:04:05"),
				}
				synced++
			}
			userMapMu.Unlock()
		}

		total := len(userStatusMap)
		addLog("INFO", fmt.Sprintf("同步完成: 新增 %d 个, 总计 %d 个用户", synced, total))
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": fmt.Sprintf("同步完成: 新增 %d, 总计 %d", synced, total)})
	})

	// ====== 登录 API ======

	// 设备类型列表
	r.GET("/api/v1/wx/login/devices", func(c *gin.Context) {
		c.JSON(200, getDeviceTypes())
	})

	// 获取登录二维码
	r.POST("/api/v1/wx/login/qrcode", func(c *gin.Context) {
		var req LoginReq
		if err := c.BindJSON(&req); err != nil {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "参数错误"})
			return
		}

		// 查找设备对应的 endpoint
		devices := getDeviceTypes()
		endpoint := ""
		defaultName := ""
		for _, group := range devices {
			for _, d := range group {
				if d.Key == req.DeviceType {
					endpoint = d.Endpoint
					defaultName = d.DeviceName
					break
				}
			}
			if endpoint != "" {
				break
			}
		}

		if endpoint == "" {
			c.JSON(200, gin.H{"Code": -1, "Success": false, "Message": "不支持的设备类型: " + req.DeviceType})
			return
		}

		if req.DeviceName == "" || req.DeviceName == "string" {
			req.DeviceName = defaultName
		}

		addLog("INFO", fmt.Sprintf("获取二维码: %s (%s)", req.DeviceType, req.DeviceName))

		// 构造 docker-wx 请求体
		wxReq := map[string]interface{}{
			"DeviceID":   req.DeviceID,
			"DeviceName": req.DeviceName,
			"Proxy":      req.Proxy,
		}

		// 发请求到 docker-wx
		url := config.WxAPI + endpoint
		jsonBytes, _ := json.Marshal(wxReq)
		resp, err := http.Post(url, "application/json", &byteReader{data: jsonBytes})
		if err != nil {
			msg := "后端服务不可达: " + err.Error()
			addLog("ERROR", msg)
			c.JSON(502, gin.H{"Code": -1, "Success": false, "Message": msg})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		// 解析响应，提取关键字段
		var result map[string]interface{}
		json.Unmarshal(body, &result)

		// docker-wx 返回格式: {Code: 1, Success: true, Data: {QrBase64: "...", Uuid: "..."}}
		if success, _ := result["Success"].(bool); success {
			addLog("INFO", "二维码获取成功")
		} else {
			msg := getString(result, "Message")
			addLog("WARN", "二维码获取失败: "+msg)
		}

		c.Header("Content-Type", "application/json")
		c.Writer.Write(body)
	})

	// 检查登录状态 (轮询)
	r.POST("/api/v1/wx/login/status", func(c *gin.Context) {
		var req struct {
			Uuid string `json:"uuid"`
		}
		if err := c.BindJSON(&req); err != nil || req.Uuid == "" {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "缺少 uuid"})
			return
		}

		// 请求 docker-wx 的 CheckQR 接口
		checkUrl := config.WxAPI + "/api/Login/LoginCheckQR?uuid=" + req.Uuid
		addLog("INFO", fmt.Sprintf("CheckQR 请求: %s", checkUrl))

		// 用 http.NewRequest 不设 Content-Type，让 Beego 正确读取 query 参数
		httpReq, _ := http.NewRequest("POST", checkUrl, nil)
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			c.JSON(502, gin.H{"Code": -1, "Success": false, "Message": "后端服务不可达"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		// 解析响应 (json.Unmarshal 到 interface{} 时数字是 float64)
		var result map[string]interface{}
		json.Unmarshal(body, &result)

		code := getInt64(result, "Code")
		success, _ := result["Success"].(bool)

		// 打印完整响应（截断避免太长）
		bodyStr := string(body)
		if len(bodyStr) > 300 {
			bodyStr = bodyStr[:300] + "..."
		}
		msg := getString(result, "Message")

		// 861版本: 登录成功时 Code=0, Success=true, Message="登录成功"
		if success && code == 0 && msg == "登录成功" {
			addLog("INFO", "登录成功")
		} else if success && code == 0 {
			// 等待扫码/确认
		} else if code < 0 {
			if msg != "" {
				addLog("WARN", "登录状态: "+msg)
			}
		}

		c.Header("Content-Type", "application/json")
		c.Writer.Write(body)
	})

	// 重新登录
	r.POST("/api/v1/wx/login/again", func(c *gin.Context) {
		var req struct {
			Wxid string `json:"wxid"`
		}
		c.BindJSON(&req)
		if req.Wxid == "" {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "缺少 wxid"})
			return
		}
		addLog("INFO", fmt.Sprintf("重新登录: %s", req.Wxid))
		proxyToWx(c, "/api/Login/LoginTwiceAutoAuth?wxid="+req.Wxid, nil)
	})

	// 唤醒登录 (861版本需要body参数)
	r.POST("/api/v1/wx/login/awake", func(c *gin.Context) {
		var req struct {
			Wxid  string    `json:"wxid"`
			Proxy ProxyInfo `json:"Proxy"`
		}
		c.BindJSON(&req)
		if req.Wxid == "" {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "缺少 wxid"})
			return
		}
		addLog("INFO", fmt.Sprintf("唤醒登录: %s", req.Wxid))

		// 构造861版本需要的body参数
		wxReq := map[string]interface{}{
			"Wxid":  req.Wxid,
			"Proxy": req.Proxy,
		}
		proxyToWx(c, "/api/Login/LoginAwaken", wxReq)
	})

	// 二次登录
	r.POST("/api/v1/wx/login/twice", func(c *gin.Context) {
		var req struct {
			Wxid string `json:"wxid"`
		}
		c.BindJSON(&req)
		if req.Wxid == "" {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "缺少 wxid"})
			return
		}
		addLog("INFO", fmt.Sprintf("二次登录: %s", req.Wxid))
		proxyToWx(c, "/api/Login/LoginTwiceAutoAuth?wxid="+req.Wxid, nil)
	})

	// 退出登录
	r.POST("/api/v1/wx/login/logout", func(c *gin.Context) {
		var req struct {
			Wxid string `json:"wxid"`
		}
		c.BindJSON(&req)
		if req.Wxid == "" {
			c.JSON(400, gin.H{"Code": -1, "Success": false, "Message": "缺少 wxid"})
			return
		}

		addLog("INFO", fmt.Sprintf("退出登录: %s", req.Wxid))
		proxyToWx(c, "/api/Login/LogOut?wxid="+req.Wxid, nil)

		userMapMu.Lock()
		if u, ok := userStatusMap[req.Wxid]; ok {
			u.Survival = 0
		}
		userMapMu.Unlock()
	})

	// ====== 用户管理 API ======

	// 用户状态列表
	r.GET("/api/v1/wx/user/status", func(c *gin.Context) {
		userMapMu.RLock()
		users := make([]*UserStatus, 0, len(userStatusMap))
		for _, u := range userStatusMap {
			users = append(users, u)
		}
		userMapMu.RUnlock()

		c.JSON(200, gin.H{"Code": 0, "Success": true, "Data": users})
	})

	// 获取用户缓存信息 (861版本需要wxid参数)
	r.GET("/api/v1/wx/user/database", func(c *gin.Context) {
		wxid := c.Query("wxid")
		if wxid == "" {
			// 没有指定wxid，返回本地缓存的用户列表
			userMapMu.RLock()
			users := make([]*UserStatus, 0, len(userStatusMap))
			for _, u := range userStatusMap {
				users = append(users, u)
			}
			userMapMu.RUnlock()
			c.JSON(200, gin.H{"Code": 0, "Success": true, "Data": users})
			return
		}

		// 指定了wxid，从后端获取该用户的缓存信息
		url := config.WxAPI + "/api/Login/GetCacheInfo?wxid=" + wxid
		httpReq, _ := http.NewRequest("POST", url, nil)
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			c.JSON(502, gin.H{"Code": -1, "Success": false, "Message": "后端服务不可达"})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		c.Header("Content-Type", "application/json")
		c.Writer.Write(body)
	})

	// 心跳
	r.POST("/api/v1/wx/user/heartbeat", func(c *gin.Context) {
		var req struct {
			Wxid   string `json:"wxid"`
			Option string `json:"option"`
		}
		c.BindJSON(&req)

		endpoint := "/api/Login/HeartBeat"
		if req.Option == "long" {
			endpoint = "/api/Login/HeartBeatLong"
		}

		addLog("INFO", fmt.Sprintf("心跳: %s", req.Wxid))
		proxyToWx(c, endpoint+"?wxid="+req.Wxid, nil)

		userMapMu.Lock()
		if u, ok := userStatusMap[req.Wxid]; ok {
			u.RefreshDate = time.Now().Format("2006-01-02 15:04:05")
		}
		userMapMu.Unlock()
	})

	// 更新代理
	r.POST("/api/v1/wx/tools/update/proxy", func(c *gin.Context) {
		var req struct {
			Wxid  string    `json:"wxid"`
			Proxy ProxyInfo `json:"Proxy"`
		}
		c.BindJSON(&req)
		addLog("INFO", fmt.Sprintf("更新代理: %s", req.Wxid))
		proxyToWx(c, "/api/Tools/setproxy", req)
	})

	// 删除用户
	r.POST("/api/v1/wx/user/delete", func(c *gin.Context) {
		var req struct {
			Wxids []string `json:"wxids"`
		}
		c.BindJSON(&req)

		userMapMu.Lock()
		for _, wxid := range req.Wxids {
			delete(userStatusMap, wxid)
		}
		userMapMu.Unlock()

		addLog("INFO", fmt.Sprintf("删除用户: %s", strings.Join(req.Wxids, ",")))
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": "已删除"})
	})

	// ====== 心跳控制 API ======

	// 获取心跳状态
	r.GET("/api/v1/wx/heartbeat/status", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"Code":    0,
			"Success": true,
			"Data": gin.H{
				"enabled":  config.AutoHeartbeat,
				"interval": config.HeartbeatInterval,
			},
		})
	})

	// 开关自动心跳
	r.POST("/api/v1/wx/heartbeat/toggle", func(c *gin.Context) {
		var req struct {
			Enabled  *bool `json:"enabled"`
			Interval *int  `json:"interval"`
		}
		c.BindJSON(&req)
		if req.Enabled != nil {
			config.AutoHeartbeat = *req.Enabled
		}
		if req.Interval != nil && *req.Interval > 0 {
			config.HeartbeatInterval = *req.Interval
		}
		status := "关闭"
		if config.AutoHeartbeat {
			status = "开启"
		}
		addLog("INFO", fmt.Sprintf("自动心跳: %s, 间隔: %d秒", status, config.HeartbeatInterval))
		c.JSON(200, gin.H{"Code": 0, "Success": true, "Message": fmt.Sprintf("自动心跳已%s", status)})
	})

	// ====== 启动自动心跳 ======
	go autoHeartbeatLoop()

	// ====== 启动日志清理 ======
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			cleanOldLogs()
		}
	}()

	// ====== 启动 ======
	log.Printf("========================================")
	log.Printf("  WX Admin 启动成功")
	log.Printf("  端口: %s", config.Port)
	log.Printf("  后端: %s", config.WxAPI)
	log.Printf("  管理密码: %s", config.AdminToken)
	log.Printf("  自动心跳: 每 %d 秒", config.HeartbeatInterval)
	log.Printf("========================================")

	r.Run(":" + config.Port)
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return int64(val)
		case int64:
			return val
		case int:
			return int64(val)
		case json.Number:
			n, _ := val.Int64()
			return n
		}
	}
	return 0
}
