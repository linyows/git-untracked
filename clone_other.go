//go:build !darwin && !linux

package untracked

// clonePath falls back to a plain copy on platforms without clone support.
func clonePath(src, dst string) (fallback bool, err error) {
	return true, copyPath(src, dst)
}
