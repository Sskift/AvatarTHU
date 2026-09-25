// Build single-binary distributions. Only maintainers need the Go toolchain.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	dest := flag.String("out", "dist", "output directory")
	version := flag.String("version", "0.2.0-alpha.3", "version without v")
	targetOS := flag.String("os", runtime.GOOS, "target operating system")
	targetArch := flag.String("arch", runtime.GOARCH, "target architecture")
	flag.Parse()
	must(os.MkdirAll(*dest, 0755))
	stage, err := os.MkdirTemp("", "avatarthu-release-")
	must(err)
	defer os.RemoveAll(stage)
	name := "avatarthu"
	if *targetOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(stage, name)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -X github.com/Sskift/AvatarTHU/internal/app.Version="+*version, "-o", binary, "./cmd/avatarthu")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+*targetOS, "GOARCH="+*targetArch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	must(cmd.Run())
	if *targetOS == "darwin" {
		buildMenubar(stage, *targetArch, *version)
	}
	copyFile("README.md", filepath.Join(stage, "README.md"))
	copyFile("README.en.md", filepath.Join(stage, "README.en.md"))
	must(filepath.WalkDir("docs/images", func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			copyFile(p, filepath.Join(stage, p))
		}
		return nil
	}))
	copyFile("THIRD_PARTY.md", filepath.Join(stage, "THIRD_PARTY.md"))
	copyFile("third_party/AutoThu-LICENSE", filepath.Join(stage, "licenses", "AutoThu-LICENSE"))
	copyFile(filepath.Join(runtime.GOROOT(), "LICENSE"), filepath.Join(stage, "licenses", "Go-LICENSE"))
	mods, err := exec.Command("go", "list", "-m", "-json", "all").Output()
	must(err)
	decoder := json.NewDecoder(strings.NewReader(string(mods)))
	for {
		var mod struct {
			Path, Dir string
			Main      bool
		}
		e := decoder.Decode(&mod)
		if e == io.EOF {
			break
		}
		must(e)
		if mod.Main {
			continue
		}
		for _, name := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"} {
			src := filepath.Join(mod.Dir, name)
			if info, e := os.Stat(src); e == nil && !info.IsDir() {
				copyFile(src, filepath.Join(stage, "licenses", strings.ReplaceAll(mod.Path, "/", "_")+"-"+name))
				break
			}
		}
	}
	files := []string{}
	must(filepath.WalkDir(stage, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	}))
	sort.Strings(files)
	base := fmt.Sprintf("avatarthu-%s-%s", *targetOS, *targetArch)
	ext := ".tar.gz"
	if *targetOS == "windows" {
		ext = ".zip"
	}
	path := filepath.Join(*dest, base+ext)
	f, err := os.Create(path)
	must(err)
	if *targetOS == "windows" {
		z := zip.NewWriter(f)
		for _, p := range files {
			rel, e := filepath.Rel(stage, p)
			must(e)
			w, e := z.Create(filepath.ToSlash(rel))
			must(e)
			src, e := os.Open(p)
			must(e)
			_, e = io.Copy(w, src)
			src.Close()
			must(e)
		}
		must(z.Close())
	} else {
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, p := range files {
			info, e := os.Stat(p)
			must(e)
			header, e := tar.FileInfoHeader(info, "")
			must(e)
			rel, e := filepath.Rel(stage, p)
			must(e)
			header.Name = filepath.ToSlash(rel)
			must(tw.WriteHeader(header))
			src, e := os.Open(p)
			must(e)
			_, e = io.Copy(tw, src)
			src.Close()
			must(e)
		}
		must(tw.Close())
		must(gz.Close())
	}
	must(f.Close())
	f, err = os.Open(path)
	must(err)
	hash := sha256.New()
	_, err = io.Copy(hash, f)
	must(err)
	f.Close()
	must(os.WriteFile(path+".sha256", []byte(hex.EncodeToString(hash.Sum(nil))+"  "+filepath.Base(path)+"\n"), 0644))
	fmt.Println(path)
}
func copyFile(src, dst string) {
	must(os.MkdirAll(filepath.Dir(dst), 0755))
	s, e := os.Open(src)
	must(e)
	defer s.Close()
	d, e := os.Create(dst)
	must(e)
	defer d.Close()
	_, e = io.Copy(d, s)
	must(e)
}

func buildMenubar(stage, arch, version string) {
	if runtime.GOOS != "darwin" {
		panic("macOS 发布包需在 macOS 上使用系统 Swift 工具链构建菜单栏")
	}
	target := map[string]string{"arm64": "arm64-apple-macos12.0", "amd64": "x86_64-apple-macos12.0"}[arch]
	if target == "" {
		panic("unsupported macOS architecture: " + arch)
	}
	app := filepath.Join(stage, "AvatarTHU.app")
	binary := filepath.Join(app, "Contents", "MacOS", "AvatarTHUMenuBar")
	must(os.MkdirAll(filepath.Dir(binary), 0755))
	cmd := exec.Command("xcrun", "swiftc", "-O", "-swift-version", "5", "-target", target, "macos/AvatarTHU/main.swift", "-o", binary)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	must(cmd.Run())
	plist, err := os.ReadFile("macos/AvatarTHU/Info.plist")
	must(err)
	must(os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(strings.ReplaceAll(string(plist), "VERSION", strings.SplitN(version, "-", 2)[0])), 0644))
	cmd = exec.Command("codesign", "--force", "--sign", "-", app)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	must(cmd.Run())
}
