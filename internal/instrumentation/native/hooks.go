package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/x64"
)

const hookStride = 8192
const largeHookStride = x64.MaxFunctionImageBytes

// installHooks runs only during the owned target's startup barrier.
// Event consumption starts after this exchange; no second goroutine reads the
// synchronous startup stream. Plans are built from the exact executable file.
func installHooks(conn io.ReadWriter, image []byte, procedures []Procedure) error {
	if len(procedures) == 0 || len(procedures) > MaxSelectedProcedures {
		return errors.New("invalid hook selection count")
	}
	if err := writeHookBytes(conn, []byte{2}); err != nil {
		return err
	}
	var offer [48]byte
	if _, err := io.ReadFull(conn, offer[:]); err != nil {
		return fmt.Errorf("hook allocation offer: %w", err)
	}
	frameSavePlans := string(offer[:8]) == "APHOOKH\x00"
	farSavePlans := frameSavePlans || string(offer[:8]) == "APHOOKG\x00"
	inheritedHandlers := farSavePlans || string(offer[:8]) == "APHOOKF\x00"
	xmmSavePlans := inheritedHandlers || string(offer[:8]) == "APHOOKE\x00"
	multipleSavePlans := xmmSavePlans || string(offer[:8]) == "APHOOKD\x00"
	chainPlans := multipleSavePlans || string(offer[:8]) == "APHOOKC\x00"
	tinyPlans := chainPlans || string(offer[:8]) == "APHOOKB\x00"
	tailPlans := tinyPlans || string(offer[:8]) == "APHOOKA\x00"
	expandedSelection := tailPlans || string(offer[:8]) == "APHOOK9\x00"
	largePlans := expandedSelection || string(offer[:8]) == "APHOOK8\x00"
	typedScopes := largePlans || string(offer[:8]) == "APHOOK7\x00"
	delphiScopes := typedScopes || string(offer[:8]) == "APHOOK6\x00"
	resuming := delphiScopes || string(offer[:8]) == "APHOOK5\x00"
	forwarding := resuming || string(offer[:8]) == "APHOOK3\x00" || string(offer[:8]) == "APHOOK4\x00"
	general := string(offer[:8]) == "APHOOK2\x00" || forwarding
	if string(offer[:8]) != "APHOOK1\x00" && !general {
		return errors.New("invalid hook allocation protocol")
	}
	u32, u64 := binary.LittleEndian.Uint32, binary.LittleEndian.Uint64
	loaded, allocation := u64(offer[8:]), u64(offer[16:])
	capacity, count := u32(offer[24:]), u32(offer[28:])
	enter, exit := u64(offer[32:]), u64(offer[40:])
	const maxAddress = uint64(1) << 47
	// New adapters reserve the runtime table after all complete code strides.
	// Preserve the bounded layout of older (at most 16-hook) adapters as well.
	stride := uint32(hookStride)
	if largePlans {
		stride = largeHookStride
	}
	if !expandedSelection && count > 64 {
		return errors.New("update the native adapter DLL: selections larger than 64 require APHOOK9")
	}
	codeCapacity := count * stride
	tableCapacity := count * 3 * 12
	if chainPlans {
		tableCapacity = count * 4 * 12
	}
	if count != uint32(len(procedures)) || capacity != codeCapacity+tableCapacity && (chainPlans || capacity != codeCapacity) || allocation == 0 || allocation%4096 != 0 || allocation >= maxAddress || uint64(capacity) >= maxAddress-allocation || enter == exit {
		return errors.New("invalid native hook allocation")
	}
	var cleanup uint64
	per := 2
	if general {
		var extra [8]byte
		if _, err := io.ReadFull(conn, extra[:]); err != nil {
			return err
		}
		cleanup = u64(extra[:])
		per = 3
	}
	sizes := make([]byte, count*4)
	if _, err := io.ReadFull(conn, sizes); err != nil {
		return err
	}
	type selectedPlan struct {
		patch, source []byte
	}
	plans := make([]selectedPlan, 0, count)
	var prepared *x64.PEImage
	if general {
		var err error
		prepared, err = x64.NewPEImage(image)
		if err != nil {
			return err
		}
	}
	// A whole stride per procedure keeps its relative RVAs independent. The
	// final runtime table follows code and is relative to allocation.
	tableOffset := capacity - uint32(int(count)*per*12)
	if chainPlans {
		tableOffset = codeCapacity
	}
	entries := 0
	targets := make([]uint32, 0, len(procedures))
	for _, p := range procedures {
		targets = append(targets, p.RVA)
	}
	payload := make([]byte, capacity)
	for i, proc := range procedures {
		size := u32(sizes[i*4:])
		offset := uint32(i) * stride
		var source, patch, code []byte
		var functions []x64.RuntimeFunction
		if general {
			plan, err := prepared.BuildSelectedFunction(proc.RVA, size, loaded, allocation+uint64(offset), enter, exit, cleanup, proc.ID, targets)
			if err != nil {
				return fmt.Errorf("procedure %d frame plan: %w", proc.ID, err)
			}
			if plan.InheritedHandler && !inheritedHandlers {
				return errors.New("update the native adapter DLL: inherited chained handlers require APHOOKF")
			}
			if len(plan.Functions) == 4 && !chainPlans {
				return errors.New("update the native adapter DLL: chained nonvolatile saves require APHOOKC")
			}
			if len(plan.Functions) == 4 && plan.Image[plan.Functions[2].Unwind+2] > 2 && !multipleSavePlans {
				return errors.New("update the native adapter DLL: multiple chained saves require APHOOKD")
			}
			if len(plan.Functions) == 4 && plan.Image[plan.Functions[2].Unwind+3] != 0 && !frameSavePlans {
				return errors.New("update the native adapter DLL: inherited frame-register saves require APHOOKH")
			}
			if len(plan.Functions) == 4 && !farSavePlans {
				info := plan.Image[plan.Functions[2].Unwind:]
				for at := 5; at < 4+int(info[2])*2; at += 4 {
					if info[at]&15 == 5 || info[at]&15 == 9 {
						return errors.New("update the native adapter DLL: far chained saves require APHOOKG")
					}
				}
			}
			if len(plan.Functions) == 4 && !xmmSavePlans {
				info := plan.Image[plan.Functions[2].Unwind:]
				for at := 5; at < 4+int(info[2])*2; at += 4 {
					if info[at]&15 == 8 {
						return errors.New("update the native adapter DLL: chained XMM saves require APHOOKE")
					}
				}
			}
			if len(plan.Patch) == 1 && !tinyPlans {
				return errors.New("breakpoint entries require APHOOKB and an owned target")
			}
			if len(plan.TailTargets) != 0 && !tailPlans {
				return errors.New("update the native adapter DLL: selected tail calls require APHOOKA")
			}
			if !largePlans && (size > 4096 || len(plan.Image) > hookStride) {
				return errors.New("update the native adapter DLL: larger function plans require APHOOK8")
			}
			if plan.HandlerScopeStyle == 3 && !typedScopes {
				return errors.New("update the native adapter DLL: typed Delphi scopes require APHOOK7")
			}
			if plan.HandlerScopeStyle == 2 && !delphiScopes {
				return errors.New("update the native adapter DLL: reviewed Delphi scopes require APHOOK6")
			}
			if plan.RelocatedHandler && !resuming {
				return errors.New("update the native adapter DLL: translated exception scopes require APHOOK5")
			}
			if plan.OriginalHandlerRVA != 0 && !forwarding {
				return errors.New("update the native adapter DLL: original unwind handlers require APHOOK3")
			}
			source, patch, code, functions = plan.Source, plan.Patch, plan.Image, plan.Functions[:]
		} else {
			plan, err := x64.BuildPELeaf(image, proc.RVA, size, loaded, allocation+uint64(offset), enter, exit, proc.ID)
			if err != nil {
				return fmt.Errorf("procedure %d hook plan: %w", proc.ID, err)
			}
			source, patch, code, functions = plan.Source, plan.Patch, plan.Image, plan.Functions[:]
		}
		if uint64(offset)+uint64(len(code)) > uint64(tableOffset) || len(patch) > 64 || uint32(len(code)) > stride {
			return errors.New("hook plan exceeds native allocation")
		}
		copy(payload[offset:], code)
		if general {
			u := int(offset) + int(functions[1].Unwind)
			handler := u + 4 + ((int(payload[u+2])+1)&^1)*2
			binary.LittleEndian.PutUint32(payload[handler:], u32(payload[handler:])+offset)
			if len(functions) == 4 {
				local := int(offset + functions[2].Unwind)
				local += 4 + ((int(payload[local+2])+1)&^1)*2
				for field := 0; field < 3; field++ {
					word := payload[local+field*4:]
					binary.LittleEndian.PutUint32(word, u32(word)+offset)
				}
			}
		}
		for _, f := range functions {
			at := int(tableOffset) + entries*12
			binary.LittleEndian.PutUint32(payload[at:], offset+f.Begin)
			binary.LittleEndian.PutUint32(payload[at+4:], offset+f.End)
			binary.LittleEndian.PutUint32(payload[at+8:], offset+f.Unwind)
			entries++
		}
		plans = append(plans, selectedPlan{patch: patch, source: source})
	}
	var header [24]byte
	copy(header[:], "APPLAN1\x00")
	binary.LittleEndian.PutUint32(header[8:], count)
	binary.LittleEndian.PutUint32(header[12:], capacity)
	binary.LittleEndian.PutUint32(header[16:], tableOffset)
	binary.LittleEndian.PutUint32(header[20:], uint32(entries))
	if err := writeHookBytes(conn, header[:]); err != nil {
		return err
	}
	if err := writeHookBytes(conn, payload); err != nil {
		return err
	}
	for _, p := range plans {
		var item [8]byte
		binary.LittleEndian.PutUint32(item[:], uint32(len(p.source)))
		binary.LittleEndian.PutUint32(item[4:], uint32(len(p.patch)))
		if err := writeHookBytes(conn, item[:]); err != nil {
			return err
		}
		if err := writeHookBytes(conn, p.source); err != nil {
			return err
		}
		if err := writeHookBytes(conn, p.patch); err != nil {
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
