package untracked

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DebugEnv enables debug logging regardless of the config file, which helps
// when the config itself cannot be loaded.
const DebugEnv = "GIT_UNTRACKED_DEBUG"

// DebugLogName is the debug log file placed under $GIT_COMMON_DIR.
const DebugLogName = "git-untracked.log"

// debugLog is an open debug session. Output of the command is copied into the
// log file as well, because hooks run from GUI tools have no visible stderr.
type debugLog struct {
	file *os.File
	out  io.Writer
}

// debugEnabled reports whether debug logging is on for dir, and returns the
// common dir of the repository when it could be resolved.
func debugEnabled(dir string) (bool, string) {
	repo, err := OpenRepo(dir)
	if err != nil {
		return envTrue(os.Getenv(DebugEnv)), ""
	}
	if envTrue(os.Getenv(DebugEnv)) {
		return true, repo.CommonDir
	}
	cfg, err := LoadConfig(repo)
	return err == nil && cfg.Debug, repo.CommonDir
}

func envTrue(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// startDebug opens the log under commonDir and redirects env.Out and env.Err
// through it. When the log cannot be opened, debug lines go to stderr only.
func startDebug(env *Env, commonDir string) *debugLog {
	d := &debugLog{out: env.Err}
	if commonDir != "" {
		f, err := os.OpenFile(filepath.Join(commonDir, DebugLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // log file in .git
		if err != nil {
			fmt.Fprintf(env.Err, "debug: cannot open log: %s\n", err)
		} else {
			d.file = f
			d.out = io.MultiWriter(env.Err, f)
			env.Out = io.MultiWriter(env.Out, f)
			env.Err = d.out
		}
	}
	d.printf("---- %s", time.Now().Format(time.RFC3339))
	d.printf("version: %s (%s)", env.Version, env.Commit)
	d.printf("args: %q", env.Args)
	d.printf("dir: %s", env.Dir)
	if exe, err := os.Executable(); err == nil {
		d.printf("executable: %s", exe)
	}
	d.printf("PATH: %s", os.Getenv("PATH"))
	for _, e := range gitEnv(os.Environ()) {
		d.printf("env: %s", e)
	}
	return d
}

// gitEnv returns the GIT_* variables, sorted, which git exports to hooks.
func gitEnv(environ []string) []string {
	var out []string
	for _, e := range environ {
		if strings.HasPrefix(e, "GIT_") {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

func (d *debugLog) printf(format string, a ...any) {
	if d == nil {
		return
	}
	fmt.Fprintf(d.out, "debug: "+format+"\n", a...)
}

func (d *debugLog) close(code int) {
	if d == nil {
		return
	}
	d.printf("exit: %d", code)
	if d.file != nil {
		_ = d.file.Close()
	}
}
