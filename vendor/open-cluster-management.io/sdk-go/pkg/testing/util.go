package testing

import (
	"os"
)

// WriteToTempFile writes data to a temporary file in the default directory for temporary files
// with the provided pattern and returns the file and an error if any.
// The permissions of the file are set to 0644 by default.
func WriteToTempFile(pattern string, data []byte) (*os.File, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(tmpFile.Name(), data, 0644); err != nil {
		return nil, err
	}

	return tmpFile, nil
}

// RemoveTempFile removes a temporary file if it exists.
func RemoveTempFile(file string) error {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
