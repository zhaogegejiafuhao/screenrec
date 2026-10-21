#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""流式识别进程（Paraformer streaming）：边喂 PCM 边出字，延迟 ~0.5 秒。

协议：
  请求 = 4 字节小端长度 N + N 字节 16k/16bit/mono PCM（N=0 表示 ping）
  响应 = 一行 JSON：{"partial": "当前草稿", "final": "整句完成"}
         final 非空表示一句话结束（端点检测触发），随后内部自动 reset。
"""
import sys
import os
import json
import struct
import array

BASE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(BASE, "pylibs"))

import sherpa_onnx  # noqa: E402

MDIR = os.path.join(BASE, "models", "stream")
SAMPLE_RATE = 16000

REC = sherpa_onnx.OnlineRecognizer.from_paraformer(
    encoder=os.path.join(MDIR, "encoder.int8.onnx"),
    decoder=os.path.join(MDIR, "decoder.int8.onnx"),
    tokens=os.path.join(MDIR, "tokens.txt"),
    num_threads=4,
    sample_rate=SAMPLE_RATE,
    feature_dim=80,
    decoding_method="greedy_search",
    enable_endpoint_detection=True,
    rule1_min_trailing_silence=2.4,
    rule2_min_trailing_silence=1.2,
    rule3_min_utterance_length=20.0,
)

STREAM = REC.create_stream()
LAST = ""

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


def emit(obj):
    stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
    stdout.flush()


def main():
    global STREAM, LAST
    while True:
        hdr = read_exact(4)
        if hdr is None:
            break
        n = struct.unpack("<I", hdr)[0]
        if n == 0:
            emit({"ok": True})
            continue
        data = read_exact(n)
        if data is None:
            break

        a = array.array("h")
        a.frombytes(data)
        samples = [s / 32768.0 for s in a]
        STREAM.accept_waveform(SAMPLE_RATE, samples)

        while REC.is_ready(STREAM):
            REC.decode_stream(STREAM)

        text = (REC.get_result(STREAM) or "").strip()

        # 协议要求每个请求都必须回一行（客户端是同步读的），没变化就回空
        if REC.is_endpoint(STREAM):
            emit({"partial": "", "final": text})
            REC.reset(STREAM)
            LAST = ""
        elif text and text != LAST:
            LAST = text
            emit({"partial": text, "final": ""})
        else:
            emit({"partial": "", "final": ""})


if __name__ == "__main__":
    main()
