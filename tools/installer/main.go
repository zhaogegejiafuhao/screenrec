// 录屏工坊安装包（单文件 setup.exe）。
//
// 做法：先编出这个 stub，再把发布 zip 追加到自己末尾，最后写 48 字节页脚
// （magic + payload 长度 + 版本号）。运行时从自己尾巴里读回 zip 解压，
// 因此不需要任何第三方打包工具，也不需要联网。
//
// 用法：
//
//	go build -o build/setup-stub.exe ./tools/installer
//	build/setup-stub.exe -pack dist/screenrec-0.1.0-win64.zip -out dist/setup-0.1.0.exe
//	dist/setup-0.1.0.exe            # 双击安装（当前用户，无需管理员）
//	dist/setup-0.1.0.exe -uninstall # 卸载
package main

import (
	"screenrec/internal/winhide"
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

const (
	pkgMagic  = "SRECPKG1"
	footerLen = 8 + 8 + 32 // magic + payload 长度 + 版本号（定长，补 \0）
	appName   = "录屏工坊"
	appExe    = "screenrec.exe"
	uninsExe  = "unins.exe"
	regKey    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\ScreenRec`
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	pSetConsoleCP   = kernel32.NewProc("SetConsoleCP")
	pSetConsoleOut  = kernel32.NewProc("SetConsoleOutputCP")
)

func main() {
	pSetConsoleCP.Call(65001)      // 控制台按 UTF-8 收发，否则中文提示是乱码
	pSetConsoleOut.Call(65001)

	installDir := flag.String("dir", "", "安装目录（默认 %LOCALAPPDATA%\\Programs\\ScreenRec）")
	uninstall := flag.Bool("uninstall", false, "卸载")
	purge := flag.String("purge", "", "内部用：由临时副本删除安装目录")
	pack := flag.String("pack", "", "把发布 zip 追加到本程序，生成安装包")
	out := flag.String("out", "", "配合 -pack：输出文件")
	ver := flag.String("version", "0.1.0", "配合 -pack：写进安装包的版本号")
	silent := flag.Bool("silent", false, "静默安装（不询问）")
	noLaunch := flag.Bool("nolaunch", false, "安装完成后不启动程序")
	flag.Parse()

	if *pack != "" {
		if err := doPack(*pack, *out, *ver); err != nil {
			fmt.Println("打包失败:", err)
			os.Exit(1)
		}
		return
	}
	if *uninstall {
		doUninstall(*silent)
		return
	}
	if *purge != "" {
		purgeDir(*purge)
		return
	}
	doInstall(*installDir, *silent, *noLaunch)
}

// ---------- 打包 ----------

func doPack(zipPath, outPath, version string) error {
	if outPath == "" {
		outPath = strings.TrimSuffix(zipPath, ".zip") + "-setup.exe"
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	stub, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	payload, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return fmt.Errorf("发布包是空的")
	}

	footer := make([]byte, footerLen)
	copy(footer, pkgMagic)
	binary.LittleEndian.PutUint64(footer[8:], uint64(len(payload)))
	copy(footer[16:], []byte(version))

	var buf bytes.Buffer
	buf.Write(stub)
	buf.Write(payload)
	buf.Write(footer)
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0755); err != nil {
		return err
	}
	fmt.Printf("已生成 %s（%.1f MB，其中程序 %.1f MB + 安装内容 %.1f MB）\n",
		outPath,
		float64(buf.Len())/1048576,
		float64(len(stub))/1048576,
		float64(len(payload))/1048576)
	return nil
}

// readPayload 从自己的尾部读回安装内容
func readPayload() ([]byte, string, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(self)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if st.Size() < footerLen {
		return nil, "", fmt.Errorf("这个 exe 是未打包的 stub，先用 -pack 生成安装包")
	}
	footer := make([]byte, footerLen)
	if _, err := f.ReadAt(footer, st.Size()-footerLen); err != nil {
		return nil, "", err
	}
	if string(footer[:8]) != pkgMagic {
		return nil, "", fmt.Errorf("这个 exe 里没有安装内容（页脚缺失）")
	}
	n := int64(binary.LittleEndian.Uint64(footer[8:]))
	if n <= 0 || n > st.Size()-footerLen {
		return nil, "", fmt.Errorf("安装内容长度异常")
	}
	version := strings.TrimRight(string(footer[16:]), "\x00")
	payload := make([]byte, n)
	if _, err := f.ReadAt(payload, st.Size()-footerLen-n); err != nil {
		return nil, "", err
	}
	return payload, version, nil
}

// ---------- 安装 ----------

func doInstall(dir string, silent, noLaunch bool) {
	fmt.Printf("%s 安装程序\n", appName)
	fmt.Println(strings.Repeat("-", 40))

	payload, version, err := readPayload()
	if err != nil {
		fmt.Println("安装包损坏：", err)
		waitExit(silent)
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		fmt.Println("安装内容无法读取：", err)
		waitExit(silent)
		return
	}
	if dir == "" {
		dir = filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ScreenRec")
	}
	dir, _ = filepath.Abs(dir)

	if isRunning(appExe) {
		if silent {
			killApp()
		} else if ask("检测到程序正在运行，需要先退出才能继续。现在关闭吗？", true) {
			killApp()
		} else {
			fmt.Println("已取消安装。")
			waitExit(silent)
			return
		}
	}

	fmt.Printf("安装到：%s\n", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Println("创建目录失败：", err)
		waitExit(silent)
		return
	}

	files := extractAll(zr, dir)
	if files == 0 {
		fmt.Println("解压失败：安装内容为空")
		waitExit(silent)
		return
	}
	fmt.Printf("已释放 %d 个文件\n", files)

	// 卸载程序就是安装包自身，复制一份放进安装目录
	self, _ := os.Executable()
	if self != "" {
		if err := copyFile(self, filepath.Join(dir, uninsExe)); err != nil {
			fmt.Println("写入卸载程序失败（不影响使用）：", err)
		}
	}

	if !silent {
		if err := makeShortcuts(dir); err != nil {
			fmt.Println("创建快捷方式失败（可从安装目录直接启动）：", err)
		} else {
			fmt.Println("已创建桌面与开始菜单快捷方式")
		}
	}
	writeUninstallKey(dir, version)

	exePath := filepath.Join(dir, appExe)
	if !hasModels(dir) && !silent {
		fmt.Println()
		fmt.Println("语音识别需要模型文件（约 1.1GB），安装包里没有带。")
		if ask("现在下载吗？（需要联网，之后也可以再运行 scripts\\fetch-models.ps1）", true) {
			fetchModels(dir)
		}
	}

	fmt.Println()
	fmt.Println("安装完成。")
	fmt.Printf("  程序：%s\n", exePath)
	fmt.Printf("  数据：%s\n", filepath.Join(dir, "data", "recordings"))
	if !noLaunch {
		cmd := winhide.Command(exePath, "-open")
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			fmt.Println("启动失败，请手动双击：", err)
		} else {
			fmt.Println("已启动，浏览器会自动打开操作界面。")
		}
	}
	waitExit(silent)
}

// extractAll 解压；发布 zip 顶层带一层的目录名，这里统一剥掉
func extractAll(zr *zip.Reader, dir string) int {
	prefix := commonTopDir(zr)
	count := 0
	for _, f := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), prefix)
		name = strings.TrimPrefix(name, "/")
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue // 防御 zip 路径穿越
		}
		dst := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			fmt.Printf("  跳过 %s：%v\n", name, err)
			continue
		}
		if err := unzipFile(f, dst); err != nil {
			fmt.Printf("  解压失败 %s：%v\n", name, err)
			continue
		}
		count++
	}
	return count
}

// commonTopDir 返回所有条目共有的顶层目录（带结尾斜杠），没有则返回空
func commonTopDir(zr *zip.Reader) string {
	top := ""
	for _, f := range zr.File {
		n := filepath.ToSlash(f.Name)
		if i := strings.IndexByte(n, '/'); i > 0 {
			cur := n[:i+1]
			if top == "" {
				top = cur
			} else if top != cur {
				return ""
			}
		} else {
			return "" // 有文件直接在根上，不剥
		}
	}
	return top
}

func unzipFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func hasModels(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "models", "sensevoice", "model.int8.onnx"))
	return err == nil && st.Size() > 20<<20
}

func fetchModels(dir string) {
	script := filepath.Join(dir, "scripts", "fetch-models.ps1")
	if _, err := os.Stat(script); err != nil {
		fmt.Println("找不到 scripts\\fetch-models.ps1，跳过下载。")
		return
	}
	fmt.Println("开始下载模型（关闭窗口会中断，已下载的部分下次会跳过）…")
	if err := runPS(fmt.Sprintf(`& '%s' -Root '%s'`, script, dir)); err != nil {
		fmt.Println("模型下载失败：", err)
		fmt.Println("可以稍后手动运行：powershell -ExecutionPolicy Bypass -File scripts\\fetch-models.ps1")
		return
	}
	fmt.Println("模型下载完成")
}

// makeShortcuts 通过 WScript.Shell 建快捷方式：桌面 + 开始菜单
func makeShortcuts(dir string) error {
	exePath := filepath.Join(dir, appExe)
	script := fmt.Sprintf(`
$W = New-Object -ComObject WScript.Shell
$targets = @([Environment]::GetFolderPath('Desktop'))
$start = Join-Path ([Environment]::GetFolderPath('Programs')) '%s'
New-Item -ItemType Directory -Force -Path $start | Out-Null
$targets += $start
foreach ($t in $targets) {
  $lnk = Join-Path $t '%s.lnk'
  $s = $W.CreateShortcut($lnk)
  $s.TargetPath = '%s'
  $s.WorkingDirectory = '%s'
  $s.IconLocation = '%s,0'
  $s.Description = '%s：录屏 + 实时转写'
  $s.Save()
}
`, appName, appName, exePath, dir, exePath, appName)
	return runPS(script)
}

func removeShortcuts() {
	script := fmt.Sprintf(`
$W = New-Object -ComObject WScript.Shell
$desktop = [Environment]::GetFolderPath('Desktop')
Remove-Item -Force -ErrorAction SilentlyContinue (Join-Path $desktop '%s.lnk')
$start = Join-Path ([Environment]::GetFolderPath('Programs')) '%s'
Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $start
`, appName, appName)
	_ = runPS(script)
}

// writeUninstallKey 写「应用和功能」里的卸载项。
// 必须用 PowerShell 写：reg.exe 走 ANSI 代码页，中文值实测会变成乱码。
func writeUninstallKey(dir, version string) {
	exePath := filepath.Join(dir, appExe)
	unins := filepath.Join(dir, uninsExe)
	sizeKB := dirSizeKB(dir)
	script := fmt.Sprintf(`
$k = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\ScreenRec'
New-Item -Path $k -Force | Out-Null
$vals = [ordered]@{
  DisplayName          = '%s（录屏 + 实时转写）'
  DisplayVersion       = '%s'
  Publisher            = 'screenrec'
  InstallLocation      = '%s'
  DisplayIcon          = '%s'
  UninstallString      = '"%s" -uninstall'
  QuietUninstallString = '"%s" -uninstall -silent'
  NoModify             = '1'
  NoRepair             = '1'
}
foreach ($n in $vals.Keys) {
  New-ItemProperty -Path $k -Name $n -Value $vals[$n] -PropertyType String -Force | Out-Null
}
New-ItemProperty -Path $k -Name 'EstimatedSize' -Value %d -PropertyType DWord -Force | Out-Null
`, appName, version, dir, exePath, unins, unins, sizeKB)
	if err := runPS(script); err != nil {
		fmt.Println("  写卸载项失败（不影响使用）：", err)
	}
}

func dirSizeKB(dir string) int {
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return int(total / 1024)
}

// ---------- 卸载 ----------

func doUninstall(silent bool) {
	self, _ := os.Executable()
	dir := installLocation()
	if dir == "" || !looksInstalled(dir) {
		// 没有注册表记录时，才退回「exe 所在目录」，但必须确认那真是个安装目录
		guess := filepath.Dir(self)
		if !looksInstalled(guess) {
			fmt.Println("找不到安装目录，无法卸载。")
			fmt.Println("如果是手动解压使用的，直接删除文件夹即可。")
			waitExit(silent)
			return
		}
		dir = guess
	}
	if !silent {
		fmt.Printf("卸载 %s\n", appName)
		fmt.Printf("安装目录：%s\n", dir)
		fmt.Println("注意：录制文件与转写结果（data 目录）会一起删除。")
		if !ask("确认卸载吗？", false) {
			fmt.Println("已取消。")
			return
		}
	}
	if isRunning(appExe) {
		killApp()
	}
	removeShortcuts()
	_ = winhide.Command("reg", "delete", regKey, "/f").Run()
	fmt.Println("已删除快捷方式与卸载项，正在清理程序目录…")

	// 自己就在安装目录里，删不掉自己：复制一份到临时目录，由它来删整个目录
	self2, _ := os.Executable()
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("screenrec-uninstall-%d.exe", os.Getpid()))
	if err := copyFile(self2, tmp); err == nil {
		c := winhide.Command(tmp, "-purge", dir)
		if err := c.Start(); err == nil {
			if !silent {
				fmt.Println("卸载完成。")
			}
			return
		}
	}
	// 兜底：原地删掉能删的文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if strings.EqualFold(p, self) {
			continue
		}
		_ = os.RemoveAll(p)
	}
	if !silent {
		fmt.Println("卸载完成（目录可能残留一个 unins.exe，可直接删除）")
	}
}

// purgeDir 由临时副本执行：把安装目录整个删掉
func purgeDir(dir string) {
	for i := 0; i < 12; i++ {
		if err := os.RemoveAll(dir); err == nil || !looksInstalled(dir) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// looksInstalled 判断目录是不是本程序的安装目录（防止卸错文件夹）
func looksInstalled(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, appExe)); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, uninsExe))
	return err == nil
}

// installLocation 从卸载项里读安装目录
func installLocation() string {
	out, err := winhide.Command("reg", "query", regKey, "/v", "InstallLocation").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "InstallLocation") {
			f := strings.Fields(line)
			if len(f) >= 3 {
				return strings.TrimSpace(f[len(f)-1])
			}
		}
	}
	return ""
}

// ---------- 小工具 ----------

func runPS(script string) error {
	script = "$ProgressPreference = 'SilentlyContinue'\n" + script
	enc := base64.StdEncoding.EncodeToString(utf16le(script))
	cmd := winhide.Command("powershell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", enc)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return b
}

func isRunning(name string) bool {
	out, err := winhide.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(name))
}

func killApp() {
	_ = winhide.Command("taskkill", "/F", "/IM", appExe).Run()
	time.Sleep(800 * time.Millisecond)
}

func ask(prompt string, defYes bool) bool {
	hint := "[Y/n]"
	if !defYes {
		hint = "[y/N]"
	}
	fmt.Printf("%s %s ", prompt, hint)
	var line string
	fmt.Scanln(&line)
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return defYes
	}
	return line == "y" || line == "yes"
}

func waitExit(silent bool) {
	if silent {
		return
	}
	fmt.Println()
	fmt.Print("按回车键退出…")
	fmt.Scanln()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
