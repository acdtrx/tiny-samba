package cli

import (
	"errors"
	"syscall"
)

// xattrProbeName is a throwaway attribute in the same `user.*` namespace
// Samba uses for user.DOSATTRIB and user.DosStream.*.
const xattrProbeName = "user.tiny-samba.probe"

// supportsXattr reports whether the filesystem at path can store user
// extended attributes. Only ENOTSUP counts as "no" — any other error (a
// permission problem, a missing path) is returned so the caller keeps the
// share on the default config instead of quietly downgrading it.
func supportsXattr(path string) (bool, error) {
	err := syscall.Setxattr(path, xattrProbeName, []byte("1"), 0)
	if errors.Is(err, syscall.ENOTSUP) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, syscall.Removexattr(path, xattrProbeName)
}
