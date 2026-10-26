package main

// 把某条录音里最清晰的一段，做成 GPT-SoVITS 的配音参考音色。
//
// 为什么需要：克隆声音要「3~10 秒干净人声 + 逐字对应的文本」。让用户自己拿 Audacity
// 切一段、再手打一遍文本，成本太高；而这里本来就有录音和校对过的转写，
// 直接挑一句最合适的截出来，文本也是现成的。

import (
	"context"
	"fmt"
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

// pickVoiceSegment 从转写里挑一句适合当参考音频的：
// 长度 8~40 字、时长 2.5~9 秒、带标点结尾（说明是一句完整的话）。
// 优先挑中间位置的，开头结尾常有人声没起来或被截断。
func pickVoiceSegment(segs []Segment) (voicePick, bool) {
	var cands []voicePick
	for _, s := range segs {
		t := strings.TrimSpace(s.Text)
		n := len([]rune(t))
		dur := s.End - s.Start
		if n < 8 || n > 40 || dur < 2.5 || dur > 9 {
			continue
		}
		cands = append(cands, voicePick{Start: s.Start, End: s.End, Text: t})
	}
	if len(cands) == 0 {
		return voicePick{}, false
	}
	// 取候选中段那句（中间位置的录音质量通常最稳）
	return cands[len(cands)/2], true
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
