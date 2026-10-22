# 录屏工坊（ScreenRec）

[English](README.en.md) · MIT License · Windows 10 2004+

本地录屏 / 录音 + 自动语音转文字。Go 单文件程序 + 内置网页界面，
识别完全跑在本地 CPU 上（SenseVoice），不联网、不上传。

## 功能

- **录屏**：DXGI 桌面复制（`ddagrab`）抓屏，H.264 编码，支持 30/60fps、三档画质
- **窗口录制**：只录某个应用窗口（按窗口句柄抓取），窗口被遮挡也能录
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

### 二十、本地纪要（没有 API Key 也能出）

之前点「AI 纪要」不填 Key 就直接报错，等于这条链路断在半路。现在没 Key 会退到**本地规则抽取**：

- 按停顿（>90 秒）和说话人变化切成议题块
- 关键词用 2~4 字 n-gram 频次 + 长度加权，含虚词的碎片直接丢（"擎的测试"这种）
- 结论/待办/风险按句式匹配；待办还会抽**负责人**（"X 负责…"）和**时间**（明天/下周三/10月25日/next week）
- 否定句不算待办（"不需要参考音频"）
- 结果同样是五段式，末尾标注"本地规则抽取（未调用大模型）"

质量当然不如大模型，但零配置、离线、瞬时出结果，而且待办是**结构化**的
（`GET /api/recordings/{id}/actions` 返回 `{text, owner, due, at}`），可以直接进 Todo。

### 二十一、MCP server

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
| `search_transcripts` | 跨记录搜关键词，返回时间点 |
| `get_summary` / `generate_summary` | 读 / 生成纪要（没 Key 走本地抽取） |
| `get_action_items` | 结构化待办 |
| `clip_recording` | 裁片段，可选烧字幕 |
| `today_agenda` | 今天剩下的日程（含入会链接） |

Claude Desktop / Cursor 里可以直接问："上周哪次会提到预算？把结论和待办列出来。"
服务没启动时会明确提示先双击 `screenrec.exe`。

### 二十二、日历到点自动录制

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
├── summary.go         # AI 纪要（OpenAI 兼容接口）
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
- [ ] 本地纪要是规则抽取，复杂会议会漏议题；要质量还是要配 Key（或本地 Ollama）
- [ ] 剪辑不勾烧录时只能切在关键帧（可能差 0.2~1 秒）；烧字幕要重编码，4K 长片段会慢
- [ ] MCP 的跨记录搜索是逐条读 JSON，记录上百条时首次搜索偏慢
- [ ] AI 纪要需要自带 API Key；转写超过 14000 字会截断
- [ ] 声音克隆配音依赖单独的 GPT-SoVITS（14GB），单次上限 800 字
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
