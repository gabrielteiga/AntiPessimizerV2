package platform

import (
	"golang.org/x/sys/windows"
	"strings"
)

func ImagePath(path string) string {
	if strings.HasPrefix(path, `\??\`) {
		return path[4:]
	}
	if !strings.HasPrefix(path, `\Device\`) {
		return path
	}
	for drive := 'A'; drive <= 'Z'; drive++ {
		name := string(drive) + ":"
		b := make([]uint16, 32768)
		if _, err := windows.QueryDosDevice(windows.StringToUTF16Ptr(name), &b[0], uint32(len(b))); err == nil {
			prefix := windows.UTF16ToString(b)
			if strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)+`\`) {
				return name + path[len(prefix):]
			}
		}
	}
	return path
}
