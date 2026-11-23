package desktop

import (
	"fmt"
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openFileName matches OPENFILENAMEW on Windows x64.
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDirectory, Title      *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExtension             *uint16
	CustomData, Hook             uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	ReservedFlags, ExtendedFlags uint32
}

var commonDialogs = windows.NewLazySystemDLL("comdlg32.dll")
var getOpenFileName = commonDialogs.NewProc("GetOpenFileNameW")
var commonDialogError = commonDialogs.NewProc("CommDlgExtendedError")

func pickExecutable(owner uintptr) (string, error) {
	const (
		noChangeDirectory = 0x00000008
		pathMustExist     = 0x00000800
		fileMustExist     = 0x00001000
		explorerDialog    = 0x00080000
	)
	filter := utf16.Encode([]rune("Executáveis (*.exe)\x00*.exe\x00\x00"))
	var path [32768]uint16
	ofn := openFileName{
		Size:        uint32(unsafe.Sizeof(openFileName{})),
		Owner:       owner,
		Filter:      &filter[0],
		FilterIndex: 1,
		File:        &path[0],
		MaxFile:     uint32(len(path)),
		Title:       windows.StringToUTF16Ptr("Selecionar executável Delphi"),
		Flags:       noChangeDirectory | pathMustExist | fileMustExist | explorerDialog,
	}
	ok, _, _ := getOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(&ofn)
	if ok != 0 {
		return windows.UTF16ToString(path[:]), nil
	}
	if code, _, _ := commonDialogError.Call(); code != 0 {
		return "", fmt.Errorf("seletor de executável do Windows (0x%04x)", code)
	}
	return "", nil // Cancel or closing the dialog.
}
