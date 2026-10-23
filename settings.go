package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

// Settings 应用级配置（持久化到 data/settings.json）
type Settings struct {
	mu   sync.Mutex
	path string

	// 首次录音前的合规确认是否已通过
	ConsentAccepted bool `json:"consentAccepted"`
	// 会议自动提醒（默认开启，故用 Off 取反存储）
	MeetingPromptOff bool `json:"meetingPromptOff"`
	// 转写后用模型修同音字（默认开启，故用 Off 取反存储）
	CorrectOff bool `json:"correctOff"`
	// 说话人聚类阈值（0.3~0.9）：越大越容易把人分成不同的人，越小越容易合并
	DiarThreshold float64 `json:"diarThreshold"`
	// 实时字幕走哪条路："" / "accurate" 用 SenseVoice 整段解码（慢 0.5 秒但准得多），
	// "fast" 用流式 Paraformer（200ms 出字，但同音字错得多）
	LiveMode string `json:"liveMode"`
	// 转写完成后自动生成纪要（默认开启，故用 Off 取反存储）
	AutoSummaryOff bool `json:"autoSummaryOff"`
	// 用户选择"不再提醒"的应用名
	MutedApps []string `json:"mutedApps"`
	// 用户手动关掉悬浮条后记住状态
	OverlayHidden bool `json:"overlayHidden"`
	// 悬浮条被拖动后的位置（左上角屏幕坐标）
	OverlayPosSet bool  `json:"overlayPosSet"`
	OverlayX      int32 `json:"overlayX"`
	OverlayY      int32 `json:"overlayY"`
	// 关闭了会议提醒的会议应用
	AcceptedCount int `json:"acceptedCount"`

	// AI 纪要：OpenAI 兼容接口（DeepSeek / 通义 / Kimi / 本地 Ollama 都行）
	LLMBaseURL string `json:"llmBaseUrl"`
	LLMAPIKey  string `json:"llmApiKey"`
	LLMModel   string `json:"llmModel"`

	// AI 纪要引擎：auto（有本地模型用本地，有 Key 用云端，都没有走规则）/
	// cloud（只用云端 API）/ local（只用本机 llama.cpp）/ off（不用模型，纯规则抽取）
	LLMEngine      string `json:"llmEngine"`
	LLMLocalURL    string `json:"llmLocalUrl"`    // 本地 llama.cpp server，默认 http://127.0.0.1:8080/v1
	LLMLocalModel  string `json:"llmLocalModel"`  // 填什么都行，llama.cpp 以加载的模型为准
	LLMLocalBin    string `json:"llmLocalBin"`    // llama-server.exe 路径（填了才会自动拉起）
	LLMLocalGGUF   string `json:"llmLocalGguf"`   // .gguf 模型路径（Qwen3-4B-Q4_K_M 等）
	LLMLocalGPULay int    `json:"llmLocalGpuLay"` // 放 GPU 的层数，-1=全部；显存小就调小

	// 实时双语字幕
	LiveTranslate bool   `json:"liveTranslate"`
	LiveTransDir  string `json:"liveTransDir"` // auto / en2zh / zh2en

	// 日历到点自动录制
	CalEnabled   bool   `json:"calEnabled"`   // 总开关，默认关
	CalSource    string `json:"calSource"`    // 空或 outlook=本机 Outlook；http(s) 订阅链接；本地 .ics 路径
	CalLeadMin   int    `json:"calLeadMin"`   // 提前几分钟提醒，默认 2
	CalAutoStart bool   `json:"calAutoStart"` // 到点直接开录（不弹提示），默认关
	CalAutoStop  bool   `json:"calAutoStop"`  // 日程结束时间后自动停，默认开
	CalMode      string `json:"calMode"`      // audio / screen / 空=沿用上次

	// 声音克隆（对接 GPT-SoVITS 的 HTTP API）
	TTSURL     string `json:"ttsUrl"`     // 例如 http://127.0.0.1:9880
	TTSRefPath string `json:"ttsRefPath"` // 参考音频的绝对路径（服务端要能读到）
	TTSPrompt  string `json:"ttsPrompt"`  // 参考音频对应的文字
	TTSLang    string `json:"ttsLang"`    // zh / en / ja
}

const (
	defaultLLMBase     = "https://api.deepseek.com/v1"
	defaultLLMModel    = "deepseek-chat"
	defaultLLMLocalURL = "http://127.0.0.1:8080/v1"
)

// LLM 返回接口配置，空值时给默认（DeepSeek）
func (s *Settings) LLM() (base, key, model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	base, key, model = s.LLMBaseURL, s.LLMAPIKey, s.LLMModel
	if base == "" {
		base = defaultLLMBase
	}
	if model == "" {
		model = defaultLLMModel
	}
	return
}

// SetLLM 保存接口配置；key 为空表示保持原值，传 "-" 表示清除
func (s *Settings) SetLLM(base, key, model string) {
	s.mu.Lock()
	s.LLMBaseURL = base
	s.LLMModel = model
	if key == "-" {
		s.LLMAPIKey = ""
	} else if key != "" {
		s.LLMAPIKey = key
	}
	s.mu.Unlock()
	s.save()
}

func (s *Settings) HasLLMKey() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LLMAPIKey != ""
}

// LLMEngineConfig 返回纪要引擎配置（engine, base, key, model, localURL, localModel, localBin, localGGUF, gpuLayers）
func (s *Settings) LLMEngineConfig() (engine, base, key, model, localURL, localModel, localBin, localGGUF string, gpuLayers int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	engine = s.LLMEngine
	if engine == "" {
		engine = "auto"
	}
	base, key, model = s.LLMBaseURL, s.LLMAPIKey, s.LLMModel
	if base == "" {
		base = defaultLLMBase
	}
	if model == "" {
		model = defaultLLMModel
	}
	localURL, localModel = s.LLMLocalURL, s.LLMLocalModel
	if localURL == "" {
		localURL = defaultLLMLocalURL
	}
	if localModel == "" {
		localModel = "local"
	}
	localBin, localGGUF, gpuLayers = s.LLMLocalBin, s.LLMLocalGGUF, s.LLMLocalGPULay
	if gpuLayers == 0 {
		gpuLayers = -1 // -1 表示尽量都放 GPU
	}
	return
}

// SetLLMEngine 保存纪要引擎配置，空值表示保持原样
func (s *Settings) SetLLMEngine(engine, localURL, localModel, localBin, localGGUF string, gpuLayers int) {
	s.mu.Lock()
	if engine != "" {
		s.LLMEngine = engine
	}
	if localURL != "" {
		s.LLMLocalURL = localURL
	}
	if localModel != "" {
		s.LLMLocalModel = localModel
	}
	if localBin != "" {
		s.LLMLocalBin = localBin
	}
	if localGGUF != "" {
		s.LLMLocalGGUF = localGGUF
	}
	if gpuLayers != 0 {
		s.LLMLocalGPULay = gpuLayers
	}
	s.mu.Unlock()
	s.save()
}

// CalendarConfig 返回日历相关配置（enabled, source, leadMin, autoStart, autoStop, mode）
func (s *Settings) CalendarConfig() (bool, string, int, bool, bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lead := s.CalLeadMin
	if lead <= 0 {
		lead = 2
	}
	return s.CalEnabled, s.CalSource, lead, s.CalAutoStart, s.CalAutoStop, s.CalMode
}

// SetCalendar 保存日历配置
func (s *Settings) SetCalendar(enabled bool, source string, leadMin int, autoStart, autoStop bool, mode string) {
	s.mu.Lock()
	s.CalEnabled = enabled
	s.CalSource = strings.TrimSpace(source)
	s.CalLeadMin = leadMin
	s.CalAutoStart = autoStart
	s.CalAutoStop = autoStop
	s.CalMode = mode
	s.mu.Unlock()
	s.save()
}

// AutoCorrect 转写完是否用模型修同音字
func (s *Settings) AutoCorrect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.CorrectOff
}

func (s *Settings) SetAutoCorrect(on bool) {
	s.mu.Lock()
	s.CorrectOff = !on
	s.mu.Unlock()
	s.save()
}

// DiarThresholdVal 说话人聚类阈值（默认 0.65）
func (s *Settings) DiarThresholdVal() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DiarThreshold <= 0 || s.DiarThreshold > 0.95 {
		return 0.65
	}
	return s.DiarThreshold
}

func (s *Settings) SetDiarThreshold(v float64) {
	if v < 0.3 {
		v = 0.3
	}
	if v > 0.9 {
		v = 0.9
	}
	s.mu.Lock()
	s.DiarThreshold = v
	s.mu.Unlock()
	s.save()
}

// LiveModeAccurate 实时字幕是否用高精度模型（默认是）
func (s *Settings) LiveModeAccurate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LiveMode != "fast"
}

func (s *Settings) SetLiveMode(mode string) {
	s.mu.Lock()
	s.LiveMode = mode
	s.mu.Unlock()
	s.save()
}

func (s *Settings) LiveTransConfig() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.LiveTransDir
	if dir == "" {
		dir = "auto"
	}
	return s.LiveTranslate, dir
}

func (s *Settings) SetLiveTranslate(on bool, dir string) {
	s.mu.Lock()
	s.LiveTranslate = on
	s.LiveTransDir = dir
	s.mu.Unlock()
	s.save()
}

// TTSConfig 返回声音克隆配置（默认中文）
func (s *Settings) TTSConfig() (url, ref, prompt, lang string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lang = s.TTSLang
	if lang == "" {
		lang = "zh"
	}
	return s.TTSURL, s.TTSRefPath, s.TTSPrompt, lang
}

func (s *Settings) SetTTS(url, ref, prompt, lang string) {
	s.mu.Lock()
	s.TTSURL, s.TTSRefPath, s.TTSPrompt, s.TTSLang = url, ref, prompt, lang
	s.mu.Unlock()
	s.save()
}

func loadSettings(dataDir string) *Settings {
	s := &Settings{path: filepath.Join(dataDir, "settings.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, s)
	}
	return s
}

func (s *Settings) save() {
	s.mu.Lock()
	b, _ := json.MarshalIndent(s, "", "  ")
	path := s.path
	s.mu.Unlock()
	if b != nil {
		os.WriteFile(path, b, 0644)
	}
}

func (s *Settings) ConsentOK() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ConsentAccepted
}

func (s *Settings) AcceptConsent() {
	s.mu.Lock()
	s.ConsentAccepted = true
	s.mu.Unlock()
	s.save()
}

// AutoSummary 录完是否自动出纪要
func (s *Settings) AutoSummary() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.AutoSummaryOff
}

func (s *Settings) SetAutoSummary(on bool) {
	s.mu.Lock()
	s.AutoSummaryOff = !on
	s.mu.Unlock()
	s.save()
}

func (s *Settings) MeetingEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.MeetingPromptOff
}

func (s *Settings) SetMeetingEnabled(on bool) {
	s.mu.Lock()
	s.MeetingPromptOff = !on
	s.mu.Unlock()
	s.save()
}

func (s *Settings) MuteApp(name string) {
	s.mu.Lock()
	for _, m := range s.MutedApps {
		if m == name {
			s.mu.Unlock()
			s.save()
			return
		}
	}
	s.MutedApps = append(s.MutedApps, name)
	s.mu.Unlock()
	s.save()
}

func (s *Settings) IsMuted(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.MutedApps {
		if m == name {
			return true
		}
	}
	return false
}

func (s *Settings) ResetConsent() {
	s.mu.Lock()
	s.ConsentAccepted = false
	s.mu.Unlock()
	s.save()
}

func (s *Settings) UnmuteApp(name string) {
	s.mu.Lock()
	out := s.MutedApps[:0]
	for _, m := range s.MutedApps {
		if m != name {
			out = append(out, m)
		}
	}
	s.MutedApps = out
	s.mu.Unlock()
	s.save()
}

// Muted 返回静音名单副本
func (s *Settings) Muted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.MutedApps...)
}

func (s *Settings) Hidden() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OverlayHidden
}

func (s *Settings) SetHidden(v bool) {
	s.mu.Lock()
	s.OverlayHidden = v
	s.mu.Unlock()
	s.save()
}

// OverlayPos 返回记住的悬浮条位置
func (s *Settings) OverlayPos() (int32, int32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OverlayX, s.OverlayY, s.OverlayPosSet
}

// SetOverlayPos 记住悬浮条位置（拖动结束后调用）
func (s *Settings) SetOverlayPos(x, y int32) {
	s.mu.Lock()
	if s.OverlayPosSet && s.OverlayX == x && s.OverlayY == y {
		s.mu.Unlock()
		return
	}
	s.OverlayX, s.OverlayY, s.OverlayPosSet = x, y, true
	s.mu.Unlock()
	s.save()
}

// ResetOverlayPos 复位到默认位置
func (s *Settings) ResetOverlayPos() {
	s.mu.Lock()
	s.OverlayPosSet = false
	s.mu.Unlock()
	s.save()
}

// ---- 原生对话框 ----
// 说明：部分机器（组策略/安全软件）会把 MessageBox 静默返回 IDOK 而不显示，
// 因此录音授权确认不依赖原生对话框，改为自绘在悬浮条上（见 Overlay.ShowConsent）。

var pMessageBoxW = u32.NewProc("MessageBoxW")

const (
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
	mbIconInfo      = 0x00000040
	mbYesNo         = 0x00000004
)

func messageBox(title, text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16(text))),
		uintptr(unsafe.Pointer(mustUTF16(title))),
		flags|mbSetForeground|mbTopMost)
	return int(r)
}

// confirmDialog 通用二次确认（用于删除等不可逆操作）
func confirmDialog(title, text string) bool {
	return messageBox(title, text, mbYesNo|mbIconInfo) == 6 // IDYES
}
