package main

import "os"

func save(path string, data []byte) error {
	os.WriteFile(path, data, 0o644)
	return nil
}
