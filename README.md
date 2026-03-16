# File Server

一个功能完备的 Go Web 文件服务器，集成代码编辑器和双终端，适合远程服务器文件管理。

## 功能特性

- **文件浏览** — 目录树导航、面包屑路径、文件排序
- **文件操作** — 上传（拖拽/点选）、下载、重命名、删除、新建目录
- **代码编辑器** — 基于 CodeMirror，支持 30+ 语言语法高亮（Monokai 暗色主题），Ctrl-S / Cmd-S 快捷保存
- **双终端** — 两个独立的 xterm.js 终端，通过 WebSocket + PTY 实时交互
- **密码认证** — 可选密码保护，会话有效期 7 天，使用 HttpOnly Cookie
- **安全防护** — 路径穿越防护、符号链接校验、常量时间密码比对、XSS 转义
- **响应式布局** — 左侧文件管理 + 右侧双终端，移动端自适应单栏

## 快速开始

### 编译

```bash
go build -o fileserver .
```

### 运行

```bash
# 基本用法：在 8080 端口提供当前目录
./fileserver

# 指定目录和端口
./fileserver -dir /srv/files -port 3000

# 启用密码保护
./fileserver -dir /srv/files -port 8080 -password "your-password"
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-dir` | `.` | 服务根目录 |
| `-port` | `8080` | 监听端口 |
| `-password` | _(空)_ | 访问密码，为空则无需认证 |

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.23, net/http |
| 终端 | [creack/pty](https://github.com/creack/pty) + [gorilla/websocket](https://github.com/gorilla/websocket) |
| 前端编辑器 | [CodeMirror 5](https://codemirror.net/5/) (Monokai 主题) |
| 前端终端 | [xterm.js 5](https://xtermjs.org/) |
| UI | 纯 CSS，暗色极客风配色 |

## API 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 主页面 |
| GET | `/api/files?path=` | 列出目录内容 |
| POST | `/api/upload?path=` | 上传文件（multipart，最大 32 MB） |
| GET | `/api/download?path=` | 下载文件 |
| POST | `/api/delete` | 删除文件/目录 |
| POST | `/api/rename` | 重命名 |
| POST | `/api/mkdir` | 创建目录 |
| GET | `/api/read?path=` | 读取文件内容（最大 10 MB） |
| POST | `/api/save` | 保存文件内容 |
| GET | `/api/terminal` | WebSocket 终端连接 |
| POST | `/api/login` | 登录认证 |
| POST | `/api/logout` | 注销 |
| GET | `/api/auth-check` | 检查认证状态 |

## 项目结构

```
file-server/
├── main.go        # Go 后端：路由、认证、文件操作、终端 WebSocket
├── index.html     # 前端单页：文件浏览器 + 编辑器 + 双终端
├── go.mod
└── go.sum
```

## 安全设计

- **路径穿越防护** — 所有文件路径经 `safePath()` 校验，解析符号链接后确保不逃逸服务根目录
- **认证** — 32 字节随机 Token，`crypto/subtle.ConstantTimeCompare` 比对密码，HttpOnly + SameSite=Lax Cookie
- **会话过期** — 7 天自动失效
- **根目录保护** — 禁止删除、重命名、覆写服务根目录
- **XSS 防护** — 前端使用 `textContent` 转义用户输入
