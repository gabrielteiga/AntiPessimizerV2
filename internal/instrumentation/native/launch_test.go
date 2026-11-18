package native

import (
	"strings"
	"testing"
	"time"
)

func TestLaunchSelectionRejectsDuplicatePhysicalCodeBeforeStartup(t *testing.T) {
	first := SelectedProcedure{ID: 1, RVA: 0x1000, Size: 16, Name: "Primary"}
	o := LaunchOptions{Image: "owned.exe", DLL: "adapter.dll", Procedures: []SelectedProcedure{first}, Capacity: 1024, Timeout: time.Minute}
	for _, rva := range []uint32{0x1000, 0x1001, 0xfff} {
		o.Procedures = []SelectedProcedure{first, {ID: 2, RVA: rva, Size: 16, Name: "Alias"}}
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatal("alias/overlap accepted for a second hook", rva, err)
		}
	}
	o.Procedures[1].RVA = 0x1010
	if err := o.validate(); err != nil {
		t.Fatal("adjacent code rejected", err)
	}
	o.Procedures[1].RVA = 0xfffffff8
	if err := o.validate(); err == nil {
		t.Fatal("RVA overflow accepted")
	}
}
