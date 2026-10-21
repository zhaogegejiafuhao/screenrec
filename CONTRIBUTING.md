# 参与贡献

## 环境

只有 Windows 能跑（依赖 `ddagrab` / `dshow` / WASAPI 回环）。开发机需要：

- Go 1.22+
- FFmpeg 6.0+（`dshow`、`gdigrab`、`ddagrab`）
- Python 3.9–3.11 + `sherpa-onnx numpy soundcard torch transformers sentencepiece`
- 模型：`scripts\fetch-models.ps1`

## 本地跑起来

```powershell
go build -o screenrec-debug.exe .
.\screenrec-debug.exe -open=false     # 保留控制台输出，日志同时写 data\app.log
```

改前端直接刷新浏览器即可（`web/` 是 `go:embed` 的，改完要重新编译）。

## 提交前

```powershell
go vet ./...
go build ./...
```

识别相关的测试（`live_test.go`、`stream_test.go`）需要本地模型和麦克风，
CI 里不跑；改到那部分请在本机验证，并在 PR 里说明实测结果。

## 不要提交

- `data/`（个人录音与转写）
- `models/` 里的权重
- `pylibs/`、任何 `*.exe`
- 截图、录屏样片

这些已在 `.gitignore` 里，但用 `git add -f` 会绕过，请注意。

## 代码约定

- Go 代码不引第三方模块，只用标准库与 `syscall`
- Windows API 调用集中在 `overlay.go` / `tray.go` / `windows.go` / `reg.go`
- Python 侧脚本保持「单文件可单独运行」，便于排查
- 中文注释没问题，但导出/日志文案尽量中英都给

## 许可证

提交即表示同意以 MIT 许可证发布你的贡献。
