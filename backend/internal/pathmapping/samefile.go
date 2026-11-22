package pathmapping

import "os"

// SameFile reports whether a and b name the same file (st_dev and st_ino
// match), which proves a translation points at the bytes the other actor
// reported.
func SameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}
