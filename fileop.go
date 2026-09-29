package untracked

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// fileCopier copies a single regular file to a path that does not exist yet.
type fileCopier func(src, dst string, perm fs.FileMode) error

// copyTree copies src (file or directory) to dst. Symlinks inside the tree
// are recreated as symlinks; sockets, devices and pipes are skipped.
func copyTree(src, dst string, copyFile fileCopier) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.Mkdir(target, info.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(p, target, info.Mode().Perm())
		}
		return nil
	})
}

// plainCopy copies file contents with io.Copy.
func plainCopy(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyPath copies src to dst without copy-on-write.
func copyPath(src, dst string) error {
	return copyTree(src, dst, plainCopy)
}

// symlinkTarget returns the link target for dst pointing at src.
func symlinkTarget(src, dst string, relative bool) (string, error) {
	if !relative {
		return src, nil
	}
	return filepath.Rel(filepath.Dir(dst), src)
}

// linkPointsTo reports whether dst is a symlink resolving to src.
func linkPointsTo(dst, src string) bool {
	fi, err := os.Lstat(dst)
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		return false
	}
	link, err := os.Readlink(dst)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(dst), link)
	}
	if filepath.Clean(link) == filepath.Clean(src) {
		return true
	}
	a, err1 := filepath.EvalSymlinks(dst)
	b, err2 := filepath.EvalSymlinks(src)
	return err1 == nil && err2 == nil && a == b
}

// sameFile reports whether two regular files have identical content.
func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	if !fa.Mode().IsRegular() || !fb.Mode().IsRegular() || fa.Size() != fb.Size() {
		return false, nil
	}
	ra, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer ra.Close()
	rb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer rb.Close()
	const size = 64 * 1024
	ba, bb := make([]byte, size), make([]byte, size)
	for {
		na, ea := io.ReadFull(ra, ba)
		nb, eb := io.ReadFull(rb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			return false, nil
		}
		if errors.Is(ea, io.EOF) || errors.Is(ea, io.ErrUnexpectedEOF) {
			return errors.Is(eb, io.EOF) || errors.Is(eb, io.ErrUnexpectedEOF), nil
		}
		if ea != nil {
			return false, ea
		}
		if eb != nil {
			return false, eb
		}
	}
}

// sameTree reports whether dst is an exact copy of src: same entries, same
// types, same file contents and same symlink targets.
func sameTree(src, dst string) (bool, error) {
	seen := map[string]bool{}
	errDiff := errors.New("differs")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		seen[rel] = true
		q := filepath.Join(dst, rel)
		qi, err := os.Lstat(q)
		if err != nil {
			return errDiff
		}
		switch {
		case d.IsDir():
			if !qi.IsDir() {
				return errDiff
			}
		case d.Type()&fs.ModeSymlink != 0:
			if qi.Mode()&fs.ModeSymlink == 0 {
				return errDiff
			}
			l1, err1 := os.Readlink(p)
			l2, err2 := os.Readlink(q)
			if err1 != nil || err2 != nil || l1 != l2 {
				return errDiff
			}
		case d.Type().IsRegular():
			if !qi.Mode().IsRegular() {
				return errDiff
			}
			same, err := sameFile(p, q)
			if err != nil {
				return err
			}
			if !same {
				return errDiff
			}
		}
		return nil
	})
	if errors.Is(err, errDiff) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	extra := errors.New("extra")
	err = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, p)
		if !seen[rel] {
			return extra
		}
		return nil
	})
	if errors.Is(err, extra) {
		return false, nil
	}
	return err == nil, err
}

// backupPath returns the first unused of p.orig, p.orig.1, p.orig.2, ...
func backupPath(p string) string {
	cand := p + ".orig"
	for i := 1; ; i++ {
		if _, err := os.Lstat(cand); errors.Is(err, os.ErrNotExist) {
			return cand
		}
		cand = p + ".orig." + strconv.Itoa(i)
	}
}
