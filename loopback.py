#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""采集电脑正在播放的声音（WASAPI 回环），以 s16le PCM 写到 stdout。

用法：
  python loopback.py --device "扬声器 (Realtek(R) Audio)" --rate 48000 --channels 2

控制协议：
  - 启动就绪后在 stderr 打印一行  READY <unix_ms>
  - Go 侧读到 READY 才启动 ffmpeg，用来计算音画对齐偏移
  - 停止：Go 直接结束进程（stdout 是裸 PCM，截断无副作用）
"""
import argparse
import queue
import sys
import threading
import time


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--device", default="")
    ap.add_argument("--rate", type=int, default=48000)
    ap.add_argument("--channels", type=int, default=2)
    ap.add_argument("--libs", default="")
    ap.add_argument("--out", default="")
    args = ap.parse_args()

    if args.libs and args.libs not in sys.path:
        sys.path.insert(0, args.libs)
    import numpy as np
    import soundcard as sc

    speaker = None
    if args.device:
        for sp in sc.all_speakers():
            if sp.name == args.device:
                speaker = sp
                break
        if speaker is None:
            for sp in sc.all_speakers():
                if args.device in sp.name:
                    speaker = sp
                    break
    if speaker is None:
        speaker = sc.default_speaker()

    try:
        mic = sc.get_microphone(id=str(speaker.name), include_loopback=True)
    except Exception as e:
        print("LOOPBACK_FAIL %s" % e, file=sys.stderr, flush=True)
        return 2

    chunk = max(480, args.rate // 50)  # 20ms
    if args.out:
        # 直接写文件：录制结束后再由 ffmpeg 与麦克风混音。
        # 比"边录边喂 ffmpeg"稳得多——消费端变慢不会反压采集，也不会把视频管线拖死。
        out = open(args.out, "wb")
    else:
        out = sys.stdout.buffer

    # 采集与写盘解耦：消费端（ffmpeg）偶尔变慢时，不能让它反压采集，
    # 否则 WASAPI 缓冲溢出会丢数据（soundcard 会报 data discontinuity）。
    q = queue.Queue(maxsize=250)  # 约 5 秒缓冲
    stop = threading.Event()

    def writer():
        while not stop.is_set():
            try:
                item = q.get(timeout=0.2)
            except queue.Empty:
                continue
            if item is None:
                return
            try:
                out.write(item)
                out.flush()
            except Exception:
                return

    wt = threading.Thread(target=writer, daemon=True)
    wt.start()

    zero_run = 0
    voiced = 0
    dropped = 0
    try:
        with mic.recorder(samplerate=args.rate, channels=args.channels) as rec:
            print("READY %d" % int(time.time() * 1000), file=sys.stderr, flush=True)
            while True:
                data = rec.record(numframes=chunk)
                if data is None:
                    continue
                arr = np.asarray(data, dtype=np.float32)
                if arr.ndim == 1:
                    arr = arr.reshape(-1, 1)
                if arr.shape[1] != args.channels:
                    if arr.shape[1] == 1 and args.channels == 2:
                        arr = np.repeat(arr, 2, axis=1)
                    else:
                        arr = arr[:, : args.channels]
                peak = float(np.abs(arr).max()) if arr.size else 0.0
                if peak == 0.0:
                    zero_run += 1
                    # 只有"从开始到现在一直没出现过声音"才判定设备不支持回环；
                    # 中间短暂静音不能算，否则静音段一多就误报。
                    if zero_run == 250 and voiced == 0:  # 连续 5 秒全 0
                        print("LOOPBACK_SILENT", file=sys.stderr, flush=True)
                else:
                    zero_run = 0
                    voiced += 1
                pcm = (np.clip(arr, -1.0, 1.0) * 32767.0).astype("<i2").tobytes()
                try:
                    q.put_nowait(pcm)
                except queue.Full:
                    # 消费端卡住：丢掉这一块并记数，绝不能让采集端阻塞
                    dropped += 1
                    if dropped in (10, 100, 1000):
                        print("LOOPBACK_DROP %d" % dropped, file=sys.stderr, flush=True)
    except KeyboardInterrupt:
        pass
    except Exception as e:
        print("LOOPBACK_ERR %s" % e, file=sys.stderr, flush=True)
        stop.set()
        return 3
    stop.set()
    return 0


if __name__ == "__main__":
    sys.exit(main())
