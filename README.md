# sysmon v2 — 统一系统监控 + 网络流量监控工具

> 一款轻量级、高性能的实时系统监控与网络流量分析工具，基于 Go 语言开发，单二进制文件部署，内置 Web 界面。

<!-- 截图占位 -->
<!-- ![系统监控概览](screenshots/overview.png) -->
<!-- ![网络流量监控](screenshots/traffic.png) -->
<!-- ![终端 Shell](screenshots/shell.png) -->

## 功能特性

### 系统监控
- **CPU 监控**：每核心使用率、平均使用率、CPU 型号信息
- **内存监控**：物理内存/Swap 使用量、使用率
- **磁盘监控**：各分区使用情况、挂载点、文件系统类型
- **网络监控**：各网卡实时收发速率、字节计数
- **系统负载**：1/5/15 分钟平均负载
- **进程列表**：按 CPU/内存排序的进程树（含 PPID）
- **Docker 容器**：容器列表、CPU/内存使用统计（通过 Docker Socket API）
- **历史趋势**：CPU/内存使用率时间线图表，SQLite 持久化存储

### 网络流量监控
- **端口级流量分析**：实时抓包（基于 libpcap），按端口/协议统计流量
- **主机级流量分析**：按源 IP/目标 IP 统计的主机间通信流量
- **DNS 反向解析**：自动解析 IP 地址对应的主机名
- **异常检测**：基于统计模型的流量异常告警
- **数据降采样**：自动按小时/天粒度聚合历史数据，节省存储空间
- **流量数据持久化**：SQLite 存储，支持按时间范围查询

### 其他特性
- **WebSocket 实时推送**：所有监控数据通过 WebSocket 实时推送到前端，无需轮询
- **Web Terminal**：内置 xterm.js 终端，支持远程 Shell 访问（需密码认证）
- **暗色/亮色主题**：支持主题切换
- **ECharts 图表**：系统指标与流量数据的可视化图表
- **Agent 模式**：Master-Agent 架构，支持多节点集中监控
- **SIGHUP 热重载**：通过发送 SIGHUP 信号重新加载配置，无需重启
- **Prometheus 集成**：内置 `/metrics` 端点，暴露 CPU/内存/磁盘/Docker/WebSocket 等指标
- **配置灵活**：JSON 配置文件 + 环境变量覆盖
- **结构化日志**：支持 text/JSON 格式日志输出，可配置日志级别

## 安装方式

### 从 GitHub Releases 下载预编译二进制

访问 [Releases](https://github.com/xiaoxinmm/sysmon/releases) 页面，下载对应平台的二进制文件：

```bash
# Linux x86_64
curl -L https://github.com/xiaoxinmm/sysmon/releases/latest/download/sysmon-linux-amd64.tar.gz | tar xz
chmod +x sysmon

# Linux ARM64
curl -L https://github.com/xiaoxinmm/sysmon/releases/latest/download/sysmon-linux-arm64.tar.gz | tar xz
chmod +x sysmon

# macOS (Apple Silicon)
curl -L https://github.com/xiaoxinmm/sysmon/releases/latest/download/sysmon-darwin-arm64.tar.gz | tar xz
chmod +x sysmon
```

### 从源码编译

要求 Go 1.25+：

```bash
git clone https://github.com/xiaoxinmm/sysmon.git
cd sysmon
git checkout v2

# 编译（纯 Go，无需 CGO）
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o sysmon .

# 或使用 make（如有 Makefile）
make build
```

> **注意**：项目使用 `modernc.org/sqlite`（纯 Go 实现的 SQLite），编译时无需 CGO 或系统 sqlite 库。

## 配置说明

创建 `config.json`（或任意名称），启动时通过 `-config` 参数指定：

```json
{
  "port": 8888,
  "password": "your_password",
  "refreshInterval": 1500,
  "maxProcesses": 50,
  "historyDuration": 3600,
  "history_db": "./sysmon.db",
  "history_retention_days": 7,
  "logLevel": "info",
  "logFormat": "text",
  "enableShell": true,
  "shell_password": "shell_password",
  "enable_traffic": true,
  "traffic_iface": "eth0",
  "traffic_interval": 60,
  "agent_mode": "",
  "agent_master": "",
  "agent_api_key": "",
  "agent_keys": [],
  "agent_interval": 60
}
```

### 配置字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `port` | int | 8888 | HTTP 监听端口 |
| `password` | string | `""` | Web 界面访问密码（空则无认证） |
| `refreshInterval` | int | 1500 | 数据采集间隔（毫秒） |
| `maxProcesses` | int | 50 | 进程列表最大数量 |
| `historyDuration` | int | 3600 | 内存中保留的历史数据点数 |
| `history_db` | string | `./sysmon.db` | SQLite 数据库文件路径 |
| `history_retention_days` | int | 7 | 历史数据保留天数 |
| `logLevel` | string | `info` | 日志级别：debug/info/warn/error |
| `logFormat` | string | `text` | 日志格式：text/json |
| `enableShell` | bool | false | 是否启用 Web Terminal |
| `shell_password` | string | `""` | Shell 访问密码（需同时开启 enableShell） |
| `enable_traffic` | bool | false | 是否启用流量监控 |
| `traffic_iface` | string | `""` | 抓包网卡（空则自动检测） |
| `traffic_interval` | int | 60 | 流量快照存储间隔（秒） |
| `agent_mode` | string | `""` | Agent 模式：`""`（禁用）/`master`/`agent` |
| `agent_master` | string | `""` | Master 节点 URL（agent 模式用） |
| `agent_api_key` | string | `""` | API Key（agent 模式用） |
| `agent_keys` | []string | `[]` | 允许的 API Key 列表（master 模式用） |
| `agent_interval` | int | 60 | Agent 上报间隔（秒） |

### 环境变量覆盖

以下环境变量可覆盖对应配置：

| 环境变量 | 覆盖字段 |
|----------|----------|
| `PORT` | port |
| `SYSMON_PASSWORD` | password |
| `SYSMON_REFRESH` | refreshInterval |
| `SYSMON_MAX_PROCS` | maxProcesses |
| `SYSMON_HISTORY` | historyDuration |
| `SYSMON_HISTORY_FILE` | history_file |
| `SYSMON_LOG_LEVEL` | logLevel |
| `SYSMON_LOG_FORMAT` | logFormat |
| `ALLOWED_ORIGINS` | WebSocket 允许的 Origin（逗号分隔） |

## 使用方法

### 启动

```bash
# 使用配置文件
./sysmon -config config.json

# 不使用密码（开放访问）
./sysmon

# 使用环境变量
SYSMON_PASSWORD=mypass PORT=9090 ./sysmon
```

### 访问

启动后在浏览器中访问：

```
http://your-server-ip:8888
```

如设置了密码，需在登录页面输入密码后方可访问监控面板。

### Agent 模式（多节点监控）

**Master 节点**配置：
```json
{
  "agent_mode": "master",
  "agent_keys": ["your-secret-key-1", "your-secret-key-2"]
}
```

**Agent 节点**配置：
```json
{
  "agent_mode": "agent",
  "agent_master": "http://master-ip:8888",
  "agent_api_key": "your-secret-key-1",
  "agent_interval": 60
}
```

### API 端点

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `/` | GET | ✅ | Web 监控面板 |
| `/login` | GET/POST | ❌ | 登录页面/认证接口 |
| `/ws` | WebSocket | ✅ | 实时监控数据推送 |
| `/ws/shell` | WebSocket | ✅+Shell | Web Terminal |
| `/health` | GET | ❌ | 健康检查 |
| `/metrics` | GET | ❌ | Prometheus 指标 |
| `/api/history` | GET | ✅ | 历史数据查询 |
| `/api/shell-status` | GET | ✅ | Shell 启用状态 |
| `/api/shell-auth` | POST | ✅ | Shell 认证 |
| `/api/traffic/ports` | GET | ✅ | 活跃端口列表 |
| `/api/traffic/port-stats` | GET | ✅ | 端口历史流量 |
| `/api/traffic/hosts` | GET | ✅ | 活跃主机列表 |
| `/api/traffic/host-stats` | GET | ✅ | 主机历史流量 |
| `/api/traffic/resolve` | GET | ✅ | DNS 反向解析 |
| `/api/traffic/anomalies` | GET | ✅ | 流量异常事件 |
| `/api/agents` | GET | ✅ | Agent 列表（master 模式） |
| `/api/agent/report` | POST | 签名 | Agent 上报（master 模式） |

## 安全特性

- **密码认证**：Web 界面和 Shell 均支持密码保护
- **Constant-Time 密码比较**：使用 `crypto/subtle.ConstantTimeCompare` 进行密码比对，防止时序攻击（Timing Attack）
- **XSS 防护**：前端使用 DOM API（`createElement` + `textContent`）替代 `innerHTML`，杜绝跨站脚本攻击
- **HMAC Token 认证**：基于 HMAC-SHA256 的 Token 机制，Token 含过期时间，重启即失效
- **Rate Limiting**：登录接口实施速率限制（每 IP 每分钟最多 5 次），含自动清理机制防止内存泄漏
- **HttpOnly Cookie**：认证 Token 通过 HttpOnly + SameSite=Strict Cookie 传输
- **WebSocket Origin 校验**：WebSocket 连接验证 Origin 头，防止跨站 WebSocket 劫持
- **Shell 双重认证**：Web Terminal 需同时通过主密码认证和 Shell 专用密码认证
- **Shell 空闲超时**：Terminal 会话 30 分钟无操作自动断开
- **Agent 签名校验**：Agent 上报数据使用 HMAC-SHA256 签名，防止伪造

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端语言 | Go 1.25+ |
| 系统信息采集 | [gopsutil](https://github.com/shirou/gopsutil) |
| 网络抓包 | [gopacket](https://github.com/google/gopacket)（libpcap） |
| 数据库 | SQLite（[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，纯 Go 实现，WAL 模式） |
| WebSocket | [gorilla/websocket](https://github.com/gorilla/websocket) |
| PTY | [creack/pty](https://github.com/creack/pty) |
| 监控指标 | [prometheus/client_golang](https://github.com/prometheus/client_golang) |
| 前端图表 | [Apache ECharts](https://echarts.apache.org/) 5.x |
| 前端终端 | [xterm.js](https://xtermjs.org/) |
| 前端主题 | 暗色/亮色双主题，CSS 变量驱动 |

## 项目结构

```
sysmon-v2/
├── main.go                  # 入口、HTTP 路由、服务启动
├── config.go                # 配置加载、RateLimiter
├── auth.go                  # 认证、Token、Origin 校验
├── logger.go                # 日志配置
├── metrics.go               # Prometheus 指标、Snapshot、Collect
├── monitor/
│   └── monitor.go           # 系统信息采集、历史环形缓冲、Docker
├── storage/
│   ├── sqlite.go            # SQLite 初始化（WAL 模式）
│   ├── system_history.go    # 系统历史持久化
│   ├── traffic_stats.go     # 流量统计数据存储
│   └── downsampling.go      # 流量数据降采样
├── traffic/
│   ├── types.go             # 流量数据结构定义
│   ├── capture.go           # 网络抓包
│   ├── aggregator.go        # 流量聚合
│   ├── detector.go          # 流量异常检测
│   ├── dns_resolver.go      # DNS 反向解析
│   └── anomaly.go           # 异常事件定义
├── terminal/
│   └── shell.go             # Web Terminal（PTY）
├── websocket/
│   └── hub.go               # WebSocket 连接管理
├── agent/
│   ├── server.go            # Agent Master 端
│   └── client.go            # Agent Client 端
├── web/
│   ├── index.html           # 主监控页面
│   ├── login.html           # 登录页面
│   ├── js/
│   │   ├── app.js           # 主前端逻辑
│   │   └── shell.js         # Shell 终端前端
│   ├── css/
│   │   ├── style.css        # 主样式（暗色/亮色主题）
│   │   └── vendor/xterm.css
│   └── js/vendor/           # xterm.js 库文件
├── .github/workflows/
│   └── build.yml            # CI/CD 流水线
└── go.mod
```

## 开发 / 贡献指南

### 开发环境

```bash
# 克隆项目
git clone https://github.com/xiaoxinmm/sysmon.git
cd sysmon && git checkout v2

# 安装依赖
go mod download

# 运行（开发模式）
go run . -config config.json

# 编译检查
CGO_ENABLED=0 go build -o /dev/null .
```

### 贡献流程

1. Fork 本仓库
2. 从 `v2` 分支创建特性分支：`git checkout -b feature/my-feature v2`
3. 提交更改并编写清晰的 commit message
4. 推送到你的 Fork：`git push origin feature/my-feature`
5. 创建 Pull Request 到 `v2` 分支

### 注意事项

- 流量监控功能需要 `CAP_NET_RAW` 权限或 root 权限运行
- 流量监控依赖 libpcap，Linux 上需安装 `libpcap-dev`（编译时），纯 Go 抓包实现也可能需要
- 请勿提交包含密码的测试配置文件

## License

本项目基于 [GNU Affero General Public License v3.0](https://www.gnu.org/licenses/agpl-3.0.html) 开源。

Copyright (C) 2025 Russell Li (xiaoxinmm)
