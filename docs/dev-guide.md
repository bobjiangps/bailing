# 开发指南：环境、修改、调试、编译打包（macOS / Windows）

> 约定：**安装和构建命令都由你本人执行**，本文只给出命令和说明。
> 部署到服务器见 [usage.md](usage.md)；架构见 [architecture.md](architecture.md)。

## 0. 先理解几个概念

| 概念 | 类比（Python） | 在本项目里 |
|---|---|---|
| **Go module**（`go.mod` + `go.sum`） | `pyproject.toml` / `requirements.txt` + 锁文件 | 根目录的 `go.mod`，模块名 `bailing`；`go mod tidy` 会根据 import 自动补全依赖并生成 `go.sum` |
| `go run ./cmd/bailing` | `python main.py` | 编译到临时目录并立即运行，开发时用 |
| `go build` | PyInstaller 打包 | 生成一个独立的可执行文件，不需要任何运行环境 |
| `GOOS` / `GOARCH` | 无 | 指定目标系统和 CPU，在 Mac 上就能编出 Linux / Windows 版本（交叉编译） |
| `//go:embed` | 把资源打包进程序 | 前端构建结果 `internal/webui/dist/` 被编译进可执行文件 |
| **npm** + `package.json` | pip + requirements | 前端依赖，装在 `web/node_modules/`（只在开发机上需要） |
| **Vite** | 开发服务器 + 打包器 | `npm run dev` 启动 5173 端口，改代码浏览器立即刷新（HMR）；`npm run build` 生成静态文件 |
| **SvelteKit** | Svelte 的应用框架（类似 Vue 的 Nuxt） | 只用它的路由，`web/src/routes/` 下一个目录就是一个页面 |
| `$state` / `$derived` / `$effect` | 响应式变量 | Svelte 5 的「符文」：`$state` 变了，用到它的界面自动更新 |

**运行时的两种形态：**

```
开发模式（两个进程）                                 发布模式（一个文件）
浏览器 → Vite :5173 ──/api,/media──→ Go :8080          浏览器 → nginx → bailing :8080
          └─ 页面、JS 实时编译                                       └─ 前端已嵌入可执行文件
```

## 1. 安装开发环境

需要的工具：**Go ≥ 1.23**、**Node.js ≥ 20（建议 22 LTS）**、**Git**、**libvips**（生成缩略图、处理 HEIC；开发机、服务器、桌面端都需要）。

### 1.1 macOS

```bash
xcode-select --install             # 命令行工具（自带 git 和 make）
brew install go node vips
go version                         # go1.23 以上
node -v && npm -v
vips --version
vips -l | grep -i heif             # 能看到 heifload 就说明支持 HEIC
```

### 1.2 Windows 10 / 11（PowerShell）

```powershell
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
winget install --id Git.Git -e
# 重新打开 PowerShell 让 PATH 生效
go version; node -v; git --version
```

**libvips（Windows 版）：**

1. 打开 <https://github.com/libvips/build-win64-mxe/releases>，下载最新的 `vips-dev-w64-all-x.y.z.zip`（`all` 版本包含 HEIC 支持；`web` 版本更小，但不一定包含 HEIC）。
2. 解压到 `C:\tools\vips`，确认存在 `C:\tools\vips\bin\vips.exe`。
3. 二选一：
   - 加入 PATH：「设置 → 系统 → 关于 → 高级系统设置 → 环境变量 → Path → 新建 `C:\tools\vips\bin`」；
   - 或者不改 PATH，在 `config.dev.yaml` 里写 `imaging.vips_bin_dir: "C:/tools/vips/bin"`（**用正斜杠**）。
4. 验证：`C:\tools\vips\bin\vips.exe --version`

**make（可选）：** Windows 默认没有 make，项目提供了等价的 PowerShell 脚本 `scripts\build.ps1`，用法和 make 目标一一对应（见第 6 节）。第一次运行如果提示脚本被禁止：

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
```

**Windows 注意事项：**

- 第一次运行 bailing 时 Windows 防火墙可能弹窗，开发时只监听 `127.0.0.1`，选「取消」也不影响本机访问。
- 想让手机访问开发机（见 3.4），需要允许 Node.js 通过防火墙的「专用网络」。
- 配置文件里的路径一律用正斜杠：`"D:/Pictures/albums"`。

### 1.3 国内网络加速（两台机器都可以设置）

```bash
go env -w GOPROXY=https://goproxy.cn,direct          # Go 模块代理
npm config set registry https://registry.npmmirror.com # npm 镜像
```

### 1.4 编辑器

| 编辑器 | 插件 / 说明 |
|---|---|
| **VS Code**（推荐，免费） | 安装推荐插件：Go（`golang.go`）、Svelte for VS Code。仓库里已有 `.vscode/launch.json` 调试配置 |
| **GoLand / IntelliJ IDEA Ultimate** | JetBrains 系；Svelte 需要安装 Svelte 插件。PyCharm 不支持 Go |

## 2. 第一次拉起项目

```bash
cd web_bailing

# 1) 下载依赖（会生成 go.sum 和 web/node_modules）
go mod tidy
cd web && npm install && cd ..
#   Windows: .\scripts\build.ps1 deps

# 2) 开发配置
cp config.dev.example.yaml config.dev.yaml           # Windows: copy config.dev.example.yaml config.dev.yaml

# 3) 测试数据（data/ 已在 .gitignore 里）
mkdir -p data/photos/测试相册 data/music             # Windows: mkdir data\photos\测试相册, data\music
#   往 data/photos/测试相册 放几张 jpg/heic，往 data/music 放几首 mp3

# 4) 创建自己的账号（会提示输入密码）
go run ./cmd/bailing user add me --admin -config config.dev.yaml
```

> 不想每次登录：`config.dev.yaml` 里把 `auth.mode` 改成 `"none"`（只允许在 127.0.0.1 上使用）。

## 3. 日常开发

### 3.1 启动（两个终端）

| | macOS | Windows（PowerShell） |
|---|---|---|
| 终端 1：Go 后端 :8080 | `make dev-api` | `.\scripts\build.ps1 dev-api` |
| 终端 2：Vite 前端 :5173 | `make dev-web` | `.\scripts\build.ps1 dev-web` |

浏览器打开 <http://localhost:5173>。

- **改前端**（`web/src/**`）：保存即生效，状态尽量保留（HMR）。
- **改 Go**（`cmd/`、`internal/`）：在终端 1 按 `Ctrl+C`，再执行一次 `make dev-api`。
  想要自动重启，可以自行安装 [air](https://github.com/air-verse/air)：`go install github.com/air-verse/air@latest`，然后用 `air -- serve -config config.dev.yaml`。
- **改配置**：重启后端。
- **从零开始**：删除 `data/runtime/`（数据库和缓存），重新创建账号。

### 3.2 代码地图：想改什么，去哪里

| 想改的东西 | 文件 |
|---|---|
| 配色、字体、全局样式 | `web/src/app.css`（5 套配色 × 深浅色，全是 CSS 变量） |
| 中英文文案 | `web/src/lib/i18n/zh.ts`、`en.ts`（两个文件的键必须一致） |
| 主页 / 登录 / 书架 / 音乐 / 设置页 | `web/src/routes/+page.svelte`、`login/`、`albums/`、`music/`、`settings/` |
| 相簿外观、推近和翻书动画 | `web/src/lib/gallery/BookCover.svelte`、`Book.svelte` |
| 照片查看器（缩放、旋转、EXIF） | `web/src/lib/gallery/Viewer.svelte` |
| 播放逻辑（队列、模式、统计、流量轮询） | `web/src/lib/player/player.svelte.ts` |
| 播放条、黑胶、歌词、流量标记 | `web/src/lib/player/*.svelte` |
| 接口路由 | `internal/httpapi/server.go` |
| 照片扫描、about.txt、缩略图 | `internal/library/photo/` + `internal/imaging/` |
| 音乐扫描、标签、封面 | `internal/library/music/` |
| 歌词（本地 → 内嵌 → LRCLIB） | `internal/lyrics/` |
| 流量统计 | `internal/traffic/meter.go` + `internal/httpapi/middleware.go` |
| 数据库表结构 | `internal/store/migrations/*.sql`（新增改动请加新文件 `002_xxx.sql`，不要改旧文件） |
| 配置项 | `internal/config/config.go` + `config.example.yaml` |

### 3.3 新增一个接口的完整例子

1. `internal/httpapi/server.go` 注册路由：`mux.HandleFunc("GET /api/hello", requireUser(s.hello))`
2. 写处理函数（任意 `handlers_*.go`）：

   ```go
   func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
       u := auth.FromContext(r.Context())
       writeJSON(w, http.StatusOK, map[string]any{"hi": u.Name})
   }
   ```

3. 前端调用：`const r = await api<{ hi: string }>('/api/hello');`
4. 重启后端，前端保存即生效。

### 3.4 用手机测试触摸手势

Vite 已配置 `host: true`，同一 Wi-Fi 下手机可以访问开发机：

1. 查开发机 IP：macOS `ipconfig getifaddr en0`；Windows `ipconfig`（看 IPv4 地址）
2. 手机浏览器打开 `http://192.168.x.x:5173`
3. 如果登录返回 403（来源校验没通过），在 `config.dev.yaml` 的 `allowed_origins` 里加上 `"http://192.168.x.x:5173"` 并重启后端

## 4. 调试

### 4.1 Go 后端

- **日志**：`config.dev.yaml` 里 `log_level: "debug"`，每个请求、扫描、vips 调用都会打印。
- **断点调试**：
  - VS Code：左侧「运行和调试」→ 选择「Go: bailing serve (config.dev.yaml)」→ F5。首次会提示安装调试器 Delve，同意即可（或手动 `go install github.com/go-delve/delve/cmd/dlv@latest`）。
  - GoLand：Run → Edit Configurations → Go Build，Package path `bailing/cmd/bailing`，Program arguments `serve -config config.dev.yaml`。
  - 命令行：`dlv debug ./cmd/bailing -- serve -config config.dev.yaml`
- **直接调接口**（绕过前端）：

  ```bash
  curl -s localhost:8080/api/health
  curl -s -c /tmp/ck -H 'Content-Type: application/json' \
       -H 'Origin: http://localhost:5173' \
       -d '{"name":"me","password":"***"}' localhost:8080/api/auth/login
  curl -s -b /tmp/ck localhost:8080/api/albums | head
  curl -s -b /tmp/ck localhost:8080/api/traffic/summary
  ```

  Windows PowerShell 里用 `curl.exe`（不是 `curl` 别名），引号写法：`-d '{\"name\":\"me\",\"password\":\"***\"}'`。

- **看数据库**：用 [DB Browser for SQLite](https://sqlitebrowser.org)（Mac / Windows 都有）打开 `data/runtime/bailing.db`；或命令行 `sqlite3 data/runtime/bailing.db ".tables"`。查看时最好先停后端，避免锁冲突。

### 4.2 Svelte 前端

- **浏览器开发者工具**（F12 / ⌥⌘I）：Network 看接口和媒体请求（`/media/track` 应该是 206 分段响应）；Console 看报错；Application → Local Storage 看 `bl.prefs`、`bl.player`。
- **Svelte DevTools** 浏览器扩展：查看组件树和状态。
- **打印响应式状态**：在组件里写 `$inspect(player.queue);`，状态变化时会自动在控制台打印（只在开发模式生效）。
- **断点**：直接在浏览器 Sources 面板里给 `.svelte` 文件下断点（Vite 提供了 source map）；或用 VS Code 的「Browser: Chrome → Vite」配置。
- **类型检查**：`cd web && npm run check`（相当于 Python 的 mypy）。
- **手机布局**：开发者工具的设备模拟（⇧⌘M / Ctrl+Shift+M），横竖屏切换可以检查相簿单页 / 双页。

### 4.3 检查与测试

```bash
make lint      # go vet + svelte-check          Windows: .\scripts\build.ps1 lint
make test      # go test + svelte-check         Windows: .\scripts\build.ps1 test
make fmt       # gofmt 格式化 Go 代码            Windows: .\scripts\build.ps1 fmt
```

P2 / P3 新增的单元测试（不需要启动服务）：

```bash
go test ./internal/proxy/          # 转发链接签名 / 过期 / HLS 播放列表改写
go test ./internal/netguard/       # 内网地址拦截、ICY 响应兼容
go test ./internal/sources/...     # m3u/pls 解析；落雪：AES/RSA/zlib、音质回退、
                                   # 以及用一个假脚本 + 本地 HTTP 服务跑通 lx.request → musicUrl 全流程
```

### 4.4 第二、三阶段新增的依赖（2026-10-15）

| 依赖 | 用途 | 安装 |
|---|---|---|
| `github.com/dop251/goja`（Go，MIT） | 在服务器上运行落雪自定义源脚本 | `go get github.com/dop251/goja@latest && go mod tidy` |
| `hls.js`（npm，Apache-2.0） | 在非 Safari 浏览器上播放 m3u8；只在用到时才加载 | `cd web && npm install` |

两条命令在 macOS 和 Windows 上相同。安装完成后，`make lint` 应该全部通过。

### 4.5 调试在线音源

- **电台、链接**：打开浏览器开发者工具的 Network 面板。直连时请求直接发往电台的地址；转发时请求是 `/proxy/stream?...`。
- **落雪脚本**：
  - 在 `config.dev.yaml` 中设置 `log_level: debug`，脚本里 `console.log` 的输出会显示在 `make dev-api` 的终端里；
  - 改完脚本保存，最多 30 秒后自动重新加载，也可以在设置页点「重新加载」；
  - 加载失败的原因会显示在设置页对应脚本的红色文字里。
- **在本机局域网内测试转发**：如果要访问内网的音频地址，在开发配置里设置 `sources.proxy.allow_private: true`。**生产环境不要开启**。

## 5. 编译打包

### 5.1 构建流程

```
npm run build（web/）      →  internal/webui/dist/      （SvelteKit 静态文件）
go build（CGO_ENABLED=0）  →  dist/bailing-<系统>-<架构>  （前端通过 go:embed 打包进来）
```

**一定要先构建前端再编译 Go**，否则可执行文件里没有页面（访问时会显示「front end has not been built」提示页）。下面的 make 目标和脚本都会自动按顺序执行。

### 5.2 目标一览

| 产物 | macOS / Linux | Windows（PowerShell） |
|---|---|---|
| 本机版本 | `make build` → `dist/bailing` | `.\scripts\build.ps1 windows` |
| **Ubuntu ECS（x86_64）** | `make build-linux` | `.\scripts\build.ps1 linux` |
| ARM 服务器 | `make build-linux-arm64` | `.\scripts\build.ps1 linux-arm64` |
| Windows exe | `make build-windows` | `.\scripts\build.ps1 windows` |
| macOS（arm64 + amd64） | `make build-mac` | `.\scripts\build.ps1 mac` |
| 子路径部署（`/bailing`） | `BASE_PATH=/bailing make build-linux` | `.\scripts\build.ps1 linux -BasePath /bailing` |

`make build-linux` 实际执行的是：

```bash
cd web && npm run build && cd ..
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags --always)" \
  -o dist/bailing-linux-amd64 ./cmd/bailing
```

| 参数 | 含义 |
|---|---|
| `CGO_ENABLED=0` | 不链接任何 C 库（SQLite 用的是纯 Go 实现），产物不依赖目标系统的 glibc 版本，在 Ubuntu 20.04 上也能直接运行 |
| `GOOS` / `GOARCH` | 目标系统和 CPU。Mac 和 Windows 都能编出另外两个平台的版本 |
| `-trimpath` | 去掉本机路径信息 |
| `-ldflags "-s -w"` | 去掉调试符号，体积更小（约 20～30MB） |
| `-X main.version=...` | 把版本号写进程序，`bailing version` 和设置页会显示 |

### 5.3 本机试运行发布版本

```bash
make build
./dist/bailing serve -config config.dev.yaml
# 打开 http://localhost:8080（此时不需要 Vite）
```

Windows：`.\scripts\build.ps1 windows; .\dist\bailing-windows-amd64.exe serve -config config.dev.yaml`

> 来源校验会自动放行「和当前访问地址同一主机」的请求，所以直接访问 8080 也能正常登录。

### 5.4 相册版 / 音乐版，PWA 与桌面端（2026-10-16）

**选择版本**：所有前端相关的目标都接受 `APP=all|photos|music`（默认 `all`）：

```bash
make dev-web APP=music          # 开发时只看音乐版
make build-linux APP=photos     # 只有相册的服务器版本
```

- 实际做的事：`VITE_BAILING_APP=$(APP) npm run build`；`npm run build` / `npm run dev` 之前会自动运行 `scripts/gen-manifest.mjs`，生成该版本的 `static/manifest.webmanifest` 和 iOS 图标（这两个文件不提交）；
- 前端代码里用 `import { features } from '$lib/features.svelte'` 判断：`features.photos` / `features.music` 已经同时考虑了构建版本和服务器的 `features` 配置；
- 图标源文件在 `web/static/icons/{all,photos,music}.svg`，改完后用 `rsvg-convert -w 512 -h 512 all.svg -o all-512.png`（以及 180、192）重新生成 PNG。

**PWA 调试**：

- 开发模式（`npm run dev`）**不注册** service worker，避免缓存干扰调试；
- 用发布版本测试：`make build && ./dist/bailing serve -config config.dev.yaml`，打开 <http://localhost:8080>，在开发者工具 → Application 查看 Manifest 和 Service Workers；
- 手机安装测试需要 https（或 localhost）。

**桌面端（Wails v2）**：

| 步骤 | macOS | Windows |
|---|---|---|
| 1. 安装 Wails CLI | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` | 同左 |
| 2. 系统依赖 | Xcode 命令行工具：`xcode-select --install` | WebView2（Windows 10/11 一般已自带） |
| 3. 下载 Go 依赖 | `make desktop-deps` | `.\scripts\build.ps1 desktop-deps` |
| 4. 检查环境 | `wails doctor` | 同左 |
| 5. 构建 | `make desktop APP=all`（或 photos / music） | `.\scripts\build.ps1 desktop -App all` |

- 产物在 `desktop/build/bin/`（macOS 是 `.app`）；
- `make desktop` 会先构建对应版本的前端，再把 `desktop/build/appicon-$(APP).png` 复制为应用图标；
- 桌面端是**单独的 Go 模块**（`desktop/go.mod`），所以根目录的 `go vet ./...`、`make build` 都不需要 Wails；
- 调试：`make desktop-dev APP=music`；桌面端的日志输出在启动它的终端里，本地服务地址是 `http://127.0.0.1:4782x`，也可以直接用浏览器打开这个地址调试。

## 6. Makefile ⇄ PowerShell 对照

| make | PowerShell |
|---|---|
| `make deps` | `.\scripts\build.ps1 deps` |
| `make dev-api` | `.\scripts\build.ps1 dev-api` |
| `make dev-web` | `.\scripts\build.ps1 dev-web` |
| `make web` | `.\scripts\build.ps1 web` |
| `make web APP=music` | `.\scripts\build.ps1 web -App music` |
| `make build-linux` | `.\scripts\build.ps1 linux` |
| `make build-windows` | `.\scripts\build.ps1 windows` |
| `make test` / `lint` / `fmt` / `clean` | `.\scripts\build.ps1 test` / `lint` / `fmt` / `clean` |
| `CONFIG=x.yaml make dev-api` | `.\scripts\build.ps1 dev-api -Config x.yaml` |

## 7. 常见开发问题

| 现象 | 处理 |
|---|---|
| `go: missing go.sum entry` | 没有执行 `go mod tidy` |
| `go build ./...` 报 `web/node_modules` 里的错误 | 不会发生：`web/go.mod` 把 web 目录隔离成了独立模块。如果看到，确认 `web/go.mod` 还在 |
| 5173 打开后接口全是 502 / 连接被拒绝 | 后端没启动，或者不在 8080 端口；`BAILING_API=http://127.0.0.1:9090 npm run dev` 可改代理目标 |
| 登录返回 403（`bad_origin`） | 请求来源与访问地址不是同一主机，且不在 `allowed_origins` 里；把来源地址加进去 |
| 登录成功但刷新后又要登录 | 开发环境的 `base_url` 写成了 https，Cookie 带了 Secure 标志，http 下不会保存 |
| 缩略图一直「冲洗中」 | vips 没找到：看后端启动日志 `libvips not found`；Windows 检查 `vips_bin_dir` |
| 端口被占用 | macOS `lsof -i :8080`；Windows `netstat -ano \| findstr :8080`，再结束对应进程 |
| Windows 上中文路径的相册扫不到 | 确认配置文件以 UTF-8 保存；路径用正斜杠 |
| 改了 `zh.ts` 后英文界面显示键名 | `en.ts` 缺了对应的键 |




