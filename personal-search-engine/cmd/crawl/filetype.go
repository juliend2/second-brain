package main

import (
	"os"
	// "fmt"
	"strings"
	"regexp"
	"net/http"
)

func IsBinary(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)

	contentType := http.DetectContentType(buf[:n])
	return !strings.HasPrefix(contentType, "text/"), nil
}

func IsSkippable(path string) bool {
	unsupportedFormats := []string{
		"mov",
		"m4v",
		"webm",
		"jpg",
		"jpeg",
		"zip",
		"sqlite3",
	}
	for _, fileType := range unsupportedFormats {
		match, _ := regexp.MatchString("\\."+fileType+"$", path)
		if match {
			return true
		}
	}
	return false
}

func IsWordDoc(path string) bool {
	match, _ := regexp.MatchString("\\.docx$", path)
	return match
}

func IsPDF(path string) bool {
	match, _ := regexp.MatchString("\\.pdf$", path)
	return match
}
