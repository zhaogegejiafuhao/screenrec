// 打包发布：把 exe 与必要文档打成一个 zip，并输出 SHA256。
// 用法：go run ./tools/release -version 0.1.0 -exe screenrec.exe
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ship = []string{
	"README.md",
	"README.en.md",
	"LICENSE",
	"THIRD_PARTY.md",
	"models/README.md",
	"scripts/fetch-models.ps1",
}

func main() {
	version := flag.String("version", "0.1.0", "版本号")
	exe := flag.String("exe", "screenrec.exe", "已编译的 exe")
	out := flag.String("out", "dist", "输出目录")
	flag.Parse()

	if _, err := os.Stat(*exe); err != nil {
		fmt.Fprintf(os.Stderr, "找不到 %s，先执行 go build -ldflags \"-H windowsgui\" -o screenrec.exe .\n", *exe)
		os.Exit(1)
	}

	name := fmt.Sprintf("screenrec-%s-win64", *version)
	stage := filepath.Join(*out, name)
	if err := os.MkdirAll(stage, 0755); err != nil {
		fatal(err)
	}

	if err := copyFile(*exe, filepath.Join(stage, "screenrec.exe")); err != nil {
		fatal(err)
	}
	for _, f := range ship {
		dst := filepath.Join(stage, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", f, err)
			continue
		}
		if err := copyFile(f, dst); err != nil {
			fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", f, err)
		}
	}

	zipPath := filepath.Join(*out, name+".zip")
	if err := zipDir(stage, zipPath); err != nil {
		fatal(err)
	}

	sum, err := sha256File(zipPath)
	if err != nil {
		fatal(err)
	}
	st, _ := os.Stat(zipPath)
	fmt.Printf("已生成 %s\n", zipPath)
	fmt.Printf("大小   %.1f MB\n", float64(st.Size())/1048576)
	fmt.Printf("SHA256 %s\n", sum)

	_ = os.WriteFile(zipPath+".sha256", []byte(sum+"  "+filepath.Base(zipPath)+"\n"), 0644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "失败:", err)
	os.Exit(1)
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

func zipDir(dir, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	now := time.Now()
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(dir), path)
		if err != nil {
			return err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     filepath.ToSlash(rel),
			Method:   zip.Deflate,
			Modified: now,
		})
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
	return err
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
