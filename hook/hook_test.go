package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hooks")
	res, err := Install(dir, false)
	if err != nil || res != Created {
		t.Fatalf("res=%s err=%v", res, err)
	}
	got := read(t, Path(dir))
	if !strings.HasPrefix(got, "#!/bin/sh\n") || !strings.Contains(got, Block) {
		t.Errorf("content = %q", got)
	}
	fi, _ := os.Stat(Path(dir))
	if fi.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if !Installed(dir) {
		t.Error("Installed = false")
	}

	res, err = Install(dir, false)
	if err != nil || res != Unchanged {
		t.Fatalf("second install: res=%s err=%v", res, err)
	}
	if strings.Count(read(t, Path(dir)), beginMarker) != 1 {
		t.Error("block duplicated")
	}
}

func TestInstallAppendAndUninstall(t *testing.T) {
	dir := t.TempDir()
	orig := "#!/bin/bash\necho existing"
	if err := os.WriteFile(Path(dir), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Install(dir, false)
	if err != nil || res != Appended {
		t.Fatalf("res=%s err=%v", res, err)
	}
	got := read(t, Path(dir))
	if !strings.HasPrefix(got, orig+"\n") || !strings.HasSuffix(got, Block+"\n") {
		t.Errorf("content = %q", got)
	}
	fi, _ := os.Stat(Path(dir))
	if fi.Mode().Perm()&0o111 != 0o111 {
		t.Errorf("hook should be made executable: %v", fi.Mode())
	}

	res, err = Uninstall(dir, false)
	if err != nil || res != Removed {
		t.Fatalf("uninstall: res=%s err=%v", res, err)
	}
	if got := read(t, Path(dir)); got != orig+"\n" {
		t.Errorf("after uninstall = %q", got)
	}
	res, _ = Uninstall(dir, false)
	if res != NotFound {
		t.Errorf("second uninstall = %s", res)
	}
}

func TestInstallUpdatesStaleBlock(t *testing.T) {
	dir := t.TempDir()
	stale := "#!/bin/sh\n" + beginMarker + "\nold\n" + endMarker + "\necho after\n"
	if err := os.WriteFile(Path(dir), []byte(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Install(dir, false)
	if err != nil || res != Updated {
		t.Fatalf("res=%s err=%v", res, err)
	}
	want := "#!/bin/sh\n" + Block + "\necho after\n"
	if got := read(t, Path(dir)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUninstallDeletesEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, false); err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall(dir, false)
	if err != nil || res != Deleted {
		t.Fatalf("res=%s err=%v", res, err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Errorf("hook should be deleted: %v", err)
	}
}

func TestDryRun(t *testing.T) {
	dir := t.TempDir()
	res, err := Install(dir, true)
	if err != nil || res != Created {
		t.Fatalf("res=%s err=%v", res, err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Error("dry-run must not create a file")
	}
	if _, err := Install(dir, false); err != nil {
		t.Fatal(err)
	}
	res, err = Uninstall(dir, true)
	if err != nil || res != Deleted {
		t.Fatalf("res=%s err=%v", res, err)
	}
	if !Installed(dir) {
		t.Error("dry-run must not remove the block")
	}
}
