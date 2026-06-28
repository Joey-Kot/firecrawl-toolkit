package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func validateTimeoutSecs(timeoutSecs int) error {
	if timeoutSecs <= 0 {
		return fmt.Errorf("--timeout must be an integer greater than 0")
	}
	if int64(timeoutSecs) > maxTimeoutSecs {
		return fmt.Errorf("--timeout is too large")
	}
	return nil
}

func addProxyFlag(fs *flag.FlagSet, target *string) {
	fs.StringVar(target, "proxy", "", proxyFlagHelp)
}

func validateProxyOption(proxyRaw string) error {
	_, err := parseProxyURL(proxyRaw)
	return err
}

func flagProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func timeoutDuration(timeoutSecs int) time.Duration {
	return time.Duration(timeoutSecs) * time.Second
}

func timeoutMilliseconds(timeoutSecs int) int64 {
	return int64(timeoutSecs) * 1000
}

func validateParseFile(filePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !supportedParseFileExt(ext) {
		return fmt.Errorf("--file extension must be one of: .html, .htm, .pdf, .docx, .doc, .odt, .rtf, .xlsx, .xls")
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("--file must be a regular file")
	}
	return nil
}

func supportedParseFileExt(ext string) bool {
	switch ext {
	case ".html", ".htm", ".pdf", ".docx", ".doc", ".odt", ".rtf", ".xlsx", ".xls":
		return true
	default:
		return false
	}
}
