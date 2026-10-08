package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	// binaryName is the executable inside a release archive.
	binaryName = "axilio"
	// downloadTimeout bounds the whole upgrade download (binaries are ~10-20MB).
	downloadTimeout = 60 * time.Second
	// maxDownload defensively caps a release asset read.
	maxDownload = 100 << 20 // 100 MiB
	// backupSuffixBytes is the randomness in a Windows upgrade's backup name,
	// .axilio.exe.<hex>.old, which hex-encodes to upgradeSuffixLen digits.
	backupSuffixBytes = 8
	upgradeSuffixLen  = 2 * backupSuffixBytes
	// installerSuffixLen is the hex length of the GUID install.ps1 puts in its
	// backup names.
	installerSuffixLen = 32
)

// Asset is a single release download.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the subset of a GitHub release the CLI uses.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// FetchLatestRelease returns the latest published GitHub release (tag + assets),
// or (nil, nil) when the repo has no releases yet (404) — the inert-until-first-
// release signal shared with the notifier. Network/parse failures return an error.
func FetchLatestRelease(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no releases yet
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases returned HTTP %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// IsHomebrew reports whether the running binary is managed by Homebrew, in which
// case `upgrade` defers to `brew upgrade` rather than replacing brew's file.
func IsHomebrew() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return isHomebrewExecutable(exe)
}

func isHomebrewExecutable(path string) bool {
	path = filepath.ToSlash(path)
	// Formula binaries live under Cellar. Cask binaries live under Caskroom and
	// are linked into bin; EvalSymlinks above exposes either managed location.
	return strings.Contains(path, "/Cellar/") || strings.Contains(path, "/Caskroom/")
}

// Apply upgrades the running binary to rel: download the release archive for
// this OS/arch, verify its SHA-256 against the release checksums.txt, extract
// the axilio binary, and replace the executable in place (atomic, with rollback
// via minio/selfupdate). warn reports problems that don't stop the upgrade.
func Apply(ctx context.Context, rel *Release, warn func(format string, a ...any)) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	asset, ok := assetForPlatform(rel)
	if !ok {
		return fmt.Errorf("no release asset for %s/%s in %s", runtime.GOOS, runtime.GOARCH, rel.Tag)
	}
	cs, ok := checksumsAsset(rel)
	if !ok {
		return errors.New("release has no checksums.txt; refusing to upgrade")
	}

	archive, err := download(ctx, asset.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}
	want, err := checksumFor(ctx, cs.URL, asset.Name)
	if err != nil {
		return err
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(archive)); got != want {
		return errors.New("checksum mismatch; refusing to upgrade")
	}

	bin, err := extractBinary(archive, asset.Name)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the running executable: %w", err)
	}
	// os.Executable can return the symlink the binary was launched through
	// (macOS does). Replace the real file, as minio's own lookup did, not
	// the link.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolve the running executable: %w", err)
	}
	if err := replaceExecutable(bytes.NewReader(bin), exe, runtime.GOOS, warn); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("upgrade failed and rollback also failed; binary may be broken: %w", errors.Join(err, rerr))
		}
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// replaceExecutable swaps target for bin through minio/selfupdate, which moves
// the old binary aside before moving the new one into place.
//
// On Windows a running executable can be renamed but not deleted, and the
// binary doing the upgrade is the one being replaced, so its moved-aside copy
// is always in use when the upgrade ends. minio's default puts that copy at a
// fixed .axilio.exe.old and first deletes the previous one, ignoring errors.
// If the binary from an earlier upgrade is still running (an open
// `axilio runs watch`), that delete fails, the rename onto it fails, and the
// upgrade aborts. So on Windows each upgrade gets its own backup name, and
// leftovers from earlier upgrades and install.ps1 runs are removed once
// nothing runs them (AXI-2225). warn reports a removal that failed.
func replaceExecutable(bin io.Reader, target, goos string, warn func(format string, a ...any)) error {
	opts := selfupdate.Options{TargetPath: target}
	if goos != "windows" {
		return selfupdate.Apply(bin, opts)
	}
	dir, base := filepath.Dir(target), filepath.Base(target)
	if err := removeOldBinaries(dir, base); err != nil {
		// Removing leftovers is cleanup, and the fresh backup name below
		// doesn't depend on it: a directory that can't be listed (some
		// managed installs allow renames but not listing) mustn't stop the
		// upgrade.
		warn("could not remove old axilio binaries: %v", err)
	}
	suffix := make([]byte, backupSuffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	opts.OldSavePath = filepath.Join(dir, "."+base+"."+hex.EncodeToString(suffix)+".old")
	return selfupdate.Apply(bin, opts)
}

// removeOldBinaries deletes the moved-aside copies of base in dir that nothing
// runs any more.
func removeOldBinaries(dir, base string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isOldBinary(e.Name(), base) {
			continue
		}
		// A copy that is still running can't be deleted. That failure is
		// expected, and the next upgrade or install retries it.
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}

// isOldBinary reports whether name is a moved-aside copy of base: the fixed
// .<base>.old that older CLIs left, or .<base>.<hex>.old with the suffix length
// an upgrade or install.ps1 writes. Nothing else may be deleted.
func isOldBinary(name, base string) bool {
	if name == "."+base+".old" {
		return true
	}
	suffix, ok := strings.CutPrefix(name, "."+base+".")
	if !ok {
		return false
	}
	digits, ok := strings.CutSuffix(suffix, ".old")
	if !ok || (len(digits) != upgradeSuffixLen && len(digits) != installerSuffixLen) {
		return false
	}
	return strings.Trim(digits, "0123456789abcdef") == ""
}

// assetForPlatform picks the release archive matching this OS/arch. goreleaser
// names assets "<project>_<version>_<os>_<arch>.<ext>", so an "_<goos>_<goarch>"
// substring plus the platform extension is an unambiguous match.
func assetForPlatform(rel *Release) (Asset, bool) {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	needle := "_" + runtime.GOOS + "_" + runtime.GOARCH
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, needle) && strings.HasSuffix(a.Name, ext) {
			return a, true
		}
	}
	return Asset{}, false
}

func checksumsAsset(rel *Release) (Asset, bool) {
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			return a, true
		}
	}
	return Asset{}, false
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

// checksumFor fetches checksums.txt and returns the hex sha256 for assetName.
// goreleaser format is "<sha256>  <filename>" per line.
func checksumFor(ctx context.Context, url, assetName string) (string, error) {
	body, err := download(ctx, url)
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", assetName)
}

// extractBinary pulls the axilio binary out of a .tar.gz or .zip release archive.
func extractBinary(archive []byte, assetName string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".zip") {
		return extractFromZip(archive)
	}
	return extractFromTarGz(archive)
}

func extractFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if filepath.Base(hdr.Name) == binaryName {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", binaryName)
}

func extractFromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base == binaryName+".exe" || base == binaryName {
			rc, oerr := f.Open()
			if oerr != nil {
				return nil, oerr
			}
			b, rerr := io.ReadAll(io.LimitReader(rc, maxDownload))
			_ = rc.Close()
			return b, rerr
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", binaryName+".exe")
}
