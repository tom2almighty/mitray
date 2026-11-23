package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildICOFromShippedSVG(t *testing.T) {
	svgBytes, err := os.ReadFile(filepath.Join("..", "..", sourceSVGFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			data, err := buildICOFromSVG(string(svgBytes), v.color, iconSizes)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) < 6+len(iconSizes)*16 {
				t.Fatalf("ICO too small: %d bytes", len(data))
			}
			if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
				t.Fatal("invalid ICONDIR header")
			}
			if got := int(binary.LittleEndian.Uint16(data[4:6])); got != len(iconSizes) {
				t.Fatalf("entry count = %d, want %d", got, len(iconSizes))
			}
			// Verify the 256x256 entry uses PNG signature.
			lastEntry := 6 + (len(iconSizes)-1)*16
			off := binary.LittleEndian.Uint32(data[lastEntry+12 : lastEntry+16])
			if !bytes.HasPrefix(data[off:], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
				t.Fatal("256x256 entry is not PNG-encoded")
			}
		})
	}
}
