// Package update implements `ccpane -update`: it downloads the latest release
// asset for the host OS/arch, verifies its checksum, and atomically replaces
// the running executable.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func repo() string {
	if r := os.Getenv("CCPANE_REPO"); r != "" {
		return r
	}
	return "hassan-alachek/ccpane"
}

// Run updates the current binary to the latest release. current is this build's
// version (e.g. "0.2.0" or "dev").
func Run(current string) error {
	latest, err := latestTag()
	if err != nil {
		return fmt.Errorf("checking latest release: %w", err)
	}
	fmt.Printf("==> current %s · latest %s\n", current, latest)
	if norm(current) == norm(latest) {
		fmt.Println("==> already up to date.")
		return nil
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	asset := fmt.Sprintf("ccpane_%s_%s.%s", runtime.GOOS, runtime.GOARCH, ext)
	base := "https://github.com/" + repo() + "/releases/latest/download/"

	fmt.Printf("==> downloading %s\n", asset)
	archive, err := download(base + asset)
	if err != nil {
		return err
	}

	if sums, err := download(base + "checksums.txt"); err == nil {
		if want := findSum(string(sums), asset); want != "" {
			if got := sha256hex(archive); !strings.EqualFold(got, want) {
				return fmt.Errorf("checksum mismatch for %s", asset)
			}
			fmt.Println("==> checksum verified")
		}
	}

	bin, err := extractBinary(archive, ext)
	if err != nil {
		return err
	}
	if err := replaceExecutable(bin); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}
	fmt.Printf("==> updated to %s\n", latest)
	return nil
}

// latestTag resolves the newest release tag via the /releases/latest redirect
// (no API token, no rate limit).
func latestTag() (string, error) {
	c := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Get("https://github.com/" + repo() + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("no Location header (no releases?)")
	}
	tag := loc[strings.LastIndex(loc, "/")+1:]
	if tag == "" || tag == "releases" {
		return "", fmt.Errorf("no published releases found")
	}
	return tag, nil
}

func download(url string) ([]byte, error) {
	c := &http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func findSum(sums, asset string) string {
	for _, ln := range strings.Split(sums, "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 && f[1] == asset {
			return f[0]
		}
	}
	return ""
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func extractBinary(archive []byte, ext string) ([]byte, error) {
	if ext == "zip" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if base := filepath.Base(f.Name); base == "ccpane.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("ccpane.exe not found in archive")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == "ccpane" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("ccpane not found in archive")
}

// replaceExecutable atomically swaps the running binary for newBin.
func replaceExecutable(newBin []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	tmp, err := os.CreateTemp(dir, ".ccpane-update-*")
	if err != nil {
		return fmt.Errorf("%w (is %s writable? try sudo or CCPANE_INSTALL_DIR)", err, dir)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(newBin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmpName, exe); err != nil {
			os.Rename(old, exe) // best-effort rollback
			return err
		}
		os.Remove(old)
		return nil
	}
	return os.Rename(tmpName, exe)
}

func norm(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
