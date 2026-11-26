package x64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func scopedLocalPE() ([]byte, []byte) {
	image, original := primaryAndLocalSavePE()
	code := append(bytes.Clone(original[:40]), 0xeb, 5, 0xb8, 0x8b, 3, 0, 0)
	code = append(code, original[40:]...)
	copy(image[0x220:], code)
	w32(image, 0x410, peLeafRVA+uint32(len(code)))
	image = append(image, make([]byte, 0x400)...)
	w32(image, peSections+40+8, 0x600)
	w32(image, peSections+40+16, 0x600)
	copy(image[0x4d0:0x4e8], image[0x4a0:0x4b8])
	w32(image, 0x414, 0x20d0)
	image[0x480] = 9
	w32(image, 0x48c, 0x1080)
	copy(image[0x280:], []byte{0xff, 0x25, 0xca, 0x10, 0, 0})
	w32(image, 0x98+120, 0x2100)
	w32(image, 0x98+124, 40)
	w32(image, 0x500, 0x2140)
	w32(image, 0x50c, 0x2180)
	w32(image, 0x510, 0x2150)
	binary.LittleEndian.PutUint64(image[0x540:], 0x2160)
	copy(image[0x562:], "__C_specific_handler\x00")
	copy(image[0x580:], "msvcrt.dll\x00")
	w32(image, 0x490, 1)
	w32(image, 0x494, peLeafRVA+37)
	w32(image, 0x498, peLeafRVA+40)
	w32(image, 0x49c, 1)
	w32(image, 0x4a0, peLeafRVA+42)
	return image, code
}

func TestScopedLocalSavesPreserveEveryShadowUnwindAction(t *testing.T) {
	image, code := scopedLocalPE()
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || len(plan.Functions) != 4 || !plan.RelocatedHandler || plan.HandlerScopeStyle != 1 {
		t.Fatal("CRT scopes with local saves rejected", err)
	}
	u32 := binary.LittleEndian.Uint32
	info := plan.Image[plan.Functions[1].Unwind:]
	descriptor := info[16:]
	shadow := descriptor[u32(descriptor[48:]):]
	alias := plan.Image[u32(shadow[8:])-0x80000:]
	want := []byte{25, 30, 7, 0, 30, 0x68, 1, 0, 25, 0x64, 5, 0, 9, 0x34, 6, 0, 4, 0x62, 0, 0}
	if !bytes.Equal(alias[:len(want)], want) {
		t.Fatal("shadow lost local or primary actions", alias[:len(want)])
	}
	if u32(shadow[4:]) != 0x80000+plan.Functions[2].End {
		t.Fatal("shadow excludes secondary range")
	}
	if u32(alias[24:]) != 7 || u32(alias[28:]) != 3 {
		t.Fatal("canonical descriptor alias moved incorrectly")
	}
}

func TestScopedLocalShadowPrologueOffsetIsBounded(t *testing.T) {
	image, _ := primaryAndLocalSavePE()
	primary, local := image[0x480:0x48c], image[0x4a0:0x4ac]
	alias, err := combineScopedUnwind(primary, local, 245)
	if err != nil || alias[1] != 255 || alias[4] != 255 {
		t.Fatal("last representable shadow offset rejected", err)
	}
	for _, offset := range []uint32{0, 246, 0xffffffff} {
		if _, err := combineScopedUnwind(primary, local, offset); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unrepresentable shadow accepted", offset, err)
		}
	}
}

func TestScopedLocalSavesDoNotSkipStoresOrReloadsThroughHandler(t *testing.T) {
	image, code := scopedLocalPE()
	for name, change := range map[string]func([]byte){
		"scope includes partial saves": func(b []byte) { w32(b, 0x494, peLeafRVA+20) },
		"scope includes reloads":       func(b []byte) { w32(b, 0x498, peLeafRVA+52) },
		"landing repeats stores":       func(b []byte) { w32(b, 0x4a0, peLeafRVA+20) },
		"landing skips a reload":       func(b []byte) { w32(b, 0x4a0, peLeafRVA+52) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(image)
			change(bad)
			if _, err := BuildPEFunction(bad, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
				t.Fatal("handler bypassed saved frame", err)
			}
		})
	}
}

func TestScopedLocalShadowKeepsOddFARSlotsWithoutPadding(t *testing.T) {
	primary := []byte{1, 4, 1, 0, 4, 0x42, 0, 0}
	local := []byte{33, 8, 3, 0, 8, 0x35, 24, 0, 0, 0, 0, 0}
	alias, err := combineScopedUnwind(primary, local, 12)
	if err != nil || !bytes.Equal(alias, []byte{1, 20, 4, 0, 20, 0x35, 24, 0, 0, 0, 4, 0x42}) {
		t.Fatal("FAR action or odd padding changed", alias, err)
	}
}

func scopedPaddedLocalPE() ([]byte, []byte) {
	return scopedPaddedLocalPEWithGap([]byte{0xcc, 0x90, 0x90, 0xcc, 0xcc, 0x90, 0x90, 0xcc})
}
func scopedPaddedLocalPEWithGap(gap []byte) ([]byte, []byte) {
	image, original := scopedLocalPE()
	code := append(bytes.Clone(original[:20]), 0xeb, byte(len(gap)))
	code = append(code, gap...)
	code = append(code, original[20:]...)
	copy(image[0x220:], code)
	w32(image, 0x404, peLeafRVA+22)
	w32(image, 0x40c, peLeafRVA+22+uint32(len(gap)))
	w32(image, 0x410, peLeafRVA+uint32(len(code)))
	copy(image[0x4dc:], image[0x400:0x40c])
	for _, at := range []int{0x494, 0x498, 0x4a0} {
		w32(image, at, binary.LittleEndian.Uint32(image[at:])+2+uint32(len(gap)))
	}
	return image, code
}
func TestScopedLocalSavesPreservePaddingAndShadowOffsets(t *testing.T) {
	image, code := scopedPaddedLocalPE()
	plan, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil || !plan.PaddedHandler || len(plan.Functions) != 4 {
		t.Fatal("CRT padding rejected", err)
	}
	if !bytes.Equal(plan.Source, code) || plan.Functions[2].Begin != plan.BodyRVA+33 {
		t.Fatal("padding changed source/local CFI", plan.Functions)
	}
	info := plan.Image[plan.Functions[1].Unwind:]
	descriptor := info[16:]
	shadow := descriptor[binary.LittleEndian.Uint32(descriptor[48:]):]
	alias := plan.Image[binary.LittleEndian.Uint32(shadow[8:])-0x80000:]
	if alias[1] != 43 || alias[4] != 43 || alias[8] != 38 {
		t.Fatal("shadow used gap instead of local stores", alias[:20])
	}
	for name, change := range map[string]func([]byte){
		"fallthrough":         func(b []byte) { b[0x234] = 0x90; b[0x235] = 0x90 },
		"branch into padding": func(b []byte) { b[0x235] = 4 },
		"nonpadding":          func(b []byte) { b[0x236] = 0xc3 },
		"scope begins in gap": func(b []byte) { w32(b, 0x494, peLeafRVA+22) },
		"landing in gap":      func(b []byte) { w32(b, 0x4a0, peLeafRVA+23) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(image)
			change(bad)
			if _, err := BuildPEFunction(bad, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid scoped gap accepted", err)
			}
		})
	}
}

func TestScopedChainPaddingLengthAndEncodingAreBounded(t *testing.T) {
	for _, n := range []int{16, 17} {
		image, code := scopedPaddedLocalPEWithGap(bytes.Repeat([]byte{0x90}, n))
		_, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if n == 16 && err != nil || n == 17 && !errors.Is(err, ErrUnsupported) {
			t.Fatal("wrong CRT padding limit", n, err)
		}
	}
	image, code := scopedPaddedLocalPEWithGap([]byte{0x66, 0x90})
	if _, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); !errors.Is(err, ErrUnsupported) {
		t.Fatal("multi-byte CRT padding accepted", err)
	}
}
