package main

// 录制中盯着「系统声音」是不是空的。
//
// 为什么需要：某些播放设备不支持 WASAPI 回环（蓝牙耳机、部分 USB 声卡），
// 采集出来的是一整段 0。原来只在停止时检查，用户录完一小时才发现「对方的话没录进去」，
// 时间已经浪费掉了。这里在录制中就判，一旦连续静音就在界面上提示换设备。

import (
	"encoding/binary"
	"log"
	"math"
	"os"
	"time"
)

// sysSilenceCheck 每 3 秒看一次回环 PCM 的最后 1 秒
const (
	sysSilenceTick  = 3 * time.Second
	sysSilenceArm   = 10 * time.Second // 开头 10 秒不判（用户可能还没开始放声音）
	sysSilenceRMS   = 24.0             // 16bit PCM 的 RMS 低于此值视为无声
	sysSilenceTries = 3                // 连续 3 次（约 9 秒）都静音才提示
)

// sysPCMLevel 读文件末尾 seconds 秒的 16bit PCM，返回 RMS
func sysPCMLevel(path string, rate, ch int, seconds float64) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false
	}
	size := st.Size()
	n := int64(rate * ch * 2 * int(seconds))
	if n > size {
		n = size
	}
	n = n / 2 * 2
	if n < 4096 {
		return 0, false
	}
	if _, err := f.Seek(size-n, 0); err != nil {
		return 0, false
	}
	buf := make([]byte, n)
	if _, err := f.Read(buf); err != nil {
		return 0, false
	}
	var sum float64
	cnt := len(buf) / 2
	for i := 0; i < cnt; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(buf[i*2:])))
		sum += v * v
	}
	if cnt == 0 {
		return 0, false
	}
	return math.Sqrt(sum / float64(cnt)), true
}

// watchSysSilence 录制期间监控系统声音，发现持续静音就写 sysWarning。
// 靠 rec.Status 自然退出：停止后最多多跑一个 tick。
func (r *Recorder) watchSysSilence(rec *Recording, sysFile string) {
	t := time.NewTicker(sysSilenceTick)
	defer t.Stop()

	start := time.Now()
	quiet := 0
	warned := false

	for range t.C {
		if time.Since(start) < sysSilenceArm {
			continue
		}

		r.mu.Lock()
		running := r.cur == rec && rec.Status == "recording"
		rate, ch := loopRate, loopCh
		r.mu.Unlock()
		if !running {
			return
		}

		rms, ok := sysPCMLevel(sysFile, rate, ch, 1.0)
		if !ok {
			continue
		}
		if rms >= sysSilenceRMS {
			// 有声音了：把之前的提示撤掉（比如用户中途才开始放视频）
			if warned {
				r.app.mu.Lock()
				rec.SysWarning = ""
				r.app.mu.Unlock()
				log.Printf("[sys] 系统声音恢复，已撤掉提示")
				warned = false
			}
			quiet = 0
			continue
		}

		quiet++
		if quiet >= sysSilenceTries && !warned {
			warned = true
			msg := "系统声音一直是静音：这个播放设备可能不支持回环采集，对方的声音录不进去。建议在设置里换成「内置扬声器」或支持回环的设备"
			r.app.mu.Lock()
			rec.SysWarning = msg
			r.app.mu.Unlock()
			log.Printf("[sys] 检测到系统声音持续静音: %s", rec.ID)
		}
	}
}
