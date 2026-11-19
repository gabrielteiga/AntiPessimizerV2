package main

import (
	"encoding/binary"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/symbols/td32"
	"strings"
	"testing"
)

func TestFixtureAddressMapping(t *testing.T) {
	b, err := fixture(strings.NewReader(" 140005020 96 T main.hotspot\n"), 0x140000000, 0x5000)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(b[26:]) != 0xffff {
		t.Fatal("fixture no longer exercises global aligned symbols")
	}
	ps, err := td32.Parse(b)
	if err != nil || len(ps) != 1 || ps[0].Name != "Lab.Hotspot" || ps[0].Offset != 32 || ps[0].Size != 96 || ps[0].Unit != "LabWorkload" {
		t.Fatal(ps, err)
	}
	for _, text := range []string{"", "100 20 T main.hotspot", "140005000 0 T main.hotspot"} {
		if _, err := fixture(strings.NewReader(text), 0x140000000, 0x5000); err == nil {
			t.Fatal("accepted invalid nm", text)
		}
	}
}
