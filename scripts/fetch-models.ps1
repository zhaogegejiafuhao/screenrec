# 下载运行所需的模型权重（约 1.1GB）
# 用法：powershell -ExecutionPolicy Bypass -File scripts\fetch-models.ps1
# 只装转写：-SkipTranslate     只装英译中：-SkipTranslate -SkipZhEn

param(
    [string]$Root = (Split-Path -Parent $PSScriptRoot),
    [switch]$SkipTranslate,
    [switch]$SkipZhEn,
    [switch]$SkipStream
)

$ErrorActionPreference = 'Stop'
Write-Host "模型下载目录: $Root\models" -ForegroundColor Cyan

function Get-Model {
    param([string]$Url, [string]$Dest, [int]$MinMB = 1)

    $full = Join-Path $Root $Dest
    if (Test-Path $full) {
        $mb = (Get-Item $full).Length / 1MB
        if ($mb -ge $MinMB) {
            Write-Host ("  跳过 {0}（{1:N1} MB）" -f $Dest, $mb) -ForegroundColor DarkGray
            return
        }
    }
    $dir = Split-Path -Parent $full
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Write-Host ("  下载 {0}" -f $Dest) -ForegroundColor Yellow

    for ($i = 1; $i -le 3; $i++) {
        try {
            Invoke-WebRequest -Uri $Url -OutFile $full -UseBasicParsing -TimeoutSec 3600
            $mb = (Get-Item $full).Length / 1MB
            Write-Host ("    完成 {0:N1} MB" -f $mb) -ForegroundColor Green
            return
        } catch {
            if ($i -eq 3) { throw "下载失败: $Url`n$_" }
            Write-Host "    第 $i 次失败，重试…" -ForegroundColor DarkYellow
            Start-Sleep -Seconds 3
        }
    }
}

$ms = 'https://www.modelscope.cn/models'
$hf = 'https://hf-mirror.com'
$gh = 'https://github.com/k2-fsa/sherpa-onnx/releases/download'

Write-Host "`n[1/5] SenseVoice 整段转写" -ForegroundColor Cyan
Get-Model "$ms/pengzhendong/sherpa-onnx-sense-voice-zh-en-ja-ko-yue/resolve/master/model.int8.onnx" 'models/sensevoice/model.int8.onnx' 100
Get-Model "$ms/pengzhendong/sherpa-onnx-sense-voice-zh-en-ja-ko-yue/resolve/master/tokens.txt" 'models/sensevoice/tokens.txt'

Write-Host "`n[2/5] Silero VAD 端点检测" -ForegroundColor Cyan
Get-Model "$gh/asr-models/silero_vad.onnx" 'models/silero_vad.onnx'

if (-not $SkipStream) {
    Write-Host "`n[3/5] Paraformer 流式字幕" -ForegroundColor Cyan
    $base = "$ms/pengzhendong/sherpa-onnx-streaming-paraformer-bilingual-zh-en/resolve/master"
    Get-Model "$base/encoder.int8.onnx" 'models/stream/encoder.int8.onnx' 50
    Get-Model "$base/decoder.int8.onnx" 'models/stream/decoder.int8.onnx' 50
    Get-Model "$base/tokens.txt" 'models/stream/tokens.txt'
} else {
    Write-Host "`n[3/5] 跳过流式模型" -ForegroundColor DarkGray
}

Write-Host "`n[4/5] 说话人分离" -ForegroundColor Cyan
Get-Model "$ms/pengzhendong/sherpa-onnx-pyannote-segmentation-3-0/resolve/master/model.int8.onnx" 'models/diar/segmentation.int8.onnx'
Get-Model "$gh/speaker-recongition-models/3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx" 'models/diar/embedding.onnx' 10

if (-not $SkipTranslate) {
    Write-Host "`n[5/5] 本地翻译模型" -ForegroundColor Cyan
    $files = @('config.json', 'generation_config.json', 'pytorch_model.bin', 'source.spm', 'target.spm', 'tokenizer_config.json', 'vocab.json')
    foreach ($f in $files) {
        Get-Model "$hf/Helsinki-NLP/opus-mt-en-zh/resolve/main/$f" "models/mt/en-zh/$f"
    }
    if (-not $SkipZhEn) {
        foreach ($f in $files) {
            Get-Model "$hf/Helsinki-NLP/opus-mt-zh-en/resolve/main/$f" "models/mt/zh-en/$f"
        }
    }
} else {
    Write-Host "`n[5/5] 跳过翻译模型" -ForegroundColor DarkGray
}

Write-Host "`n全部完成。运行前请确认已安装 FFmpeg 与 Python 依赖：" -ForegroundColor Cyan
Write-Host "  pip install sherpa-onnx numpy soundcard torch transformers sentencepiece" -ForegroundColor Gray
