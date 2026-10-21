#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""进程级回环采集（Windows 10 2004+ 的 AUDIOCLIENT_ACTIVATION_TYPE_PROCESS_LOOPBACK）。

和 soundcard 的设备回环不同，它抓的是「进程树的音频流」，
**与输出设备无关** —— 戴着蓝牙耳机也能录到声音。

用法：
  python loopback_proc.py --exclude-self --out rec.sys.pcm
  python loopback_proc.py --pid 12345 --out rec.sys.pcm      # 只录某个进程树

协议（与 loopback.py 一致）：
  就绪后 stderr 打印 READY <unix_ms>
  停止：被父进程结束（输出的是裸 PCM，截断无副作用）
"""
import argparse
import ctypes
import os
import sys
import threading
import time

ole32 = ctypes.WinDLL("ole32")
mmdevapi = ctypes.WinDLL("Mmdevapi")

# ---- 常量 ----
VAD_PROCESS_LOOPBACK = "VAD\\Process_Loopback"
CLSCTX_ALL = 0x17
AUDCLNT_SHAREMODE_SHARED = 0
AUDCLNT_STREAMFLAGS_LOOPBACK = 0x00020000
AUDCLNT_STREAMFLAGS_EVENTCALLBACK = 0x00040000
AUDCLNT_BUFFERFLAGS_SILENT = 0x2
WAVE_FORMAT_PCM = 1
VT_BLOB = 65
ACTIVATION_TYPE_PROCESS_LOOPBACK = 1
MODE_INCLUDE = 0
MODE_EXCLUDE = 1


class GUID(ctypes.Structure):
    _fields_ = [("Data1", ctypes.c_ulong), ("Data2", ctypes.c_ushort),
                ("Data3", ctypes.c_ushort), ("Data4", ctypes.c_ubyte * 8)]

    def __init__(self, s=None):
        super().__init__()
        if s:
            ctypes.oledll.ole32.CLSIDFromString(s, ctypes.byref(self))


IID_IAudioClient = GUID("{1CB9AD4C-DBFA-4c32-B178-C2F568A703B2}")
IID_IActivateAudioInterfaceCompletionHandler = GUID("{41D949AB-9862-444A-80F6-C261334DA5EB}")
IID_IActivateAudioInterfaceAsyncOperation = GUID("{72A22D78-CDE4-431D-B8CC-843A71199B6D}")

IID_HANDLER_BYTES = bytes(ctypes.string_at(ctypes.byref(IID_IActivateAudioInterfaceCompletionHandler),
                                           ctypes.sizeof(GUID)))
_iid_unknown = GUID("{00000000-0000-0000-C000-000000000046}")
IID_UNKNOWN_BYTES = bytes(ctypes.string_at(ctypes.byref(_iid_unknown), ctypes.sizeof(GUID)))
# IAgileObject：跨 apartment 传递回调时必须应答，否则激活会直接 E_UNEXPECTED
_iid_agile = GUID("{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}")
IID_AGILE_BYTES = bytes(ctypes.string_at(ctypes.byref(_iid_agile), ctypes.sizeof(GUID)))


class WAVEFORMATEX(ctypes.Structure):
    _fields_ = [("wFormatTag", ctypes.c_ushort), ("nChannels", ctypes.c_ushort),
                ("nSamplesPerSec", ctypes.c_ulong), ("nAvgBytesPerSec", ctypes.c_ulong),
                ("nBlockAlign", ctypes.c_ushort), ("wBitsPerSample", ctypes.c_ushort),
                ("cbSize", ctypes.c_ushort)]


class PROCESS_LOOPBACK_PARAMS(ctypes.Structure):
    _fields_ = [("TargetProcessId", ctypes.c_ulong), ("ProcessLoopbackMode", ctypes.c_int)]


class ACTIVATION_PARAMS(ctypes.Structure):
    _fields_ = [("ActivationType", ctypes.c_int), ("Params", PROCESS_LOOPBACK_PARAMS)]


class PROPVARIANT(ctypes.Structure):
    _fields_ = [("vt", ctypes.c_ushort), ("r1", ctypes.c_ushort),
                ("r2", ctypes.c_ushort), ("r3", ctypes.c_ushort),
                ("cbSize", ctypes.c_ulong), ("pBlobData", ctypes.c_void_p)]


# ---- COM 调用辅助 ----
def vtbl_call(ptr, index, restype, argtypes, *args):
    vtbl = ctypes.cast(ptr, ctypes.POINTER(ctypes.POINTER(ctypes.c_void_p))).contents
    fn = ctypes.WINFUNCTYPE(restype, ctypes.c_void_p, *argtypes)(vtbl[index])
    return fn(ptr, *args)


def release(ptr):
    if ptr:
        try:
            vtbl_call(ptr, 2, ctypes.c_ulong, [])
        except Exception:
            pass


# ---- 完成回调（IActivateAudioInterfaceCompletionHandler）----
class CompletionHandler:
    def __init__(self):
        self.event = threading.Event()
        self.operation = None
        self.hr_activate = -1
        self.iface = None
        # 必须保住回调对象，否则 C 侧调用会崩
        self._qi = ctypes.WINFUNCTYPE(ctypes.c_long, ctypes.c_void_p, ctypes.c_void_p,
                                      ctypes.c_void_p)(self._query_interface)
        self._addref = ctypes.WINFUNCTYPE(ctypes.c_ulong, ctypes.c_void_p)(self._add_ref)
        self._rel = ctypes.WINFUNCTYPE(ctypes.c_ulong, ctypes.c_void_p)(self._release)
        self._done = ctypes.WINFUNCTYPE(ctypes.c_long, ctypes.c_void_p,
                                        ctypes.c_void_p)(self._completed)
        vtbl = (ctypes.c_void_p * 4)(
            ctypes.cast(self._qi, ctypes.c_void_p),
            ctypes.cast(self._addref, ctypes.c_void_p),
            ctypes.cast(self._rel, ctypes.c_void_p),
            ctypes.cast(self._done, ctypes.c_void_p))
        self._vtbl = vtbl
        self._obj = (ctypes.c_void_p * 1)(ctypes.cast(vtbl, ctypes.c_void_p))

    @property
    def ptr(self):
        return ctypes.cast(self._obj, ctypes.c_void_p)

    def _query_interface(self, this, riid, out):
        # 必须正确应答 IUnknown / IActivateAudioInterfaceCompletionHandler，
        # 否则激活层 QI 失败，ActivateAudioInterfaceAsync 会直接返回错误。
        try:
            g = ctypes.cast(riid, ctypes.POINTER(GUID)).contents
            raw = bytes(ctypes.string_at(ctypes.byref(g), ctypes.sizeof(GUID)))
            if raw in (IID_HANDLER_BYTES, IID_UNKNOWN_BYTES, IID_AGILE_BYTES):
                ctypes.cast(out, ctypes.POINTER(ctypes.c_void_p))[0] = this
                return 0
        except Exception:
            pass
        return -2147467262  # E_NOINTERFACE

    def _add_ref(self, this):
        return 2

    def _release(self, this):
        return 1

    def _completed(self, this, operation):
        self.operation = operation
        self.event.set()
        return 0


def activate_process_loopback(pid, exclude):
    """拿到指定进程树（或排除它）的回环 IAudioClient 指针"""
    params = ACTIVATION_PARAMS()
    params.ActivationType = ACTIVATION_TYPE_PROCESS_LOOPBACK
    params.Params.TargetProcessId = pid
    params.Params.ProcessLoopbackMode = MODE_EXCLUDE if exclude else MODE_INCLUDE

    pv = PROPVARIANT()
    pv.vt = VT_BLOB
    pv.cbSize = ctypes.sizeof(ACTIVATION_PARAMS)
    pv.pBlobData = ctypes.cast(ctypes.pointer(params), ctypes.c_void_p)

    mmdevapi.ActivateAudioInterfaceAsync.restype = ctypes.c_long
    mmdevapi.ActivateAudioInterfaceAsync.argtypes = [
        ctypes.c_wchar_p, ctypes.POINTER(GUID), ctypes.POINTER(PROPVARIANT),
        ctypes.c_void_p, ctypes.POINTER(ctypes.c_void_p)]

    handler = CompletionHandler()
    op = ctypes.c_void_p()
    dev = ctypes.c_wchar_p(VAD_PROCESS_LOOPBACK)
    hr = mmdevapi.ActivateAudioInterfaceAsync(
        dev, ctypes.byref(IID_IAudioClient), ctypes.byref(pv),
        handler.ptr, ctypes.byref(op))
    if hr < 0:
        raise OSError("ActivateAudioInterfaceAsync 失败 0x%08x" % (hr & 0xFFFFFFFF))

    if not handler.event.wait(8):
        raise TimeoutError("等待激活回调超时")

    # IActivateAudioInterfaceAsyncOperation::GetActivateResult
    result = ctypes.c_long(0)
    iface = ctypes.c_void_p()
    hr2 = vtbl_call(op, 3, ctypes.c_long,
                    [ctypes.POINTER(ctypes.c_long), ctypes.POINTER(ctypes.c_void_p)],
                    ctypes.byref(result), ctypes.byref(iface))
    release(op)
    if hr2 < 0:
        raise OSError("GetActivateResult 失败 0x%08x" % (hr2 & 0xFFFFFFFF))
    if result.value < 0:
        raise OSError("激活被拒绝 0x%08x" % (result.value & 0xFFFFFFFF))
    if not iface:
        raise OSError("激活返回空接口")
    return iface


def init_capture(client, rate, channels, bits):
    fmt = WAVEFORMATEX()
    fmt.wFormatTag = WAVE_FORMAT_PCM
    fmt.nChannels = channels
    fmt.nSamplesPerSec = rate
    fmt.wBitsPerSample = bits
    fmt.nBlockAlign = channels * bits // 8
    fmt.nAvgBytesPerSec = rate * fmt.nBlockAlign
    fmt.cbSize = 0
    flags = AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK
    hr = vtbl_call(client, 3, ctypes.c_long,
                   [ctypes.c_int, ctypes.c_ulong, ctypes.c_longlong, ctypes.c_longlong,
                    ctypes.POINTER(WAVEFORMATEX), ctypes.c_void_p],
                   AUDCLNT_SHAREMODE_SHARED, flags, 20 * 10000, 0,
                   ctypes.byref(fmt), None)
    if hr < 0:
        return False, hr
    ev = ctypes.windll.kernel32.CreateEventW(None, False, False, None)
    hr = vtbl_call(client, 13, ctypes.c_long, [ctypes.c_void_p], ctypes.c_void_p(ev))
    if hr < 0:
        return False, hr
    return True, ev


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--pid", type=int, default=0)
    ap.add_argument("--exclude-self", action="store_true")
    ap.add_argument("--rate", type=int, default=48000)
    ap.add_argument("--channels", type=int, default=2)
    ap.add_argument("--bits", type=int, default=16)
    ap.add_argument("--out", default="")
    ap.add_argument("--seconds", type=float, default=0)
    args = ap.parse_args()

    ole32.CoInitializeEx(None, 2)  # APARTMENTTHREADED
    target = os.getpid() if args.exclude_self else args.pid
    if not target:
        print("LOOPBACK_FAIL 需要 --pid 或 --exclude-self", file=sys.stderr, flush=True)
        return 2

    try:
        client = activate_process_loopback(target, args.exclude_self)
    except Exception as e:
        print("LOOPBACK_FAIL %s" % e, file=sys.stderr, flush=True)
        return 3

    ok, ev_or_hr = init_capture(client, args.rate, args.channels, args.bits)
    if not ok:
        print("LOOPBACK_FAIL Initialize 0x%08x" % (ev_or_hr & 0xFFFFFFFF), file=sys.stderr, flush=True)
        return 4
    ev = ev_or_hr

    # IAudioClient::GetService(IAudioCaptureClient)
    IID_Capture = GUID("{C8ADBD64-E71E-48a0-A4DE-185C395CD317}")
    cap = ctypes.c_void_p()
    hr = vtbl_call(client, 14, ctypes.c_long,
                   [ctypes.POINTER(GUID), ctypes.POINTER(ctypes.c_void_p)],
                   ctypes.byref(IID_Capture), ctypes.byref(cap))
    if hr < 0 or not cap:
        print("LOOPBACK_FAIL GetService 0x%08x" % (hr & 0xFFFFFFFF), file=sys.stderr, flush=True)
        return 5

    hr = vtbl_call(client, 10, ctypes.c_long, [])  # Start
    if hr < 0:
        print("LOOPBACK_FAIL Start 0x%08x" % (hr & 0xFFFFFFFF), file=sys.stderr, flush=True)
        return 6

    out = open(args.out, "wb") if args.out else sys.stdout.buffer
    print("READY %d" % int(time.time() * 1000), file=sys.stderr, flush=True)

    deadline = time.time() + args.seconds if args.seconds else None
    blocksize = ctypes.c_uint32(0)
    data = ctypes.POINTER(ctypes.c_ubyte)()
    flags = ctypes.c_ulong(0)
    devpos = ctypes.c_ulonglong(0)
    qpcpos = ctypes.c_ulonglong(0)
    written = 0
    silent_blocks = 0
    voiced = 0
    warned = False
    while True:
        if deadline and time.time() > deadline:
            break
        ctypes.windll.kernel32.WaitForSingleObject(ctypes.c_void_p(ev), 200)
        while True:
            hr = vtbl_call(cap, 3, ctypes.c_long,
                           [ctypes.POINTER(ctypes.POINTER(ctypes.c_ubyte)),
                            ctypes.POINTER(ctypes.c_uint32), ctypes.POINTER(ctypes.c_ulong),
                            ctypes.POINTER(ctypes.c_ulonglong), ctypes.POINTER(ctypes.c_ulonglong)],
                           ctypes.byref(data), ctypes.byref(blocksize),
                           ctypes.byref(flags), ctypes.byref(devpos), ctypes.byref(qpcpos))
            # S_BUFFER_EMPTY(0x08890001) 是正数，必须用 hr != 0 判断；
            # 只看 hr < 0 会把上一包的 blocksize 当成新数据反复写入。
            if hr != 0 or blocksize.value == 0:
                break
            n = blocksize.value * args.channels * args.bits // 8
            if flags.value & AUDCLNT_BUFFERFLAGS_SILENT:
                buf = b"\x00" * n
            else:
                buf = ctypes.string_at(data, n)
            if not any(buf):
                silent_blocks += 1
                if not warned and voiced == 0 and silent_blocks * blocksize.value >= args.rate * 5:
                    warned = True
                    print("LOOPBACK_SILENT", file=sys.stderr, flush=True)
            else:
                voiced += 1
                silent_blocks = 0
            out.write(buf)
            out.flush()
            written += n
            vtbl_call(cap, 4, ctypes.c_long, [ctypes.c_uint32], blocksize.value)
    try:
        vtbl_call(client, 11, ctypes.c_long, [])  # Stop
    except Exception:
        pass
    print("LOOPBACK_DONE %d" % written, file=sys.stderr, flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
