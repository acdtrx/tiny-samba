//go:build !linux

package cli

// supportsXattr is Linux-only in practice (the image is Linux); elsewhere
// assume support so the package still builds and tests on a dev machine.
func supportsXattr(path string) (bool, error) {
	return true, nil
}
