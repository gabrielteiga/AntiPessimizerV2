package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
)

type hookStream struct {
	in    *bytes.Reader
	out   bytes.Buffer
	chunk int
	fail  bool
}

func (s *hookStream) Read(b []byte) (int, error) { return s.in.Read(b) }
func (s *hookStream) Write(b []byte) (int, error) {
	if s.fail {
		return 0, io.ErrClosedPipe
	}
	if s.chunk > 0 && len(b) > s.chunk {
		b = b[:s.chunk]
	}
	return s.out.Write(b)
}
func hookImage() []byte {
	image := testfixture.Image(false)
	binary.LittleEndian.PutUint32(image[128+24+60:], 0x200)
	copy(image[0x220:], append(bytes.Repeat([]byte{0x90}, 16), 0x48, 0x89, 0xc8, 0xc3))
	return image
}
func hookOffer() []byte {
	b := make([]byte, 53)
	copy(b, "APHOOK1\x00")
	binary.LittleEndian.PutUint64(b[8:], 0x100000)
	binary.LittleEndian.PutUint64(b[16:], 0x110000)
	binary.LittleEndian.PutUint32(b[24:], 8192)
	binary.LittleEndian.PutUint32(b[28:], 1)
	binary.LittleEndian.PutUint64(b[32:], 0x200000)
	binary.LittleEndian.PutUint64(b[40:], 0x200100)
	binary.LittleEndian.PutUint32(b[48:], 20)
	b[52] = 1
	return b
}
func TestHookExchangeIncludesExactSourceAndCompleteUnwindTable(t *testing.T) {
	for _, chunk := range []int{0, 1, 17} {
		stream := &hookStream{in: bytes.NewReader(hookOffer()), chunk: chunk}
		if err := installHooks(stream, hookImage(), []Procedure{{1, 0x5020}}); err != nil {
			t.Fatal(err)
		}
		b := stream.out.Bytes()
		if b[0] != 2 || string(b[1:9]) != "APPLAN1\x00" || binary.LittleEndian.Uint32(b[9:]) != 1 {
			t.Fatal("invalid installation command")
		}
		header, payload := b[1:25], b[25:25+8192]
		at := binary.LittleEndian.Uint32(header[16:])
		if binary.LittleEndian.Uint32(header[20:]) != 2 || at != 8192-24 {
			t.Fatal("missing probe runtime ranges")
		}
		for i := 0; i < 2; i++ {
			row := payload[int(at)+i*12:]
			begin, end, unwind := binary.LittleEndian.Uint32(row), binary.LittleEndian.Uint32(row[4:]), binary.LittleEndian.Uint32(row[8:])
			if begin >= end || end >= at || unwind+8 >= at || !bytes.Equal(payload[unwind:unwind+8], []byte{1, 7, 2, 0, 7, 1, 25, 0}) {
				t.Fatal("invalid runtime-function metadata")
			}
		}
		item := b[25+8192:]
		if binary.LittleEndian.Uint32(item) != 20 || binary.LittleEndian.Uint32(item[4:]) != 14 || !bytes.Equal(item[8:28], hookImage()[0x220:0x234]) || binary.LittleEndian.Uint64(item[34:]) != 0x110000 {
			t.Fatal("missing original code or incorrect entry target")
		}
	}
}

func TestHookExchangeSupports64ProcedureRuntimeTables(t *testing.T) {
	const count = 64
	image := testfixture.Image(false)
	binary.LittleEndian.PutUint32(image[128+24+60:], 0x200)
	offer := make([]byte, 56+count*4+1)
	copy(offer, "APHOOK2\x00")
	binary.LittleEndian.PutUint64(offer[8:], 0x100000)
	binary.LittleEndian.PutUint64(offer[16:], 0x110000)
	binary.LittleEndian.PutUint32(offer[24:], count*8192)
	binary.LittleEndian.PutUint32(offer[28:], count)
	binary.LittleEndian.PutUint64(offer[32:], 0x200000)
	binary.LittleEndian.PutUint64(offer[40:], 0x200100)
	binary.LittleEndian.PutUint64(offer[48:], 0x200200)
	var procedures []Procedure
	for i := 0; i < count; i++ {
		copy(image[0x220+i*6:], []byte{0xb8, byte(i + 1), 0, 0, 0, 0xc3})
		binary.LittleEndian.PutUint32(offer[56+i*4:], 6)
		procedures = append(procedures, Procedure{uint32(i + 1), 0x5020 + uint32(i)*6})
	}
	offer[len(offer)-1] = 1
	stream := &hookStream{in: bytes.NewReader(offer)}
	if err := installHooks(stream, image, procedures); err != nil {
		t.Fatal(err)
	}
	data := stream.out.Bytes()
	header, payload := data[1:25], data[25:25+count*8192]
	table := binary.LittleEndian.Uint32(header[16:])
	if binary.LittleEndian.Uint32(header[8:]) != count || binary.LittleEndian.Uint32(header[20:]) != count*3 {
		t.Fatal("full catalog lost runtime entries")
	}
	var end uint32
	for i := 0; i < count*3; i++ {
		entry := payload[int(table)+i*12:]
		begin, next := binary.LittleEndian.Uint32(entry), binary.LittleEndian.Uint32(entry[4:])
		if begin < end || next <= begin || next > table {
			t.Fatal("runtime table unsorted or overlaps code", i, begin, next)
		}
		end = next
	}
}
func TestHookExchangeRejectsInvalidOfferWithoutSendingCode(t *testing.T) {
	for _, mutate := range []func([]byte){
		func(b []byte) { b[0] = 0 }, func(b []byte) { binary.LittleEndian.PutUint32(b[28:], 17) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[24:], 1<<31) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[16:], 0) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[16:], 0x110001) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[16:], 1<<47) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[40:], 0x200000) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[48:], 4097) },
	} {
		offer := hookOffer()
		mutate(offer)
		s := &hookStream{in: bytes.NewReader(offer)}
		if err := installHooks(s, hookImage(), []Procedure{{1, 0x5020}}); err == nil || s.out.Len() != 1 {
			t.Fatal("bad offer sent an install plan", err, s.out.Len())
		}
	}
	for n := 0; n < 53; n++ {
		s := &hookStream{in: bytes.NewReader(hookOffer()[:n])}
		if err := installHooks(s, hookImage(), []Procedure{{1, 0x5020}}); err == nil {
			t.Fatalf("truncated at %d accepted", n)
		}
	}
	s := &hookStream{in: bytes.NewReader(hookOffer()), fail: true}
	if err := installHooks(s, hookImage(), []Procedure{{1, 0x5020}}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	offer := hookOffer()
	offer[52] = 0
	if err := installHooks(&hookStream{in: bytes.NewReader(offer)}, hookImage(), []Procedure{{1, 0x5020}}); err == nil {
		t.Fatal("native rejection ignored")
	}
}

func TestGeneralHookExchangeRelocatesEachProcedureCleanupThunk(t *testing.T) {
	image := hookImage()
	copy(image[0x260:], image[0x220:0x234])
	offer := make([]byte, 65)
	copy(offer, hookOffer()[:48])
	copy(offer, "APHOOK2\x00")
	binary.LittleEndian.PutUint32(offer[24:], 2*hookStride)
	binary.LittleEndian.PutUint32(offer[28:], 2)
	binary.LittleEndian.PutUint64(offer[48:], 0x200200)
	binary.LittleEndian.PutUint32(offer[56:], 20)
	binary.LittleEndian.PutUint32(offer[60:], 20)
	offer[64] = 1
	s := &hookStream{in: bytes.NewReader(offer), chunk: 3}
	if err := installHooks(s, image, []Procedure{{1, 0x5020}, {2, 0x5060}}); err != nil {
		t.Fatal(err)
	}
	h, p := s.out.Bytes()[1:25], s.out.Bytes()[25:25+2*hookStride]
	table := int(binary.LittleEndian.Uint32(h[16:]))
	if binary.LittleEndian.Uint32(h[20:]) != 6 {
		t.Fatal("missing body ranges")
	}
	previous := uint32(0)
	for i := 0; i < 6; i++ {
		row := p[table+i*12:]
		begin, end, u := binary.LittleEndian.Uint32(row), binary.LittleEndian.Uint32(row[4:]), binary.LittleEndian.Uint32(row[8:])
		if begin < previous || end <= begin || end >= uint32(table) {
			t.Fatal("invalid sorted ranges")
		}
		previous = end
		if i%3 != 1 {
			continue
		}
		info := p[u:]
		at := 4 + ((int(info[2])+1)&^1)*2
		thunk := binary.LittleEndian.Uint32(info[at:])
		stride := uint32(i/3) * hookStride
		if info[0] != 17 || thunk < stride || thunk >= stride+hookStride || binary.LittleEndian.Uint32(info[at+4:]) != uint32(i/3+1) || binary.LittleEndian.Uint64(p[thunk+6:]) != 0x200200 {
			t.Fatal("wrong per-procedure cleanup relocation", i, thunk)
		}
	}
}
