# 使用与部署手册

> 按照约定，**所有构建、安装命令都由你本人执行**，本文只给出命令和说明。
> 标注「（P2）」「（P3）」的部分要等对应阶段完成后才能使用。
>
> - 开发环境（macOS / Windows）、修改代码、调试、编译打包 → [dev-guide.md](dev-guide.md)
> - 本文内容：媒体目录约定、部署到服务器（nginx）、日常使用、流量统计、歌词、常见问题

## 1. 媒体目录约定

```
photos/                        ← config.yaml → paths.photos
├── 京都 · 春/                 ← 一个子目录 = 一本相簿
│   ├── about.txt              ← 可选：相簿名、封面、日期、描述
│   ├── IMG_0012.HEIC
│   └── ...
└── 2025 海边/                 ← 没有 about.txt：目录名就是相簿名，第一张照片当封面
（photos/ 里直接放的照片）       ← 也算一本相簿，名字就是目录名（这里是「photos」）；about.txt 同样有效
music/                         ← config.yaml → paths.music（递归扫描）
├── 某专辑/
│   ├── 01 晴天.mp3            ← 标题/歌手/专辑/封面优先读取文件内嵌标签（ID3、FLAC、MP4）
│   ├── 01 晴天.lrc            ← 同名歌词文件（也可以放在 Lyrics/ 子目录），UTF-8 / GBK 都可以
│   └── cover.jpg              ← 没有内嵌封面时，使用同目录的 cover / folder / front 图片
└── ...
```

**多个目录**：`paths.photos` 和 `paths.music` 都可以写一个路径，也可以写一个列表，多个目录合并成一个曲库 / 一个书架：

```yaml
paths:
  photos: ["/srv/bailing/photos", "/mnt/nas/相册"]
  music:  ["/srv/bailing/music", "/mnt/usb/music", "~/Music"]
```

- 第一个目录里的歌曲、相簿编号和以前完全一样（收藏、歌单、统计不受影响）；后面的目录用路径的短哈希区分。**新目录请加在列表末尾**；改了某个目录的路径，等于换了一个目录（它的收藏等记录会失效）。
- 「文件夹」标签里，第一个目录下的子目录照常显示，其他目录的子目录前面带上目录名（如 `music/周杰伦`）。
- 相册目录可以直接是一个放满照片的文件夹（比如截图文件夹）：里面直接放的照片会成为一本以文件夹名命名的相簿；如果还有子文件夹，子文件夹各自是一本相簿。只认这一层，更深的子文件夹不会扫描。
- 某个目录暂时不存在（U 盘没插、NAS 没挂载）时，只跳过它，不会删除它的曲目和相簿记录，重新挂上后照常使用。
- 服务器上用 systemd 运行时，`/srv/bailing` 以外的目录要加到 `deploy/bailing.service` 的 `ReadOnlyPaths=`（音乐目录要写歌词时用 `ReadWritePaths=`），否则服务看不到这些目录。

**没有标签的文件**按文件名猜歌名和歌手：`01 - 周杰伦 - 晴天.mp3`、`晴天 - 周杰伦.mp3` 都可以。「歌名 - 歌手」还是「歌手 - 歌名」由**同目录里有标签的文件**判断（它们的文件名和标签一对比就知道顺序），同目录没有可参考的文件时参考整个曲库，再没有就按「歌手 - 歌名」。猜错时最可靠的办法是给文件补上标签（Mac 上「音乐」App、Windows 上 Mp3tag 都可以），补完后重新扫描。

**封面**只来自文件内嵌图片或同目录的 cover / folder 图片，不会联网查找。很多下载来的 MP3 没有内嵌封面，曲库里就显示占位图。

`about.txt` 示例（`---` 以下是描述，可以写多行）：

```text
title: 京都 · 春
cover: IMG_0012.HEIC
date: 2024-04
---
樱花落在鸭川上，
风里有抹茶的味道。
```

## 2. 部署到阿里云 ECS（Debian 13 / Ubuntu 20.04+，1 核 1G，nginx）

### 2.0 部署前须知

- bailing 是**静态编译**的单个文件（纯 Go，SQLite 也是纯 Go 实现），不依赖系统的 glibc 版本，Debian 13 和 Ubuntu 20.04 用**同一个** `bailing-linux-amd64`。系统只需要额外装 libvips（缩略图）和 nginx。
- 本节命令在 Debian 13（trixie）和 Ubuntu 20.04+ 上通用，两者的差别在命令旁边注明。从旧服务器搬到新系统见 **2.9**。
- Debian 13 软件源里的 libvips 是 8.16.x；**HEIC 解码器是单独的插件包**（见 2.2），不装的话 iPhone 照片不出缩略图。Ubuntu 20.04 的 libvips 是 8.9.x，标准支持已在 2025 年 5 月结束（bailing 不受影响）。
- 用 `uname -m` 确认架构：`x86_64` 用 `bailing-linux-amd64`，`aarch64` 用 `bailing-linux-arm64`。
- 中国内地地域的服务器用域名提供网站服务需要 **ICP 备案**；在阿里云安全组里放行 80、443 端口。
- **服务器按流量计费**：bailing 会统计自己发出的每一个字节（见第 4 节），建议在 `config.yaml` 里设置 `traffic.monthly_quota_gb`。

### 2.1 初始化系统（只做一次）

Debian 最小安装可能没有 `sudo`，也可能没有普通用户。先用 root 执行：

```bash
apt update && apt install -y sudo rsync curl
usermod -aG sudo <你的用户名>       # 之后重新登录该用户；阿里云镜像通常已建好用户
```

```bash
# 1G 交换分区（生成缩略图时兜底；先用 swapon --show 确认镜像没有自带交换分区）
sudo fallocate -l 1G /swapfile && sudo chmod 600 /swapfile
sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-swap.conf && sudo sysctl --system

# 专用用户和目录
sudo useradd --system --home /var/lib/bailing --shell /usr/sbin/nologin bailing
sudo mkdir -p /opt/bailing /etc/bailing /var/lib/bailing /srv/bailing/{photos,music}
sudo chown -R bailing:bailing /var/lib/bailing
sudo chown -R $USER:bailing /srv/bailing && sudo chmod -R g+rX /srv/bailing
# 音乐目录允许 bailing 写入：下载的歌词会存成 <歌名>.lrc 放在歌曲旁边
sudo chmod -R g+w /srv/bailing/music && sudo find /srv/bailing/music -type d -exec chmod g+s {} +
```

> **不想建专用用户、直接用 root 运行**：把 `bailing.service` 里的 `User=` / `Group=` 改成 `root`，上面的 `useradd`、`chown`、`chmod` 都可以省略。service 里的沙箱设置对 root 同样有效：除 `ReadWritePaths` 列出的目录外，其余位置都只读，所以 `paths.data` 和音乐目录必须写在 `ReadWritePaths` 里。代价是万一程序被攻破，攻击者拿到的是 root 权限，因此仍然建议只监听 `127.0.0.1`、由 nginx 对外提供服务。

### 2.2 安装 libvips

```bash
sudo apt update
# Debian 13（以及 Debian 12、Ubuntu 24.04）：HEIC 解码在插件包里
sudo apt install -y libvips-tools libheif1 libheif-plugin-libde265
# Ubuntu 20.04：没有插件包，只装前两个
# sudo apt install -y libvips-tools libheif1
vips --version
vips -l | grep -i heif        # 应该能看到 VipsForeignLoadHeif
```

`vips -l` 能看到 heif 也不代表能解 iPhone 照片（HEVC 编码），用一张真实的 HEIC 验证最可靠：`vips thumbnail IMG_0001.HEIC /tmp/t.jpg 300`。失败时检查 `libheif-plugin-libde265` 是否已安装；实在不行，在 Mac 上先把 HEIC 转成 JPEG（`sips -s format jpeg in.HEIC --out out.jpg`）。不建议在 1G 内存的机器上从源码编译 libvips。

### 2.3 安装 nginx 和证书工具

如果服务器上已经有 nginx（和其他网站共用），跳过安装，直接看 2.5。

```bash
sudo apt install -y nginx certbot python3-certbot-nginx
nginx -v                       # Debian 13 是 1.26；Ubuntu 20.04 是 1.18，模板两者都兼容
```

Debian 默认没有启用防火墙（没有 ufw），端口只需在阿里云安全组里放行 80、443。

模板 `deploy/nginx/bailing.conf` 按 1.18 写的是 `listen 443 ssl http2;`。在 Debian 13（nginx 1.26）上它仍能用，但 `nginx -t` 会提示 `the "listen ... http2" directive is deprecated`；想消除提示，把两行 `listen` 改成 `listen 443 ssl;` / `listen [::]:443 ssl;`，并在下面加一行 `http2 on;`。

### 2.4 上传并运行 bailing

在 Mac 上执行（Windows 用 `.\scripts\build.ps1 linux`，见 dev-guide）：

```bash
make build-linux      or   BASE_PATH=/bailing make build-linux                               # → dist/bailing-linux-amd64（已包含前端）
scp dist/bailing-linux-amd64   user@ECS:/tmp/bailing
scp config.example.yaml        user@ECS:/tmp/config.yaml
scp deploy/bailing.service     user@ECS:/tmp/
scp deploy/nginx/*.conf        user@ECS:/tmp/
```

在 ECS 上执行：

```bash
sudo install -m 755 /tmp/bailing /opt/bailing/bailing
sudo install -m 640 -g bailing /tmp/config.yaml /etc/bailing/config.yaml
sudo nano /etc/bailing/config.yaml
#   server.listen    "127.0.0.1:8080"   ← 只监听本机，由 nginx 对外提供服务
#   server.base_url  "https://music.example.com"
#   paths.*          保持默认即可
#   traffic.monthly_quota_gb  按你的流量包填写
sudo cp /tmp/bailing.service /usr/lib/systemd/system/ or /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now bailing
systemctl status bailing
curl -s http://127.0.0.1:8080/api/health          # 返回 {"ok":true,...}
journalctl -u bailing -f                           # 实时日志
```

### 2.5 配置 nginx

有两种接入方式，任选其一。两份模板都在 `deploy/nginx/`。

**方式 A：独立子域名（推荐）**，例如 `music.example.com`

```bash
sudo cp /tmp/bailing.conf /etc/nginx/sites-available/bailing.conf
sudo nano /etc/nginx/sites-available/bailing.conf    # 把 music.example.com 改成你的域名
sudo ln -s /etc/nginx/sites-available/bailing.conf /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d music.example.com            # 申请证书并写入 ssl_* 配置，之后自动续期
```

**方式 B：挂在已有网站的子路径下**，例如 `www.example.com/bailing/`

1. `config.yaml`：`server.base_path: "/bailing"`，`server.base_url: "https://www.example.com"`
2. 构建时带上相同的前缀：`BASE_PATH=/bailing make build-linux`（前端资源路径是编译进去的）
3. 把 `bailing-subpath.conf` 复制到 `/etc/nginx/snippets/bailing.conf`，在已有站点的 `server { listen 443 ... }` 里加一行 `include snippets/bailing.conf;`
4. 按模板顶部的说明，在 `/etc/nginx/conf.d/` 下添加 `upstream bailing_backend`
5. `sudo nginx -t && sudo systemctl reload nginx`

**模板里几处关键配置的含义：**

| 配置 | 原因 |
|---|---|
| `proxy_pass http://bailing_backend;`（不带 URI） | 原样转发路径；子路径模式下由 bailing 自己去掉 `/bailing` 前缀 |
| `/media/`、`/proxy/` 设置 `proxy_buffering off` | ① **流量统计准确**：开缓冲时 nginx 会提前把整首歌读走，用户切歌了也照样计数；② 拖动进度条立即响应 |
| `/proxy/` 设置 `proxy_read_timeout 6h` | 网络电台是无限流，不能被默认的 60 秒超时掐断 |
| `gzip` 只压缩 JSON / JS / CSS | 图片、音频本来就是压缩格式，再压缩只会浪费 CPU |
| `X-Real-IP`、`X-Forwarded-*` | bailing 只信任 `trusted_proxies`（默认 127.0.0.1）发来的这些头，用于登录限速和来源校验 |
| `client_max_body_size 2m` | 第一阶段没有上传功能；P3 导入落雪脚本时也够用 |

### 2.6 账号管理

```bash
B="sudo -u bailing /opt/bailing/bailing"
C="-config /etc/bailing/config.yaml"
$B user add me --admin --display "我" $C       # 会提示输入密码
$B user add mom --display "妈妈" $C
$B user list $C
$B user passwd mom $C
$B user del mom $C
```

网站没有注册入口，账号只能用命令行添加。管理员可以在设置页手动触发扫描、查看媒体库状态。

**修改密码**：

- 自己改：登录后「设置 → 账号 → 修改密码」，输入当前密码和两次新密码（至少 6 位）。改完后**其他设备上的登录会退出**，当前浏览器保持登录。当前密码连续输错会被暂时锁定，与登录的限速相同。
- 管理员重置任意用户（忘记密码时）：在服务器上运行 `bailing user passwd 用户名 -config <配置文件>`，按提示输入新密码；该用户**所有设备**都需要重新登录。服务不用停。
  - 按本手册以 root 部署在 `/home/bob/3rd/bailing` 时：`/home/bob/3rd/bailing/bailing-linux-amd64 user passwd bob -config /home/bob/3rd/bailing/config.yaml`

### 2.7 同步照片和音乐

```bash
rsync -avh --progress ~/Pictures/albums/ user@ECS:/srv/bailing/photos/
rsync -avh --progress ~/Music/library/   user@ECS:/srv/bailing/music/
```

- 同步后等待定时扫描（默认 6 小时一次），或在「设置 → 媒体库 → 立即扫描」手动触发，也可以执行 `$B scan $C`。
- 阿里云通常只对**出方向**（服务器发出）计费，上传照片（入方向）一般不收流量费，但会占用带宽。
- Windows 上可以用 WinSCP 同步，或在 WSL 里使用 rsync。

### 2.8 升级与备份

```bash
# 升级：替换可执行文件后重启（数据库迁移在启动时自动执行）
sudo install -m 755 /tmp/bailing /opt/bailing/bailing && sudo systemctl restart bailing

# 备份：只需要备份数据库；缓存目录（/var/lib/bailing/cache）可以重新生成
sudo systemctl stop bailing && sudo cp /var/lib/bailing/bailing.db ~/bailing-$(date +%F).db && sudo systemctl start bailing
```

某些版本升级后第一次启动会**重新索引一遍音乐库**（例如数据库迁移 004：重新识别文件名里「歌名 - 歌手」的顺序），曲库较大时会持续几分钟，期间页面显示「正在扫描」，不影响播放。

**数据库里有什么**（`bailing.db`，SQLite，2026-10-27 记录）：

| 类别 | 表 | 内容 | 能否重建 |
|---|---|---|---|
| 账号 | `users`、`sessions`、`prefs` | 用户名、密码哈希、登录会话、每人的界面偏好（主题、语言、音量、音质…） | ❌ 丢了要重新建账号 |
| 收藏与歌单 | `favorites`、`playlists`、`playlist_items` | 每人的收藏歌曲 / 电台、歌单 | ❌ |
| 统计 | `play_events`、`view_events` | 播放、看相簿记录（首页「最近 / 最常」） | ❌ |
| 流量 | `traffic_daily`、`stream_sessions` | 按天流量、每次播放的流量（保留 400 / 90 天） | ❌ |
| 歌词 | `lyrics_cache`、`lyrics_override` | 下载 / 手动选择的歌词，「没找到」的记录 | 部分（自动下载的可以重新查，手动选择的要重新选） |
| 媒体索引 | `albums`、`photos`、`tracks` | 扫描出来的相簿、照片、歌曲信息（标签、时长、封面键…） | ✅ 重新扫描即可 |
| 落雪 | `lx_scripts` | 每个脚本是否启用（脚本本身在 `lx-scripts/` 目录） | ✅ |

查看：`sudo sqlite3 /var/lib/bailing/bailing.db ".tables"`（`apt install sqlite3`）。**只读查看即可，不要在服务运行时手动改数据。**

**运行时旁边多出来的两个文件**：`bailing.db-wal` 和 `bailing.db-shm`。

- 数据库开启了 WAL（预写日志）模式：新写入先追加到 `-wal`，`-shm` 是多个读写方共享的索引（内存映射文件）。这样播放、统计写入时页面读取不会被卡住；
- 服务**正常停止**时，SQLite 把 `-wal` 里的内容合并回 `bailing.db`，然后删除这两个文件——所以停了服务就看不到它们，这是正常的；
- 服务被强杀（断电、`kill -9`）时它们会留下，下次启动自动恢复，**不要手动删除**，否则会丢最近的写入；
- 备份：先停服务再复制 `bailing.db`（上面的命令就是这样做的）；不停服务时用 `sudo sqlite3 /var/lib/bailing/bailing.db ".backup /tmp/bailing-backup.db"`，不要直接 `cp` 正在运行的数据库。

### 2.9 换系统 / 搬到新服务器（例如 Ubuntu 20.04 → Debian 13）

可执行文件不用重新编译，直接用本地 `make build-linux` 产出的 `dist/bailing-linux-amd64`（arm 机器用 `make build-linux-arm64`）。

**1. 从旧服务器取出数据**（重装系统前做；系统盘已经重装的，从快照或备份里取同样的文件）：

```bash
sudo systemctl stop bailing
sudo tar czf ~/bailing-state.tgz -C / etc/bailing var/lib/bailing/bailing.db \
     var/lib/bailing/relay.key var/lib/bailing/lx-scripts
# 照片、音乐在 /srv/bailing，直接用 rsync 从本机重新同步即可（2.7）
```

| 文件 | 内容 | 丢了会怎样 |
|---|---|---|
| `/etc/bailing/config.yaml` | 配置 | 按 2.4 重新填写 |
| `/var/lib/bailing/bailing.db` | 账号、收藏、歌单、播放统计、流量记录、已下载的歌词 | 需要重新建账号，收藏和统计清空 |
| `/var/lib/bailing/relay.key` | 转发链接的签名密钥 | 自动重新生成；已经发出的转发链接失效，没有其他影响 |
| `/var/lib/bailing/lx-scripts/` | 上传的落雪音源脚本 | 在设置页重新上传 |
| `/var/lib/bailing/cache/` | 缩略图、封面缓存 | **不用搬**，会自动重新生成（1 核机器需要一段时间） |

**2. 在新服务器上**依次执行 2.1（用户和目录）→ 2.2（libvips）→ 2.3（nginx）→ 2.4（上传可执行文件、`bailing.service`），然后在 `systemctl enable --now bailing` **之前**放回数据：

```bash
scp ~/bailing-state.tgz user@新ECS:/tmp/        # 先从旧服务器下载到本机，再传上去
sudo tar xzf /tmp/bailing-state.tgz -C /
sudo chown -R bailing:bailing /var/lib/bailing
sudo chown root:bailing /etc/bailing/config.yaml && sudo chmod 640 /etc/bailing/config.yaml
sudo systemctl daemon-reload && sudo systemctl enable --now bailing
curl -s http://127.0.0.1:8080/api/health
```

**3. nginx 与证书**：把旧服务器的 `/etc/nginx/sites-available/bailing.conf`（或子路径方式的 snippet）复制过来，或按 2.5 重新配置；域名解析指向新 IP 之后再执行 `sudo certbot --nginx -d 你的域名` 重新申请证书（旧证书不用搬）。

**4. 同步媒体**：按 2.7 用 rsync 把照片和音乐传到 `/srv/bailing/`，然后「设置 → 媒体库 → 立即扫描」。

**注意：**

- Debian 13 的 `/tmp` 默认是内存盘，重启后清空。上传到 `/tmp` 的文件请在重启前装好。
- 路径（`/opt/bailing`、`/etc/bailing`、`/var/lib/bailing`、`/srv/bailing`）和旧服务器保持一致，`bailing.service` 就不用改。如果改了路径，同步修改 service 里的 `ReadWritePaths` / `ReadOnlyPaths`，否则 bailing 会报只读或没有权限。
- 新的二进制比旧数据库新时，启动时会自动迁移数据库；反过来（旧二进制打开新数据库）不支持，所以先备份。

## 3. 日常使用

| 功能 | 操作 |
|---|---|
| 配色、深浅色、语言 | 右上角的调色盘和太阳/月亮图标；或「设置 → 外观」 |
| 设置页 | 右上角**圆形头像**（显示名字首字）→「设置」；也可以直接访问 `/settings` |
| 打开相簿 | 在书架上点击相簿；翻页：← →、拖动书角、点击两侧箭头、手指滑动 |
| 相簿翻到最后 | 底部出现「回到第一页」「返回书架」，右侧箭头变成「回到第一页」。电脑上底部一直显示快捷键：← → 翻页、`Home` 回到第一页、`Esc` 返回书架；按钮上悬停也会显示对应的键 |
| 看照片 | 点击照片。滚轮或双指缩放；拖动移动；**Shift + 拖动、拖动外圈手柄或双指转动 = 任意角度旋转**（−180°～+180°，接近 0°/90°/180° 时会吸附）；`R` 转 90°；`0` 复位；`I` 拍摄信息；`Esc` 关闭并退回相簿 |
| 拍摄信息（EXIF） | 默认不显示；在查看器里点 ⓘ 临时打开，或在「设置 → 看照片」里设为默认显示；GPS 位置需要单独开启 |
| 播放音乐 | 曲库里双击（手机上单击）播放；空格：播放/暂停；← →：快退/快进 5 秒；Ctrl/⌘ + ← →：上一首/下一首；↑ ↓：音量；`M`：静音 |
| 播放模式 | 播放条左侧按钮循环切换：列表循环 → 顺序播放 → 单曲循环 → 随机播放 |
| 曲库排序 | 歌曲列表上方「排序」：目录顺序（默认）、歌名、歌手、专辑、最新添加、修改时间、时长，旁边切换「正序 / 倒序」。选「最新添加」「修改时间」时自动切到倒序（新的在前）。选择保存在本浏览器。「最新添加」= 第一次被扫描到的时间（升级前已有的歌曲按文件修改时间算） |
| 定位正在播放 | **只有切歌时**（下一首 / 上一首 / 随机 / 自动播下一首）列表才自动滚到正在播放的那一行（已经看得到时不动）。排序、切换标签或文件夹、自己滚动都不会被打断：排序后回到列表开头，切换标签再切回来回到你原来的位置。宽屏自动收起模式下，抽屉收着时切了歌，下次展开时直接停在正在播放的那一行。也可以点列表上方的 ◎ 按钮手动定位。正在搜索时不自动滚动 |
| 曲库顶部标签 | 「全部 / 文件夹 / … / 电台 / 在线」和搜索框在列表滚动时固定在顶部；再点一次当前标签回到列表顶部 |
| 歌词 | 宽屏电脑（窗口宽度 ≥ 1100px）：歌词在右侧，曲库 / 在线 / 电台收进抽屉，默认**开始播放时自动收起、暂停时自动展开**（切歌、加载下一首不算暂停，抽屉不会弹出来；曲库顶部的复选框可以关掉）；任何时候点右上角「曲库 L」按钮或按 `L` 打开，按 `Esc` 或「收起曲库」按钮收起；窄屏和手机保持原来的布局（切换到「歌词」）。点击某一行跳到该处；歌词不对时点「重新查找」或「换一个歌词」，没有歌词时点「手动搜索歌词」（5.4） |
| 未知歌手 / 未知专辑 | 没有歌手、专辑信息的歌曲归到「未知歌手」「未知专辑」，点进去只列出这些歌曲 |
| 下载歌曲 | 曲库每行（行右侧的一排按钮，平时半透明，鼠标移上去变清楚）、落雪搜索结果每行、正在播放的歌曲名下方都有下载按钮，文件名为「歌手 - 歌名.扩展名」。本地歌曲直接下载原文件；落雪歌曲经服务器转发下载（**消耗服务器流量**，见第 4 节）；电台和直播流不能下载。**落雪歌曲下载前先弹出确认框**（立即弹出）：歌名、歌手、平台，音质下拉框（平台列出的全部音质和大小，默认是设置里的音质，可以当场换），以及所选音质的文件大小。点「下载」后服务器先**核对真实文件**：只有几十字节、不是音乐文件、和平台标注的大小相差很多，或者其实是别的音质（例如酷狗某些歌的 128k 实际是 39 字节的错误内容，另一个脚本又给了 320k 的文件），就**不下载**——只会下载你选的那个音质，在框里提示「文件异常」，该音质标为「文件异常」，可以换个音质再点下载；核对通过才开始保存。核对过程在框里**逐行显示每个音源脚本的结果**，例如「✗「A」源的 128k 文件有错误：只有 39 B，不是音频」→「切换到「B」源，获取 128k…」→「✓ 已切换到「B」源，从这里下载 128k · 3.03 MB」，所以总能看到文件是从哪个源下载的；下载开始后确认框不会自动关闭，点「完成」关闭。点「取消」、按 `Esc` 或点框外关闭。勾选「以后不再询问」后直接按默认音质下载（下载按钮旁会提示「已从「B」源下载」，前面的源有问题时提示「「A」源有问题，已切换到「B」源下载」），可在「设置 → 播放与在线音源」重新打开 |
| 统计便签 | 登录后在主页底部：最近播放、最常播放、最近看过、最常看过 |
| 服务器流量 | 播放条上的流量标记；「设置 → 服务器流量」 |
| 安装为应用（P2） | Chrome / Edge 地址栏的「安装」图标；iPhone Safari：分享 → 添加到主屏幕 |

## 4. 服务器流量统计（按流量计费）

bailing 在最外层的中间件里统计**自己发出的每一个响应字节**，按天、按类别累计，每 30 秒写入一次数据库。

**播放时的提示**（播放条和音乐页都有）：

| 标记 | 含义 | 是否消耗服务器流量 |
|---|---|---|
| 🖥 服务器传输 `3:25 · 8.12 MB` | 本地音乐，由你的服务器传给浏览器 | 是 |
| ⇄ 经服务器转发 `12:40 · 23.5 MB`（强调色，呼吸闪烁） | P2：在线源因为 http、跨域或防盗链，只能由服务器中转 | **是，而且会持续消耗** |
| ⚡ 直连 · 不耗流量 | P2 / P3：浏览器直接连接在线源 | 否 |

- 计时是本次实际播放的时长；大小是服务器为这次播放实际发出的字节数，每 4 秒刷新一次，按 1024 进制显示（例如 `1.00 GB`）。
- 拖动进度条、暂停后继续所产生的多次分段请求，会合并到同一个计数里。

**「设置 → 服务器流量」面板**：今天和本计费月的用量、月额度进度条、按类别统计（本地音乐、转发、照片、封面、页面与接口）、近 30 天柱状图、最近经过服务器的播放（时长和大小）。

**额度提醒**（`config.yaml`）：

```yaml
traffic:
  monthly_quota_gb: 50            # 你的流量包大小；0 = 不设额度
  warn_percent: 80                # 超过 80% 时页面顶部出现提示条
  reset_day: 1                    # 计费月从每月几号开始
  block_proxy_over_quota: true    # P2：超出额度后停止服务器转发（直连不受影响）
```

> 这里统计的是应用层字节数；阿里云实际计费还包含 TCP/TLS 和 HTTP 头等开销（约 2～5%），以阿里云账单为准。

**省流量的设计**：书架和列表只用约 480px 的 WebP 缩略图（几十 KB）；翻书和查看时用约 2048px 的 WebP；原图只在放大超过 100% 时才加载；音频设置为 `preload="none"`，不点播放就不下载；前端静态资源缓存一年。

### 4.1 哪些内容经过服务器、哪些有缓存、能否清理

**经过服务器的内容（都计入流量统计）**

| 内容 | 什么时候 | 统计类别 |
|---|---|---|
| 本地歌曲、照片、本地封面 | 总是：文件就在服务器上，播放 / 查看就是从服务器下载 | 音乐 / 照片 / 封面 |
| 落雪、链接、电台的音频 | 只在第 6.3 节列出的情况下转发；平时浏览器直连，不耗流量 | 转发 |
| 落雪歌曲下载 | 总是转发（浏览器不允许直接把别的网站的文件存成下载） | 转发 |
| 在线封面 | 只在浏览器直连图床失败时转发（`/media/rcover`） | 封面 |
| 搜索、解析、歌词查询 | 服务器替浏览器请求各平台接口，只有几 KB 的 JSON | 接口 |

**转发不落盘**：转发的音频、封面边收边发，服务器不保存。

**服务器上的缓存**

| 缓存 | 位置 | 内容 | 能否清理 |
|---|---|---|---|
| 照片缩略图 | `<data>/cache/photo/`（服务器上是 `/var/lib/bailing/cache/photo/`） | 每张照片的小图、中图（WebP），HEIC 的全尺寸 WebP | 可以。「设置 → 缓存与存储 → 清理无用缓存」删除已不存在的照片的文件；整个目录删掉也行，打开时会重新生成（1 核机器上会忙一阵） |
| 歌曲封面 | `<data>/cache/cover/` | 从歌曲文件里提取的封面原图和缩略图 | 「清理无用缓存」只删没有歌曲使用的封面。**不要整个删除**：封面只在扫描时提取，删了要等歌曲文件有改动才会重新提取 |
| 歌词 | 数据库 `<data>/bailing.db` 的 `lyrics_cache` 表（本地歌曲）、`lyrics_override` 表（在线歌曲的手动选择）；以及（`save_beside_music: true` 时）歌曲旁边的 `.lrc` | 下载 / 手动选择的歌词；没找到的记录 7 天内不重查 | 某首歌的歌词不对时点「重新查找」或「换一个歌词」；`.lrc` 可以直接删除 |
| 落雪解析结果 | 内存 | 播放地址，10 分钟（`cache_ttl`） | 重启服务即清空 |
| 电台列表、在线歌词查询 | 内存 | 电台 10 分钟、地区 1 天、歌词 6 小时 | 重启服务即清空 |
| 落雪脚本 | `<data>/lx-scripts/` | 你上传的源脚本 | 在设置页删除 |

**浏览器里的缓存**（在你自己的设备上，不占服务器空间）：照片、封面 30 天，本地歌曲 1 天，在线封面转发 7 天，页面程序文件长期缓存（升级后自动更新）。清除方法：浏览器的「清除网站数据」。

「设置 → 缓存与存储」（管理员）显示以上目录各占多少空间。

## 5. 歌词

### 5.1 查找顺序

查找顺序（找到即停止）：

0. **手动选择的歌词**（见 5.4，优先级最高）
1. **同名 `.lrc` 文件**（`晴天.mp3` 对应 `晴天.lrc`，也可以放在 `Lyrics/` 子目录；UTF-8 / GBK 自动识别）
2. **文件内嵌歌词**（例如 mp3 的 ID3 USLT 标签）
3. **之前已下载并缓存的歌词**
4. **在线下载：[LRCLIB](https://lrclib.net)** —— 免费、开放、无需密钥的公共歌词库。依次尝试：
   - 按「歌名 + 歌手 + 专辑 + 时长」精确匹配；
   - 按「歌名 + 歌手」搜索，取时长相差 5 秒以内、优先带时间轴的一条；
   - 只按歌名搜索（应对歌手写法不同，例如「周杰倫 / 周杰伦」），同样要求时长相差 5 秒以内；
   - 歌名、歌手都一致但时长对不上（例如电台剪辑版、现场版）：只用**不带时间轴的纯文本**显示，不会滚动，也不写入 `.lrc` 文件。
5. 都没有时显示「这首歌没有歌词，安静听吧」；纯音乐显示「纯音乐，请欣赏」

相关配置：

```yaml
lyrics:
  online: true              # false = 只使用本地歌词
  negative_ttl: "168h"      # 没找到的歌 7 天内不再重复查询（省流量，也是礼貌使用公共服务）
  save_beside_music: true   # 把下载的歌词写成 <歌名>.lrc 放在歌曲旁边（音乐目录需对 bailing 用户可写，否则只存数据库缓存）
```

- 下载的歌词存在数据库里，下次直接读取。
- 匹配错了：在歌词面板底部点「重新查找」，或用「换一个歌词」手动搜索（5.4）。也可以自己放一个同名 `.lrc`。
- 中文歌在 LRCLIB 上的覆盖率不如英文歌，找不到时可以先手动搜索，再不行就自己准备 `.lrc` 文件。

### 5.2 歌词服务：LRCLIB 以外还有哪些（2026-10-27 记录）

`lyrics.lrclib_base` **只能填 LRCLIB 协议的服务**（接口 `/api/get`、`/api/search`、`/api/get/{id}`）。目前公网上公开、免密钥、能直接替换的只有 LRCLIB 官方站；其他服务协议不同，要改代码才能接入。

| 服务 | 地址 / 方式 | 能否直接填进 `lrclib_base` | 说明 |
|---|---|---|---|
| **LRCLIB 官方**（默认） | `https://lrclib.net` | ✅ | 开源、免费、无需密钥，带时间轴；英文歌多，中文歌覆盖一般 |
| **自建 LRCLIB** | 开源项目 `tranxuanthang/lrclib`（Rust），官方提供整库数据库下载 | ✅ | 填自己的地址，例如 `http://127.0.0.1:3300`。适合 lrclib.net 访问慢或不稳定时；1 核 1G 机器上跑偏重，建议放在别的机器 |
| 网易云音乐（非官方接口） | `music.163.com/api/song/lyric?id=…&lv=1&tv=-1`，先用搜索接口拿歌曲 id | ❌ 需新写 provider | 中文歌覆盖最好，常带翻译（`tlyric`）；非公开接口，随时可能改或限流 |
| QQ 音乐（非官方接口） | `c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg?songmid=…`，需要 `Referer: https://y.qq.com` | ❌ 需新写 provider | 中文歌覆盖好；返回 base64 |
| 酷狗（非官方接口） | `krcs.kugou.com/search?keyword=…&duration=…` → `lyrics.kugou.com/download?id=…&accesskey=…&fmt=lrc` | ❌ 需新写 provider | 按时长匹配，返回 base64 的 LRC/KRC |
| LrcApi（第三方开源） | 项目 `HisAtri/LrcApi`，可自建，按歌名 + 歌手返回 LRC | ❌ 需新写 provider | 底层聚合国内平台，接口简单（`/lyrics?title=&artist=`）；公共实例不保证可用 |
| Musixmatch | 官方 API，需要申请 key | ❌ | 免费档只返回 30% 歌词，不适合 |
| Genius | 官方 API | ❌ | API 不返回歌词正文，只能抓网页，不建议 |
| lyrics.ovh | `api.lyrics.ovh/v1/歌手/歌名` | ❌ | 只有纯文本、无时间轴，经常不可用 |

> 国内平台的接口都是非官方的，仅适合个人自用。落雪源已经实现了这几个平台的**搜索**（`internal/sources/lx/platform_impl.go`），以后要接国内歌词，可以复用它们拿到歌曲 id，再调对应的歌词接口；做法见架构文档 8.5。需要时告诉开发者「加一个 xx 歌词 provider」即可。

### 5.3 歌词存在哪里

| 歌曲类型 | 自动下载的歌词 | 手动选择的歌词（5.4） |
|---|---|---|
| **本地歌曲** | 数据库 `lyrics_cache` 表；`save_beside_music: true` 且音乐目录可写时，**同时**写成歌曲旁边的 `<歌名>.lrc`（时长对不上的近似匹配只存数据库，不写文件） | 数据库 `lyrics_cache`（`source = manual`）；同时写 `.lrc`，原来已有的 `.lrc` 先改名为 `.lrc.bak` |
| **落雪 / 链接等在线歌曲** | 没有本地文件，**不写任何 .lrc，也不存数据库**：只在服务器**内存**里缓存 6 小时，重启服务就没了，下次播放重新查 | 数据库 `lyrics_override` 表（按「歌名 + 歌手 + 专辑」记），重启后仍然有效 |

- 数据库位置：`<paths.data>/bailing.db`（服务器上是 `/var/lib/bailing/bailing.db`）。
- 下载落雪歌曲到本地时只下载音频，不附带歌词；把它放进音乐目录、扫描后，就按本地歌曲的规则自动找歌词。

### 5.4 手动搜索歌词（2026-10-27）

自动匹配用的是标签里（或从文件名猜出的）歌名和歌手。文件名、标签不规范时会找不到或找错，这时可以自己输入关键词：

1. 播放这首歌，打开歌词面板；
2. 没有歌词时，面板中间有「**手动搜索歌词**」按钮；已经有歌词但不对时，点面板底部的「**换一个歌词**」；
3. 在弹出的框里改**歌名、歌手**（专辑可不填），点「搜索」；
4. 结果按「和当前歌曲时长是否接近」排序：时长相差 5 秒以内的排在前面，时长用强调色显示（只有这些的时间轴才对得上）；每条显示「带时间轴 / 纯文本」和前两句歌词预览；
5. 点「**使用**」：歌词立即生效并保存在服务器上，以后每次播放、换设备都用这一份；
6. 想回到自动匹配：点面板底部的「**恢复自动匹配**」。

搜索技巧：

- 搜不到时先**只填歌名**；或换成繁体 / 英文名 / 原唱歌手；
- 翻唱、现场版时长不同，可以选纯文本的，或者时间轴对不上也能接受时选带时间轴的；
- 电台、直播流不能搜索歌词；
- 服务器关闭了在线歌词（`lyrics.online: false`）时不能手动搜索。

## 6. 电台、链接播放与服务器转发（P2，已实现）

音乐页书库右上方新增两个标签：「电台」和「在线」。

### 6.1 电台

- 数据来自开放目录 **Radio-Browser**，默认按热度排序。
- 可以按名称搜索，按地区（下拉框）和风格（chillout、jazz 等）筛选。
- 点星星收藏。收藏保存在**服务器**上，换手机、换电脑登录后都在（「音乐 → 我的 → 收藏的电台」或电台页的「收藏」）；以前保存在浏览器里的电台收藏，登录后会自动上传一次。
- 勾选「只看 https 电台」，只列出可以直连的电台（不耗服务器流量）；列表里带 `http` 小标签的电台可能需要转发，原因见 6.4。
- 播放时进度条显示「● 直播」，不能拖动；歌词区显示「电台直播中」。
- 听满 30 秒记一次播放，会出现在首页的「最近播放」里，点一下可以再听。

### 6.2 链接播放（「在线 → 链接播放」）

- 粘贴 mp3、m4a、flac、ogg、**m3u8**，或 **m3u、pls** 播放列表地址（取第一条）。标题可以不填。
- 可以「立即播放」或「加入队列」；最近 20 个链接会保存在本浏览器。
- m3u8 的处理：Safari 原生支持；其他浏览器使用 hls.js（按需加载）。

### 6.3 什么时候会经过服务器转发（消耗流量）

| 情况 | 方式 |
|---|---|
| 普通情况 | **浏览器直连**音源，不耗服务器流量，播放条显示「直连 · 不耗流量」 |
| 网站是 https，而音频地址是 http（浏览器会拦截“混合内容”） | 先试同一地址的 **https 直连**，9 秒内没有声音才转发 |
| 开启了均衡器或频谱，且对方不允许跨站读取音频（见 8.2） | 转发 |
| m3u8 的服务器没有允许跨域，且浏览器不支持原生 HLS | 转发 |
| 音源需要特殊请求头（落雪脚本给出的地址偶尔会有） | 转发 |
| 直连播放失败（防盗链、网络等） | 自动改为转发**再试一次** |

转发时的提示：

- 播放条标题下方显示「⇄ 已改为服务器转发（会消耗服务器流量）」；
- 流量芯片变为强调色并闪烁；
- 字节数计入「本页面」累计流量，以及设置页的月度统计（类别「转发」）。

不想产生任何转发流量时，到「设置 → 播放与在线音源」关闭「直连不了时自动经服务器转发」，这类音频会直接报错。

安全措施：

- 转发链接带 HMAC 签名和有效期（`link_ttl`），只有登录用户能用，不会变成开放代理；
- 服务器拒绝访问内网、本机和云厂商元数据地址（`100.100.100.200`）；
- 同时转发的音频流数量有上限（`max_concurrent`）；
- 设置了月额度并且已经超出时，转发会暂停（`traffic.block_proxy_over_quota`）。

### 6.4 为什么电台有时会消耗服务器流量

电台本身**不经过**我们的服务器：

- Radio-Browser 只是一个**电台目录**（名称、地区、风格、播放地址），它不提供音频；
- 真正的音频流来自每个电台自己的服务器，默认由**浏览器直接连接**，不消耗服务器流量。

只有浏览器**自己连不上**时，才需要服务器帮忙转发。最常见的原因是**混合内容**：

- 我们的网站是 https；
- 很多老电台只提供 http 地址（例如 `http://xxx:8000/stream`）；
- 浏览器规定 https 页面不能播放 http 音频，会直接拦截。

此时，唯一能播放的办法是让服务器去取 http 音频，再通过 https 发给浏览器。这一段就会消耗服务器流量。

**现在的处理顺序（2026-10-16 起）**：

1. 先试 **https 直连**：把地址里的 `http:` 换成 `https:` 直接播放。很多电台两种协议都支持，成功就不耗服务器流量（播放条短暂显示「正在尝试 https 直连…」）；
2. 9 秒内没有声音或报错，才**改用服务器转发**，并显示 ⇄ 提示；
3. 关闭「设置 → 直连不了时自动经服务器转发」后，第 2 步改为直接报错，**完全不产生转发流量**。

**怎样尽量不耗流量**：

- 电台列表勾选「**只看 https 电台**」：只列出 https 地址的电台，它们一定可以直连；
- 列表里带 `http` 小标签的电台，可能需要转发；
- 把常听的 https 电台加入收藏。

## 7. 落雪音乐自定义源（P3，已实现）

> ⚠️ 落雪源的底层来源是各大音乐平台，仅供个人自用和调试，风险由使用者自行承担。

1. 配置文件里默认已开启：

   ```yaml
   sources:
     lx:
       enabled: true
       platforms: [kw, kg, tx, wy, mg]   # 有搜索功能的平台：酷我、酷狗、QQ、网易、咪咕；不需要的删掉
       request_timeout: "15s"
       cache_ttl: "10m"
   ```

2. 安装脚本，两种方式任选：
   - 管理员在「设置 → 落雪音源脚本 → 上传脚本」选择 `.js` 文件；
   - 直接把文件复制到 `<paths.data>/lx-scripts/`（服务器上是 `/var/lib/bailing/lx-scripts/`），约 30 秒内自动加载；也可以点「重新加载」。
3. 在设置页查看每个脚本的状态：运行中 / 出错（附原因）/ 已停用，以及它声明支持的平台和音质。可以单独停用或删除。
4. 在音乐页「在线 → 落雪音源」中操作：
   - 选平台和音质，输入歌名或歌手搜索；
   - 点一首歌播放整个结果列表，或「下一首播放」；
   - 平台按钮变淡，表示没有脚本支持这个平台：能搜索，但播放会失败。
5. 音质：会用你选的音质；脚本不支持时，自动降到它支持的最接近的音质。
6. 歌词：在线歌曲的歌词按「歌名 + 歌手 + 时长」从 LRCLIB 查找。
7. 调试：
   - 脚本的 `console.warn` / `console.error` 输出到日志（`journalctl -u bailing`）；
   - `log_level: debug` 时，`console.log` 也会输出；
   - 修改脚本文件后会自动重新加载。

**为什么各平台的搜索要单独实现（以及之前为什么单独提到咪咕）**：

- 落雪的源脚本**只负责解析**：把「一首歌」变成「播放地址」，它不会搜索；
- 搜索是落雪桌面版**自带**的功能，每个平台的接口都不一样，所以 bailing 要为每个平台各写一个搜索适配器；
- 酷我、酷狗、QQ、网易的接口比较直接，最先完成；咪咕的搜索接口要求每次请求带**签名**（用固定盐值和时间戳计算 md5），之前没有实现，所以单独列为未完成；
- **2026-10-16 已补上咪咕搜索**，搜索结果带 `copyrightId` 等字段，咪咕的源脚本可以直接解析。

工作原理（详见架构文档第 7 节）：

- **搜索**：bailing 内置了各平台的搜索（移植自落雪桌面版的做法）。
- **解析**：搜到的歌曲交给脚本解析出播放地址，脚本运行在服务器上的 JavaScript 沙箱（goja）里。
- **播放**：得到的地址由浏览器直连播放，只有必要时才转发。
- **缓存**：解析结果缓存 10 分钟。

兼容的脚本接口：

- `globalThis.lx` 上的 `EVENT_NAMES`、`on`、`send`、`request`、`version`、`env`、`currentScriptInfo`；
- `lx.utils.crypto`（md5、aesEncrypt、rsaEncrypt、randomBytes）、`lx.utils.buffer`、`lx.utils.zlib`；
- 常用的浏览器全局对象：`setTimeout`、`fetch`、`atob`、`btoa`、`TextEncoder`、`Buffer`。

脚本**不能**访问文件系统，也不能访问内网地址。

**音质（播放和下载都用「设置 → 播放 → 落雪音质」或在线页的音质选择）**：

- 同一首歌在不同脚本里拿到的音质可能不同。实测（2026-10-26，「狂野之城」）：有的脚本对网易云不论要什么音质都只给 128k（约 3MB），有的对酷我要 flac 给的是 320k；同一平台换一个脚本就能拿到 320k（约 8MB）或 flac（约 22MB）。
- 所以 bailing 会**检查脚本给的文件大小**：搜索结果里带有平台给出的各音质文件大小，服务器探测链接时拿到实际大小，两者相差 15% 以内就认定是那个音质。实际音质低于你要的，就**继续问下一个已启用的脚本**；所有脚本都给不了更好的，才用其中最好的一个，并在下载时提示「已下载 128k：音源没有提供更高的音质」。
- 因此**建议同时启用 2～3 个脚本**。落雪桌面版能下到更大的文件，通常是因为它用的是另一个脚本或另一个平台的同名歌曲；在 bailing 里换平台（酷狗 / 网易 / 酷我…）搜索同一首歌也可以。
- 平台本身没有该音质（搜索结果里没有那一档）时，自动用该歌曲最好的一档。

## 8. 收藏、歌单与音效

### 8.1 收藏与歌单（跨设备）

- 歌曲行、在线搜索结果、电台、播放条上都有 ☆（收藏）和「加入歌单」按钮；
- 「音乐 → 我的」里有三栏：**收藏的歌**、**收藏的电台**、**歌单**；
- 歌单可以新建、重命名、删除，歌曲可以上移、移除；「把播放列表存为歌单」会把当前队列整个保存下来；
- 本地歌曲、在线歌曲（落雪源、链接）和电台可以放进同一个歌单；
- 全部保存在服务器上（每个账号各自一份），换设备登录后都在。

### 8.2 音效与定时（播放条右侧的 ⚙ 按钮）

| 功能 | 说明 |
|---|---|
| 播放速度 | 0.75× ～ 2×，变速不变调；电台直播固定 1× |
| 定时停止 | 15 / 30 / 45 / 60 / 90 分钟，或「放完这首」；最后 8 秒音量渐弱；按钮上显示剩余分钟数 |
| 均衡器 | 10 段（31 Hz ～ 16 kHz，±12 dB），预设：原声、低音、人声、流行、摇滚、爵士、古典、高音，也可以手动拖动 |
| 频谱 | 显示在音乐页唱片下方 |

速度、均衡器和频谱的设置会跟随账号同步。

**均衡器和频谱的限制**（浏览器的安全规定）：

- 它们需要读取音频数据。本地音乐和经服务器转发的音频总能用；
- 直连的在线歌曲和电台，只有在对方服务器**允许跨站读取**（CORS）时才能直连；不允许的会改走服务器转发（提示「已开启音效……改为服务器转发」），会消耗服务器流量；
- 关闭均衡器和频谱后，要**刷新页面**才会恢复直连（浏览器不允许把已经接入音效的播放器再拆开）。
- 不想因为音效产生转发流量，就只在听本地音乐时打开。

## 9. 安装到手机 / 电脑（PWA）

网站可以「安装」成一个独立的应用：

- **iPhone / iPad（Safari）**：分享 → 添加到主屏幕；
- **Android（Chrome）**：菜单 → 安装应用（或地址栏的安装提示）；
- **电脑（Chrome / Edge）**：地址栏右侧的「安装」图标。

安装后的应用有独立窗口和图标，打开更快。离线时只能打开界面本身：照片、音乐和数据都直接从服务器获取，**不会缓存在手机里**。部署新版本后，应用顶部会出现「有新版本 · 刷新」。

### 9.1 相册和音乐分开发布

同一份代码可以构建三个版本：

| 版本 | 构建命令 | 应用名 |
|---|---|---|
| 完整版 | `make build-linux`（默认 `APP=all`） | 百灵 |
| 只有相册 | `make build-linux APP=photos` | 百灵相册 |
| 只有音乐 | `make build-linux APP=music` | 百灵音乐 |

- 不同版本的图标、名称、启动页不同，导航里只出现对应的部分；
- 服务器端还可以用配置关闭某一部分（关闭后对应的接口、扫描都不运行）：

  ```yaml
  features:
    photos: true
    music: false
  ```

- 两个开关同时生效：页面上只显示「构建版本」和「服务器配置」都允许的部分；
- 想在同一台服务器上同时提供「百灵相册」和「百灵音乐」两个应用：部署两个实例（不同端口、不同 `base_path` 或子域名、各自的 `features`），分别构建对应版本即可。

## 10. 桌面端（Windows / macOS，Wails）

> **`make build-windows` 不是桌面端**（2026-10-27 记录）。它产出的 `bailing-windows-amd64.exe` 是**服务器程序**，和 Linux 上跑的是同一个东西，只是编译成 Windows 版：运行后在命令行窗口里监听端口，没有界面，要用浏览器打开 `http://localhost:8080`。
>
> 真正带窗口、双击就打开、不需要浏览器的桌面端，要用 **`make desktop`**（Wails v2）构建，产物在 `desktop/build/bin/`。
>
> | 命令 | 产物 | 运行方式 |
> |---|---|---|
> | `make build-windows` | `dist/bailing-windows-amd64.exe` | `bailing-windows-amd64.exe serve -config config.yaml`，再用浏览器访问 |
> | `make desktop APP=music` | `desktop/build/bin/bailing-music.exe`（macOS 是 `.app`） | 双击，直接出现「百灵音乐」窗口 |
>
> 构建桌面端需要：Wails CLI（`go install github.com/wailsapp/wails/v2/cmd/wails@latest`）、cgo（macOS 装 Xcode 命令行工具）、Windows 上的 WebView2（Win10/11 自带）。macOS 版只能在 Mac 上构建；**Windows 版可以在 Mac 上直接交叉编译**：`make desktop-windows APP=music` → `desktop/build/bin/bailing-music.exe`，拷到 Windows 双击运行（首次运行 SmartScreen 拦截时点「更多信息 → 仍要运行」）。也可以在 Windows 上用 `.\scripts\build.ps1 desktop -App music`。详见开发指南 5.4。
>
> 桌面端窗口里显示的仍然是同一套网页界面（内嵌了一个只监听本机的 bailing 服务），只是不需要自己开浏览器、没有地址栏，有独立图标和窗口。

桌面端是一个原生窗口，内置完整的 bailing 服务（只监听本机、免登录），直接读取本机的照片和音乐文件夹，不经过 ECS，不产生服务器流量。

- 同样有三个版本：百灵、百灵相册、百灵音乐，可以只装其中一个；
- 缩略图和 HEIC 同样需要本机安装 libvips（见 2.2）。

### 10.1 桌面端的配置文件（2026-10-27 记录）

服务器版靠 `serve -config <文件>` 指定配置；**桌面端没有命令行参数**，双击启动时自动去**固定位置**找配置文件，第一次启动时没有就自动生成一份默认的：

| 版本 | Windows | macOS |
|---|---|---|
| 百灵（all） | `%AppData%\Bailing\` | `~/Library/Application Support/Bailing/` |
| 百灵相册（photos） | `%AppData%\Bailing Photos\` | `~/Library/Application Support/Bailing Photos/` |
| 百灵音乐（music） | `%AppData%\Bailing Music\` | `~/Library/Application Support/Bailing Music/` |

`%AppData%` 一般是 `C:\Users\<用户名>\AppData\Roaming`，在资源管理器地址栏输入 `%AppData%\Bailing Music` 回车即可打开。每个目录里有：

| 文件 | 作用 |
|---|---|
| `config.yaml` | 和服务器版**同一种格式**（可参考 `config.example.yaml`），桌面端最常改的是 `paths` |
| `desktop.yaml` | 只属于桌面端：`remote_url`（远程模式）、`width` / `height`（窗口大小） |
| `data/` | 数据库 `bailing.db`、缩略图和封面缓存、落雪脚本（`config.yaml` 里 `data: "./data"`，相对于配置文件所在目录） |

**修改步骤**：先运行一次应用让它生成文件 → 关闭应用 → 用记事本编辑 `config.yaml` → 重新打开应用。改完路径后会自动扫描新目录。

生成的默认 `config.yaml`（以音乐版为例，Windows 上 `~` 会展开成 `C:/Users/<用户名>`）：

```yaml
server:
  listen: "127.0.0.1:47823"        # 固定本机端口，每个版本不同，一般不用改
  base_url: ""
paths:                             # 单个目录，或列表 ["D:/Music", "E:/More"]
  photos: "C:/Users/me/Pictures"
  music:  "C:/Users/me/Music"
  data:   "./data"                 # 相对于本文件所在目录
auth:
  mode: "none"                     # 本机单用户，免登录
features:
  photos: false
  music: true
sources:
  proxy: { enabled: true, max_concurrent: 4, link_ttl: "24h", allow_private: true }
  radio: { enabled: true }
  lx:    { enabled: true, scripts_dir: "", platforms: [kw, kg, tx, wy, mg] }
```

常见修改：

```yaml
paths:
  music: ["D:/Music", "E:/歌曲/华语"]   # 多个音乐目录；Windows 路径一律用正斜杠 /
imaging:
  vips_bin_dir: "C:/tools/vips/bin"    # 装了 libvips 但没加入 PATH 时（相册版需要）
lyrics:
  save_beside_music: false             # 不想在音乐目录里生成 .lrc 时
```

- 没写的配置项用默认值，可以从 `config.example.yaml` 里复制任意一节（`lyrics`、`scan`、`imaging`…）过来；
- 桌面端只监听 `127.0.0.1`、免登录，`server.listen` / `auth` 不要改成对外开放；
- `features` 只能关掉、不能打开版本本身没有的部分（音乐版改 `photos: true` 无效）；
- 想从零开始：关闭应用，删掉整个目录（会清空收藏、歌单、统计）；
- **远程模式**：在 `desktop.yaml` 里写 `remote_url: "https://music.example.com"`，窗口就直接打开你的家庭服务器（相当于一个独立的浏览器窗口），这时 `config.yaml` 不起作用；
- 配置写错导致打不开时，从命令行启动可以看到报错：Windows 在 PowerShell 里运行 `.\bailing-music.exe`。

构建方法见开发指南第 5.4 节。

## 11. 常见问题

| 现象 | 可能原因与处理 |
|---|---|
| 页面显示「bailing API is running … front end has not been built」 | 编译 Go 之前没有构建前端。用 `make build-linux`（会先构建前端），不要直接 `go build` |
| 手动运行提示 `Permission denied`，`sudo` 运行提示 `command not found` | 可执行文件没有执行权限（scp / 网页上传都会丢掉权限）：`chmod 755 bailing-linux-amd64`。仍然不行时用 `findmnt -T <所在目录> -o OPTIONS` 看是否有 `noexec` |
| `systemctl status` 一直在重启，看不出原因 | `journalctl -u bailing -n 50 --no-pager` 看真正的错误。`status=203/EXEC`：没有执行权限，或者放在 `/home` 下却设了 `ProtectHome=true`（改成 `read-only`）；`217/USER`：没有 `bailing` 用户（见 2.1）；`226/NAMESPACE`：`ReadOnlyPaths` / `ReadWritePaths` 里有不存在的目录 |
| 服务起来了但报 `permission denied` / `readonly database` | `bailing` 用户对目录没有权限：数据目录和音乐目录要 `chown -R bailing:bailing`；放在 `/home/xxx` 下时上级目录要允许进入（`chmod o+x /home/xxx …`）；`paths.data` 必须在 `ReadWritePaths` 里 |
| 子路径模式下页面空白、资源 404 | 构建时没带 `BASE_PATH=/bailing`，或者 `base_path` 和 nginx 的 location 不一致 |
| HEIC 照片一直「冲洗中」或显示裂图 | 服务器的 vips 不支持 heif（见 2.2）；或者正在排队生成，稍后刷新 |
| 第一次打开相册很慢 | 缩略图在首次扫描后逐张生成；1 核机器上每张 HEIC 大约 0.5～2 秒 |
| 进度条拖不动 | 检查 `/media/` 是否设置了 `proxy_buffering off`；`curl -I -H "Range: bytes=0-1" -b "bl_session=<cookie>" https://域名/media/track/<id>` 应该返回 206 |
| 流量统计比阿里云账单少 | 正常，见第 4 节的协议开销说明；差距明显时，检查是否有其他服务共用这台机器 |
| 登录后马上又要求登录 | `base_url` 是 https，但实际用 http 访问（Cookie 带 Secure 标志）；或者 nginx 没有传 `X-Forwarded-Proto` |
| 登录提示「尝试次数太多」 | 被限速锁定了，15 分钟后再试，或重启服务 |
| 内存占用高 | 查看 `systemctl status bailing`；`imaging.workers` 保持为 1；确认 swap 已启用 |
| 歌词对不上 | 点「重新查找」；或「换一个歌词」手动搜索（5.4）；或放一个同名 `.lrc` |
| 本地歌曲没有歌词 | 先看曲库里歌名和歌手是否正确（没有标签的文件靠文件名猜，见第 1 节）；歌名、歌手错了 LRCLIB 也查不到，可以在歌词面板点「手动搜索歌词」自己填关键词（5.4） |
| 落雪歌曲的歌词存在哪 | 不落盘：自动查到的只在内存缓存 6 小时；手动选择的存在数据库（5.3） |
| 数据目录里有 `bailing.db-wal`、`bailing.db-shm`，停服务后消失 | 正常，是 SQLite 的 WAL 日志和共享索引，见 2.8。不要手动删除 |
| `make build-windows` 的 exe 打开还是要用浏览器 | 那是服务器程序；带窗口的桌面端用 `make desktop` 构建（第 10 节） |
| 「歌手」标签下出现歌名 | 文件没有标签，文件名顺序被猜反了。给文件补标签，或保证同目录有带标签、同样命名的文件，然后重新扫描 |
| 在线歌曲搜得到、放不了，提示「播放链接打不开」 | 落雪音源脚本只负责给出播放链接，链接指向脚本作者自己的服务器。服务器停了，脚本照样「已启用」，但所有平台都放不了。换一个音源脚本（设置 → 落雪音源）；bailing 会先试一下链接，失效时自动换下一个已启用的脚本 |
| 在线搜索结果没有封面 | 封面先由浏览器直接从各平台图床加载；加载失败时（实测酷狗、网易图床在部分网络、代理或广告拦截插件下会失败）自动改由服务器转发（`/media/rcover`，只允许各音乐平台的图床域名，单张 ≤ 3MB，计入「封面」流量）。仍然没有封面：服务器也访问不了该图床，或者这首歌本来就没有封面 |
| 下载按钮提示「下载失败」 | 落雪歌曲：播放链接已失效（同上一条「播放链接打不开」）或是 HLS 流（不能下载）；本地歌曲：文件已被删除，重新扫描即可 |
| 相簿书架上的相簿名被挡住 | 已修复：封面照片固定为方框，过长或过窄的照片会裁切，不再把名字挤出封面 |
| 找不到设置页 | 右上角圆形头像 → 设置 |
| 想加多个相册 / 音乐目录，设置页里找不到 | 目录只能在配置文件里改（第 1 节「多个目录」），改完重启服务；设置页「媒体库」只显示当前生效的目录和是否存在。出于安全考虑，网页上不能修改服务器目录 |
| 相簿的关闭按钮点不到、底部按钮看不到 | 旧版本中相簿被顶栏和播放条盖住了，已修复；升级后如果还是旧界面，刷新页面或点「新版本」提示 |
| `nginx -t` 报 `http2` 相关错误 | nginx 1.25 及以上写 `listen 443 ssl;` 加 `http2 on;`；1.18 写 `listen 443 ssl http2;` |
| 落雪歌曲选了 320k / flac，下载的还是 3MB 左右 | 旧版本直接用第一个脚本返回的链接，有的脚本会悄悄降成 128k。新版本按文件大小识别实际音质，不够时自动换下一个脚本（第 7 节「音质」）；只启用了一个脚本时换不了，多启用几个，或换一个平台搜索 |
| 怎么改密码 | 「设置 → 账号 → 修改密码」；忘记密码由管理员在服务器上运行 `bailing user passwd 用户名 -config …`（2.6） |
| 手机上音乐页右边有一截在屏幕外，电台页加载后被撑宽 | 旧版本中长电台名、地区下拉框把单列布局撑宽了，已修复：手机上搜索框单独一行，「全部 / 收藏」和地区下拉框在一行，长文字省略显示 |

