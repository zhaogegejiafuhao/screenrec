// 测试辅助：用 Windows SAPI 合成指定语种的语音，供识别效果验证
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: tts <zh|en> <text> <out.wav>")
		os.Exit(1)
	}
	lang, text, out := os.Args[1], os.Args[2], os.Args[3]
	abs, err := filepath.Abs(out)
	if err != nil {
		panic(err)
	}

	dir, err := os.MkdirTemp("", "tts-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	txt := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(txt, []byte(text), 0644); err != nil {
		panic(err)
	}
	ps := filepath.Join(dir, "s.ps1")
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Speech
$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
$v = $synth.GetInstalledVoices() | Where-Object { $_.VoiceInfo.Culture.Name -like '%s*' } | Select-Object -First 1
if (-not $v) { Write-Output 'NOVOICE'; exit 3 }
$synth.SelectVoice($v.VoiceInfo.Name)
Write-Output ('VOICE=' + $v.VoiceInfo.Name + ' | ' + $v.VoiceInfo.Culture.Name)
$synth.SetOutputToWaveFile('%s')
$synth.Speak([System.IO.File]::ReadAllText('%s', [System.Text.Encoding]::UTF8))
$synth.Dispose()
`, lang, q(abs), q(txt))
	if err := os.WriteFile(ps, []byte(script), 0644); err != nil {
		panic(err)
	}

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ps)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(2)
	}
	if st, err := os.Stat(abs); err == nil {
		fmt.Printf("OK %d bytes -> %s\n", st.Size(), abs)
	}
}

// q 转义 PowerShell 单引号字符串
func q(s string) string { return strings.ReplaceAll(s, "'", "''") }
