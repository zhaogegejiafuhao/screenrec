package main

// 把某条录音里最清晰的一段，做成 GPT-SoVITS 的配音参考音色。
//
// 为什么需要：克隆声音要「3~10 秒干净人声 + 逐字对应的文本」。让用户自己拿 Audacity
// 切一段、再手打一遍文本，成本太高；而这里本来就有录音和校对过的转写，
// 直接挑一句最合适的截出来，文本也是现成的。

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"screenrec/internal/winhide"
	"strings"
	"time"
)

// voicePick 挑中的参考片段
type voicePick struct {
	Start float64
	End   float64
	Text  string
}

// pickVoiceSegment 从转写里挑一句最适合当参考音频的。
//
// 筛选：8~40 字、2.5~9 秒。排序不是随便取一句，而是打分——
// GPT-SoVITS 的参考音频 5~10 秒最稳，太短（3 秒）音色不够充分、太长会被截断。
// 打分项：时长越接近 6.5 秒越高、字数越接近 20 字越高；带标点结尾的加分（说明是完整句）。
func pickVoiceSegment(segs []Segment) (voicePick, bool) {
	type scored struct {
		p voicePick
		s float64
	}
	var cands []scored
	add := func(p voicePick) {
		n := len([]rune(p.Text))
		dur := p.End - p.Start
		if n < 8 || n > 45 || dur < 2.5 || dur > 9 {
			return
		}
		// 时长分：6.5 秒满分，偏离越远扣得越多
		ds := 1 - math.Abs(dur-6.5)/3.5
		if ds < 0 {
			ds = 0
		}
		// 字数分：20 字满分
		ns := 1 - math.Abs(float64(n)-20)/20
		if ns < 0 {
			ns = 0
		}
		score := ds*2 + ns // 时长权重更高
		if strings.HasSuffix(p.Text, "。") || strings.HasSuffix(p.Text, "！") || strings.HasSuffix(p.Text, "？") {
			score += 0.3
		}
		cands = append(cands, scored{p, score})
	}

	for i, s := range segs {
		t := strings.TrimSpace(s.Text)
		add(voicePick{Start: s.Start, End: s.End, Text: t})
		// 单句太短时，把紧接着的下一句并进来凑够 5~9 秒。
		// 只并间隔 <0.5 秒的（几乎连续说话），否则中间的语气词会让文本和音频对不上。
		if i+1 < len(segs) {
			nx := segs[i+1]
			if nx.Start-s.End < 0.5 && nx.End-s.Start <= 9 {
				add(voicePick{
					Start: s.Start,
					End:   nx.End,
					Text:  t + strings.TrimSpace(nx.Text),
				})
			}
		}
	}
	if len(cands) == 0 {
		return voicePick{}, false
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.s > best.s {
			best = c
		}
	}
	return best.p, true
}

// extractVoiceRef 从媒体文件里截一段 32k 单声道 wav（GPT-SoVITS 的输入要求）
func (a *App) extractVoiceRef(media string, pick voicePick, out string) error {
	if a.ffmpeg == "" {
		return fmt.Errorf("找不到 ffmpeg")
	}
	ss := pick.Start - 0.15 // 前后各留一点，避免切掉字头字尾
	if ss < 0 {
		ss = 0
	}
	dur := pick.End - ss + 0.2
	if dur > 10 {
		dur = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.2f", ss), "-t", fmt.Sprintf("%.2f", dur),
		"-i", media, "-vn", "-ac", "1", "-ar", "32000", "-c:a", "pcm_s16le", out)
	cmd.Dir = filepath.Dir(a.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("截取参考音频失败: %v %s", err, firstN(out, 200))
	}
	if st, err := os.Stat(out); err != nil || st.Size() < 16000 {
		return fmt.Errorf("截出来的参考音频太短，这段录音可能没有可用的人声")
	}
	return nil
}

// UseFileAsVoice 从用户指定的任意音频文件里挑一句做成音色。
//
// 只处理前 5 分钟：克隆音色用不了那么长，而整段识别会让用户干等
// （一小时会议录音识别要几分钟）。
func (a *App) UseFileAsVoice(src string) (string, string, error) {
	if a.ffmpeg == "" || a.python == "" {
		return "", "", fmt.Errorf("缺少 ffmpeg 或识别引擎")
	}
	st, err := os.Stat(src)
	if err != nil || st.IsDir() {
		return "", "", fmt.Errorf("找不到音频文件：%s", src)
	}
	tmpDir, err := os.MkdirTemp(a.dataDir, "voice-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	clip := filepath.Join(tmpDir, "clip.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-i", src, "-t", "300", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", clip)
	cmd.Dir = filepath.Dir(a.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("读取音频失败（支持 wav/mp3/m4a/mp4 等）：%v %s", err, firstN(out, 200))
	}

	segs, err := a.asr.Batch(clip)
	if err != nil {
		return "", "", fmt.Errorf("识别失败: %v", err)
	}
	pick, ok := pickVoiceSegment(segs)
	if !ok {
		return "", "", fmt.Errorf("这个文件里没找到合适的一句（需要 8~40 字、2.5~9 秒的完整说话），换一段人声清晰的录音试试")
	}

	dir := filepath.Join(a.dataDir, "voices")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	out := filepath.Join(dir, "me-"+time.Now().Format("20060102-150405")+".wav")
	// 从原文件截，不经过 16k 中转，保音质
	if err := a.extractVoiceRef(src, pick, out); err != nil {
		return "", "", err
	}
	return out, pick.Text, nil
}

// UseRecordingAsVoice 把录音做成音色，返回参考音频路径与文本
func (a *App) UseRecordingAsVoice(rec *Recording) (string, string, error) {
	a.mu.RLock()
	file := rec.File
	segs := append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()

	if file == "" {
		return "", "", fmt.Errorf("这条记录没有音频文件")
	}
	if len(segs) == 0 {
		return "", "", fmt.Errorf("这条记录还没有转写内容，先转写再来设音色")
	}
	pick, ok := pickVoiceSegment(segs)
	if !ok {
		return "", "", fmt.Errorf("没找到合适的一句（需要 8~40 字、2.5~9 秒的完整句子）")
	}

	dir := filepath.Join(a.dataDir, "voices")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	out := filepath.Join(dir, "me-"+time.Now().Format("20060102-150405")+".wav")
	if err := a.extractVoiceRef(file, pick, out); err != nil {
		return "", "", err
	}
	return out, pick.Text, nil
}
