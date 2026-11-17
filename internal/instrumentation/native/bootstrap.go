package native

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

type debugSelection struct{ ID, RVA, Size uint32 }

func bootstrapRequest(pipe string, selected []debugSelection, capacity uint32) ([]byte, error) {
	if len(selected) == 0 || len(selected) > 16 || capacity == 0 || capacity > 1024 {
		return nil, errors.New("invalid launch selection/capacity")
	}
	chars := append(utf16.Encode([]rune(pipe)), 0)
	if len(chars) > 512 {
		return nil, errors.New("launch pipe name too long")
	}
	b := make([]byte, 32+len(selected)*12+len(chars)*2)
	copy(b, "APBOOT1\x00")
	binary.LittleEndian.PutUint32(b[8:], uint32(len(b)))
	binary.LittleEndian.PutUint32(b[12:], uint32(len(selected)))
	binary.LittleEndian.PutUint32(b[16:], capacity)
	binary.LittleEndian.PutUint32(b[20:], uint32(len(chars)))
	for i, p := range selected {
		at := 32 + i*12
		binary.LittleEndian.PutUint32(b[at:], p.ID)
		binary.LittleEndian.PutUint32(b[at+4:], p.RVA)
		binary.LittleEndian.PutUint32(b[at+8:], p.Size)
	}
	for i, c := range chars {
		binary.LittleEndian.PutUint16(b[32+len(selected)*12+i*2:], c)
	}
	return b, nil
}
