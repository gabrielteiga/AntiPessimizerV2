package x64

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/arch/x86/x86asm"
)

type indexedJumpTable struct {
	load, data int
	targets    []int
}

// The entry compares the full unsigned index before dispatch. JA reaches code
// outside the table. LEA loads its address into volatile R11, and the following
// JMP reads the selected pointer without modifying flags or the caller's stack.
func (p *PEImage) indexedJumpTable(rva uint32, code []byte, patchSize int) *indexedJumpTable {
	if len(code) < 34 || !bytes.Equal(code[:3], []byte{0x48, 0x83, 0xf9}) || code[3] == 0 || code[3] >= 32 ||
		(code[4] != 0x77 && (code[4] != 0x0f || code[5] != 0x87)) {
		return nil
	}
	branch, err := x86asm.Decode(code[4:], 64)
	if err != nil || branch.Op != x86asm.JA || branch.Len != 2 && branch.Len != 6 {
		return nil
	}
	rel, ok := branch.Args[0].(x86asm.Rel)
	if !ok {
		return nil
	}
	load := 4 + branch.Len
	data := load + 11
	count := int(code[3]) + 1
	end := data + count*8
	fallback := load + int(rel)
	if patchSize > load || end >= len(code) || fallback < end || fallback >= len(code) ||
		!bytes.Equal(code[load:data], []byte{0x4c, 0x8d, 0x1d, 4, 0, 0, 0, 0x41, 0xff, 0x24, 0xcb}) ||
		!p.hasIndexedTableRelocations(rva, len(code), data, count) {
		return nil
	}
	starts := make([]bool, len(code))
	for offset := end; offset < len(code); {
		inst, err := x86asm.Decode(code[offset:], 64)
		if err != nil || inst.Op == 0 || inst.Len == 0 || offset+inst.Len > len(code) {
			return nil
		}
		starts[offset] = true
		offset += inst.Len
		for _, arg := range inst.Args {
			switch arg := arg.(type) {
			case x86asm.Rel:
				if int64(offset)+int64(arg) < int64(end) {
					return nil
				}
			case x86asm.Reg:
				if tableRegister(arg) {
					return nil
				}
			case x86asm.Mem:
				if tableRegister(arg.Base) || tableRegister(arg.Index) {
					return nil
				}
			}
		}
	}
	if !starts[fallback] || p.preferredBase == 0 || p.preferredBase >= maxUserAddress-uint64(p.imageSize) {
		return nil
	}
	table := &indexedJumpTable{load: load, data: data, targets: make([]int, count)}
	for i := range table.targets {
		address := binary.LittleEndian.Uint64(code[data+i*8:])
		preferred := p.preferredBase + uint64(rva)
		if address < preferred || address-preferred >= uint64(len(code)) || !starts[address-preferred] {
			return nil
		}
		table.targets[i] = int(address - preferred)
	}
	return table
}

func tableRegister(reg x86asm.Reg) bool {
	return reg == x86asm.R11 || reg == x86asm.R11L || reg == x86asm.R11W || reg == x86asm.R11B
}

func (p *PEImage) hasIndexedTableRelocations(rva uint32, size, start, count int) bool {
	if p.relocSize < 8 || p.relocSize > 8<<20 {
		return false
	}
	data, ok := p.mapped(p.relocRVA, p.relocSize, false)
	if !ok {
		return false
	}
	var seen [32]bool
	found := 0
	for len(data) != 0 {
		if len(data) < 8 {
			return false
		}
		page, length := binary.LittleEndian.Uint32(data), int(binary.LittleEndian.Uint32(data[4:]))
		if page%4096 != 0 || length < 8 || length%4 != 0 || length > len(data) {
			return false
		}
		for at := 8; at < length; at += 2 {
			entry := binary.LittleEndian.Uint16(data[at:])
			kind := entry >> 12
			if kind == 0 {
				continue
			}
			if kind != 10 { // IMAGE_REL_BASED_DIR64 in this PE32+ subset
				return false
			}
			offset := int64(page) + int64(entry&0xfff) - int64(rva)
			if offset >= int64(size) || offset+8 <= 0 {
				continue
			}
			slot := offset - int64(start)
			if slot < 0 || slot >= int64(count*8) || slot%8 != 0 || seen[slot/8] {
				return false
			}
			seen[slot/8] = true
			found++
		}
		data = data[length:]
	}
	return found == count
}

// The normal body rewriter sees equal-length placeholders for the checked
// dispatch and table. The helper below restores the dispatch and writes the
// clone targets after the original branch and return expansions are known.
func (t *indexedJumpTable) plannerCode(code []byte) []byte {
	body := bytes.Clone(code)
	for i := t.load; i < t.data+len(t.targets)*8; i++ {
		body[i] = 0x90
	}
	return body
}

func (t *indexedJumpTable) relocate(source, body []byte, rva uint32, loaded, base uint64, flow *leafBody) error {
	translate, err := scopePCMap(body, *flow, rva, 0)
	if err != nil {
		return err
	}
	fail := func() error { return fmt.Errorf("%w: invalid relocated index table boundary", ErrUnsupported) }
	load, loadOK := translate(rva + uint32(t.load))
	data, dataOK := translate(rva + uint32(t.data))
	if !loadOK || !dataOK || data-load != uint32(t.data-t.load) {
		return fail()
	}
	copy(flow.code[load:data], source[t.load:t.data])
	for i, off := range t.targets {
		target, ok := translate(rva + uint32(off))
		if !ok {
			return fail()
		}
		binary.LittleEndian.PutUint64(flow.code[int(data)+i*8:], base+512+uint64(target))
		// Installer compares against the loaded source, including each exact
		// PE relocation. Instruction and all other source bytes stay intact.
		binary.LittleEndian.PutUint64(source[t.data+i*8:], loaded+uint64(off))
	}
	return nil
}
