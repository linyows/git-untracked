package untracked

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTree(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "b")
	if err := os.Chmod(filepath.Join(root, "sub", "b.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub/b.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
}

func TestCopyPathAndSameTree(t *testing.T) {
	tmp := t.TempDir()
	src, dst := filepath.Join(tmp, "src"), filepath.Join(tmp, "dst")
	makeTree(t, src)

	if err := copyPath(src, dst); err != nil {
		t.Fatal(err)
	}
	if l, err := os.Readlink(filepath.Join(dst, "link")); err != nil || l != "sub/b.txt" {
		t.Errorf("symlink = %q, %v", l, err)
	}
	fi, err := os.Stat(filepath.Join(dst, "sub", "b.txt"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", fi.Mode(), err)
	}
	if same, err := sameTree(src, dst); err != nil || !same {
		t.Errorf("sameTree = %v, %v", same, err)
	}

	writeFile(t, filepath.Join(dst, "sub", "b.txt"), "B")
	if same, _ := sameTree(src, dst); same {
		t.Error("modified content should differ")
	}
	writeFile(t, filepath.Join(dst, "sub", "b.txt"), "b")
	writeFile(t, filepath.Join(dst, "extra"), "x")
	if same, _ := sameTree(src, dst); same {
		t.Error("extra file should differ")
	}
	os.Remove(filepath.Join(dst, "extra"))
	os.Remove(filepath.Join(dst, "a.txt"))
	if same, _ := sameTree(src, dst); same {
		t.Error("missing file should differ")
	}
}

func TestClonePath(t *testing.T) {
	tmp := t.TempDir()
	src, dst := filepath.Join(tmp, "src"), filepath.Join(tmp, "dst")
	makeTree(t, src)
	if _, err := clonePath(src, dst); err != nil {
		t.Fatal(err)
	}
	if same, err := sameTree(src, dst); err != nil || !same {
		t.Errorf("sameTree = %v, %v", same, err)
	}
	// A clone must be independent from its source.
	writeFile(t, filepath.Join(dst, "a.txt"), "changed")
	if b, _ := os.ReadFile(filepath.Join(src, "a.txt")); string(b) != "a" {
		t.Errorf("source modified: %q", b)
	}
}

func TestSameFile(t *testing.T) {
	tmp := t.TempDir()
	a, b := filepath.Join(tmp, "a"), filepath.Join(tmp, "b")
	big := make([]byte, 200*1024)
	writeFile(t, a, string(big))
	writeFile(t, b, string(big))
	if same, err := sameFile(a, b); err != nil || !same {
		t.Errorf("sameFile = %v, %v", same, err)
	}
	big[len(big)-1] = 1
	writeFile(t, b, string(big))
	if same, _ := sameFile(a, b); same {
		t.Error("should differ at the last byte")
	}
	writeFile(t, b, "short")
	if same, _ := sameFile(a, b); same {
		t.Error("should differ in size")
	}
}

func TestLinkPointsTo(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "main", "certs")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []bool{true, false} {
		dst := filepath.Join(tmp, "wt", "certs")
		if err := os.RemoveAll(filepath.Dir(dst)); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		target, err := symlinkTarget(src, dst, relative)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.IsAbs(target) == relative {
			t.Errorf("relative=%v target=%s", relative, target)
		}
		if err := os.Symlink(target, dst); err != nil {
			t.Fatal(err)
		}
		if !linkPointsTo(dst, src) {
			t.Errorf("relative=%v: linkPointsTo = false", relative)
		}
		if linkPointsTo(dst, filepath.Join(tmp, "other")) {
			t.Error("should not point to other")
		}
	}
	if linkPointsTo(src, src) {
		t.Error("a directory is not a link")
	}
}

func TestBackupPath(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, ".env")
	if got := backupPath(p); got != p+".orig" {
		t.Errorf("got %s", got)
	}
	writeFile(t, p+".orig", "")
	writeFile(t, p+".orig.1", "")
	if got := backupPath(p); got != p+".orig.2" {
		t.Errorf("got %s", got)
	}
}

func TestInsideRoot(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(tmp, "wt")
	outside := filepath.Join(tmp, "main", "node_modules")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		dir  string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "not", "yet", "created"), true},
		{filepath.Join(root, "node_modules"), false},
		{filepath.Join(root, "node_modules", "pkg"), false},
	}
	for _, tt := range tests {
		got, err := insideRoot(root, tt.dir)
		if err != nil || got != tt.want {
			t.Errorf("insideRoot(%s) = %v, %v; want %v", tt.dir, got, err, tt.want)
		}
	}
}
