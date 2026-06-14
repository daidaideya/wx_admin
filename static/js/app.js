const { createApp, ref, computed, onMounted, onUnmounted } = Vue;

const app = createApp({
  setup() {
    // ========== 状态 ==========
    const sidebarOpen = ref(false);
    const currentView = ref('users');
    const showLoginDialog = ref(false);

    const menuItems = [
      { view: 'users', icon: 'people', text: '用户管理' },
      { view: 'status', icon: 'dashboard', text: '系统状态' },
      { view: 'settings', icon: 'settings', text: '系统设置' },
    ];

    const currentTitle = computed(() => {
      const item = menuItems.find(m => m.view === currentView.value);
      return item ? item.text : '';
    });

    // 用户数据
    const users = ref([]);
    const selectedUsers = ref([]);
    const allSelected = computed(() =>
      users.value.length > 0 && selectedUsers.value.length === users.value.length
    );
    const onlineCount = computed(() =>
      users.value.filter(u => u.survival === 1).length
    );

    // 设备类型
    const deviceTypes = ref({});
    const selectedDevice = ref(null);
    const proxy = ref({ ProxyIp: '', ProxyUser: '', ProxyPassword: '' });
    const qrImage = ref('');
    const loginMsg = ref('');
    const loginMsgType = ref('info');
    let checkTimer = null;
    let currentUuid = '';

    // 系统状态 & 日志
    const sysStatus = ref({});
    const sysInfo = ref({});
    const sysLogs = ref([]);
    const logConfig = ref({ maxCount: 5000, maxDays: 3 });
    let logEventSource = null;

    // 心跳控制
    const heartbeatEnabled = ref(true);
    const heartbeatInterval = ref(150);

    // 设置
    const settings = ref({ token: '', proxy: '' });

    // Toast
    const toast = ref({ show: false, message: '', type: 'info', icon: 'info' });
    let toastTimer = null;

    // ========== 方法 ==========
    function navigate(view) {
      currentView.value = view;
      sidebarOpen.value = false;
      if (view === 'status') {
        loadSystemStatus();
        connectLogStream();
      } else {
        disconnectLogStream();
      }
      if (view === 'settings') {
        loadSystemInfo();
        loadLogConfig();
      }
    }

    function showToast(message, type = 'info') {
      const icons = { success: 'check_circle', error: 'error', info: 'info' };
      toast.value = { show: true, message, type, icon: icons[type] || 'info' };
      clearTimeout(toastTimer);
      toastTimer = setTimeout(() => { toast.value.show = false; }, 3000);
    }

    function formatTime(dateStr) {
      if (!dateStr) return '-';
      return dateStr;
    }

    // ========== 用户管理 ==========
    async function loadUsers() {
      try {
        const res = await fetch('/api/v1/wx/user/status');
        const data = await res.json();
        if (data.Code === 0 && data.Data) {
          users.value = data.Data;
        }
      } catch (e) {
        console.error('加载用户失败:', e);
      }
    }

    function toggleSelect(wxid) {
      const idx = selectedUsers.value.indexOf(wxid);
      if (idx >= 0) selectedUsers.value.splice(idx, 1);
      else selectedUsers.value.push(wxid);
    }

    function toggleAll() {
      if (allSelected.value) {
        selectedUsers.value = [];
      } else {
        selectedUsers.value = users.value.map(u => u.wxid);
      }
    }

    async function doAction(action, wxid) {
      const endpoints = {
        again: '/api/v1/wx/login/again',
        awake: '/api/v1/wx/login/awake',
        logout: '/api/v1/wx/login/logout',
        heartbeat: '/api/v1/wx/user/heartbeat',
      };
      const endpoint = endpoints[action];
      if (!endpoint) return;

      try {
        const res = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ wxid }),
        });
        const data = await res.json();
        showToast(data.Message || (data.Success ? '操作成功' : '操作失败'), data.Success ? 'success' : 'error');

        // 心跳失败时标记离线
        if (action === 'heartbeat' && !data.Success) {
          const idx = users.value.findIndex(u => u.wxid === wxid);
          if (idx >= 0) users.value[idx].survival = 0;
        }
        // 心跳成功时标记在线
        if (action === 'heartbeat' && data.Success) {
          const idx = users.value.findIndex(u => u.wxid === wxid);
          if (idx >= 0) users.value[idx].survival = 1;
        }
        if (action === 'logout') loadUsers();
      } catch (e) {
        showToast('请求失败: ' + e.message, 'error');
      }
    }

    async function batchAction(action) {
      if (action === 'delete') {
        if (!confirm(`确认删除 ${selectedUsers.value.length} 个用户？`)) return;
        try {
          await fetch('/api/v1/wx/user/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ wxids: selectedUsers.value }),
          });
          showToast('已删除', 'success');
          selectedUsers.value = [];
          loadUsers();
        } catch (e) {
          showToast('删除失败', 'error');
        }
        return;
      }
      for (const wxid of selectedUsers.value) {
        await doAction(action, wxid);
      }
    }

    // ========== 心跳控制 ==========
    async function loadHeartbeatStatus() {
      try {
        const res = await fetch('/api/v1/wx/heartbeat/status');
        const data = await res.json();
        if (data.Code === 0 && data.Data) {
          heartbeatEnabled.value = data.Data.enabled;
          heartbeatInterval.value = data.Data.interval;
        }
      } catch (e) {
        console.error('加载心跳状态失败:', e);
      }
    }

    async function toggleHeartbeat() {
      try {
        const newState = !heartbeatEnabled.value;
        const res = await fetch('/api/v1/wx/heartbeat/toggle', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled: newState, interval: heartbeatInterval.value }),
        });
        const data = await res.json();
        heartbeatEnabled.value = newState;
        showToast(data.Message, 'success');
      } catch (e) {
        showToast('操作失败', 'error');
      }
    }

    async function saveHeartbeatInterval() {
      try {
        const val = Math.max(30, Math.min(600, heartbeatInterval.value || 150));
        heartbeatInterval.value = val;
        const res = await fetch('/api/v1/wx/heartbeat/toggle', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ interval: val }),
        });
        const data = await res.json();
        showToast('心跳间隔已更新: ' + val + ' 秒', 'success');
      } catch (e) {
        showToast('保存失败', 'error');
      }
    }

    // ========== 同步用户 ==========
    async function syncUsers() {
      try {
        showToast('正在同步...', 'info');
        const res = await fetch('/api/v1/wx/user/sync', { method: 'POST' });
        const data = await res.json();
        showToast(data.Message, data.Success ? 'success' : 'error');
        if (data.Success) loadUsers();
      } catch (e) {
        showToast('同步失败: ' + e.message, 'error');
      }
    }

    // ========== 设备 & 登录 ==========
    async function loadDeviceTypes() {
      try {
        const res = await fetch('/api/v1/wx/login/devices');
        deviceTypes.value = await res.json();
      } catch (e) {
        console.error('加载设备类型失败:', e);
      }
    }

    function selectDevice(device) {
      selectedDevice.value = device;
      qrImage.value = '';
      loginMsg.value = '';
      getQRCode(device);
    }

    async function getQRCode(device) {
      try {
        const res = await fetch('/api/v1/wx/login/qrcode', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            DeviceType: device.key,
            DeviceName: device.deviceName,
            Proxy: proxy.value,
          }),
        });
        const data = await res.json();

        if (data.Success && data.Data) {
          const qrData = data.Data;
          // docker-wx 返回 QrBase64 已经包含 data:image/jpg;base64, 前缀
          if (qrData.QrBase64) {
            qrImage.value = qrData.QrBase64;
          }
          if (qrData.Uuid) {
            currentUuid = qrData.Uuid;
            startCheckLogin();
          }
          loginMsg.value = '请扫描二维码登录';
          loginMsgType.value = 'info';
        } else {
          loginMsg.value = data.Message || '获取二维码失败';
          loginMsgType.value = 'error';
        }
      } catch (e) {
        loginMsg.value = '请求失败: ' + e.message;
        loginMsgType.value = 'error';
      }
    }

    function startCheckLogin() {
      stopCheckLogin();
      checkTimer = setInterval(async () => {
        if (!currentUuid) return;
        try {
          const res = await fetch('/api/v1/wx/login/status', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ uuid: currentUuid }),
          });
          const data = await res.json();

          // 861版本: Code=0 且 Success=true → 登录成功
          if (data.Success && data.Code === 0 && data.Message === '登录成功') {
            loginMsg.value = '登录成功！';
            loginMsgType.value = 'success';
            stopCheckLogin();
            showToast('登录成功', 'success');
            setTimeout(() => {
              closeLoginDialog();
              loadUsers();
            }, 1500);
          }
          // Code=0 且 Success=true，但不是登录成功 → 等待扫码/确认
          else if (data.Success && data.Code === 0 && data.Data) {
            const loginData = data.Data;
            const status = loginData.Status || loginData.status || 0;
            if (status === 2) {
              loginMsg.value = '已确认登录，正在完成认证...';
              loginMsgType.value = 'success';
            } else if (status === 1) {
              loginMsg.value = '已扫码，请在手机上确认';
              loginMsgType.value = 'info';
            } else {
              loginMsg.value = '等待扫码...';
              loginMsgType.value = 'info';
            }
          }
          // Code=-3 → 需要验证码
          else if (data.Code === -3) {
            loginMsg.value = '需要验证码验证';
            loginMsgType.value = 'error';
            stopCheckLogin();
          }
          // Code=-8 → 登录异常（可能是二维码过期或登录失败）
          else if (data.Code === -8) {
            loginMsg.value = data.Message || '登录异常，请重新扫码';
            loginMsgType.value = 'error';
            stopCheckLogin();
          }
          // Code=-2 → 解包异常
          else if (data.Code === -2) {
            loginMsg.value = '登录流程异常，请重试';
            loginMsgType.value = 'error';
            stopCheckLogin();
          }
        } catch (e) {
          console.error('检查登录状态失败:', e);
        }
      }, 2000);
    }

    function stopCheckLogin() {
      if (checkTimer) {
        clearInterval(checkTimer);
        checkTimer = null;
      }
    }

    function closeLoginDialog() {
      showLoginDialog.value = false;
      selectedDevice.value = null;
      qrImage.value = '';
      loginMsg.value = '';
      currentUuid = '';
      stopCheckLogin();
    }

    // ========== 系统状态 & 日志 ==========
    async function loadSystemStatus() {
      try {
        const res = await fetch('/system/status');
        sysStatus.value = await res.json();
      } catch (e) {
        console.error('加载系统状态失败:', e);
      }
    }

    // 先加载历史日志
    async function loadLogs() {
      try {
        const res = await fetch('/system/logs');
        const data = await res.json();
        if (data.Code === 0 && data.Data) {
          sysLogs.value = data.Data.slice(-100); // 只显示最近100条
        }
      } catch (e) {
        console.error('加载日志失败:', e);
      }
    }

    // SSE 实时日志流
    function connectLogStream() {
      disconnectLogStream();
      loadLogs(); // 先加载历史

      logEventSource = new EventSource('/system/logs/stream');
      logEventSource.onmessage = (event) => {
        try {
          const newLogs = JSON.parse(event.data);
          sysLogs.value = [...sysLogs.value, ...newLogs].slice(-200);

          // 自动滚动到底部
          setTimeout(() => {
            const container = document.querySelector('.log-container');
            if (container) container.scrollTop = container.scrollHeight;
          }, 50);
        } catch (e) {
          console.error('解析日志失败:', e);
        }
      };
      logEventSource.onerror = () => {
        // 断线重连
        setTimeout(connectLogStream, 3000);
      };
    }

    function disconnectLogStream() {
      if (logEventSource) {
        logEventSource.close();
        logEventSource = null;
      }
    }

    async function loadSystemInfo() {
      try {
        const res = await fetch('/system/info');
        sysInfo.value = await res.json();
      } catch (e) {
        console.error('加载系统信息失败:', e);
      }
    }

    async function loadLogConfig() {
      try {
        const res = await fetch('/system/log/config');
        const data = await res.json();
        if (data.Code === 0 && data.Data) {
          logConfig.value = data.Data;
        }
      } catch (e) {
        console.error('加载日志配置失败:', e);
      }
    }

    async function saveLogConfig() {
      try {
        const res = await fetch('/system/log/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(logConfig.value),
        });
        const data = await res.json();
        showToast(data.Message || '已保存', 'success');
      } catch (e) {
        showToast('保存失败', 'error');
      }
    }

    async function activate() {
      try {
        const res = await fetch('/system/activate', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token: settings.value.token }),
        });
        const data = await res.json();
        showToast(data.Message, data.Success ? 'success' : 'error');
      } catch (e) {
        showToast('激活失败', 'error');
      }
    }

    async function saveSetting(type) {
      try {
        const res = await fetch('/system/' + type, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ proxy: settings.value.proxy }),
        });
        const data = await res.json();
        showToast(data.Message || '已保存', 'success');
      } catch (e) {
        showToast('保存失败', 'error');
      }
    }

    function refreshData() {
      loadUsers();
      if (currentView.value === 'status') {
        loadSystemStatus();
        loadLogs();
      }
      showToast('已刷新', 'info');
    }

    // ========== 下载日志 ==========
    function downloadLogs() {
      if (sysLogs.value.length === 0) {
        showToast('暂无日志可下载', 'info');
        return;
      }

      // 格式化日志内容
      const logContent = sysLogs.value.map(log => {
        return `[${log.time}] ${log.level} ${log.message}`;
      }).join('\n');

      // 创建下载
      const blob = new Blob([logContent], { type: 'text/plain;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `wx-admin-logs-${new Date().toISOString().slice(0, 10)}.txt`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      showToast('日志已下载', 'success');
    }

    // ========== 生命周期 ==========
    let refreshInterval = null;

    onMounted(async () => {
      loadDeviceTypes();
      loadHeartbeatStatus();
      // 启动时自动同步已有用户
      try {
        await fetch('/api/v1/wx/user/sync', { method: 'POST' });
      } catch (e) {}
      loadUsers();
      refreshInterval = setInterval(loadUsers, 30000);
    });

    onUnmounted(() => {
      stopCheckLogin();
      disconnectLogStream();
      if (refreshInterval) clearInterval(refreshInterval);
    });

    return {
      sidebarOpen, currentView, currentTitle, menuItems,
      users, selectedUsers, allSelected, onlineCount,
      deviceTypes, selectedDevice, proxy, qrImage, loginMsg, loginMsgType,
      sysStatus, sysInfo, sysLogs, logConfig, settings, toast, showLoginDialog,
      heartbeatEnabled, heartbeatInterval, toggleHeartbeat, saveHeartbeatInterval,
      navigate, formatTime, showToast,
      toggleSelect, toggleAll, doAction, batchAction,
      selectDevice, closeLoginDialog,
      loadSystemStatus, loadSystemInfo, loadLogConfig, saveLogConfig, activate, saveSetting,
      refreshData, loadUsers, syncUsers, downloadLogs,
    };
  }
});

app.mount('#app');
