package native

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func selectedTailFixture() ([]byte, []SelectedProcedure) {
	image := hookImage()
	copy(image[0x220:], []byte{0x48, 0x83, 0xc1, 1, 0xe9, 0x57, 0, 0, 0})
	copy(image[0x280:], []byte{0xb8, 77, 0, 0, 0, 0xc3})
	return image, []SelectedProcedure{{ID: 1, RVA: 0x5020, Size: 9, Name: "Tail.Start"}, {ID: 2, RVA: 0x5080, Size: 6, Name: "Tail.Target"}}
}
func TestTailPreflightRequiresTheCompletePhysicalSelection(t *testing.T) {
	image, selected := selectedTailFixture()
	for _, set := range [][]SelectedProcedure{selected[:1], selected} {
		checks, err := CheckImageProcedures(image, set)
		if err != nil {
			t.Fatal(err)
		}
		if len(set) == 1 {
			if RequireSupported(checks) == nil || !strings.Contains(checks[0].Reason, "selecting their exact target") {
				t.Fatal("unselected tail destination accepted", checks)
			}
		} else if err := RequireSupported(checks); err != nil {
			t.Fatal("complete tail selection rejected", checks, err)
		}
	}
}
func TestTailPlansRequireCohortCapableAdapterBeforePayload(t *testing.T) {
	image, selected := selectedTailFixture()
	for _, version := range []string{"APHOOK9\x00", "APHOOKA\x00"} {
		offer := make([]byte, 56+8+1)
		copy(offer, version)
		binary.LittleEndian.PutUint64(offer[8:], 0x100000)
		binary.LittleEndian.PutUint64(offer[16:], 0x110000)
		binary.LittleEndian.PutUint32(offer[24:], 2*(largeHookStride+36))
		binary.LittleEndian.PutUint32(offer[28:], 2)
		binary.LittleEndian.PutUint64(offer[32:], 0x200000)
		binary.LittleEndian.PutUint64(offer[40:], 0x200100)
		binary.LittleEndian.PutUint64(offer[48:], 0x200200)
		binary.LittleEndian.PutUint32(offer[56:], 9)
		binary.LittleEndian.PutUint32(offer[60:], 6)
		offer[len(offer)-1] = 1
		stream := &hookStream{in: bytes.NewReader(offer)}
		err := installHooks(stream, image, []Procedure{{selected[0].ID, selected[0].RVA}, {selected[1].ID, selected[1].RVA}})
		if version[6] == '9' {
			if err == nil || !strings.Contains(err.Error(), "require APHOOKA") || stream.out.Len() != 1 {
				t.Fatal("old DLL could leave tail frames open", err, stream.out.Len())
			}
		} else if err != nil {
			t.Fatal("tail adapter rejected valid plan", err)
		}
	}
}
