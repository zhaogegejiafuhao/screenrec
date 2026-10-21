#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""常驻识别进程：从 stdin 读长度前缀的 16k/16bit/mono PCM，输出一行 JSON 结果。

先过 Silero VAD，只有检测到语音才解码，避免把底噪"听"成文字。
协议：
  请求 = 4 字节小端长度 N + N 字节 PCM（N=0 表示 ping）
  响应 = 一行 JSON {"text": "..."}（每行 flush）
"""
import sys
import os
import json
import struct
import array

BASE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(BASE, "pylibs"))

import sherpa_onnx  # noqa: E402

MODEL_DIR = os.path.join(BASE, "models", "sensevoice")
VAD_MODEL = os.path.join(BASE, "models", "silero_vad.onnx")
SAMPLE_RATE = 16000

REC = sherpa_onnx.OfflineRecognizer.from_sense_voice(
    model=os.path.join(MODEL_DIR, "model.int8.onnx"),
    tokens=os.path.join(MODEL_DIR, "tokens.txt"),
    num_threads=4,
    use_itn=True,
    language="auto",
    debug=False,
)

VAD_AVAILABLE = os.path.exists(VAD_MODEL)


def new_vad(seconds=6):
    cfg = sherpa_onnx.VadModelConfig()
    cfg.silero_vad.model = VAD_MODEL
    cfg.silero_vad.threshold = 0.5
    cfg.silero_vad.min_silence_duration = 0.25
    cfg.silero_vad.min_speech_duration = 0.2
    cfg.silero_vad.max_speech_duration = 20.0
    cfg.sample_rate = SAMPLE_RATE
    return sherpa_onnx.VoiceActivityDetector(cfg, buffer_size_in_seconds=seconds)


def speech_only(samples):
    """返回 VAD 判定为语音的样本；没有语音则返回空列表。
    按 512 样本窗口喂入（与 asr.py 的批处理路径一致），一次性喂整段会得到错误切分。"""
    if not VAD_AVAILABLE:
        return samples
    vad = new_vad()
    window = 512
    for i in range(0, len(samples), window):
        chunk = samples[i:i + window]
        if len(chunk) < window:
            chunk = chunk + [0.0] * (window - len(chunk))
        vad.accept_waveform(chunk)
    vad.flush()
    out = []
    while not vad.empty():
        out.extend(vad.front.samples)
        vad.pop()
    return out


stdin = sys.stdin.buffer
stdout = sys.stdout


def read_exact(n):
    buf = b""
    while len(buf) < n:
        chunk = stdin.read(n - len(buf))
        if not chunk:
            return None
        buf += chunk
    return buf


def main():
    while True:
        hdr = read_exact(4)
        if hdr is None:
            break
        n = struct.unpack("<I", hdr)[0]
        if n == 0:
            stdout.write(json.dumps({"ok": True}) + "\n")
            stdout.flush()
            continue
        data = read_exact(n)
        if data is None:
            break
        a = array.array("h")
        a.frombytes(data)
        samples = [s / 32768.0 for s in a]

        speech = speech_only(samples)
        if len(speech) < SAMPLE_RATE * 0.15:  # 不足 0.15 秒的语音直接丢弃
            stdout.write(json.dumps({"text": ""}) + "\n")
            stdout.flush()
            continue

        s = REC.create_stream()
        s.accept_waveform(SAMPLE_RATE, speech)
        REC.decode_stream(s)
        stdout.write(json.dumps({"text": (s.result.text or "").strip()}, ensure_ascii=False) + "\n")
        stdout.flush()


if __name__ == "__main__":
    main()
