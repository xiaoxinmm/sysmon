# Sysmon v2

A lightweight system and network monitoring tool with real-time web interface.

[中文文档](#中文文档) | [English](#english-documentation)

---

## English Documentation

### What is this?

Sysmon v2 is a monitoring tool I built for keeping an eye on Linux servers. It tracks system resources (CPU, memory, processes) and network traffic in real-time through a clean web interface. No heavy dependencies, just a single binary.

### Features

- **System Monitoring**: CPU, memory, disk, processes, Docker containers
- **Network Traffic**: Port-based traffic analysis, host tracking, anomaly detection
- **Real-time Updates**: WebSocket-based live data streaming
- **Historical Data**: SQLite storage with automatic downsampling
- **Web Terminal**: Optional shell access through the browser
- **Agent Mode**: Deploy agents across multiple servers, collect data centrally
- **Prometheus**: Built-in `/metrics` endpoint for Prometheus scraping
- **Lightweight**: Single binary, ~24MB, minimal resource usage

### Quick Start

#### One-line Install

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoxinmm/sysmon/v2/install.sh | bash
```

Or download and run manually:

```bash
wget https://raw.githubusercontent.com/xiaoxinmm/sysmon/v2/install.sh
chmod +x install.sh
sudo ./install.sh
```

#### Manual Installation

1. Download the binary from [Releases](https://github.com/xiaoxinmm/sysmon/releases)
2. Create a config file `sysmon.json`:

```json
{
  "port": 8888,
  "password": "your-password-here",
  "enable_traffic": true,
  "traffic_iface": "eth0",
  "log_level": "info"
}
```

3. Run it:

```bash
chmod +x sysmon
./sysmon -config sysmon.json
```

4. Open `http://your-server:8888` in browser

### Configuration

All options can be set via JSON config file or environment variables:

| Config Key | Env Variable | Default | Description |
|------------|--------------|---------|-------------|
| `port` | `PORT` | 8888 | HTTP server port |
| `password` | `SYSMON_PASSWORD` | "" | Web UI password (empty = no auth) |
| `refreshInterval` | `SYSMON_REFRESH` | 1500 | Data refresh interval (ms) |
| `maxProcesses` | `SYSMON_MAX_PROCS` | 50 | Max processes to display |
| `historyDuration` | `SYSMON_HISTORY` | 3600 | In-memory history size (seconds) |
| `history_db` | - | ./sysmon.db | SQLite database path |
| `history_retention_days` | - | 7 | How long to keep historical data |
| `enable_traffic` | - | false | Enable network traffic monitoring |
| `traffic_iface` | - | auto | Network interface to monitor |
| `traffic_interval` | - | 60 | Traffic snapshot interval (seconds) |
| `enableShell` | - | false | Enable web terminal |
| `shell_password` | - | "" | Separate password for shell access |
| `logLevel` | `SYSMON_LOG_LEVEL` | info | Log level (debug/info/warn/error) |

### Agent Mode

Run sysmon in distributed mode:

**Master server** (`master.json`):
```json
{
  "port": 8888,
  "password": "admin123",
  "agent_mode": "master",
  "agent_keys": ["secret-key-1", "secret-key-2"]
}
```

**Agent servers** (`agent.json`):
```json
{
  "agent_mode": "agent",
  "agent_master": "http://master-server:8888",
  "agent_api_key": "secret-key-1",
  "agent_interval": 60
}
```

Agents will report to master every 60 seconds. View all agents at `http://master:8888/api/agents`

### Building from Source

Requirements: Go 1.25+

```bash
git clone https://github.com/xiaoxinmm/sysmon.git
cd sysmon
git checkout v2
go build -o sysmon .
```

### API Endpoints

- `GET /health` - Health check
- `GET /metrics` - Prometheus metrics
- `POST /login` - Authenticate (returns token)
- `GET /api/history` - Historical system data
- `GET /api/traffic/ports` - Active port statistics
- `GET /api/traffic/hosts` - Active host statistics
- `GET /api/traffic/anomalies` - Detected traffic anomalies
- `GET /ws` - WebSocket for real-time updates

### Notes

- Traffic monitoring requires `CAP_NET_RAW` capability or root privileges
- Web shell is disabled by default for security reasons
- Use strong passwords in production environments
- SQLite database grows over time, adjust `history_retention_days` as needed

### License

MIT

---

## 中文文档

### 这是什么？

Sysmon v2 是我写的一个轻量级服务器监控工具，用来实时查看 Linux 服务器的系统资源和网络流量。不需要复杂的依赖，就一个二进制文件。

### 功能特性

- **系统监控**：CPU、内存、磁盘、进程、Docker 容器
- **网络流量**：按端口统计流量、主机追踪、异常检测
- **实时更新**：基于 WebSocket 的实时数据推送
- **历史数据**：SQLite 存储，自动降采样
- **Web 终端**：可选的浏览器 Shell 访问
- **Agent 模式**：多服务器部署，集中收集数据
- **Prometheus**：内置 `/metrics` 接口
- **轻量级**：单个二进制文件，约 24MB，资源占用低

### 快速开始

#### 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/xiaoxinmm/sysmon/v2/install.sh | bash
```

或者手动下载安装：

```bash
wget https://raw.githubusercontent.com/xiaoxinmm/sysmon/v2/install.sh
chmod +x install.sh
sudo ./install.sh
```

#### 手动安装

1. 从 [Releases](https://github.com/xiaoxinmm/sysmon/releases) 下载二进制文件
2. 创建配置文件 `sysmon.json`：

```json
{
  "port": 8888,
  "password": "你的密码",
  "enable_traffic": true,
  "traffic_iface": "eth0",
  "log_level": "info"
}
```

3. 运行：

```bash
chmod +x sysmon
./sysmon -config sysmon.json
```

4. 浏览器打开 `http://服务器IP:8888`

### 配置说明

所有配置项都可以通过 JSON 文件或环境变量设置：

| 配置项 | 环境变量 | 默认值 | 说明 |
|--------|----------|--------|------|
| `port` | `PORT` | 8888 | HTTP 服务端口 |
| `password` | `SYSMON_PASSWORD` | "" | Web 界面密码（空=不需要认证） |
| `refreshInterval` | `SYSMON_REFRESH` | 1500 | 数据刷新间隔（毫秒） |
| `maxProcesses` | `SYSMON_MAX_PROCS` | 50 | 显示的最大进程数 |
| `historyDuration` | `SYSMON_HISTORY` | 3600 | 内存中保留的历史数据时长（秒） |
| `history_db` | - | ./sysmon.db | SQLite 数据库路径 |
| `history_retention_days` | - | 7 | 历史数据保留天数 |
| `enable_traffic` | - | false | 启用网络流量监控 |
| `traffic_iface` | - | auto | 监控的网卡接口 |
| `traffic_interval` | - | 60 | 流量快照间隔（秒） |
| `enableShell` | - | false | 启用 Web 终端 |
| `shell_password` | - | "" | Shell 访问的独立密码 |
| `logLevel` | `SYSMON_LOG_LEVEL` | info | 日志级别（debug/info/warn/error） |

### Agent 模式

分布式部署模式：

**主服务器** (`master.json`)：
```json
{
  "port": 8888,
  "password": "admin123",
  "agent_mode": "master",
  "agent_keys": ["secret-key-1", "secret-key-2"]
}
```

**Agent 服务器** (`agent.json`)：
```json
{
  "agent_mode": "agent",
  "agent_master": "http://主服务器:8888",
  "agent_api_key": "secret-key-1",
  "agent_interval": 60
}
```

Agent 每 60 秒向主服务器上报数据。访问 `http://主服务器:8888/api/agents` 查看所有 agent。

### 从源码编译

需要 Go 1.25+

```bash
git clone https://github.com/xiaoxinmm/sysmon.git
cd sysmon
git checkout v2
go build -o sysmon .
```

### API 接口

- `GET /health` - 健康检查
- `GET /metrics` - Prometheus 指标
- `POST /login` - 登录认证（返回 token）
- `GET /api/history` - 历史系统数据
- `GET /api/traffic/ports` - 活跃端口统计
- `GET /api/traffic/hosts` - 活跃主机统计
- `GET /api/traffic/anomalies` - 检测到的流量异常
- `GET /ws` - WebSocket 实时数据

### 注意事项

- 流量监控需要 `CAP_NET_RAW` 权限或 root 权限
- Web 终端默认关闭，出于安全考虑
- 生产环境请使用强密码
- SQLite 数据库会随时间增长，根据需要调整 `history_retention_days`

### 开源协议

MIT
