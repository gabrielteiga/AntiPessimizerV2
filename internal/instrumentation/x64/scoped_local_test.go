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
