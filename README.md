# 录屏工坊（ScreenRec）

[English](README.en.md) · MIT License · Windows 10 2004+

**[下载最新版 v0.8.0](https://github.com/zhaogegejiafuhao/screenrec/releases/latest)**
（`setup-0.8.0.exe` 安装版 / `screenrec-0.8.0-win64.zip` 免安装）

本地录屏 / 录音 + 自动语音转文字。Go 单文件程序 + 内置网页界面，
识别完全跑在本地 CPU 上（SenseVoice），不联网、不上传。

## 功能

- **录屏**：DXGI 桌面复制（`ddagrab`）抓屏，H.264 编码，支持 30/60fps、三档画质
- **窗口录制**：只录某个应用窗口。硬件加速的窗口（浏览器、微信、Electron 应用）抓不到画面时
  自动改成「抓整屏再裁剪」，不会录出一片黑
- **自动分段**：长时间录制每 30/60 分钟切一个文件，段内可单独播放
- **录音**：仅录麦克风，适合会议、口述、访谈
- **录系统声音**：把电脑里播放的声音（对方说话、视频声）也录进来，与麦克风混音；**戴蓝牙耳机也能录**；这些声音同样进实时字幕
- **麦克风体检**：一键采集 1.2 秒，给出峰值/均值 dB 和**可执行的修复建议**
- **增益补偿**：0~+30dB，用 `volume` + `alimiter` 防削波，专治笔记本内置麦声音太小
- **硬件编码自动探测**：优先 NVENC，失败自动回退 libx264（状态栏显示当前用哪个）
- **流式字幕**：Paraformer 流式识别，**0.2~0.5 秒出草稿**；断句后用 SenseVoice 精校（带标点）
- **悬浮控制条**：豆包式置顶条，自带**开始 / 结束 / 关闭**按钮，半透明、鼠标穿透、不进任务栏
- **会议自动提醒**：检测到腾讯会议/钉钉/飞书/Zoom 等正在占用麦克风时，自动弹条问你"要开始录音转写吗"
- **单实例互斥**：同一台机器只允许一个录制进程，重复启动会唤醒已有实例
- **授权确认**：首次录音前必须确认"已取得在场人员同意"，网页端与悬浮条各有一套
- **说话人分离**：一键区分"谁在说话"（pyannote 分段 + 3D-Speaker 声纹 + 快速聚类）
- **整段精校**：停止后自动用 VAD 重新分段转写，准确率更高
- **时间戳**：每段带起止时间，点一下跳到对应画面
- **纪要导出**：Markdown / Word 一键生成，按说话人合并成段、带时间轴，可选「公式整理」
- **日历到点录制**：读 Outlook / ICS / 订阅链接，会前提醒或直接开录，会后自动停
- **AI 纪要**：接 OpenAI 兼容接口（DeepSeek / 通义 / 本地 Ollama），输出议题 / 结论 / 待办；**没 Key 时自动退到本地规则抽取**
- **片段导出**：框选起止点裁一段，可把字幕烧进画面，直接发出去
- **MCP server**：把录制、文稿、纪要、待办、剪辑暴露给 Claude Desktop / Cursor 等 AI 客户端
- **实时双语**：断句即译，悬浮条上下两行；本地 MarianMT，热态 0.3 秒/句
- **声音克隆配音**：对接本机 GPT-SoVITS，把转写用你的音色读出来
- **双语字幕**：本地 MarianMT 离线翻译（英→中 / 中→英），术语原样保留，可导出双语 SRT
- **托盘与自启**：无控制台窗口，托盘右键可开界面/开关录音/退出，支持开机自启
- **导出**：TXT 全文 / SRT 字幕
- 历史记录本地持久化，媒体支持拖进度（HTTP Range）

## 快速开始

**装好即用**：双击 `setup.exe`（从 Releases 下载），装到当前用户目录，自动建快捷方式，
首次可顺手下载模型，不需要管理员权限。卸载在「设置 → 应用」里。

**绿色版**：解压 `screenrec-x.y.z-win64.zip`，双击 `screenrec.exe`。

**从源码跑**：

```bash
# 发布版：无控制台窗口，自动开浏览器
go build -ldflags "-H windowsgui" -o screenrec.exe .
./screenrec.exe

# 调试版：保留控制台输出
go build -o screenrec-debug.exe .
./screenrec-debug.exe -open=false
```

双击 `screenrec.exe` 即可：托盘出现图标、浏览器自动打开 `http://127.0.0.1:8790`。
选麦克风 → **点一次「检测麦克风电平」** → 点「开始录制」→ 停止后自动转写。

命令行参数：`-addr`、`-port`（默认 8790）、`-data`（默认 ./data）、
`-assets`（asr.py / models / pylibs 所在目录，默认当前目录）、`-open`。

## 从源码构建

本仓库只含源码，**不含模型权重和 FFmpeg**。

### 依赖

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | 1.22+ | 编译只需标准库，无第三方模块 |
| FFmpeg / ffprobe | 6.0+ | 需含 `dshow`、`gdigrab`、`ddagrab` |
| Python | 3.9 ~ 3.11 | `pip install sherpa-onnx numpy soundcard torch transformers sentencepiece` |

### 步骤

```powershell
# 1. 拉模型（约 1.1GB，可加 -SkipTranslate / -SkipStream 只装需要的）
powershell -ExecutionPolicy Bypass -File scripts\fetch-models.ps1

# 2. 编译（发布版无控制台窗口；想保留控制台就去掉 ldflags）
go build -ldflags "-H windowsgui" -o screenrec.exe .

# 3. 运行
.\screenrec.exe
```

> 前端（`web/index.html`、`app.js`、`style.css`）是用 `go:embed` 编进二进制的，
> 改完要重新 `go build` 才生效——只刷新浏览器看不到变化（这一条是实测踩出来的）。

### 指定 FFmpeg / Python 的三种方式

1. 放进 `PATH`（最简单）
2. 设环境变量 `SCREENREC_FFMPEG`、`SCREENREC_FFPROBE`、`SCREENREC_PYTHON`
3. 放到 exe 同目录：`ffmpeg\bin\ffmpeg.exe`、`runtime\python.exe`

`-assets` 指向 `asr.py` / `models` / `pylibs` 所在目录，默认就是 exe 所在目录。
`pylibs` 可以不用——只要 Python 环境里装了上面那五个包。

## 麦克风没声音？先做这一步

点左侧「🎤 检测麦克风电平」，对着麦克风正常说话 1 秒，看诊断结果：

| 峰值 | 含义 | 处理 |
|---|---|---|
| < -60 dB | 完全没信号，麦克风被静音 | 设置 → 系统 → 声音 → 输入，取消静音 |
| -60 ~ -45 dB | 有信号但极弱（笔记本内置麦常见） | 输入音量调到 80+，或把「麦克风增益补偿」设为 +18/+24 dB |
| -45 ~ -32 dB | 偏小，识别率会下降 | 调高音量或加 +12 dB 增益 |
| > -2 dB | 过大，可能削波 | 调低输入音量 |

实测：本机内置麦原始峰值 -49.4 dB，加 +30 dB 后录音均值从 -61 dB 提到 **-31.7 dB**，
实时字幕立刻出字。增益链路是 `volume=XdB,alimiter=limit=0.95`，限制器防爆音。

### 依赖

| 组件 | 说明 |
|---|---|
| ffmpeg | 必需。自动在 exe 同目录、PATH 中查找，可用 `SCREENREC_FFMPEG` 指定 |
| Python 3.9 + sherpa-onnx | 识别用。默认复用 GPT-SoVITS 自带的 runtime，可用 `SCREENREC_PYTHON` 指定 |
| SenseVoice 模型 | `models/sensevoice/model.int8.onnx`（239MB）+ `tokens.txt` |
| Silero VAD | `models/silero_vad.onnx`（644KB），用于切分语音段 |

安装识别依赖：

```bash
# 1) 安装 sherpa-onnx 到独立目录（不污染原环境）
python -m pip install --target ./pylibs -i https://pypi.tuna.tsinghua.edu.cn/simple sherpa-onnx

# 2) 下载模型（ModelScope，国内快）
curl -L -o models/sensevoice/model.int8.onnx \
  "https://www.modelscope.cn/models/pengzhendong/sherpa-onnx-sense-voice-zh-en-ja-ko-yue/resolve/master/model.int8.onnx"
curl -L -o models/sensevoice/tokens.txt \
  "https://www.modelscope.cn/models/pengzhendong/sherpa-onnx-sense-voice-zh-en-ja-ko-yue/resolve/master/tokens.txt"
curl -L -o models/silero_vad.onnx \
  "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx"
```

## 它是怎么工作的

### 一、录屏：三条采集路径

| 路径 | 原理 | 本项目 |
|---|---|---|
| `ddagrab` | DXGI Desktop Duplication，GPU 直接抓帧 | ✅ 全屏录制 |
| `gdigrab -i hwnd=0x...` | GDI 按窗口句柄抓取 | ✅ 窗口录制 |
| Windows Graphics Capture | Win10 1803+ 新 API，可精确指定窗口 | 未用（需原生代码） |

窗口列表通过 `EnumWindows` 枚举（过滤工具窗口、本进程、过小窗口），拿到 HWND 后交给
gdigrab。**坑**：窗口尺寸经 DPI 缩放后可能是奇数（实测 1305×1003），libx264 要求偶数，
必须在滤镜里加 `scale=trunc(iw/2)*2:trunc(ih/2)*2`。

**关键坑**：ddagrab 输出的是 D3D11 硬件帧，必须 `hwdownload,format=bgra` 转回系统内存才能
交给 libx264；直接接 `h264_nvenc` 在本机会话下会报 `-22 Invalid argument`（实测）。

录制命令等价于：

```bash
ffmpeg -f lavfi -i "ddagrab=0:framerate=30" \
       -f dshow -i "audio=麦克风阵列" \
       -map 0:v -vf "hwdownload,format=bgra" -c:v libx264 -preset veryfast -crf 23 -pix_fmt yuv420p \
       -map 1:a -c:a aac -b:a 160k out.mp4 \
       -map 1:a -f s16le -ar 16000 -ac 1 -flush_packets 1 out.pcm
```

**关键坑 2**：`-flush_packets 1` 必须加在 PCM 输出上。ffmpeg 默认缓冲 256KB（16k 单声道约 8 秒）
才落盘，实时字幕会滞后 8 秒以上；加了之后每 2 秒稳定写入。

**关键坑 3**：多输出时 `-map` 的输入序号随模式变化（录屏时音频是输入 1，纯录音时是输入 0）。
写错会得到 `Stream map '0:a' matches no streams`。

### 二、音频采集：DirectShow

Windows 上 ffmpeg 只能通过 `dshow` 拿设备，枚举命令：

```bash
ffmpeg -f dshow -list_devices true -i dummy
```

**录不到电脑内部声音**是 Windows 的设计限制：dshow 没有 loopback 设备。两条路：

1. 启用「立体声混音」（声音设置 → 录制 → 显示禁用设备）；Intel 智音等驱动常常没有这一项
2. 装免费的 **VB-CABLE** 虚拟声卡，系统输出设为 CABLE Input，录制设备选 CABLE Output

装好后会出现在应用的「麦克风」下拉里，无需改代码。

### 三、语音识别：流式草稿 + 批量精校

```
麦克风 ──dshow──▶ ffmpeg ──┬──▶ mp4/m4a（存档）
                          └──▶ PCM 16k 单声道（32KB/s，实时写盘）
                                     │
                     ┌───────────────┴────────────────┐
                     ▼                                ▼
          实时通道（每 0.2 秒喂一次）           整段通道（停止后）
          Paraformer 流式 → 草稿                asr.py + Silero VAD
          端点检测断句 → SenseVoice 精校        （按静音切分，逐段识别）
                     ▼                                ▼
          亚秒出字 + 带标点的准确结果          全量精确文本 + 时间戳
```

**混合模式是这里最关键的取舍**：流式 Paraformer 快（0.2~0.5 秒出字），但准确率略低
（实测「想说什么」→「小说什么」）且不带标点；SenseVoice 准但只能整段跑。
所以用流式出草稿给即时反馈，端点检测一断句就立刻用 SenseVoice 把**这一句**重解码，
既保住亚秒延迟，又拿到带标点的准确文本。

- **模型**：实时 Paraformer streaming（encoder 165MB + decoder 72MB，INT8）；
  精校 SenseVoice（239MB）；两者都在 CPU 跑
- **端点检测**：`rule2_min_trailing_silence=1.2s`，即说完停顿 1.2 秒断句
- **实测**：12.6 秒语音，草稿在 0.2~0.5 秒内出现，断句后 1.2 秒得到带标点全文
- **VAD**：Silero，必须按 **512 样本小窗口**喂，一次性灌整段会切错（识别变乱码）
- **回退**：流式进程异常时自动退回 2.5 秒分块 + 0.3 秒重叠的旧路径

### 四、说话人分离

```
音频 ──▶ pyannote segmentation-3.0（1.5MB，谁在什么时候说话）
      ──▶ 3D-Speaker ERes2Net（40MB，提取声纹向量）
      ──▶ 快速聚类（阈值 0.6，自动决定人数）
      ──▶ 与转写分段做时间重叠匹配 → 标注「说话人 N」
```

点「区分说话人」后运行，结果按时间重叠度大于 0.05 秒归属到对应句子。

### 五、悬浮字幕条（照豆包做的）

**先逆向，再复刻。**豆包「录音转写」的完整流程、文案、色值和动画参数都从它的安装包里挖出来了，
详见 [`doubao-recording-analysis.md`](doubao-recording-analysis.md)。

复刻的是**视觉与交互规范**，不是抄代码：

| 元素 | 豆包规格 | 我们的实现 |
|---|---|---|
| 声波 | 7 根 2px 竖条，间距 1.5px，高 4→16px，圆角 4px，色 `#474746`，`height .12s ease-in-out` | 一致（比例 `[.3,.6,.9,1,.9,.6,.3]`，加正弦微抖） |
| 底色 | `#f9fafb` | `#FAFAFA` + 1px `#E8E8E9` 边框 |
| 强调色 | 豆包蓝 | `#3370FF`（草稿文字 / 空闲蓝点） |
| 圆角 | 8px（输入框胶囊） | 12px（整条字幕） |
| 关闭 | 20px 按钮 | 44×96 热区 + GDI 画的 × |

实现要点（全部 syscall，无 cgo、无 WebView2）：

| 需求 | Win32 做法 |
|---|---|
| 置顶 | `WS_EX_TOPMOST` |
| 半透明 | `WS_EX_LAYERED` + `SetLayeredWindowAttributes(249)` |
| 圆角 | `CreateRoundRectRgn` + `SetWindowRgn` |
| 鼠标穿透（仅按钮可点） | **不用** `WS_EX_TRANSPARENT`，改在 `WM_NCHITTEST` 返回 `HTTRANSPARENT`，仅关闭按钮区返回 `HTCLIENT` |
| 不抢焦点 | `WS_EX_NOACTIVATE` + `SW_SHOWNOACTIVATE` |
| 画字 | GDI `CreateFontW` + `DrawTextW`（先 `DT_CALCRECT` 量高再垂直居中） |
| 全局热键 | `RegisterHotKey(Ctrl+Alt+Space)` |

**四个坑**：
1. **`WM_NCHITTEST` 是屏幕坐标**，`WM_LBUTTONUP` 是客户区坐标，混用会导致按钮永远点不中。
2. **DPI**：不声明感知时 `GetSystemMetrics` 返回逻辑像素，窗口被放大 1.25 倍；
   这也解释了窗口录制的奇数尺寸（1305×1003 = 125% 缩放）。
3. **`SetTextAlign(TA_CENTER)` 与 `DT_CENTER` 叠加**会把文字推出窗口。
4. GDI 不支持彩色 emoji，字幕里别放表情。

### 六、悬浮条上的按钮（开始 / 结束 / 关闭）

豆包把录音入口放在输入框里，我们放在**字幕条自己的右侧**——这样不切窗口就能操作。

| 状态 | 左侧指示 | 文案 | 按钮 |
|---|---|---|---|
| 空闲 | 蓝色圆点 | 点「开始」或按 Ctrl+Alt+Space 说话 | `开始` + `×` |
| 录音中 | 7 根声波（随电平跳动） | 实时字幕 | `结束`（红） + `×` |
| 会议提醒 | 橙色圆点 | 「腾讯会议」正在通话，要开始录音转写吗？ | `忽略` + `开始` + `×` |
| 授权确认 | 橙色圆点 | 录音前请确认：已取得在场人员的录音同意… | `取消` + `同意并开始` + `×` |

实现要点：

- **布局与命中判定共用同一个 `layout()`**，避免"画在这、点在那"的错位。
- **拖动**：左侧 2×3 圆点是拖动手柄，`WM_NCHITTEST` 对它返回 `HTCAPTION`，把移动交给系统的
  非客户区拖动循环（不用自己算鼠标增量）。注意整条默认是穿透的，只有手柄和按钮能接鼠标，
  所以**必须留一个可见的手柄**，否则用户会发现"拖不动"。
- **位置记忆**：`WM_EXITSIZEMOVE` 时把窗口矩形夹进屏幕并写进 `settings.json`（`overlayX/Y`），
  下次启动沿用；网页端有「复位位置」。夹紧规则是**整条保持在屏幕内**——
  只留 120px 的话，右侧的「开始 / 结束」按钮会跑到屏幕外，反而更糟。
- 双击手柄要显式吞掉（`WM_NCLBUTTONDBLCLK` 返回 0），否则系统会把它最大化。
- 命中测试用 `WM_NCHITTEST`：按钮区返回 `HTCLIENT`，其余返回 `HTTRANSPARENT`（穿透到下层窗口）。
  不能用 `WS_EX_TRANSPARENT`——那会让整个窗口（包括按钮）都收不到鼠标消息。
- 悬停用 `TrackMouseEvent(TME_LEAVE)` 收 `WM_MOUSELEAVE`，配合 `WM_SETCURSOR` 换成手型光标；
  手柄区换成四向箭头（`IDC_SIZEALL`）。
- 状态机在 `watchOverlay` 里只做**状态迁移**（`prev` 记上一次状态），不每帧重复 `ShowWindow`，
  否则会和用户点 × 关闭的操作打架。

### 七、会议自动提醒（本机能做到的最可靠信号）

豆包靠云端判断"正在会议中"；本地能用的信号有两个，我优先用第一个：

1. **注册表：谁正在占用麦克风**（和 Windows 任务栏那个麦克风小图标同源）

```
HKCU\Software\Microsoft\Windows\CurrentVersion\
  CapabilityAccessManager\ConsentStore\microphone\NonPackaged\
    D:#program#Doubao#app#Doubao.exe        ← 路径里的 \ 变成 #
      LastUsedTimeStart = FILETIME
      LastUsedTimeStop  = FILETIME          ← 0 表示"现在还在用"
```

判定：`Start != 0 && (Stop == 0 || Stop < Start)` → 该进程正在采集麦克风。全部用
`RegOpenKeyExW / RegEnumKeyExW / RegQueryValueExW` 直接读，不依赖第三方库。

2. **窗口标题兜底**：`正在会议中 / 语音通话中 / Zoom Meeting / 正在共享屏幕` 等保守关键词。

只有命中**已知会议应用**（腾讯会议 / 钉钉 / 飞书 / 企业微信 / 微信 / QQ / Zoom / Teams / Discord）
才提示；浏览器要再加会议标题才算；其他进程占用麦克风只写日志不打扰。

去抖与礼貌：连续命中 2 次（约 8 秒）才提示；同一应用 30 分钟内不重复；点「忽略」静默 30 分钟；
点 × 关闭时若有待处理提醒，一并静默（否则会被下一秒的轮询又顶回来）。

### 八、单实例互斥

豆包是"本机录音中 / 其他设备录音中 / 其他应用录音中"三档，其中**跨设备那档必须有账号和服务端会话**，
纯本地做不到。本机这一档做满：

- `CreateMutexW("Local\\ScreenRecStudio_SingleInstance_v1")`，第二个实例拿到
  `ERROR_ALREADY_EXISTS(183)` 就**不再启动**，改为 `POST /api/overlay` 唤醒已运行实例的悬浮条，
  再打开网页，然后自己退出。
- 录制进程本身也靠 `Recorder.cur` 保证同一时刻只有一路。
- "其他应用录音中"用上面的注册表信号感知，以会议提醒的形式呈现（不硬性阻止，
  Windows 本身允许多路共享采集）。

### 九、一个踩到的系统级坑：MessageBox 会被静默吞掉

原计划用 `MessageBoxW` 做首次录音的授权确认。实测这台机器上：

```go
r, _, err := MessageBoxW.Call(0, text, caption, MB_OKCANCEL|MB_ICONWARNING|MB_TOPMOST)
// 立刻返回 r=1 (IDOK)，屏幕上什么都没有
```

`MB_TOPMOST`、`MB_SETFOREGROUND` 都试过，对话框就是不显示（组策略/安全软件拦截模态框的典型表现）。
因此**授权确认不能依赖原生对话框**——否则等于"自动同意"，比不做还糟。

改成把确认画在悬浮条上（`stConsent` 状态），点「同意并开始」才落盘 `consentAccepted=true`。
好处是：置顶可见、和产品视觉一致、不依赖系统策略。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/state` | ffmpeg / Python / 显示器 / 音频设备 / 编码器 / 麦克风占用 / 设置 |
| GET | `/api/windows` | 可录制的窗口列表（hwnd / 标题 / 尺寸） |
| POST | `/api/overlay` | `{show, text, level}` 控制悬浮条 |
| GET | `/api/loopback` | 播放设备列表（含 `bluetooth` 标记）；进程级回环用伪设备名 `__process__` |
| GET | `/api/settings` | `{consentAccepted, meetingPrompt, mutedApps, overlayHidden}` |
| PUT | `/api/settings` | `{meetingPrompt, overlayHidden, acceptConsent, resetConsent, unmute}` |
| POST | `/api/record/start` | `{mode, screen, hwnd, mic, fps, crf, gainDb, segmentMin}` |
| POST | `/api/record/stop` | 停止并收尾 |
| GET | `/api/live` | 录制中的实时分段 |
| GET | `/api/recordings` | 记录列表 |
| GET/PUT/DELETE | `/api/recordings/{id}` | 详情 / 改名 / 删除 |
| POST | `/api/recordings/{id}/transcribe` | 重新转写 |
| POST | `/api/recordings/{id}/diarize` | 说话人分离 |
| GET | `/api/recordings/{id}/media` | 媒体（支持 Range） |
| GET | `/api/recordings/{id}/transcript?format=txt\|srt` | 导出 |
| GET | `/api/calendar` | 今天剩余日程 + 日历配置 + 读取错误 |
| POST | `/api/calendar/refresh` | 立刻重拉一次日历 |

## 实测数据（RTX 3050 4GB / i7 笔记本）

| 项目 | 结果 |
|---|---|
| 1080p30 录屏 | 12 秒 → 8.3MB，CPU 占用约 25%（libx264 veryfast） |
| 窗口录制 | Edge 窗口 1304×1002，13.7 秒 → 282KB，画面正确 |
| 自动分段 | 每段精确 60.000 秒（60s/60s/8.7s 三段），可单独播放 |
| PCM 实时落盘 | 2s→112KB、4s→192KB、8s→352KB，稳定不滞后 |
| 精校识别速度 | RTF 0.04，1 小时录音约 2.5 分钟转完 |
| 流式草稿 | 0.2~0.5 秒出字，断句后 1.2 秒得到带标点全文 |
| 说话人分离 | 双/三音色 24.5 秒测试音频，正确分出 3 人并标注到 12 个句子 |
| 悬浮字幕 | 1920×1080 下 1267×150 字幕条，两行居中渲染正常，鼠标可穿透 |
| 悬浮条按钮 | 点「开始」→ 1.2 秒后进入录制；点「结束」→ 录制落盘；点 × → 隐藏且状态持久化 |
| 拖动悬浮条 | 手柄拖动 (520,888)→(747,598) 精确跟手；往屏外拖自动夹到 (1040,258)，按钮不会跑丢 |
| 会议提醒 | 模拟腾讯会议占用麦克风：8 秒内弹条，点「忽略」后 30 分钟不再提示 |
| 单实例互斥 | 第二次启动 0 秒退出，并把已运行实例的悬浮条唤到最前 |
| 授权确认 | 未确认时点「开始」→ 悬浮条切到确认态；点「同意并开始」→ 落盘 `consentAccepted` 并开始录制 |
| 编码器探测 | 本机 NVENC 不可用 → 自动判定 libx264，状态栏可见 |
| 增益补偿 | 内置麦 +30dB：均值 -61dB → -31.7dB，实时字幕恢复出字 |
| 底噪抑制 | +30dB 增益下电平 55 但文本为空（VAD 门控），不产生幻觉文字 |

### 十、录制电脑里播放的声音（系统声音）

会议录音最大的坑不是识别，是**对方的声音根本进不来**——`dshow` 只能拿真实输入设备，
Windows 也没有给 ffmpeg 提供 WASAPI 回环输入。

实测可行路径：Python + `soundcard` 走 WASAPI loopback，纯用户态、不用装 VB-CABLE：

```python
sp  = [s for s in sc.all_speakers() if 'Realtek' in s.name][0]
mic = sc.get_microphone(id=str(sp.name), include_loopback=True)
```

**关键取舍：不在录制过程中实时混音。** 我先试过把 PCM 通过 stdin 喂给 ffmpeg 的
`pipe:0` 当第二路输入、用 `amix` 实时混——音频一供不上，整条 filter 图连同视频一起卡死：
录屏模式输出卡在 3.5 秒、停止要等 12 秒超时、mp4 直接缺 moov 头。

最后改成**先分开录、事后再混**：

```
麦克风/画面 ──ffmpeg──▶ rec.mp4（照旧，停止用 q）
                          │
系统声音 ──python──▶ rec.sys.pcm（48k 立体声 s16le，写文件）
                          ▼
停止后：ffmpeg -i rec.mp4 -f s16le -i rec.sys.pcm
        filter: [0:a]增益[mic]; [1:a]atrim=start=<lead>[sys]; amix duration=first
        -map 0:v? -c:v copy  ← 视频直接复制，秒级完成
```

- **对齐**：python 比 ffmpeg 早启动多少毫秒就用 `atrim` 裁掉多少（`READY <unix_ms>` 协议传时间）
- **视频不重编码**：`-c:v copy`，10 分钟的录屏混音也只要一两秒
- **先混音再转写**：转写跑在混好的文件上，所以**对方说的话也能转出来**
- **蓝牙耳机大概率不行**：AirPods 的回环返回全 0（实测 peak 0.0003），Realtek 内置扬声器正常（peak 0.213）。
  连续 5 秒全 0 会在记录上打一条警告提示换设备

**代价**：成片里的混音发生在停止之后（实时混音会把录制管线拖死，见上），但实时字幕能听到系统声音——
两条管道是分开的：一条写文件事后混音，一条按端点切句喂识别。整段精校的文本仍然最完整。

#### 蓝牙耳机怎么办：进程级回环

设备级回环（soundcard）对蓝牙耳机无效——实测向 AirPods 播放正弦波，回环 peak 只有 0.00037；
而同一时刻内置扬声器正常。原因是蓝牙不暴露 WASAPI 回环端点。

解法是 Windows 10 2004+ 的**进程级回环**（`AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK`，
OBS「应用音频采集」用的同一套 API）：它抓的是**进程树的音频流**，与输出设备无关。

用法：下拉里选「全部应用（推荐 · 戴蓝牙耳机也能录）」= 以 `EXCLUDE_PROCESS_TREE` 模式
排除本程序自己，等于录下所有应用的声音。

实现要点（纯 ctypes，无 C 编译器、无 comtypes 依赖）：

```
ActivateAudioInterfaceAsync("VAD\Process_Loopback", IID_IAudioClient,
    PROPVARIANT{ VT_BLOB, AUDIOCLIENT_ACTIVATION_PARAMS{1, pid, EXCLUDE} },
    completionHandler, &op)
  → GetActivateResult → IAudioClient::Initialize(LOOPBACK|EVENTCALLBACK, 48k/2ch/16bit)
  → SetEventHandle → GetService(IAudioCaptureClient) → Start → GetBuffer/ReleaseBuffer
```

四个必须踩对的点：
1. **`PROPVARIANT` 的 VT_BLOB**：`cbSize=12`（`ACTIVATION_TYPE` + `TargetProcessId` + `Mode`），
   `pBlobData` 指向这个 12 字节结构；结构布局必须是 24 字节（cbSize@8、pBlobData@16）。
2. **回调对象要应答 `IAgileObject`**：`QueryInterface` 只答 IUnknown / 完成接口会直接返回
   `E_UNEXPECTED (0x8000000E)`，看起来像"内存不足"，其实是跨 apartment 检查失败。
3. **`GetBuffer` 返回 `S_BUFFER_EMPTY (0x08890001)` 是正数**：只判断 `hr < 0` 会把上一包的
   `blocksize` 当成新数据反复写——实测 3 秒写了 2GB。必须 `hr != 0`。
4. 停止：结束 python 进程即可（写的是裸 PCM，截断无副作用）。

实测：向 AirPods 播放中文语音，录 8 秒 → 停止 5.1 秒完成混音+转写 → 文本
「我是你的专属声音。从今天起。你想让我说什么？我就说什么。」

### 十一、英文与数学公式

- **英文**：SenseVoice 自带中/英/日/韩/粤，`language="auto"` 自动判别，界面无需设置。
  实测自然语句一字不差；流式草稿英文会粗糙（`at three`→`ted three`），断句后由 SenseVoice 精校覆盖。
- **公式**：识别输出的是口语文字（「X的平方」），不是符号。需要一层规范化才能变成公式。
  `tools/mathnorm` 用纯 Go 规则做这件事：幂、根号、分数、积分上下标、运算符、`O(n²)`、`dX→dx`，
  并且不会误伤「这个方案等于白做」这类普通句子。
- **规则层的边界**：单字母仍会混淆（`z`→`c`，声学模型问题），矩阵/多行/上下标嵌套覆盖不了，
  需要 LaTeX 时得接大模型。

详细实测数据与复现方法见 `asr-english-math.md`。

### 十二、纪要导出（Markdown / Word）

转写只是原料，最终要的是能发出去的东西。详情页右上角有两个按钮：

- **导出 Markdown**：标题 + 时间/时长/说话人/来源 + 按说话人合并的段落 + 时间轴
- **导出 Word**：同样结构，说话人蓝色加粗、时间灰色、译文灰色斜体

实现上没有依赖任何库——`.docx` 本质就是一个固定结构的 zip，
用 `archive/zip` 手写 `[Content_Types].xml`、`_rels/.rels`、`word/document.xml`、`word/styles.xml`
就能生成 Word/WPS 都能打开的文件（实测 2.2KB，XML 全部可解析）。

两个细节：

- **按说话人合并**：连续同一说话人的句子合成一段，不是一句一行，读起来像纪要而不是字幕
- **文件名**：中文标题用 RFC 5987 的 `filename*=UTF-8''`，并给一个 ASCII 回退名，
  避免浏览器下载出乱码

勾选「公式整理」后，导出时会把口语公式转成符号（`x² + y² = c²`、`∫₀^∞`、`O(n²)`）。

### 十三、双语字幕（本地翻译）

英文会议的转写已经可用，但阅读成本高。这一版加了**完全离线**的翻译：

| 方向 | 模型 | 大小 |
|---|---|---|
| 英 → 中 | Helsinki-NLP/opus-mt-en-zh | 301MB |
| 中 → 英 | Helsinki-NLP/opus-mt-zh-en | 301MB |

MarianMT + PyTorch（复用 GPT-SoVITS 内置的 Python 3.9 运行时），CPU 推理：

- 冷启动（加载模型）约 **20 秒**
- 热态 3 句 **0.45 秒**，做实时字幕也够
- 方向自动判断：中文占比高就中译英，否则英译中

**术语保护**是这里最关键的一步。直接翻译会把技术名词毁掉：

```
原文：We use a Transformer architecture, and the complexity of attention is O(n squared).
直译：我们使用变形器结构, 关注的复杂性是O(n)正方形。
```

做法是翻译前把专有名词、全大写缩写、驼峰词、`O(...)` 换成 `Q0P` 这类占位符，
翻完再还原；`glossary.json` 可以自己加词。现在的结果：

```
我们使用Transformer结构，而attention的复杂性是O(n²)。
```

三个坑：

1. **占位符会被粘在一起**：`Z0Z Z1Z` 可能变成 `Z0ZZ1Z`。改成前后缀不同的 `Q0P`
   后，即使粘连也能用 `Q(\d+)P` 分别匹配还原。
2. **`[A-Z]{2,}` 加了 IGNORECASE** 会匹配所有单词，导致整句被当成缩写掩掉——必须大小写敏感。
3. **上标会被压平**：`x²` 送进去会变成 `x2`。纯公式句（字母少且含 ²/^/∫）直接跳过翻译，保留原文。

### 十四、托盘 / 无控制台 / 开机自启

发布版用 `-ldflags "-H windowsgui"` 编译，双击不再弹黑框：

- **托盘图标**：`Shell_NotifyIconW` + 右键菜单（打开界面 / 开始或结束录音 / 显示或隐藏悬浮条 /
  复位位置 / 退出）；监听 `TaskbarCreated` 广播，Explorer 重启后自动补图标
- **开机自启**：写 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，
  不需要管理员权限；网页里一个开关即可
- **日志**：GUI 版没有控制台，日志写 `data/app.log`。
  这里踩了一个坑：`io.MultiWriter` 在 stderr 句柄无效时会**直接返回**，文件那条也写不进去，
  必须用容错的双写实现
- **相对路径按 exe 目录解析**：否则开机自启时工作目录是 `System32`，
  `data` 会被建到系统目录里（实测真的建出来了）

### 十五、AI 纪要（可选，需要 API Key）

转写只是原料，纪要是成品。详情页点「AI 纪要」，会把带时间戳和说话人的转写发给
**OpenAI 兼容接口**，返回固定五段式：一句话总结 / 议题 / 结论 / 待办 / 风险与分歧。

- 支持 DeepSeek（默认）、通义、Kimi、本地 Ollama —— 改接口地址和模型名即可
- Key 存在 `data/settings.json`（明文，已 gitignore），只发文本、不发音频
- 转写超过 14000 字会截断，并在纪要末尾标注
- 导出 Markdown / Word 时，纪要自动排在正文前面
- 常见错误有明确提示：401 Key 无效、404 地址不对、429 额度用完

> 没有 Key 也能用其它所有功能。本地 4GB 显存跑不动可用的摘要模型，所以这一项必须外接。

### 十六、实时双语字幕

开启后，每句话断点会异步翻译，悬浮条变成两行：上行原文、下行译文（灰色小字）。

- 复用 `translate.py --serve` 常驻进程，热态实测 **0.30 秒/句**
- 冷启动要加载 301MB 模型，实测 49~84 秒；**开启时会在后台预热**，避免第一句干等
- 翻译方向按首句锁定（`auto` / 英→中 / 中→英），避免中英混说时反复换模型
- 只翻「已断句的最终结果」，不翻流式草稿——草稿会反复改，翻它只会闪

**电脑里播放的声音也有字幕**（`recorder.go` 的 `sysLiveLoop`）。默认情况下实时字幕只听麦克风，
看网课、刷抖音、开外放的会议时字幕条会是空的。现在的做法：

- 回环 PCM 是 48k 立体声，先做 3 点均值低通再 3:1 抽成 16k 单声道，才喂给识别引擎
  （直接把 48k 当 16k 用会导致时间轴快 6 倍、文字全乱——实测踩过）
- 按**能量端点**切句：攒够 0.35 秒人声且停顿 0.45 秒就切一刀，一直说不停则最多攒 5 秒。
  比固定 2.5 秒切块完整得多，延迟约 1 秒
- 麦克风和回环用同一条字幕流，靠 `src` 字段区分；两路文字相似度高（字符二元组 Jaccard ≥ 0.62）
  且时间接近时判为**外放回声**，只保留一条
- 字幕条的音量柱取两路电平的较大值，只看视频不说话时也会跳

### 十七、声音克隆配音

对接本机 GPT-SoVITS 的 HTTP 接口，把转写文本用你的音色读出来。

配置（左下角「配音设置」）：

| 项 | 说明 |
|---|---|
| 服务地址 | 默认 `http://127.0.0.1:9880`，先跑 `启动API.bat` |
| 参考音频 | 绝对路径，3~30 秒干音，服务端要能读到 |
| 参考文本 | 和参考音频**逐字一致**，差一个字都会影响相似度 |
| 语种 | zh / en / ja |

生成后写到 `data/recordings/{id}-tts.wav`，直接播放。单次上限 800 字，超出会截断并提示。

> 这一项依赖 GPT-SoVITS 单独安装（约 14GB），不是本仓库的依赖；不装不影响其它功能。

### 十八、打包发布

```powershell
go build -ldflags "-H windowsgui" -o build\screenrec.exe .
go build -o build\screenrec-mcp.exe .\tools\mcpserver
go run ./tools/release -version 0.4.0 -exe build\screenrec.exe -mcp build\screenrec-mcp.exe

# 单文件安装包：stub + 把发布 zip 追加到自己末尾 + 页脚
go build -o build\setup-stub.exe .\tools\installer
build\setup-stub.exe -pack dist\screenrec-0.4.0-win64.zip -out dist\setup-0.4.0.exe -version 0.4.0
```

输出两个产物：

| 文件 | 说明 |
|---|---|
| `dist/screenrec-0.4.0-win64.zip`（约 9MB） | 绿色版，解压即用；同名 `.sha256` |
| `dist/setup-0.4.0.exe`（约 13MB） | 安装包：单文件，双击就装，**不需要管理员权限** |

安装包做的事（`tools/installer`，纯 Go，不依赖 Inno Setup / NSIS）：

- 装到 `%LOCALAPPDATA%\Programs\ScreenRec`，`-dir` 可改
- 桌面 + 开始菜单快捷方式（WScript.Shell，走 `-EncodedCommand` 避免中文路径乱码）
- 写「应用和功能」卸载项；中文必须用 PowerShell 写，`reg.exe` 走 ANSI 代码页会变乱码
- 首次安装可顺手下载模型（1.1GB），也可以之后跑 `scripts\fetch-models.ps1`
- 卸载：`unins.exe` 会拉一个临时副本删整个目录（自己删不掉自己）
- 打包原理：`setup.exe = stub + zip + 48 字节页脚`。CI 里没有 zip 也能正常编 stub

### 十九、片段导出（剪辑 + 烧字幕）

录完一整条 40 分钟的会，往往只需要其中 2 分钟发给别人。详情页的「剪辑」行：

- 播放到位置点「取起点 / 取终点」，或直接填秒数
- 勾「烧录字幕」= 字幕渲染进画面（重编码，慢一点）；不勾 = 流拷贝，几秒就出
- 生成后作为一条**新记录**出现在列表里，`clipOf` 字段记着它从哪条剪出来

实现上有两个小决定：

- **字幕文件放临时目录，ffmpeg 的 cwd 也切过去**。filter 里只写 `subtitles=clip.srt`，
  彻底绕开 Windows 路径里 `C:` 冒号在 filtergraph 中的转义问题
- **不勾烧录时用 `-c copy`**：只切在关键帧上，可能差个零点几秒，但 10 秒的片段 1 秒就出；
  要精确到帧就勾上烧录（走 libx264 重编码）
- 录音记录剪出来是 `.m4a`，不硬塞进 mp4 容器；视频记录才是 `.mp4`

样式用 `force_style`：微软雅黑 20 号、白字黑边、底部 28px 边距。

### 二十、AI 纪要的四种引擎（云端 / 本机 / 融合 / 规则）

纪要不再是「有 Key 才能用」。设置里可以明确选一条路：

| 引擎 | 怎么走 | 适合谁 |
|---|---|---|
| `auto` 自动（默认） | 有模型走**融合**，没有模型走规则 | 大多数人 |
| `fusion` 融合 | 规则抽候选 → 模型甄别改写 | 待办想抽得准（评测最强） |
| `local` 本机模型 | 只调本机 llama.cpp，**完全离线** | 涉密会议、不想把会议内容发出去 |
| `cloud` 云端 API | 只调 OpenAI 兼容接口 | 想要最好的效果 |
| `off` 纯规则 | 不联网不跑模型，本地抽取 | 极低配机器、只要个骨架 |

#### 融合：为什么它比前两种都强

单独用规则或单独用模型，失败的方式不一样：

- 规则**分不清**哪些话是待办——召回 0.61 很高，但十条里抽中六条、混进七条噪声
- 模型**读长文会漏**——精确 0.34，但该抽的它有三成视而不见

那就让规则负责「别漏」、模型负责「别抽错」：规则先把候选句连时间戳一起捞出来
（上限 40 条），连同原文交给模型，模型只做一件事——**甄别哪些是真行动项，
并改写成不超过 25 字的祈使句**，顺带补上候选里漏掉的。成本几乎为零：
耗时和纯模型一样（26.6s vs 26.8s），待办 F1 从 0.39 提到 **0.56**。

代价是它依赖模型——没模型时自动退化成规则版（前半段本来就是规则），不会报错。

#### 本机模型怎么装（一次）

不内嵌几 GB 的模型，也不自己做推理——直接对接本机 [llama.cpp](https://github.com/ggml-org/llama.cpp)
的 `llama-server`（OpenAI 兼容接口），所以代码里换个 base URL 就能在云端/本机之间切。

```powershell
# 1. 下载 llama.cpp（约 260MB，CUDA 版；没有 N 卡就换 cpu 版）
#    https://github.com/ggml-org/llama.cpp/releases
# 2. 下载模型（Qwen3-4B Q4_K_M，2.5GB；HuggingFace 不通时用魔搭）
curl -L -o Qwen3-4B-Q4_K_M.gguf ^
  https://www.modelscope.cn/models/Qwen/Qwen3-4B-GGUF/resolve/master/Qwen3-4B-Q4_K_M.gguf
# 3. 启动（也可以不手动启，填了 exe 和 gguf 路径后程序会自己拉起来）
llama-server.exe -m Qwen3-4B-Q4_K_M.gguf --port 8080 -c 4096 -ngl 999
```

然后在网页「AI 纪要设置」里填：本机地址 `http://127.0.0.1:8080/v1`、
`llama-server.exe` 路径、`.gguf` 路径，点「测本机模型」会拉起服务并让它说一句话，
告诉你**能不能用、多少秒**。程序退出时会把自己拉起的服务关掉，不留后台进程吃显存。

细节：请求里带 `chat_template_kwargs.enable_thinking=false` 关掉 Qwen3 的思考过程
（纪要要的是结果，不要长篇推理）；超时给到 10 分钟，因为纯 CPU 上 4B 模型会慢很多。

#### 规则抽取（零配置兜底）

本机模型和 Key 都没有时，走本地规则抽取：

- 按停顿（>90 秒）和说话人变化切成议题块
- 关键词用 2~4 字 n-gram 频次 + 长度加权，含虚词的碎片直接丢（"擎的测试"这种）
- 结论/待办/风险按句式匹配；待办还会抽**负责人**（"X 负责…"）和**时间**（明天/下周三/10月25日/next week）
- 否定句不算待办（"不需要参考音频"）

结果同样是五段式。待办是**结构化**的（`GET /api/recordings/{id}/actions`
返回 `{text, owner, due, at}`），可以直接进 Todo。

### 二十一、纪要正确率到底怎么比

「本地模型到底比规则抽取好多少」不能靠感觉。做法是把评测做成一条命令，
考卷和判分标准都进仓库，谁都能复现。

**考卷**：`tools/summarybench/testdata/gold.json`，3 条仿真实会议（口语、插话、闲聊、改主意都有），
人工标注了每条的**待办**（含负责人、时间）、**议题关键词**、**决定**和一份**参考摘要**。

**判分口径**（`tools/summarybench`）：

| 指标 | 怎么算 | 看什么 |
|---|---|---|
| 待办精确率 P | 命中的预测 / 全部预测 | 低 = 在编待办（幻觉） |
| 待办召回率 R | 命中的预测 / 标准答案 | 低 = 漏待办 |
| 待办 F1 | P 和 R 的调和平均 | 综合分 |
| 负责人 / 时间准确率 | 只在命中的条目上比 | 抽对了事，有没有抽对人 |
| 议题 / 决定召回 | 标准关键词出现在纪要里的比例 | 有没有漏掉整块内容 |
| ROUGE-L | 字符级 LCS 的 F1（对人工参考摘要） | 摘要写得像不像人写的 |

命中判定用**字符二元组包含度**（交集 / 较短一方长度，阈值 0.5），而不是 Jaccard：
规则抽取给的是一整句原话（二十几个字），模型给的是凝练后的任务（几个字），
Jaccard 会被长句的分母压到接近 0，等于变相惩罚规则抽取——两边只有同口径才公平。

**跑一次**：

```bash
go run ./tools/summarybench -data <数据目录> -engines rules,local,cloud -repeat 3 -out bench.md
```

`-repeat 3` 是必须的：4B 小模型即使 temperature 只有 0.3，同一份输入两次生成的措辞
也可能差很多，单次采样 F1 能差 ±0.2（实测过一次把模型判成不如规则）。

实测（RTX 3050 Laptop 4GB，Qwen3-4B-Q4_K_M，3 条会议 / 10 条待办，每条跑 3 次取平均）：

| 引擎 | 待办 F1 | 精确率 | 召回率 | 负责人 | 时间 | 议题召回 | ROUGE-L | 平均耗时 |
|---|---|---|---|---|---|---|---|---|
| 规则抽取 | 0.33 | 0.23 | 0.61 | **100%** | 17% | 48% | 0.13 | **0.0s** |
| 本机 Qwen3-4B | 0.39 | 0.34 | 0.45 | 63% | 61% | **83%** | **0.31** | 26.8s |
| 融合（规则+模型） | **0.56** | **0.48** | **0.69** | **100%** | **83%** | 74% | 0.25 | 26.6s |
| 云端 API | — | — | — | — | — | — | — | 需自备 Key 后跑同一条命令 |

怎么读这张表（每条跑 3 次取平均，小模型单次采样波动能到 ±0.2，只跑一次容易得出反的结论）：

- **待办抽取**：融合 F1 0.56，比两个单独的都高一大截。规则召回最高（0.61）但精确只有
  0.23，八条里混六条噪声；模型反过来，抽得少但准。**融合的召回 0.69 甚至超过规则**——
  模型能从候选之外的上下文里补上规则漏掉的待办（实测补出"顺手把历史数据导一份"）。
- **负责人/时间**：融合 100% / 83%，规则基本抓不到"周四""下周三"这类说法（17%）。
- **摘要质量**：纯模型仍然最好（ROUGE-L 0.31、议题召回 83%），融合略降（0.25 / 74%）——
  因为融合的输入多带一段候选列表，挤占了模型的生成空间。
- **代价**：26.6 秒和 2.5GB 显存/内存（纯 CPU 会更慢），和纯模型持平。

所以「内嵌还是引导下载」的结论是**不内嵌**：包会从 9MB 涨到 2.5GB，收益主要在摘要
可读性上。默认走 `auto`：装了模型走融合（待办最准），没装就规则抽取秒出，
填了 Key 也可以手动切云端——用户自己决定要不要花这 2.5GB。

已知的不足：融合在"待办和陈述句式很像"的会议上会多抽（产品迭代评审那条 F1 只有 0.25，
把"保留 A 方案"这种结论也当成了待办）。下一步可以把"结论/待办"的边界也写进提示词。

### 二十二、MCP server

`tools/mcpserver` 是一个 stdio 的 Model Context Protocol 服务，把本机数据接进 AI 客户端：

```json
{
  "mcpServers": {
    "screenrec": { "command": "D:\\path\\screenrec-mcp.exe" }
  }
}
```

工具（都走 `127.0.0.1:8790`，不联网）：

| 工具 | 作用 |
|---|---|
| `list_recordings` | 列出录制（标题/时长/是否已转写/有无纪要） |
| `get_transcript` | 取文稿，带 `[mm:ss]` 和说话人 |
| `search_transcripts` | 跨记录搜关键词（走服务端索引，203 条记录下 2~3ms），返回时间点 |
| `get_summary` / `generate_summary` | 读 / 生成纪要（没 Key 走本地抽取） |
| `get_action_items` | 结构化待办 |
| `clip_recording` | 裁片段，可选烧字幕 |
| `today_agenda` | 今天剩下的日程（含入会链接） |

Claude Desktop / Cursor 里可以直接问："上周哪次会提到预算？把结论和待办列出来。"
服务没启动时会明确提示先双击 `screenrec.exe`。

### 二十三、日历到点自动录制

「检测麦克风占用」是**事后**发现：等提示弹出来，前几句已经说完了。日历是**事前**已知的，
所以再补一条更靠前的触发路径。

数据源三种，设置里填一个即可：

| 填什么 | 怎么取 |
|---|---|
| 留空 / `outlook` | 调 PowerShell 的 Outlook COM 导出未来两天日程 |
| `D:\xxx.ics` | 直接读本地 ICS 文件 |
| `https://…` | 下载日历订阅链接（Google / 飞书 / 腾讯会议的 iCal 地址） |

流程：每 10 分钟拉一次日历，每 20 秒检查一次。触发窗口是 `[开始 - 提前分钟数, 开始 + 10 分钟]`，
命中后：

- **默认**：悬浮条弹「『周会』2 分钟后开始，要开始录制吗？」，点「开始」才录（合规上更稳妥）
- **勾了「到点直接开录」**：到点自动起录，标题直接用日程名
- **日程结束后自动停**：结束时间 +5 分钟宽限，只对日历起的那一条生效

ICS 解析是自己写的（`calendar.go`，无第三方库），覆盖了实际会碰到的情况：

- `DTSTART` 的三种形态：本地时间、`Z` 结尾的 UTC、`VALUE=DATE` 全天
- `TZID=China Standard Time` 这类 Windows 时区名 Go 的 `LoadLocation` 认不出来，
  有一张兜底偏移表；实在认不出就按本地浮动时间处理
- `RRULE` 展开（DAILY / WEEKLY+BYDAY / MONTHLY / YEARLY，含 INTERVAL / COUNT / UNTIL）
- `DURATION:PT1H30M` 代替 `DTEND`；折行拼接；`\,` `\;` `\n` 反转义
- `STATUS:CANCELLED` 丢弃；`RECURRENCE-ID`（重复日程的单次改期）跳过，避免重复计数
- LOCATION / DESCRIPTION 里抠出入会链接（腾讯会议 / Zoom / Teams / 飞书 / 钉钉）

两个细节：**全天日程（生日、节假日）不提醒**；**提示没被响应时不会被下一条顶掉**，
但过了触发窗口会自动撤掉——人不在电脑前时不至于一直挂着。

### 二十四、录窗口黑屏：为什么 gdigrab 抓不到、怎么救回来

真实使用里最容易踩的一个坑：**录浏览器/微信/播放器这类窗口，出来是整段黑屏，只有声音**。

原因是 gdigrab 用 `hwnd=` 抓的是窗口的重绘消息，而 Edge/Chrome、微信小程序、Electron 应用
都是 GPU 合成渲染——画面根本不经过 GDI，于是抓到的是一块黑。实测同一台机器上：
录 Edge 窗口平均亮度 **16**（YUV 有限范围里 16 就是纯黑），录微信窗口 **226**（正常）。

程序里没法用返回值判断（ffmpeg 一切正常，文件也正常生成），只能看画面本身。所以加了预检：

- 录窗口前先抓 3 帧算平均亮度，低于 22 视为黑屏
- 黑了就**改抓整屏再裁剪到窗口矩形**（ddagrab 能抓到 GPU 画面），并记住这个窗口位置
- 裁剪时宽高用 `min()` 夹到画面尺寸——最大化窗口有 1938×1038，比 1920×1080 的屏还宽，
  直接 crop 会让 ffmpeg 报 "Invalid too big or non positive size" 当场退出
- 降级会在记录上留一条说明：`captureNote`，前端会提示「窗口被遮挡时画面会不准」

代价是窗口被别的窗口挡住时会录到遮挡物，但从"整段黑屏"变成"可能被遮挡"是值得的。
**全屏录制不做预检**：黑屏几乎只发生在录窗口，而预检要额外等 1~2 秒（建 D3D11 设备、等首帧）。

顺带一个细节：`signalstats` 滤镜处理不了 ddagrab 的 D3D11 硬件帧，会报
`Function not implemented`，预检必须先 `hwdownload,format=bgra` 再送进去，
否则每次都会被误判成黑屏。

### 二十五、录完自己出纪要（不用再点一下）

上一版要手动点「AI 纪要」。现在转写一结束就自动生成，用后台跑，不挡转写收尾。
设置里可以关（「录完自动出纪要」），关掉就还是手动点。

失败不算错：模型没起来、Key 失效都只是 log 一行，记录上仍可手动重试。

### 二十六、字幕精度：默认不再走流式模型

实时字幕原来优先用流式 Paraformer（200ms 出字），但它是 int8 量化 + 上下文只有几百毫秒，
真实会议里同音字错得明显。现在默认改成 **SenseVoice 每 0.7 秒解一段**，延迟多半秒，准确率高一档。

配套改了两处：

- **按静音端点切句**，不再固定 2.5 秒硬切。硬切会把「我们下周把接口文档」+「更新一下」
  拦腰截断，后半句没有上下文；现在等出现 0.4 秒静音再切，最长攒 5 秒
- 设置里留了开关（「字幕优先准确」），关掉就回到流式模型，适合只想要低延迟的场景

说话人分离的门槛也调了：聚类阈值 0.5 → 0.65（0.5 是两人对话的值，多人会议容易把两个人
并成一个），同一人换气停顿的合并时长 0.4 → 0.6 秒。另外修了一个对齐 bug——
原来转写段落只要和某个说话人区间重叠 0.05 秒就归给他，别人的一句插话就能把整段判错，
现在要求重叠至少 0.3 秒且不低于段长的 25%。

### 二十七、界面闪烁与「点了结束没反应」

两个独立的毛病，都修了：

**悬浮条闪**：每 50ms 的定时器无条件 `InvalidateRect` 整个窗口，而且直接往窗口 DC 上画——
每帧「填背景 → 画内容」的中间状态会被看见。改成：

- 双缓冲：整帧先画到内存 DC，最后一次性 BitBlt
- 电平条没变化就不重绘（原来 20FPS 全窗重画）
- `WM_ERASEBKGND` 直接返回 1，不让系统先擦成白底

**主界面闪**：实时字幕每 1.2 秒重建 `innerHTML`，内容没变也重建，光标动画重置 + 文本重排。
现在内容签名没变就不动 DOM，电平条变化小于 2% 也不写样式。

**点了结束还要去工坊再点一次**：`+faststart` 会让 ffmpeg 停止时重写整个文件（实测多等 3~4 秒），
期间悬浮条毫无反馈，用户以为没点中。现在去掉 faststart（本地播放走 Range 请求，不需要），
停止从 4.2 秒降到 3 秒左右；同时悬浮条立刻显示「正在保存录制…」并隐藏按钮防止重复点击，
网页按钮也立刻变「正在保存…」并禁用。

### 二十八、没有黑框闪出来

程序编译成 `windowsgui` 子系统，双击运行没有控制台——但 `exec.Command` 启动的
ffmpeg / python / 命令行工具都是控制台程序，Windows 会给**每个子进程**新建一个黑框。
一次「开始录制」同时拉起 ffmpeg、回环采集、识别 worker、屏幕探测（4 个并发），
屏幕上能连弹五六个；启动、转写、说话人分离、配音、日历同步各来一轮。

原来只有 3 处设了 `HideWindow`，其余 27 处都是裸调。现在统一走 `internal/winhide`：

```go
cmd := winhide.Command(ffmpeg, args...)          // 等价 exec.Command，但不弹框
cmd := winhide.CommandContext(ctx, python, ...)  // 等价 exec.CommandContext
```

两个细节：

- 只设 `SysProcAttr.HideWindow` 有时仍会闪一下（控制台先创建再隐藏），
  要同时加 `CREATE_NO_WINDOW`（`0x08000000`）才是从根上不创建控制台
- 原来 3 处手写 `cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}` 的，
  改成 helper 后要把这行删掉——它是**整体赋值**，会覆盖掉 `CREATE_NO_WINDOW`

顺带把采集降级的说明接到了界面上：录窗口时如果因为 GPU 合成拿不到画面、
被改成「全屏采集 + 裁剪」，会弹一条 7 秒的黄色提示，而不是等用户回放时才发现画面被遮挡。

### 二十九、点「开始录制」不再等五秒

原来第一次录制要等将近 6 秒，之后约 2 秒。慢的不是录屏本身，是几个**同步探测**：

| 探测 | 干什么 | 实测耗时 |
|---|---|---|
| `detectEncoder` | 拉起 ffmpeg 试一次硬件编码 | 3~4 秒（加载 CUDA 运行库很慢） |
| `listDevices` | 跑 `ffmpeg -list_devices` 枚举麦克风/摄像头 | 1~2 秒 |
| `probeScreens` | 并发试 4 个 ddagrab 序号 | 1~2 秒 |

更糟的是这三个**都在 `/api/state` 里**，而前端每 1.2 秒轮询一次——`listDevices` 缓存只有 10 秒，
等于每 10 秒给界面来一次顿卡，轮询看起来像卡死。

改法：

- 三个探测全部改成「立刻返回缓存值 + 后台探一次」，缓存期拉长（编码器 6 小时、设备 3 分钟、显示器 10 分钟）
- 启动时开一个后台 goroutine 预热，第一次打开页面就有完整设备列表
- 判断「ffmpeg 是不是启动失败」的观察窗口从 1.2 秒缩到 800ms——
  实测参数/设备打不开时 ffmpeg 在 200~400ms 内就退出了，多等那 400ms 纯粹在拖慢按钮响应

效果：首次录制 **5.86s → 2.12s**，`/api/state` 从「偶尔卡 1~2 秒」变成稳定 **2~4ms**。

剩下的 2 秒是硬件开销（ddagrab 建 D3D11 设备 + 打开麦克风），已接近下限。

### 三十、转写纠错：把「埋点」从「买点」改回来

换模型、改切句只能把同音字错误压到一定程度——SenseVoice 在嘈杂环境、口音、专业术语上
仍然会把「埋点」识别成「买点」、「登录页」识别成「当陆夜」。这类错误靠 ASR 自己解决不了，
但**模型读过上下文就知道「登录页改版」是个产品术语**，能把它改回来。

所以转写完成后加了一道纠错（`correct.go`）：

- 把转写按 30 行一批交给模型，要求「只改同音字和明显错别字」
- 三条硬约束写进提示词：不改写句子、不增删内容、拿不准就保持原样
- 输出必须「序号|文本」且**行数与输入完全一致**，缺一行就整批丢弃——
  文本错位比错别字严重得多，宁可留着错字
- 解析层容忍模型常见的格式噪声（代码块标记、列表符号、全角分隔符、中文引号）

实测（真实识别错误样本）：

```
这次会议的结论是。                          → 没动
当陆夜改版本周五上线。                      → 登录页改版本周五上线。
付费转化的买点，请小李在周四之前补完。      → 付费转化的埋点，请小李在周四之前补完。
```

耗时：模型已加载时 3 段约 3.7 秒；冷启动要等模型加载（首次约 2 分钟，之后都是秒级）。
设置里可以关（「自动校对文字」），也可以对单条记录点「校对文字」手动跑。

顺序上是**先纠错、再出纪要**——纪要的待办是从转写文本抽的，错字会一路传下去。
所以转写完成后会依次经过「校对」和「生成纪要」两步，详情里能看到状态。

### 三十一、实时字幕「边录边改」

上一版只在转写完成后才纠错，悬浮条上的实时字幕仍是原样——这一点和豆包差得最明显。

差在哪：豆包用的是端到端强模型（Seed-ASR），解码时就把上下文和热词用上了，一步到位；
我们的链路是「ASR 出结果 → 模型后处理」，两步必然有延迟，做不到每条字幕都等模型。

所以换个思路：**字幕先出，后台改好了再原地替换**。

- 每说完一句，把段落下标丢进队列，后台逐条送模型（单句，不是整批）
- 改完先核对「原文还是不是那句」，是才替换——期间可能已被整段转写覆盖
- 队列只留 16 句，满了直接丢：模型跟不上说话速度时不能拖住录制线程
- 短于 5 个字的句子（"对""嗯"）不送模型，省算力

实测单句纠错 **1~4 秒**（4B 模型、已加载状态）：

```
付费转化的买点要在周四之前补完。   → 付费转化的埋点要在周四之前补完。
当陆夜改版本周五上线，设计稿今天给。→ 当登录页改版本周五上线，设计稿今天给。
接口文当要同步给前端，顺带把埋点对一遍。→ 接口文案要同步给前端，顺带把埋点对一遍。
这个方案我觉得可以，下周一开始做。  → 没动（本来就没错）
```

**怎么自己验证**：录着音的时候点录制面板上的「试一句校对」，它会往字幕里丢一句带同音字的
话（比如「付费转化的买点……」），1~2 秒后你会看到那句自己变成「埋点」。

用户看到的效果：字幕先正常出现，个别字过一两秒闪一下变对。丢掉的句子（队列满）
会在停止后的全量纠错里补上，所以最终文本不会漏。

调 prompt 时踩了个坑，记一下：只写「修正同音字」时 4B 模型会保守到原样输出，**必须给示例**
（两个「错→对」加一个「不用改」的对照）它才肯动手；另外 Qwen3 默认开思考模式，思考会把
`max_tokens` 吃光导致返回空字符串（表现就是「模型没改」），对本地 llama-server 要显式
`enable_thinking: false`。

两个前提：模型已加载（冷启动要等，期间的字幕不纠错）、设置里开着「字幕边录边校对」。
视频录制时如果觉得占资源，可以单独关掉这一项，不影响转写后的全量纠错。

### 三十二、「录系统声音」的两个坑

**坑一：不是所有播放设备都支持回环。**

同一台机器上实测：

| 设备 | 结果 |
|---|---|
| 扬声器 (Realtek(R) Audio) | 采集出来**全是 0**，混进去是一段静音 |
| 扬声器 (AB13X USB Audio) | 正常 |

WASAPI 回环依赖驱动的实现，部分板载声卡、蓝牙耳机、USB 声卡就是给不出数据，
而 ffmpeg 和 Windows 都不会报错——文件正常生成，就是没声音。

原来只在**停止时**检查，录完一小时才发现对方的话没录进去。现在录制中就盯着：

- 每 3 秒读一次回环 PCM 末尾 1 秒，算 RMS
- 开头 10 秒不判（用户可能还没开始放声音）
- 连续 3 次静音（约 9 秒）就在录制面板上黄字提示换设备
- 之后检测到有声音会自动撤掉提示

实测从开始录到弹提示约 21 秒。

**坑二：系统声音那一路的字幕"很少出现"，是故意的。**

外放时同一句话会先被回环收到、再被麦克风收一遍。两条字幕内容近似就直接丢掉一条
（`textSimilar ≥ 0.62`），否则字幕会一句一句重复两遍。所以看到字幕来源标着 mic 是正常的，
不代表系统声音没录进去——**成片里的混音是完整的**。

另外，`.sys.pcm` 是中间文件，混音完成后会删掉。想确认系统声音有没有录到，看两样东西：
录制中的「系统声音」电平条，或者停止后的 `sysWarning`。

## 目录结构

```
screenrec/
├── main.go            # 入口、单实例互斥、悬浮条状态同步、优雅退出
├── api.go             # REST API、设置读写、SRT/TXT 导出
├── app.go             # 应用上下文、启动/停止统一入口、悬浮条动作分发
├── overlay.go         # 悬浮条窗口（GDI 绘制 + 按钮 + 鼠标命中）
├── settings.go        # 配置持久化（授权/会议提醒/关闭状态）
├── meeting.go         # 会议检测与提醒（去抖、静默、不再提醒）
├── calendar.go        # ICS 解析 + Outlook/订阅拉取 + 到点提醒/自动录制
├── reg.go             # 注册表读「谁在占用麦克风」
├── recorder.go        # ffmpeg 录制进程、实时转写循环（流式/分块双路径）
├── devices.go         # dshow 设备枚举、显示器探测、ffprobe
├── windows.go         # EnumWindows 枚举可录窗口（syscall，无 cgo）
├── mic.go             # 麦克风电平采样、增益、NVENC 探测、实时电平
├── asr.go             # SenseVoice 客户端 + 批量转写 + 媒体合并
├── asr_stream.go      # Paraformer 流式客户端
├── diarize.go         # 说话人分离与标签归属
├── export.go          # 纪要导出：Markdown + 手写 OOXML 的 Word
├── translate.go       # 本地翻译调度（opus-mt，术语保护在 Python 侧）
├── translate.py       # MarianMT 翻译（单次 / 常驻两种模式）
├── translate_live.go  # 实时字幕用的翻译常驻客户端
├── search.go          # 转写全文增量索引（MCP 跨记录搜索用）
├── llm_local.go       # 本机 llama.cpp 推理服务的拉起与探测
├── capture_probe.go   # 录屏预检：抓帧判黑屏，窗口抓不到就改全屏+裁剪
├── internal/winhide/  # 子进程封装：不弹控制台黑框
├── correct.go         # 转写纠错：用模型修同音字（买点→埋点）
├── live_correct.go    # 实时字幕纠错队列：先出字幕，后台改好再原地替换
├── sys_silence.go     # 录制中检测系统声音是否一直是静音（回环不支持的设备）
├── summary_fusion.go  # 融合纪要：规则抽候选 + 模型甄别改写
├── summary.go         # AI 纪要（云端 / 本机 / 融合 / 规则四选一）
├── summary_local.go   # 无 Key 的本地抽取纪要 + 结构化待办
├── editor.go          # 片段导出（流拷贝 / 重编码 / 烧字幕）
├── tools/mcpserver/   # MCP server（stdio，给 Claude/Cursor 用）
├── tts.go             # 声音克隆配音（GPT-SoVITS）
├── version.go         # 版本号
├── glossary.json      # 术语保护表（可自行追加）
├── tray.go            # 系统托盘图标与右键菜单（syscall）
├── autostart.go       # 开机自启（HKCU Run 键）
├── internal/mathnorm/ # 口语数学→符号（导出与翻译共用）
├── asr.py             # 整段识别（VAD 分段，输出 JSON）
├── asr_worker.py      # 常驻识别（长度前缀 PCM 协议）
├── asr_stream.py      # 常驻流式识别（每请求必回一行）
├── diarize.py         # 说话人分离（分段 + 声纹 + 聚类）
├── live_test.go       # 实时链路测试
├── stream_test.go     # 流式客户端测试
├── tools/mathnorm/    # 口语数学→符号（纯规则，无依赖）
├── tools/tts/         # SAPI 合成测试样本
├── tools/click/       # 自动化点击/拖动/滚轮（UI 验证）
├── tools/release/     # 打包发布 zip + sha256
├── tools/mocksummary/ # 模拟 OpenAI 接口（无 Key 也能测纪要）
├── tools/summarybench/# 纪要评测：三引擎对比打分，出 Markdown 报告
└── tools/summarybench/testdata/  # 人工标注的会议标准答案集（考卷）
├── tools/mocktts/     # 模拟 GPT-SoVITS（无引擎也能测配音）
├── tools/installer/   # 单文件安装包（stub + 追加 zip + 卸载）
├── web/               # 内置前端
├── models/
│   ├── sensevoice/    # 精校模型（239MB）
│   ├── stream/        # 流式模型（encoder 165MB + decoder 72MB）
│   ├── diar/          # pyannote 分段 1.5MB + ERes2Net 40MB
│   └── silero_vad.onnx
├── pylibs/            # sherpa-onnx 安装目录
└── data/recordings/   # 录制文件 + JSON 元数据
```

## 已知限制与下一步

- [x] ~~麦克风增益~~：已内置「检测电平 + 0~30dB 增益补偿」
- [x] ~~硬件编码~~：已加自动探测 + 失败回退
- [x] ~~窗口级录制~~：已支持（gdigrab + hwnd）
- [x] ~~长录制分文件~~：已支持（segment muxer，30/60 分钟）
- [x] ~~流式字幕~~：已支持（Paraformer 流式 + SenseVoice 精校）
- [x] ~~说话人分离~~：已支持（pyannote + ERes2Net）
- [x] ~~悬浮条按钮~~：已支持（开始 / 结束 / 忽略 / 关闭）
- [x] ~~会议自动提醒~~：已支持（注册表麦克风占用 + 标题兜底）
- [x] ~~单机互斥~~：已支持（命名互斥量 + 唤醒已有实例）
- [x] ~~录音授权确认~~：已支持（网页弹窗 + 悬浮条确认态）
- [x] ~~AI 纪要~~：已支持（OpenAI 兼容接口，五段式输出，可进导出文档）
- [x] ~~单文件安装包~~：已支持（setup.exe，快捷方式 + 卸载项 + 可选模型下载）
- [x] ~~电脑声音进实时字幕~~：已支持（能量端点切句 + 回声去重）
- [x] ~~日历到点录制~~：已支持（ICS 解析 + Outlook/订阅 + 到点提醒或自动开录）
- [ ] **跨设备互斥做不到**：需要账号体系与服务端会话，纯本地无解
- [ ] 公式只做到「规则可读化」：复杂公式（矩阵、多行推导、上下标嵌套）需要接 LLM 转 LaTeX
- [ ] 实时双语只翻断句后的最终结果（不翻流式草稿），冷启动 49~84 秒，已在开启时后台预热
- [x] ~~本地纪要质量~~：已支持本机模型（llama.cpp + Qwen3-4B-GGUF）与「规则+模型」融合，可量化评测见第二十一节；不内嵌模型（2.5GB），按需下载
- [ ] 本机模型要自己下载（llama-server.exe + .gguf 约 2.8GB）；纯 CPU 上 4B 模型生成较慢
- [ ] 融合纪要会把个别结论误判成待办（F1 在"待办像陈述"的会议上只有 0.25），结论/待办边界还要调
- [x] ~~录窗口黑屏~~：已加抓帧预检 + 全屏裁剪降级（见第二十四节）
- [ ] 窗口被其它窗口遮挡时，降级后的裁剪画面会录到遮挡物
- [x] ~~同音字纠错~~：转写后全量纠错（第三十节）+ 实时字幕边录边改（第三十一节，1.2~1.6 秒后原地修正）
- [ ] 剪辑不勾烧录时只能切在关键帧（可能差 0.2~1 秒）；烧字幕要重编码，4K 长片段会慢
- [x] ~~MCP 跨记录搜索慢~~：已加增量索引（203 条记录：1187ms → 2~3ms，索引文件 0.6MB）
- [x] ~~AI 纪要要手动点~~：转写完自动生成，可在设置里关
- [ ] AI 纪要若用云端需要自带 API Key（不填也能用本机模型或规则抽取）；转写超过 14000 字会截断
- [ ] 声音克隆配音依赖单独的 GPT-SoVITS（14GB），单次上限 800 字；参考音频要真人音、且文字要逐字一致
- [ ] 分词/断句仍按标点，长句翻译质量一般；术语表需要自己维护
- [ ] 单字母识别仍会混淆（实测 `z`→`c`），规则层无法自纠
- [ ] 系统声音是"录完再混"：实时字幕能听到，但成片里的混音要等停止后才做；分段录制暂不与系统声音混音
- [ ] 实时字幕对电脑声音是 2.5 秒级延迟（端点检测 + 分块识别），麦克风走流式更快
- [ ] 蓝牙耳机不支持 WASAPI 设备回环，要用「进程级回环」或换成有线输出
- [ ] 会议检测是启发式：只认已知会议应用；小众软件（飞书网页版等）可能漏报
- [ ] 日历只支持 ICS（Outlook / 文件 / 订阅链接）；不含 `VTIMEZONE` 的陌生 `TZID` 会按本地时间处理
- [ ] 日历的「自动停止」只对日历自己起的那条生效，宽限 5 分钟；会议拖堂要手动续录
- [ ] 窗口录制依赖 GDI，独占全屏的游戏/DirectX 应用可能抓到黑屏
- [ ] 说话人分离在长音频上耗时较长（1 小时录音约 1~2 分钟）
- [ ] 转写文本的说话人标签是按时间重叠近似，快速抢话时可能串
- [ ] 长时间录制文件切分后，转写时间戳是拼接后的连续时间轴

## 合规

- 录制他人画面/声音需获得同意；会议录音建议开场明示
- 生成的文字稿涉及个人信息，注意保管与脱敏

## 许可证

代码以 [MIT](LICENSE) 发布，可自由使用、修改、分发与商用，保留版权声明即可。

第三方组件与模型权重的许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。
**模型不随本仓库分发**，请自行下载并遵守各自条款。
