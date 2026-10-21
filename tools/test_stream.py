import subprocess, struct, json, wave, threading, time, sys

PY = r"D:\tools\GPT-SoVITS\GPT-SoVITS-v2pro-20250604\runtime\python.exe"
CWD = r"D:\Test\screenrec"

p = subprocess.Popen([PY, "asr_stream.py"], cwd=CWD,
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.STDOUT, bufsize=0)

lines = []
done = threading.Event()

def reader():
    while True:
        line = p.stdout.readline()
        if not line:
            break
        txt = line.decode("utf-8", "replace").strip()
        lines.append(txt)
        print("<<", txt[:160], flush=True)

threading.Thread(target=reader, daemon=True).start()

# ping
p.stdin.write(struct.pack("<I", 0)); p.stdin.flush()
time.sleep(1.0)

w = wave.open(r"D:\Test\screenrec\test16k.wav")
data = w.readframes(w.getnframes())
print("audio bytes", len(data), "duration", len(data) / 32000.0)

chunk = 3200  # 0.1s
t0 = time.time()
for i in range(0, len(data), chunk):
    part = data[i:i+chunk]
    p.stdin.write(struct.pack("<I", len(part)) + part)
    p.stdin.flush()
    time.sleep(0.05)  # 2x realtime

time.sleep(2.5)
p.stdin.close()
time.sleep(0.5)
p.terminate()
print("--- 共收到", len(lines), "条，用时 %.1fs" % (time.time() - t0))
