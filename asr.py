#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""本地语音识别（SenseVoice + Silero VAD），输出带时间戳的 JSON。

用法: python asr.py <16k单声道wav> [输出json]
"""
import sys
import os
import json
import wave
import array
import time

BASE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(BASE, "pylibs"))

import sherpa_onnx  # noqa: E402

MODEL_DIR = os.path.join(BASE, "models", "sensevoice")
MODEL = os.path.join(MODEL_DIR, "model.int8.onnx")
TOKENS = os.path.join(MODEL_DIR, "tokens.txt")
VAD_MODEL = os.path.join(BASE, "models", "silero_vad.onnx")
SAMPLE_RATE = 16000


def read_wav(path):
    with wave.open(path, "rb") as f:
        if f.getframerate() != SAMPLE_RATE or f.getnchannels() != 1 or f.getsampwidth() != 2:
            raise SystemExit("需要 16kHz 单声道 16bit WAV: %s" % path)
        raw = f.readframes(f.getnframes())
    a = array.array("h")
    a.frombytes(raw)
    return [s / 32768.0 for s in a], len(a) / SAMPLE_RATE


def build_recognizer():
    return sherpa_onnx.OfflineRecognizer.from_sense_voice(
        model=MODEL,
        tokens=TOKENS,
        num_threads=max(2, min(4, (os.cpu_count() or 4) // 2)),
        use_itn=True,
        language="auto",
        debug=False,
    )


def decode(rec, samples):
    s = rec.create_stream()
    s.accept_waveform(SAMPLE_RATE, samples)
    rec.decode_stream(s)
    r = s.result
    return (r.text or "").strip(), list(getattr(r, "tokens", []) or []), list(getattr(r, "timestamps", []) or [])


def main():
    if len(sys.argv) < 2:
        raise SystemExit("usage: asr.py <wav> [out.json]")
    wav_path = sys.argv[1]
    out_path = sys.argv[2] if len(sys.argv) > 2 else ""

    t0 = time.time()
    samples, duration = read_wav(wav_path)
    rec = build_recognizer()
    load_s = time.time() - t0

    t1 = time.time()
    segments = []

    # 有 VAD 就按语音段切分，否则整段识别
    if os.path.exists(VAD_MODEL):
        cfg = sherpa_onnx.VadModelConfig()
        cfg.silero_vad.model = VAD_MODEL
        cfg.silero_vad.threshold = 0.5
        cfg.silero_vad.min_silence_duration = 0.35
        cfg.silero_vad.min_speech_duration = 0.25
        cfg.silero_vad.max_speech_duration = 20.0
        cfg.sample_rate = SAMPLE_RATE
        vad = sherpa_onnx.VoiceActivityDetector(cfg, buffer_size_in_seconds=60)

        window = 512
        buf = []
        for i in range(0, len(samples), window):
            chunk = samples[i:i + window]
            if len(chunk) < window:
                chunk = chunk + [0.0] * (window - len(chunk))
            buf.extend(chunk)
            while len(buf) >= window:
                vad.accept_waveform(buf[:window])
                buf = buf[window:]
            while not vad.empty():
                seg = vad.front
                st = seg.start / SAMPLE_RATE
                text, toks, tss = decode(rec, seg.samples)
                if text:
                    segments.append({
                        "start": round(st, 2),
                        "end": round(st + len(seg.samples) / SAMPLE_RATE, 2),
                        "text": text,
                    })
                vad.pop()
        vad.flush()
        while not vad.empty():
            seg = vad.front
            st = seg.start / SAMPLE_RATE
            text, _, _ = decode(rec, seg.samples)
            if text:
                segments.append({
                    "start": round(st, 2),
                    "end": round(st + len(seg.samples) / SAMPLE_RATE, 2),
                    "text": text,
                })
            vad.pop()
    else:
        text, _, _ = decode(rec, samples)
        if text:
            segments.append({"start": 0.0, "end": round(duration, 2), "text": text})

    infer_s = time.time() - t1
    result = {
        "text": "".join(s["text"] for s in segments),
        "segments": segments,
        "duration": round(duration, 2),
        "loadSeconds": round(load_s, 2),
        "inferSeconds": round(infer_s, 2),
        "rtf": round(infer_s / duration, 3) if duration else 0,
    }
    js = json.dumps(result, ensure_ascii=False, indent=2)
    if out_path:
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(js)
    print(js)


if __name__ == "__main__":
    main()
