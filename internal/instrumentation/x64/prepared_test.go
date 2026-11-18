package x64

import (
	"encoding/binary"
	"reflect"
	"sync"
	"testing"
)

func TestPreparedImageOwnsSourceAndReturnsIndependentPlans(t *testing.T) {
	image, code := cleanupPE()
	p, err := NewPEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	build := func() FunctionPlan {
		t.Helper()
		plan, err := p.BuildFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	want, err := BuildPEFunction(image, peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
	if err != nil {
		t.Fatal(err)
	}
	if p.ImageSize() != 0x3000 || !reflect.DeepEqual(want, build()) {
		t.Fatal("preparation changed executable plan")
	}
	// Alter source headers, code and handler data after preparation.
	clear(image)
	first := build()
	first.Source[0] ^= 1
	first.Image[0] ^= 1
	first.Patch[0] ^= 1
	first.Original[0] ^= 1
	if !reflect.DeepEqual(want, build()) {
		t.Fatal("caller mutation changed the prepared source")
	}
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			plan, err := p.BuildFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7)
			if err != nil || !reflect.DeepEqual(want, plan) {
				t.Error("concurrent plan changed source/metadata", err)
			}
		})
	}
	group.Wait()
}

func TestPreparationValidatesEntireRuntimeTableAndEverySelection(t *testing.T) {
	image, code := framedPE()
	// The unrelated second entry must still be validated during preparation.
	binary.LittleEndian.PutUint32(image[0x414:], 0x2ffc)
	if _, err := NewPEImage(image); err == nil {
		t.Fatal("unrelated non-raw unwind metadata was skipped")
	}
	image, code = framedPE()
	p, err := NewPEImage(image)
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range [][2]uint32{{peLeafRVA + 1, uint32(len(code) - 1)}, {peLeafRVA - 1, uint32(len(code) + 1)}, {peLeafRVA, uint32(len(code) + 1)}, {0xffffffff, 8}, {peLeafRVA, 4}} {
		if _, err := p.BuildFunction(interval[0], interval[1], 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); err == nil {
			t.Fatal("prepared lookup weakened extent checks", interval)
		}
	}
	var empty PEImage
	if _, err := empty.BuildFunction(peLeafRVA, uint32(len(code)), 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 7); err == nil {
		t.Fatal("uninitialized image planned code")
	}
}

// A representative large table: 2,048 ordinary six-byte methods, with 64
// explicitly selected methods per planning batch. No external binary required.
func largeRuntimePE() []byte {
	image := make([]byte, 0xc200)
	copy(image, leafPE())
	put := binary.LittleEndian.PutUint32
	const optional = 0x80 + 24
	put(image[optional+56:], 0xe000)
	put(image[optional+136:], 0x6000)
	put(image[optional+140:], 2048*12)
	const section = optional + 240
	put(image[section+8:], 0x4000)
	put(image[section+16:], 0x4000)
	put(image[section+40+8:], 0x8000)
	put(image[section+40+12:], 0x6000)
	put(image[section+40+16:], 0x8000)
	put(image[section+40+20:], 0x4200)
	for i := uint32(0); i < 2048; i++ {
		copy(image[0x200+i*6:], []byte{0xb8, 42, 0, 0, 0, 0xc3})
		put(image[0x4200+i*12:], 0x1000+i*6)
		put(image[0x4204+i*12:], 0x1000+i*6+6)
		put(image[0x4208+i*12:], 0xc000)
	}
	copy(image[0xa200:], []byte{1, 0, 0, 0})
	return image
}

func TestPreparedLookupMatchesIndependentIntervalScan(t *testing.T) {
	p, err := NewPEImage(largeRuntimePE())
	if err != nil {
		t.Fatal(err)
	}
	for rva := uint32(0xff0); rva < 0x5010; rva += 7 {
		for _, size := range []uint32{0, 4, 5, 6, 7, 16, 4096, 4097, 8192, 8193} {
			valid := size >= 5 && size <= MaxFunctionBytes && rva >= 0x1000 && uint64(rva)+uint64(size) <= 0x5000
			if valid {
				for i := uint32(0); i < 2048; i++ {
					begin, end := 0x1000+i*6, 0x1000+i*6+6
					if begin < rva+size && rva < end && (rva != begin || rva+size != end) {
						valid = false
						break
					}
				}
			}
			var metadata functionUnwind
			if _, _, err := p.readCode(rva, size, &metadata); (err == nil) != valid {
				t.Fatalf("indexed overlap lookup differs at rva=%#x size=%d: %v", rva, size, err)
			}
		}
	}
}

func BenchmarkPlanSelection64(b *testing.B) {
	image := largeRuntimePE()
	for _, reuse := range []bool{false, true} {
		name := "reparse"
		if reuse {
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				// Include construction/copy in every batch, as in preflight/install.
				var prepared *PEImage
				if reuse {
					var err error
					prepared, err = NewPEImage(image)
					if err != nil {
						b.Fatal(err)
					}
				}
				for i := uint32(0); i < 64; i++ {
					var err error
					if reuse {
						_, err = prepared.BuildFunction(0x1000+i*6, 6, 0x100000, 0x180000+uint64(i)*8192, 0x300000, 0x400000, 0x500000, i+1)
					} else {
						_, err = BuildPEFunction(image, 0x1000+i*6, 6, 0x100000, 0x180000+uint64(i)*8192, 0x300000, 0x400000, 0x500000, i+1)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func FuzzPreparedImagePlan(f *testing.F) {
	image, _ := cleanupPE()
	f.Add(image, uint32(peLeafRVA), uint32(33))
	f.Fuzz(func(t *testing.T, data []byte, rva, size uint32) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		p, err := NewPEImage(data)
		if err != nil {
			return
		}
		plan, err := p.BuildFunction(rva, size, 0x100000, 0x180000, 0x300000, 0x400000, 0x500000, 1)
		if err != nil && len(plan.Image) > 0 {
			t.Fatal("failed prepared plan returned executable bytes")
		}
	})
}
