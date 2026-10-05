package config

import (
	"encoding/binary"
	"unicode/utf16"
)

func decodeUTF16(b []byte) []byte {
	words := make([]uint16, len(b)/2)
	for i := range words {
		words[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return []byte(string(utf16.Decode(words)))
}
