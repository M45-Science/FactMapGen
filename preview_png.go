package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"io"
	"sync"

	"github.com/klauspost/compress/zlib"
)

const previewPNGHeader = "\x89PNG\r\n\x1a\n"

type previewPNGEncoder struct {
	row []byte
	zw  *zlib.Writer
}

var previewPNGEncoderPool sync.Pool

// encodeOpaquePreviewPNG is specialized for the opaque RGBA images produced by
// both preview renderers. PNG filter None is substantially faster for these map
// images and also compresses them well with the BestSpeed deflate level.
func encodeOpaquePreviewPNG(img *image.RGBA) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid preview image size: %dx%d", width, height)
	}
	if width > maxPreviewOutputSize || height > maxPreviewOutputSize {
		return nil, fmt.Errorf(
			"preview image size %dx%d exceeds the %dx%d encoder limit",
			width, height, maxPreviewOutputSize, maxPreviewOutputSize,
		)
	}

	encoder := acquirePreviewPNGEncoder()
	defer releasePreviewPNGEncoder(encoder)

	var out bytes.Buffer
	out.Grow(128 + width*height/4)
	out.WriteString(previewPNGHeader)

	var ihdr [13]byte
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // truecolor, without alpha
	appendPreviewPNGChunk(&out, "IHDR", ihdr[:])

	idatStart := out.Len()
	var idatHeader [8]byte
	out.Write(idatHeader[:])

	if encoder.zw == nil {
		zw, err := zlib.NewWriterLevel(&out, zlib.BestSpeed)
		if err != nil {
			return nil, err
		}
		encoder.zw = zw
	} else {
		encoder.zw.Reset(&out)
	}

	rowSize := 1 + width*3
	if cap(encoder.row) < rowSize {
		encoder.row = make([]byte, rowSize)
	} else {
		encoder.row = encoder.row[:rowSize]
	}
	encoder.row[0] = 0 // PNG filter None
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		start := img.PixOffset(bounds.Min.X, y)
		source := img.Pix[start : start+width*4]
		compactOpaqueRGBARow(encoder.row[1:], source)
		if _, err := encoder.zw.Write(encoder.row); err != nil {
			return nil, err
		}
	}
	if err := encoder.zw.Close(); err != nil {
		return nil, err
	}

	idatEnd := out.Len()
	idatSize := idatEnd - idatStart - len(idatHeader)
	if uint64(idatSize) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("preview PNG IDAT chunk is too large: %d bytes", idatSize)
	}
	encoded := out.Bytes()
	binary.BigEndian.PutUint32(encoded[idatStart:idatStart+4], uint32(idatSize))
	copy(encoded[idatStart+4:idatStart+8], "IDAT")
	appendPreviewPNGChecksum(&out, encoded[idatStart+4:idatEnd])
	appendPreviewPNGChunk(&out, "IEND", nil)
	return out.Bytes(), nil
}

func acquirePreviewPNGEncoder() *previewPNGEncoder {
	if encoder, ok := previewPNGEncoderPool.Get().(*previewPNGEncoder); ok {
		return encoder
	}
	return new(previewPNGEncoder)
}

func releasePreviewPNGEncoder(encoder *previewPNGEncoder) {
	if encoder.zw != nil {
		// The zlib writer otherwise retains the bytes.Buffer containing the
		// returned PNG while it is idle in the pool.
		encoder.zw.Reset(io.Discard)
	}
	previewPNGEncoderPool.Put(encoder)
}

// compactOpaqueRGBARow removes four known-opaque alpha bytes at a time. Using
// LittleEndian helpers is portable and compiles to unaligned word moves on the
// common 64-bit targets; it does not depend on native byte order or unsafe.
func compactOpaqueRGBARow(dst, source []byte) {
	for len(source) >= 16 {
		first := binary.LittleEndian.Uint64(source[:8])
		second := binary.LittleEndian.Uint64(source[8:16])
		packed0 := (first & 0x00ffffff) |
			((first >> 8) & 0x0000ffffff000000) |
			((second & 0x0000ffff) << 48)
		packed1 := uint32((second>>16)&0x000000ff | (second>>24)&0xffffff00)
		binary.LittleEndian.PutUint64(dst[:8], packed0)
		binary.LittleEndian.PutUint32(dst[8:12], packed1)
		source = source[16:]
		dst = dst[12:]
	}
	for len(source) >= 4 {
		copy(dst[:3], source[:3])
		source = source[4:]
		dst = dst[3:]
	}
}

func appendPreviewPNGChunk(out *bytes.Buffer, name string, data []byte) {
	var header [8]byte
	binary.BigEndian.PutUint32(header[:4], uint32(len(data)))
	copy(header[4:], name)
	out.Write(header[:])
	out.Write(data)
	checksum := crc32.Update(0, crc32.IEEETable, header[4:])
	checksum = crc32.Update(checksum, crc32.IEEETable, data)
	var footer [4]byte
	binary.BigEndian.PutUint32(footer[:], checksum)
	out.Write(footer[:])
}

func appendPreviewPNGChecksum(out *bytes.Buffer, data []byte) {
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], crc32.ChecksumIEEE(data))
	out.Write(checksum[:])
}
