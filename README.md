# Phosphor

一个功能完备的 Go Web 文件服务器，集成代码编辑器和双终端，适合远程服务器文件管理。单二进制部署，所有前端资源内嵌，开箱即用。

## 功能特性

### 文件管理
- **文件浏览** — 目录树导航、面包屑路径、按名称/大小/修改时间排序
- **文件上传** — 支持拖拽上传、点选上传，最大 2 GB 单文件，3 路并发
- **文件夹上传** — 整个文件夹上传并保留目录结构层级
- **文件操作** — 下载、重命名、删除（带确认）、新建目录

### 代码编辑器
- **基于 CodeMirror 5** — Monokai 暗色主题
- **30+ 语言语法高亮** — JavaScript、Python、Go、Markdown、Shell、YAML、SQL 等
- **多标签编辑** — 同时打开多个文件，标签页切换
- **快捷键保存** — Ctrl-S / Cmd-S
- **Markdown 预览** — .md 文件支持实时渲染预览（基于 marked.js）
- 文件编辑上限 10 MB

### 双终端
- **基于 xterm.js + WebSocket + PTY** — 真实终端，非模拟（Linux/macOS: Bash + PTY, Windows: PowerShell/cmd + ConPTY）
- **多会话管理** — 独立创建、关闭、重命名终端会话
- **会话持久化** — UUID 标识会话，断线后可自动重连恢复
- **输出缓冲** — 每会话 100 KB 缓冲，重连时回放历史输出
- **终端缩放** — 随面板/窗口大小自动调整
- **全屏终端** — 独立 `/terminal` 页面，全屏终端体验

### 安全防护
- **可选密码认证** — 32 字节随机 Token，`crypto/subtle.ConstantTimeCompare` 常量时间比对
- **会话管理** — HttpOnly + SameSite=Lax Cookie，7 天自动过期
- **路径穿越防护** — `safePath()` 解析符号链接，确保不逃逸服务根目录
- **根目录保护** — 禁止删除、重命名、覆写服务根目录
- **XSS 防护** — 前端使用 `textContent` 转义用户输入

### 服务管理
- **热重启** — 通过 Web UI 触发重启服务（部署模式直接重启，开发模式先编译再重启）
- **优雅关停** — 通过 Web UI 安全关闭服务器

### 响应式 UI
- **布局** — 左侧文件管理 + 右侧双终端，50/50 分屏
- **暗色极客风** — 深色背景 (#0a0e14) + 亮绿色点缀 (#00ff41)
- **移动端适配** — 可折叠面板，自适应单栏布局
- **等宽字体** — Fira Code / JetBrains Mono / Cascadia Code

## 快速开始

### 编译

```bash
# 编译当前平台
go build -ldflags "-s -w" -o phosphor .

# 编译所有平台（需要 make）
make
```

编译产物位于 `dist/` 目录：

| 文件名 | 平台 |
|--------|------|
| `phosphor-linux-amd64` | Linux x86_64 |
| `phosphor-linux-arm64` | Linux ARM64 |
| `phosphor-darwin-amd64` | macOS Intel |
| `phosphor-darwin-arm64` | macOS Apple Silicon |
| `phosphor-windows-amd64.exe` | Windows x86_64 |

### 部署

编译后只需一个二进制文件即可运行，无需任何外部依赖：

```bash
# 复制到目标机器即可运行
scp dist/phosphor-linux-amd64 server:/usr/local/bin/phosphor
ssh server 'phosphor -dir /srv/files -port 8080'
```

### 运行

```bash
# Linux / macOS
./phosphor
./phosphor -dir /srv/files -port 3000
./phosphor -dir /srv/files -port 8080 -password "your-password"

# 开发模式（重启时自动重新编译，需要 Go 编译器）
./phosphor -dev -dir . -port 8080
```

```powershell
# Windows
.\phosphor.exe
.\phosphor.exe -dir C:\Files -port 3000
.\phosphor.exe -dir C:\Files -port 8080 -password "your-password"
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-dir` | `.` | 服务根目录 |
| `-port` | `8080` | 监听端口 |
| `-password` | _(空)_ | 访问密码，为空则无需认证 |
| `-dev` | `false` | 开发模式：重启时从源码重新编译 |

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.23, net/http |
| 终端 PTY (Unix) | [creack/pty](https://github.com/creack/pty) v1.1.24 |
| 终端 ConPTY (Windows) | [UserExistsError/conpty](https://github.com/UserExistsError/conpty) v0.1.4 |
| WebSocket | [gorilla/websocket](https://github.com/gorilla/websocket) v1.5.3 |
| 会话 ID | [google/uuid](https://github.com/google/uuid) v1.6.0 |
| 前端编辑器 | [CodeMirror 5.65](https://codemirror.net/5/) (Monokai, 内嵌) |
| 前端终端 | [xterm.js 5.3](https://xtermjs.org/) + xterm-addon-fit (内嵌) |
| Markdown | [marked 12.0](https://marked.js.org/) (内嵌) |
| UI | 纯 CSS，暗色极客风配色 |

## API 接口

### 页面

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 主页面（单页应用） |
| GET | `/terminal` | 全屏终端页面 |

### 文件操作

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/files?path=` | 列出目录内容 |
| GET | `/api/read?path=` | 读取文件内容（最大 10 MB） |
| GET | `/api/download?path=` | 下载文件 |
| POST | `/api/upload?path=` | 上传文件（multipart，最大 128 MB） |
| POST | `/api/upload-folder?path=` | 上传文件夹（保留结构，最大 256 MB） |
| POST | `/api/save` | 保存文件内容 |
| POST | `/api/delete` | 删除文件/目录 |
| POST | `/api/rename` | 重命名文件/目录 |
| POST | `/api/mkdir` | 创建目录 |

### 终端

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/terminal` | WebSocket 终端连接 |
| GET | `/api/terminal/sessions` | 列出活跃终端会话 |
| DELETE | `/api/terminal/sessions?id=` | 关闭指定终端会话 |
| POST | `/api/terminal/sessions/rename` | 重命名终端会话 |

### 认证

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/login` | 登录认证 |
| POST | `/api/logout` | 注销 |
| GET | `/api/auth-check` | 检查认证状态 |

### 服务管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/restart` | 重启服务（部署模式直接重启，开发模式先编译） |
| POST | `/api/shutdown` | 优雅关停服务器 |

## 项目结构

```
Phosphor/
├── main.go                # Go 后端：路由、认证、文件操作、终端 WebSocket、服务管理
├── terminal_unix.go       # Unix 终端实现（PTY + Bash）
├── terminal_windows.go    # Windows 终端实现（ConPTY + PowerShell/cmd）
├── index.html             # 前端单页应用：文件浏览器 + 编辑器 + 双终端
├── static/                # 内嵌前端静态资源（CodeMirror、xterm.js、marked.js）
├── Makefile               # 跨平台编译脚本
├── go.mod                 # Go 模块定义
├── go.sum                 # 依赖校验
├── Phosphor_P.png         # Logo 图标
└── Phosphor_logo          # Logo 横幅
```

## 跨平台支持

| 平台 | 终端后端 | Shell | 最低版本 |
|------|----------|-------|----------|
| Linux | creack/pty | /bin/bash | — |
| macOS | creack/pty | /bin/bash | — |
| Windows | ConPTY | pwsh / powershell / cmd | Windows 10 1809+ |

> **Windows 注意事项:** ConPTY 需要 Windows 10 版本 1809 (2018年10月更新) 或更高版本。终端会自动选择可用的最佳 shell（优先级：pwsh.exe > powershell.exe > cmd.exe）。

## 安全设计

| 威胁 | 防御措施 |
|------|----------|
| 路径穿越 | `safePath()` 解析符号链接，校验是否在服务根目录内 |
| 暴力破解 | 32 字节随机 Token + 常量时间比对 |
| 会话劫持 | HttpOnly + SameSite=Lax Cookie，7 天过期 |
| XSS | 前端 `textContent` 转义，HTML 实体编码 |
| 资源耗尽 | 文件读取 10 MB 限制，上传 128/256 MB 限制 |
| 根目录篡改 | 禁止删除、重命名、覆写服务根目录 |

## WebSocket 协议

终端通过 WebSocket 通信，协议格式：

- **会话绑定** — 客户端发送 `session:<UUID>` 绑定已有会话
- **窗口调整** — 客户端发送 `resize:{"cols":N,"rows":N}` 调整终端大小
- **数据传输** — 二进制帧传输终端输出，文本帧传输控制消息

## 许可证

MIT
