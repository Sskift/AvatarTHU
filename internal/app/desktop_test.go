package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDesktopBundlePreservesFrameworkLinksAndExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("framework symlinks are macOS bundle entries")
	}
	source, target := filepath.Join(t.TempDir(), "source.app"), filepath.Join(t.TempDir(), "installed.app")
	writeFile(filepath.Join(source, "Contents", "Frameworks", "Runtime.framework", "Versions", "A", "Runtime"), []byte("native"), 0755)
	check(os.Symlink("A", filepath.Join(source, "Contents", "Frameworks", "Runtime.framework", "Versions", "Current")))
	check(os.Symlink("Versions/Current/Runtime", filepath.Join(source, "Contents", "Frameworks", "Runtime.framework", "Runtime")))
	if e := copyDesktopBundle(source, target); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(target, "Contents", "Frameworks", "Runtime.framework", "Runtime")
	link, e := os.Readlink(p)
	if e != nil || link != "Versions/Current/Runtime" {
		t.Fatal("framework link changed", link, e)
	}
	data, e := os.ReadFile(p)
	if e != nil || string(data) != "native" {
		t.Fatal("framework is not readable", e)
	}
	info, e := os.Stat(p)
	if e != nil || info.Mode().Perm() != 0755 {
		t.Fatal("executable permissions changed", e)
	}
}

func TestDesktopBundleRejectsExternalSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlink privileges")
	}
	source := t.TempDir()
	writeFile(filepath.Join(filepath.Dir(source), "outside.txt"), []byte("outside"), 0600)
	check(os.Symlink("../outside.txt", filepath.Join(source, "escape")))
	if e := copyDesktopBundle(source, filepath.Join(t.TempDir(), "target")); e == nil {
		t.Fatal("accepted link outside application")
	}
}

func TestDesktopSourceFindsBundledGoExecutable(t *testing.T) {
	root := t.TempDir()
	name := "AvatarTHU"
	if runtime.GOOS == "darwin" {
		name += ".app"
	}
	bundle := filepath.Join(root, name)
	writeFile(desktopExecutable(bundle), []byte("app"), 0755)
	current := filepath.Join(bundle, "resources", "backend", "avatarthu")
	if runtime.GOOS == "darwin" {
		current = filepath.Join(bundle, "Contents", "Resources", "backend", "avatarthu")
	}
	if desktopSource(current) != bundle {
		t.Fatal("standalone App cannot install backend")
	}
}
