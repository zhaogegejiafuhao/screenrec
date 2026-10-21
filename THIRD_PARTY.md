# 第三方组件与许可证

本项目代码以 MIT 发布，但运行时依赖以下第三方组件。**模型权重不随本仓库分发**，
请按 `scripts/fetch-models.ps1` 自行下载，并遵守各自的许可证。

## 可执行依赖

| 组件 | 用途 | 许可证 | 说明 |
|---|---|---|---|
| [FFmpeg](https://ffmpeg.org/) / ffprobe | 录屏、录音、混音、转码 | LGPL-2.1+ 或 GPL-2.0+（取决于构建选项） | **以独立进程调用**，不链接、不随仓库分发。请自行安装并确认所用构建的许可条款 |
| [Python](https://www.python.org/) 3.9+ | 运行识别 / 分离 / 翻译脚本 | PSF License | 独立进程调用 |

## Python 包

| 包 | 用途 | 许可证 |
|---|---|---|
| [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) | 本地语音识别、VAD、说话人分离运行时 | Apache-2.0 |
| [numpy](https://numpy.org/) | 音频数组运算 | BSD-3-Clause |
| [soundcard](https://github.com/bastibe/SoundCard) | WASAPI 回环采集 | BSD-3-Clause |
| [torch](https://pytorch.org/) | 翻译模型推理 | BSD-3-Clause |
| [transformers](https://github.com/huggingface/transformers) | MarianMT 加载 | Apache-2.0 |
| [sentencepiece](https://github.com/google/sentencepiece) | 分词 | Apache-2.0 |

## 模型权重

| 模型 | 用途 | 大小 | 许可证 / 来源 |
|---|---|---|---|
| [SenseVoice](https://github.com/FunAudioLLM/SenseVoice)（sherpa-onnx 转换版） | 整段转写（中英日韩粤） | 239MB | 以 FunAudioLLM 官方仓库为准 |
| [Paraformer streaming](https://github.com/modelscope/FunASR)（sherpa-onnx 转换版） | 实时字幕草稿 | 227MB | 以 FunASR 官方仓库为准 |
| [Silero VAD](https://github.com/snakers4/silero-vad) | 语音端点检测 | 2MB | MIT |
| [pyannote segmentation-3.0](https://huggingface.co/pyannote/segmentation-3.0)（转换版） | 说话人分段 | 1.5MB | 以模型页为准 |
| [3D-Speaker ERes2Net](https://github.com/modelscope/3D-Speaker) | 声纹嵌入 | 40MB | Apache-2.0 |
| [opus-mt-en-zh](https://huggingface.co/Helsinki-NLP/opus-mt-en-zh) | 英译中 | 301MB | Apache-2.0 |
| [opus-mt-zh-en](https://huggingface.co/Helsinki-NLP/opus-mt-zh-en) | 中译英 | 301MB | Apache-2.0 |

## 可选组件（不随仓库分发）

| 组件 | 用途 | 许可证 |
|---|---|---|
| [GPT-SoVITS](https://github.com/RVC-Boss/GPT-SoVITS) | 声音克隆配音（`tts.go` 通过 HTTP 调用其 `api.py`） | MIT |
| [Ollama](https://ollama.com/) / DeepSeek / 通义等 | AI 纪要，任意 OpenAI 兼容接口 | 各自条款 |

> 「以官方仓库为准」表示本项目未逐一核验其模型条款。若你要商用或再分发模型，
> 请先到对应仓库确认。代码仓库只包含转换后的推理脚本，不包含权重。

## 设计参考

悬浮字幕条的视觉参数（声波条尺寸、圆角、配色）参考了豆包桌面端的公开界面表现，
仅用于交互设计参考；本仓库不包含任何来自该客户端的代码、样式表或资源文件。
