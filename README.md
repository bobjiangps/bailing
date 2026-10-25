# 百灵 · web_bailing

> 项目名暂取「百灵」（一种善鸣的小鸟），可随时修改。

> 一个小清新、慵懒、放松风格的私人「相册 + 音乐」Web 系统。
> 支持 Web、PWA，以及 Windows / macOS 桌面端；面向自己和家人使用。

## 功能一览

| 模块 | 能力 |
|---|---|
| 主页 | 艺术感背景与缓慢动效；相册 / 音乐入口；登录后显示小巧的「最近 / 最常」便签区 |
| 相册 | 一个文件夹 = 一本相簿；`about.txt` 描述相册；拟物相簿书架；推近 + 翻开动画；桌面双页翻书、手机单页（横屏双页）；照片放大 / 缩放 / 拖动 / 任意角度旋转；可选 EXIF 面板 |
| 音乐 | 扫描本地目录；读取内嵌封面、元数据、歌词；转动的唱片封面；播放模式 / 队列 / 音量 / 歌词滚动；在线直链、m3u8、网络电台；收藏与歌单（跨设备）；播放速度、定时停止、均衡器、频谱；兼容落雪音乐自定义源 |
| 通用 | 账号登录；浅色 / 深色 + 5 套艺术配色；中文 / English；响应式与触控；PWA |

## 技术栈速览

- **后端**：Go（标准库 `net/http`）+ SQLite（纯 Go）+ libvips（图片处理）
- **前端**：Svelte 5 + SvelteKit（SPA 静态输出）+ TypeScript + Vite + StPageFlip；动画和手势用原生 Web API 实现
- **歌词**：本地 `.lrc` → 内嵌歌词 → LRCLIB 在线下载 → 默认提示
- **流量**：服务器按流量计费，所有出站字节计量；经服务器传输的播放实时显示时长和大小
- **部署**：单个可执行文件（前端已嵌入）+ nginx（HTTPS，可与其他站点共用）+ systemd，目标 Debian 13 / Ubuntu 20.04+，1 核 1G

## 快速开始（开发）

```bash
go mod tidy && (cd web && npm install)            # Windows: .\scripts\build.ps1 deps
cp config.dev.example.yaml config.dev.yaml
go run ./cmd/bailing user add me --admin -config config.dev.yaml
make dev-api      # 终端 1          Windows: .\scripts\build.ps1 dev-api
make dev-web      # 终端 2，打开 http://localhost:5173
```

详见 [docs/dev-guide.md](docs/dev-guide.md)。
- **桌面端**：Wails（复用 Go 后端与 Svelte 前端）

## 文档导航

| 文档 | 内容 |
|---|---|
| [docs/requirements.md](docs/requirements.md) | 需求规格、功能清单、分阶段路线 |
| [docs/design.md](docs/design.md) | 视觉风格、配色方案、动效规范、页面布局 |
| [docs/architecture.md](docs/architecture.md) | 架构、目录结构、数据模型、API、音源插件（含落雪接口）、性能策略 |
| [docs/usage.md](docs/usage.md) | 开发环境、构建、部署到 Ubuntu、桌面端、日常使用与运维 |
| [docs/qa-log.md](docs/qa-log.md) | 需求讨论问答记录（方便回溯与学习） |

## 当前状态

- [x] 需求与方案确定，文档完成
- [x] 第一阶段代码：Web 核心（登录、主题、主页、相册、本地音乐、歌词、统计、流量计量）—— 待首次构建联调
- [x] 第二阶段：在线源 / 电台 / 收藏歌单（跨设备）/ 定时停止、播放速度、频谱、均衡器 / PWA / 桌面端（Wails，需本机安装 Wails CLI 后构建）
- [x] 第三阶段：落雪音乐自定义源兼容（kw、kg、tx、wy、mg 搜索）
- [x] 相册版 / 音乐版可以分开构建和发布（`APP=photos|music`），也可以一起发布

> 约定：构建、安装依赖等命令由开发者本人执行，文档与代码中只给出命令。

