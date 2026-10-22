#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""说话人分离：输入 16k 单声道 wav，输出 [{start,end,speaker}]。

模型：pyannote segmentation-3.0（分段）+ 3D-Speaker ERes2Net（声纹嵌入）+ 快速聚类。
"""
import sys
import os
import json
import wave
import array

BASE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(BASE, "pylibs"))

import sherpa_onnx  # noqa: E402

SEG = os.path.join(BASE, "models", "diar", "segmentation.int8.onnx")
EMB = os.path.join(BASE, "models", "diar", "embedding.onnx")
SR = 16000


def read_wav(path):
    with wave.open(path, "rb") as f:
        if f.getframerate() != SR or f.getnchannels() != 1 or f.getsampwidth() != 2:
            raise SystemExit("需要 16kHz 单声道 16bit WAV")
        raw = f.readframes(f.getnframes())
        n = f.getnframes()
    a = array.array("h")
    a.frombytes(raw)
    return [s / 32768.0 for s in a], n / SR


def main():
    if len(sys.argv) < 2:
        raise SystemExit("usage: diarize.py <wav> [threshold]")
    # 0.5 是两人对话的常用值，但多人会议下偏小——不同的人容易被并成同一个（欠分割）。
    # 0.65 是 sherpa-onnx 多人示例的常见取值，宁可多分一个人也别把两个人混成一个。
    thr = float(sys.argv[2]) if len(sys.argv) > 2 else 0.65
    samples, dur = read_wav(sys.argv[1])

    cfg = sherpa_onnx.OfflineSpeakerDiarizationConfig(
        segmentation=sherpa_onnx.OfflineSpeakerSegmentationModelConfig(
            pyannote=sherpa_onnx.OfflineSpeakerSegmentationPyannoteModelConfig(model=SEG)
        ),
        embedding=sherpa_onnx.SpeakerEmbeddingExtractorConfig(model=EMB),
        clustering=sherpa_onnx.FastClusteringConfig(num_clusters=-1, threshold=thr),
        min_duration_on=0.25,
        # 同一人换气停顿常达半秒，0.4 会把一个人的话切成好几段，再被聚类分到不同人
        min_duration_off=0.6,
    )
    sd = sherpa_onnx.OfflineSpeakerDiarization(cfg)
    result = sd.process(samples).sort_by_start_time()

    segs = []
    for s in result:
        segs.append({
            "start": round(float(s.start), 2),
            "end": round(float(s.end), 2),
            "speaker": int(s.speaker),
        })
    print(json.dumps({"segments": segs, "duration": round(dur, 2)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
