//go:build windows

package cmd

// Windows does not provide a portable directory-fsync operation through
// os.File.Sync. The file itself is synced before the atomic rename.
func syncRecordDirectory(string) error {
	return nil
}
