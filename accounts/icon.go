package accounts

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var imageMagic = [][]byte{
	{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
	{0xff, 0xd8, 0xff},
	[]byte("GIF87a"),
	[]byte("GIF89a"),
	[]byte("BM"),
}

func looksLikeImage(data []byte) bool {
	for _, m := range imageMagic {
		if bytes.HasPrefix(data, m) {
			return true
		}
	}
	return len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func readIcon(path string, uid uint32, admin bool) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("the icon path must be absolute")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open the icon")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("the icon is not a regular file")
	}
	if info.Size() > maxIconSize {
		return nil, fmt.Errorf("the icon is larger than 1 MB")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("cannot inspect the icon")
	}
	if !admin && st.Uid != uid {
		real, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
		if err != nil || !strings.HasPrefix(real, "/usr/share/") || info.Mode().Perm()&0o004 == 0 {
			return nil, fmt.Errorf("the icon must belong to the user")
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxIconSize+1))
	if err != nil || len(data) > maxIconSize {
		return nil, fmt.Errorf("cannot read the icon")
	}
	if !looksLikeImage(data) {
		return nil, fmt.Errorf("the icon is not an image")
	}
	return data, nil
}
