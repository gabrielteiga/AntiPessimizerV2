package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
)

const hookStride = 8192

// installLeafHooks runs only during the cooperating host's startup barrier.
// Event consumption starts after this exchange; no second goroutine reads the
// synchronous startup stream. Plans are built from the exact executable file.
func installLeafHooks(conn io.ReadWriter, image []byte, procedures []Procedure) error {
	if len(procedures) == 0 || len(procedures) > 16 {
		return errors.New("invalid hook selection count")
	}
	if err := writeHookBytes(conn, []byte{2}); err != nil {
		return err
	}
	var offer [48]byte
	if _, err := io.ReadFull(conn, offer[:]); err != nil {
		return fmt.Errorf("hook allocation offer: %w", err)
	}
	if string(offer[:8]) != "APHOOK1\x00" {
		return errors.New("invalid hook allocation protocol")
	}
	u32, u64 := binary.LittleEndian.Uint32, binary.LittleEndian.Uint64
	loaded, allocation := u64(offer[8:]), u64(offer[16:])
	capacity, count := u32(offer[24:]), u32(offer[28:])
	enter, exit := u64(offer[32:]), u64(offer[40:])
	const maxAddress = uint64(1) << 47
	if count != uint32(len(procedures)) || capacity != count*hookStride || allocation == 0 || allocation%4096 != 0 || allocation >= maxAddress || uint64(capacity) >= maxAddress-allocation || enter == exit {
		return errors.New("invalid native hook allocation")
	}
	sizes := make([]byte, count*4)
	if _, err := io.ReadFull(conn, sizes); err != nil {
		return err
	}
	type selectedPlan struct {
		plan   x64.LeafPlan
		source []byte
	}
	plans := make([]selectedPlan, 0, count)
	// A whole stride per procedure keeps its relative RVAs independent. The
	// final runtime table follows every code image and is relative to allocation.
	tableOffset := count*hookStride - uint32(count*24)
	payload := make([]byte, capacity)
	for i, proc := range procedures {
		size := u32(sizes[i*4:])
		offset := uint32(i) * hookStride
		plan, err := x64.BuildPELeaf(image, proc.RVA, size, loaded, allocation+uint64(offset), enter, exit, proc.ID)
		if err != nil {
			return fmt.Errorf("procedure %d hook plan: %w", proc.ID, err)
		}
		if uint64(offset)+uint64(len(plan.Image)) > uint64(tableOffset) || len(plan.Patch) > 64 {
			return errors.New("hook plan exceeds native allocation")
		}
		copy(payload[offset:], plan.Image)
		for j, f := range plan.Functions {
			at := int(tableOffset) + (i*2+j)*12
			binary.LittleEndian.PutUint32(payload[at:], offset+f.Begin)
			binary.LittleEndian.PutUint32(payload[at+4:], offset+f.End)
			binary.LittleEndian.PutUint32(payload[at+8:], offset+f.Unwind)
		}
		plans = append(plans, selectedPlan{plan, plan.Source})
	}
	var header [24]byte
	copy(header[:], "APPLAN1\x00")
	binary.LittleEndian.PutUint32(header[8:], count)
	binary.LittleEndian.PutUint32(header[12:], capacity)
	binary.LittleEndian.PutUint32(header[16:], tableOffset)
	binary.LittleEndian.PutUint32(header[20:], count*2)
	if err := writeHookBytes(conn, header[:]); err != nil {
		return err
	}
	if err := writeHookBytes(conn, payload); err != nil {
		return err
	}
	for _, p := range plans {
		var item [8]byte
		binary.LittleEndian.PutUint32(item[:], uint32(len(p.source)))
		binary.LittleEndian.PutUint32(item[4:], uint32(len(p.plan.Patch)))
		if err := writeHookBytes(conn, item[:]); err != nil {
			return err
		}
		if err := writeHookBytes(conn, p.source); err != nil {
			return err
		}
		if err := writeHookBytes(conn, p.plan.Patch); err != nil {
			return err
		}
	}
	var installed [1]byte
	if _, err := io.ReadFull(conn, installed[:]); err != nil {
		return fmt.Errorf("native hook installation: %w", err)
	}
	if installed[0] != 1 {
		return errors.New("native hook installation rejected")
	}
	return nil
}

func writeHookBytes(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return errors.New("invalid hook writer count")
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
