# screenrec

A local-first screen recorder and meeting transcriber for Windows — **no cloud, no account, no telemetry**.

Records screen / a single window / microphone / system audio, transcribes with SenseVoice,
labels speakers, translates bilingual subtitles, and exports meeting notes to Markdown or Word.

## Features

- **Screen capture** via DXGI Desktop Duplication (`ddagrab`); falls back to `gdigrab`
- **Window capture** by HWND — works even when the window is covered
- **System audio** via WASAPI loopback, including **process-level loopback** so it works with Bluetooth headphones
- **Live subtitles** (streaming Paraformer draft, ~0.3s) corrected by SenseVoice at each sentence boundary
- **System audio in the live captions**: loopback PCM is downsampled to 16k mono and cut on energy endpoints,
  so videos / calls played on the machine are captioned too, with cross-source echo suppression
- **Speaker diarization** (pyannote segmentation + 3D-Speaker embeddings)
- **Bilingual subtitles** with a fully offline MarianMT model, plus a glossary that keeps technical terms intact
- **Export** to Markdown / Word (hand-written OOXML, no dependencies), with optional spoken-math → `x² + y² = c²` normalization
- **AI summary**: three engines — cloud (any OpenAI-compatible endpoint), local (llama.cpp + Qwen3-4B GGUF, fully offline), or rule-based. Auto picks the best available
- **Live bilingual subtitles**: each finished sentence is translated locally (~0.3s warm) and shown as a second line
- **Voice cloning voice-over** (optional): calls a local GPT-SoVITS server to read the transcript in your own voice
- **Meeting reminder** when a known conferencing app starts using the microphone
- **Calendar-triggered recording**: reads ICS (local Outlook, a `.ics` file, or a subscription URL),
  prompts ahead of the start time — or starts recording on its own and stops when the event ends
- **Tray app**: no console window, autostart, global hotkey `Ctrl+Alt+Space`
- Single 12MB Go binary + Python sidecars. No third-party Go modules.

## Requirements

- Windows 10 2004+ (process loopback needs this version)
- Go 1.22+
- FFmpeg 6.0+ with `dshow`, `gdigrab`, `ddagrab`
- Python 3.9–3.11 with `sherpa-onnx numpy soundcard torch transformers sentencepiece`
- ~1.1GB of model weights (not bundled)

## Quick start

```powershell
# easiest: single-file installer (per-user, no admin rights, shortcuts + uninstall entry)
.\setup-0.4.0.exe

# or from source
powershell -ExecutionPolicy Bypass -File scripts\fetch-models.ps1   # 1. models (~1.1 GB)
go build -ldflags "-H windowsgui" -o screenrec.exe .                # 2. build
.\screenrec.exe                                                     # 3. run
```

FFmpeg / Python are found in this order: `PATH` → `SCREENREC_FFMPEG` / `SCREENREC_PYTHON`
environment variables → `ffmpeg\bin\` and `runtime\` next to the exe.

## Documentation

The full documentation is currently in Chinese: [README.md](README.md).

## License

MIT — see [LICENSE](LICENSE). Third-party components and model licenses: [THIRD_PARTY.md](THIRD_PARTY.md).
Model weights are **not** included in this repository.
