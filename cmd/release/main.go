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
	version := flag.String("version", "0.3.0-alpha.5", "version without v")
	targetOS := flag.String("os", runtime.GOOS, "target operating system")
	targetArch := flag.String("arch", runtime.GOARCH, "target architecture")
	cliOnly := flag.Bool("cli-only", false, "build only the native Go CLI")
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
	copyFile("third_party/PDF.js-LICENSE", filepath.Join(stage, "licenses", "PDF.js-LICENSE"))
	must(filepath.WalkDir("internal/app/pdfjs", func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "LICENSE") {
			relative, e := filepath.Rel("internal/app/pdfjs", p)
			if e != nil {
				return e
			}
			copyFile(p, filepath.Join(stage, "licenses", "pdfjs", relative))
		}
		return nil
	}))
	goLicense := filepath.Join(runtime.GOROOT(), "LICENSE")
	if _, e := os.Stat(goLicense); os.IsNotExist(e) && filepath.Base(runtime.GOROOT()) == "libexec" {
		goLicense = filepath.Join(filepath.Dir(runtime.GOROOT()), "LICENSE")
	}
	copyFile(goLicense, filepath.Join(stage, "licenses", "Go-LICENSE"))
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
	if !*cliOnly {
		buildDesktop(stage, *targetOS, *targetArch, *version, binary)
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
			info, e := os.Lstat(p)
			must(e)
			link := ""
			if info.Mode()&os.ModeSymlink != 0 {
				link, e = os.Readlink(p)
				must(e)
			}
			header, e := tar.FileInfoHeader(info, link)
			must(e)
			rel, e := filepath.Rel(stage, p)
			must(e)
			header.Name = filepath.ToSlash(rel)
			must(tw.WriteHeader(header))
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
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

func buildDesktop(stage, targetOS, arch, version, binary string) {
	platform := targetOS
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	cmd := exec.Command("node", "desktop/package.cjs", "--platform", platform, "--arch", arch, "--version", version, "--out", stage, "--backend", binary)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e := cmd.Run(); e != nil {
		panic(fmt.Errorf("desktop build failed; run npm ci --prefix desktop first: %w", e))
	}
}
