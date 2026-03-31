# Phosphor 移动端 Vibe Coding 设计

## 概述

为 Phosphor 文件服务器新增独立的移动端页面（`/mobile`），以全屏终端为核心，配合底部快捷键工具栏和语音输入，专为手机上使用 Claude Code 进行 Vibe Coding 优化。

## 使用场景

随时随地通过手机终端与 Claude Code 交互：语音输入指令、快捷键操控、斜杠命令快速触发。终端是核心，文件浏览为辅助。

## 架构

```
浏览器（手机）                         Phosphor 后端
┌──────────────┐                 ┌──────────────────┐
│  /mobile     │                 │                  │
│  mobile.html │── WebSocket ──→│ /api/terminal    │ (复用现有)
│              │                 │                  │
│  录音(webm)  │── POST ──────→│ /api/voice       │ (新增)
│              │                 │   ↓ 转发音频     │
│  ←── 文字 ──│← JSON ────────│   OpenRouter API  │
└──────────────┘                 │   (Gemini 2.5)   │
                                 └──────────────────┘
```

### 新增文件

- `mobile.html` — 移动端独立页面，通过 `go:embed` 嵌入二进制
- `static/js/terminal-shared.js` — 从 index.html 抽取的终端 WebSocket 复用逻辑

### 新增后端接口

- `POST /api/voice` — 语音识别接口

### 路由

- `/mobile` — 移动端入口
- 桌面端 `/` 完全不变

## 页面布局

全屏终端 + 底部固定工具栏，三层结构从上到下：

### 1. 顶部栏

- 左侧：Phosphor 图标 + 当前 Session 名称（双击重命名）+ 连接状态指示灯
- 右侧：＋（新建 Session）、☰（菜单）

### 2. 终端区域

- 全屏 xterm.js 终端，占据顶部栏和底部工具栏之间的所有空间
- 复用现有 WebSocket 终端逻辑（session 管理、重连、输出缓冲）
- 字体：12px JetBrains Mono

### 3. 底部工具栏（固定）

#### 收起状态（默认）

一行按键 + 输入行：

```
[ ESC ] [ ↑ ] [ ↓ ] [ ← ] [ → ] [ ⏎ ] [ ▲ ]
[ 输入命令...                          ] [🎤]
```

#### 展开状态（点击 ▲）

四个分区：

**基础键：**
ESC、Ctrl（粘性）、Enter、↑ ↓ ← →

**Ctrl 组合：**
Ctrl+C、Ctrl+Z、Ctrl+L、Ctrl+D

**Claude Code 斜杠命令：**
/using-superpowers（置顶）、/compact、/clear、/cost、/help、/commit、/pr、/review、/fast

**快捷回复：**
y、n、yes!

底部：`[▼] [输入命令...] [🎤]`

展开/收起状态记忆到 localStorage。

### Ctrl 粘性键

点击 Ctrl 后保持高亮激活状态，再点其他键自动组合发送（如 Ctrl+C），发送后 Ctrl 自动取消。类似 iOS 键盘 Shift 行为。

### 斜杠命令交互

- **无参命令**（点击直接发送+回车）：/clear、/cost、/help、/fast、/compact
- **有参命令**（点击填入输入框，等待补参数）：/commit、/pr、/review、/using-superpowers
- 判断逻辑：前端硬编码一个 `NO_ARGS_COMMANDS` 集合，不在集合内的命令默认为有参

## ☰ 菜单面板

从右侧滑出覆盖终端：

1. **Session 列表** — 切换、新建、删除终端 session
2. **文件浏览** — 简化版只读文件列表，点击可查看文件内容
3. **设置** — OpenRouter API Key 配置、语音 prompt 自定义

点击菜单外区域或左滑关闭。

## 语音输入

### 技术方案

浏览器录音 → 后端代理 → Gemini 2.5 Flash（通过 OpenRouter）→ 返回文字。单一多模态模型直接处理音频，不嵌套 STT + LLM。

### 触发方式

Push-to-talk：按住麦克风按钮录音，松开发送。

### 交互流程

1. 按住 🎤 → 按钮放大 + 脉冲动画 + 显示"正在录音..."
2. 松开 → 音频 POST 到 `/api/voice` → 显示"识别中..."
3. 返回文字 → 填入输入框（可编辑）
4. 确认无误 → 点 ⏎ 或回车键发送到终端

### 斜杠命令 + 语音组合

1. 展开工具栏 → 点击斜杠命令（如 `/using-superpowers`）
2. 输入框显示 `/using-superpowers `（带尾部空格）
3. 按住 🎤 说参数内容
4. 拼接为 `/using-superpowers 帮我写一个登录页面` → 确认发送

## `/api/voice` 接口

```
POST /api/voice
Content-Type: multipart/form-data

参数：
  audio: 音频文件 (webm/opus, MediaRecorder 输出)
  prompt: 可选上下文提示 (如 "这是编程相关的语音命令")

响应：
  200: {"text": "识别出的文字"}
  500: {"error": "错误信息"}
  501: {"error": "未配置 OpenRouter API Key"}
```

### 实现

- 后端接收音频，base64 编码后调 OpenRouter API（模型：`google/gemini-2.5-flash`）
- Prompt：要求将语音转为文字，保持原意，修正口误
- API Key 通过启动参数 `-openrouter-key` 或环境变量 `OPENROUTER_API_KEY` 传入
- 音频大小限制：10MB（约 5 分钟）
- 无 API Key 时接口返回 501，前端语音按钮灰显

## 视觉风格

沿用现有 Phosphor 暗黑主题：
- 背景：#0a0e14
- 面板：#161b22
- 主色调：#00ff41（绿色）
- 强调色：#58a6ff（蓝色）、#f0883e（橙色）
- 字体：JetBrains Mono（终端）、Inter（UI）
- 工具栏按键：#21262d 背景 + #30363d 边框
- 麦克风按钮：#da3633（红色）

## 不做的事

- 手机端不做代码编辑器（手机编辑体验差，用 Claude Code 语音指令改代码更高效）
- 不做桌面端自动跳转到移动端（用户手动访问 `/mobile`）
- 不做离线语音识别（依赖 OpenRouter API）
