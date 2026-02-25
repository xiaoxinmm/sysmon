# sysmon

一个用 Go 写的轻量级系统探针，编译出来就一个二进制文件，丢到服务器上直接跑。

实时监控 CPU、内存、磁盘、网络、系统负载和进程列表，通过 WebSocket 推送到浏览器，不用刷新页面。如果你的机器上跑了 Docker，还会自动识别并显示容器状态。

## 截图

（待补充）

## 功能

- CPU 使用率（总体 + 每核心）
- 内存 / Swap 使用情况
- 磁盘挂载点和使用率
- 网络接口流量和实时速率
- 系统负载（1/5/15 分钟）
- 进程列表（Top 50，可按 CPU/内存/PID 排序）
- CPU + 内存历史趋势图（最近 1 小时，Canvas 绘制）
- Docker 容器监控（自动检测，无容器时隐藏）
- 可选密码保护
- JSON 配置文件
- WebSocket 实时推送，1.5 秒刷新
- 断线自动重连
- 深色主题，手机端适配

## 快速开始

### 下载编译

需要 Go 1.21 以上。

```bash
git clone https://github.com/xiaoxinmm/sysmon.git
cd sysmon
CGO_ENABLED=0 go build -ldflags="-s -w" -o sysmon .
```

编译完就一个 `sysmon` 文件，大概 5-6MB。

### 直接运行

```bash
./sysmon
```

默认监听 `0.0.0.0:8888`，打开浏览器访问 `http://你的IP:8888` 就能看到了。

### 指定端口

```bash
./sysmon -port 9090
```

或者用环境变量：

```bash
PORT=9090 ./sysmon
```

### 设置密码

不设密码就是公开访问，设了密码会弹出登录页面。

```bash
SYSMON_PASSWORD=你的密码 ./sysmon
```

或者写在配置文件里（见下面）。

### 配置文件

默认读取 `/etc/sysmon.json`，也可以手动指定：

```bash
./sysmon -config /path/to/config.json
```

配置文件示例：

```json
{
  "port": 8888,
  "refresh_interval": 1.5,
  "max_processes": 50,
  "password": "",
  "history_duration": 3600
}
```

| 字段 | 说明 | 默认值 |
|------|------|--------|
| `port` | 监听端口 | 8888 |
| `refresh_interval` | 数据刷新间隔（秒） | 1.5 |
| `max_processes` | 进程列表显示数量 | 50 |
| `password` | 访问密码，留空不启用 | 空 |
| `history_duration` | 历史数据保留秒数 | 3600 |

环境变量会覆盖配置文件：`PORT`、`SYSMON_PASSWORD`、`SYSMON_REFRESH`、`SYSMON_MAX_PROCS`、`SYSMON_HISTORY`。

## 注册为系统服务

```bash
# 复制到系统路径
sudo cp sysmon /usr/local/bin/

# 创建 systemd service
sudo tee /etc/systemd/system/sysmon.service << 'EOF'
[Unit]
Description=sysmon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sysmon -port=8888
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

# 启用并启动
sudo systemctl daemon-reload
sudo systemctl enable sysmon
sudo systemctl start sysmon
```

如果要加密码，在 `[Service]` 里加一行：

```ini
Environment=SYSMON_PASSWORD=你的密码
```

## Docker 容器监控

如果服务器上装了 Docker，sysmon 会自动通过 `/var/run/docker.sock` 检测容器。不需要额外配置，有容器就显示，没有就隐藏。

如果 sysmon 跑在非 root 用户下，需要把用户加到 docker 组：

```bash
sudo usermod -aG docker 你的用户名
```

## 技术栈

- Go + net/http（不依赖 Web 框架）
- gorilla/websocket（WebSocket 通信）
- shirou/gopsutil（系统信息采集）
- 前端原生 HTML/CSS/JS + Canvas（零依赖）
- 所有前端资源通过 `embed` 嵌入二进制

## 目录结构

```
├── main.go              # 入口、路由、WebSocket、认证
├── monitor/
│   └── monitor.go       # 系统信息采集、Docker API、历史数据
├── web/
│   ├── index.html       # 主页面
│   ├── css/style.css    # 样式
│   └── js/app.js        # 前端逻辑
├── go.mod
├── go.sum
└── sysmon.example.json  # 配置示例
```

## License

MIT
