## Why

Visual Tmux Client 目前只能以「启动服务 → 复制令牌 → 打开浏览器 → 粘贴令牌」的方式使用。对在本机管理 tmux 的用户而言，这比打开一个终端应用多出好几步，而浏览器标签页也缺少桌面应用应有的交互：菜单栏与快捷键、右键菜单、保存到指定位置、系统剪贴板、拖放文件、记住窗口大小、退出前提醒未保存的修改。更关键的是，若直接把页面放进 macOS 的 WKWebView，`window.confirm` 与 `window.prompt` 会被一律回答为「否」，删除、覆盖、重命名都会无声失败；`<a download>` 在任何原生 web view 里也不会生效。

## What Changes

- 新增独立 Go 模块 `hub/desktop`（Wails v2），在进程内以回环地址启动与命令行服务端完全相同的 hub，并用原生窗口打开 hub 自身提供的页面，因此浏览器端的全部功能在桌面端原样可用。
- 每次启动生成新的令牌，经 URL 片段交给页面；页面不再显示登录界面与「Disconnect」按钮。hub 只监听回环地址，优先使用固定端口以保留页面的本地偏好，端口被占用时回退到任意空闲端口。
- 页面与窗口之间新增经认证的桥接路由 `/api/desktop/*`（窗口标题、全屏、剪贴板读写、原生保存、在系统浏览器打开链接、未保存状态），由 hub 的 `app.Options.Wrap` 挂载；窗口向页面发送菜单命令则通过在页面中执行 `window.__vtcDesktop.dispatch`。
- 原生菜单栏与快捷键：macOS 全部使用 ⌘，Linux 在 Ctrl 基础上加 Shift，确保不抢占终端按键。快捷键只在一处定义，菜单与页面上的提示共用同一份表。
- 页面在桌面模式下：确认/输入改用应用内对话框；下载走系统保存对话框；会话卡片与终端区域提供右键菜单；侧栏宽度可拖动并持久化；标题栏显示当前会话；拖放到窗口的文件以带引号的路径输入终端。浏览器端行为保持不变。
- 窗口大小与最大化状态在启动之间保留；单实例运行；存在未保存的文件修改时退出前询问；退出只停止 hub，不结束任何 tmux 会话。
- 从程序坞或启动器打开时补全 `PATH` 中的常见包管理器目录，并在未设置时提供 UTF-8 locale，使 tmux 能被找到且正确显示非 ASCII 字符。
- 代码编辑器改用与页面一致的深色主题（浏览器端同样受益）。
- 构建：`make desktop` / `desktop-dev` / `desktop-test` / `desktop-lint` / `install-wails`；CI 新增 Ubuntu 与 macOS 的桌面构建与测试；发布流程新增 Linux（amd64）与 macOS（universal）桌面包。

## Capabilities

### New Capabilities

- `desktop-app`：原生桌面应用——内嵌 hub 的启动与认证、页面与窗口之间的桥接、菜单与快捷键、桌面交互（对话框、保存、剪贴板、右键菜单、拖放）、窗口生命周期与环境修复。

### Modified Capabilities

无行为变化的修改：`web-session-manager` 的浏览器端行为保持原样，桌面相关分支仅在页面确认运行于桌面壳时生效。

## Impact

- `hub/internal/app`：`Options` 新增 `Wrap`，允许嵌入方在路由外层挂载自己的路由；不设置时行为不变。
- `hub/web`：新增 `desktop.ts`、`dialogs.ts`、`AppDialog.vue`、`ContextMenu.vue`；`App.vue`、`SessionList.vue`、`FileManagerOverlay.vue`、`terminal.ts`、`bootstrap.ts`、`main.ts` 增加桌面分支；文件管理器中的 `confirm`/`prompt` 改为 Promise 形式（浏览器端仍使用原生对话框）。
- `hub/desktop`：新模块，依赖 Wails v2.16.0；服务端模块 `hub` 的依赖、构建与 `make test` 不受影响，不需要 C 工具链。
- 不改变 REST/WebSocket 协议、会话语义或配置文件格式；桌面端忽略服务端的监听地址与令牌配置。
