// Copyright 2024, Philip Conrad.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package gzipstreamwriter

import (
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

const (
	gzipID1     = 0x1f
	gzipID2     = 0x8b
	gzipDeflate = 8
	flagText    = 1 << 0
	flagHdrCrc  = 1 << 1
	flagExtra   = 1 << 2
	flagName    = 1 << 3
	flagComment = 1 << 4
)

const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

var ErrBlob = errors.New("gzip: invalid gzip blob")

type CompressedBlobWriter interface {
	WriteCompressed(p []byte) (n int, err error)
}

// Design Goals:
// - Allow writing either raw bytes or compressed gzip blobs to a destination, resulting in a valid, concatenated gzip blob at the destination, as if the blob had been written "all at once" as a single gzip byte stream.
// - Avoid decompressing the compressed blobs.
// - Avoid excessive memory and CPU burn if possible.
// - Avoid exposing the awful complex parts to happy-path users. Still provide some support scaffolding for hard-mode folks.

// Design Anti-Goals:
// - Best possible compression performance: We know that some usage patterns can result in a less-than-optimal overall compression ratio.
// -

// - When it's time to ship off events for upload:
//   - We construct a chunk from a subslice of the event blobs
//   - Chop off the header/trailer from each blob. Save the header from the first one.
//   - Update each CRC32 from the trailer appropriately for its byte position, and save the updated CRC32's in a slice.
//     - XOR the slice CRC32's together for the final CRC32.
//   - Write header to the byte stream.
//   - Write each blob to the byte stream.
//   - Write the trailer to the byte stream.
//   - We then return each []byte and gzip.Writer to the pool for later reuse.
//
// - The blob writer takes a list of gzip compressed blobs, and writes to an io.WriteCloser?
//   - This allows writing a "snapshot" of blobs, with minimal effort. The queue management is separate then.

// GzipBlobStream efficiently concatenates gzipped blobs together, and ensures a correct header/trailer is written to the output.
// Note: All blobs need to follow the same compressor settings, and need to include their own header/trailers.
// type GzipBlobStream struct {
// 	buffers     [][]byte
// 	w           io.Writer
// 	level       int
// 	wroteHeader bool
// 	closed      bool
// 	digest      uint32
// 	length      uint32
// }

// func NewGzipBlobStream(dest io.WriteCloser, source [][]byte) *GzipBlobStream {
// 	return &GzipBlobStream{buffers: source, writer: dest}
// }

// // Writes all blobs to the io.WriteCloser. Returns any errors.
// func (g *GzipBlobStream) Flush() error {
// }

// func (g *GzipBlobStream) Reset(dest io.WriteCloser, source [][]byte) {
// 	g.buffers = source
// 	g.writer = dest
// 	g.digest = 0
// }

// // Appends a blob to the buffers list.
// func (g *GzipBlobStream) Write(bs []byte) (n int, err error) {
// }

// // Flushes all available data to the output. Writes the accumulated trailer to the output.
// func (g *GzipBlobStream) Close() error {
// }

type GzipStreamWriter struct {
	gzip.Header // written at first call to Write, Flush, or Close
	w           io.Writer
	compressor  *flate.Writer
	level       int
	err         error
	digest      uint32
	size        uint32
	stateFlags  uint32 // 0x0: wroteHeader, 0x1: closed
}

func NewGzipStreamWriter(w io.Writer) *GzipStreamWriter {
	z, _ := NewGzipStreamWriterLevel(w, DefaultCompression)
	return z
}

func NewGzipStreamWriterLevel(w io.Writer, level int) (*GzipStreamWriter, error) {
	z := new(GzipStreamWriter)
	z.init(w, level)
	return z, nil
}

func (z *GzipStreamWriter) init(w io.Writer, level int) {
	compressor := z.compressor
	if compressor != nil {
		compressor.Reset(w)
	}

	*z = GzipStreamWriter{
		Header: gzip.Header{
			OS: 255, // unknown
		},
		w:          w,
		level:      level,
		compressor: compressor,
	}
}

func (z *GzipStreamWriter) setWroteHeader(value bool) {
	flag := uint32(0)
	if value {
		flag = 1
	}
	z.stateFlags = (z.stateFlags & ^uint32(0x0001)) | flag
}

func (z *GzipStreamWriter) setClosed(value bool) {
	flag := uint32(0)
	if value {
		flag = 1
	}
	z.stateFlags = (z.stateFlags & ^uint32(0x0002)) | (flag << 1)
}

func (z *GzipStreamWriter) checkWroteHeader() bool {
	flag := z.stateFlags & 0x1
	if flag == 1 {
		return true
	}
	return false
}

func (z *GzipStreamWriter) checkClosed() bool {
	flag := (z.stateFlags & 0x2) >> 1
	if flag == 1 {
		return true
	}
	return false
}

func (z *GzipStreamWriter) writeHeader() (int, error) {
	// Write the GZIP header lazily.
	z.setWroteHeader(true)
	buf := [10]byte{}
	buf[0] = gzipID1
	buf[1] = gzipID2
	buf[2] = gzipDeflate
	buf[3] = 0
	if z.Extra != nil {
		buf[3] |= 0x04
	}
	if z.Name != "" {
		buf[3] |= 0x08
	}
	if z.Comment != "" {
		buf[3] |= 0x10
	}
	binary.LittleEndian.PutUint32(buf[4:8], uint32(z.ModTime.Unix()))
	if z.level == BestCompression {
		buf[8] = 2
	} else if z.level == BestSpeed {
		buf[8] = 4
	} else {
		buf[8] = 0
	}
	buf[9] = z.OS
	var n int
	n, z.err = z.w.Write(buf[:10])
	if z.err != nil {
		return n, z.err
	}
	if z.Extra != nil {
		z.err = z.writeHeaderBytes(z.Extra)
		if z.err != nil {
			return n, z.err
		}
	}
	if z.Name != "" {
		z.err = z.writeHeaderString(z.Name)
		if z.err != nil {
			return n, z.err
		}
	}
	if z.Comment != "" {
		z.err = z.writeHeaderString(z.Comment)
		if z.err != nil {
			return n, z.err
		}
	}
	if z.compressor == nil {
		z.compressor, _ = flate.NewWriter(z.w, z.level)
	}
	return n, z.err
}

// writeHeaderBytes writes a length-prefixed byte slice to z.w.
func (z *GzipStreamWriter) writeHeaderBytes(b []byte) error {
	if len(b) > 0xffff {
		return errors.New("gzip.Write: Extra data is too large")
	}
	var lengthPrefix [2]byte
	binary.LittleEndian.PutUint16(b[:2], uint16(len(b)))
	_, err := z.w.Write(lengthPrefix[:2])
	if err != nil {
		return err
	}
	_, err = z.w.Write(b)
	return err
}

// writeHeaderString writes a UTF-8 string s in GZIP's format to z.w.
// GZIP (RFC 1952) specifies that strings are NUL-terminated ISO 8859-1 (Latin-1).
func (z *GzipStreamWriter) writeHeaderString(s string) (err error) {
	// GZIP stores Latin-1 strings; error if non-Latin-1; convert if non-ASCII.
	needconv := false
	for _, v := range s {
		if v == 0 || v > 0xff {
			return errors.New("gzip.Write: non-Latin-1 header string")
		}
		if v > 0x7f {
			needconv = true
		}
	}
	if needconv {
		b := make([]byte, 0, len(s))
		for _, v := range s {
			b = append(b, byte(v))
		}
		_, err = z.w.Write(b)
	} else {
		_, err = io.WriteString(z.w, s)
	}
	if err != nil {
		return err
	}
	// GZIP strings are NUL-terminated.
	_, err = z.w.Write([]byte{0})
	return err
}

// Writes the byte slice to the Gzip output stream.
// This will trigger a Flush call on the underlying compressor, emitting a sync marker at a minimum.
func (z *GzipStreamWriter) Write(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}

	var n int
	if n, z.err = z.writeHeader(); z.err != nil {
		return n, z.err
	}

	z.size += uint32(len(p))
	z.digest = crc32.Update(z.digest, crc32.IEEETable, p)

	if n, z.err = z.compressor.Write(p); z.err != nil {
		return n, z.err
	}
	z.err = z.compressor.Flush()
	return n, z.err
}

// func (z *GzipStreamWriter) WriteTo(w io.Writer) (n int64, err error)
// func (z *GzipStreamWriter) WriteByte(c byte) error
// func (z *GzipStreamWriter) WriteRune(c rune) error
// func (z *GzipStreamWriter) WriteString(s string) (n int, err error)

// Writes a compressed gzip byte blob through to the underlying writer.
func (z *GzipStreamWriter) WriteCompressed(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}

	var n int
	if n, z.err = z.writeHeader(); z.err != nil {
		return n, z.err
	}

	// Not a compliant Gzip blob. We can reject this up front.
	// This assumes header: 10 bytes, trailer: 8 bytes.
	if len(p) < 18 {
		return n, ErrBlob
	}
	trailerChecksum := binary.LittleEndian.Uint32(p[(len(p) - 8):(len(p) - 4)])
	trailerLength := binary.LittleEndian.Uint32(p[(len(p) - 4):])
	content := getDeflateSlice(p) // TODO: Implement the content slice-out function.

	z.size += trailerLength // uint32(len(p))

	z.digest = crc32Combine(z.digest, trailerChecksum, int(trailerLength))
	n, z.err = z.w.Write(content)
	// We would flush if we could here, but z.w is an io.Writer, and those do
	// not have to implement Flush().
	return n, z.err
}

func crc32Combine(front, back uint32, length int) uint32 {
	var zeroes [64]byte
	if length > 64 {
		for length > 64 {
			crc32.Update(front, crc32.IEEETable, zeroes[:])
			length -= 64
		}
	}

	return crc32.Update(front, crc32.IEEETable, zeroes[0:length]) ^ back
}

// Close closes the [Writer] by flushing any unwritten data to the underlying
// [io.Writer] and writing the GZIP footer.
// It does not close the underlying [io.Writer].
func (z *GzipStreamWriter) Close() error {
	if z.err != nil {
		return z.err
	}

	if z.checkClosed() {
		return nil
	}
	z.setClosed(true)

	if !z.checkWroteHeader() {
		z.Write(nil)
		if z.err != nil {
			return z.err
		}
	}

	if z.err = z.compressor.Close(); z.err != nil {
		return z.err
	}

	buf := [8]byte{}
	binary.LittleEndian.PutUint32(buf[:4], z.digest)
	binary.LittleEndian.PutUint32(buf[4:8], z.size)
	_, z.err = z.w.Write(buf[:8])
	return z.err
}

// Flush flushes any pending compressed data to the underlying writer.
//
// It is useful mainly in compressed network protocols, to ensure that
// a remote reader has enough data to reconstruct a packet. Flush does
// not return until the data has been written. If the underlying
// writer returns an error, Flush returns that error.
//
// In the terminology of the zlib library, Flush is equivalent to Z_SYNC_FLUSH.
func (z *GzipStreamWriter) Flush() error {
	if z.err != nil {
		return z.err
	}

	if z.checkClosed() {
		return nil
	}

	if !z.checkWroteHeader() {
		if _, err := z.Write(nil); err != nil {
			return z.err
		}
	}
	z.err = z.compressor.Flush()
	return z.err
}

func (z *GzipStreamWriter) Reset(w io.Writer) {
	z.init(w, z.level)
}

// Assertions for checking that we implemented the interfaces.
// The compiler will optimize all of these away.
var (
	_ io.Writer = (*GzipStreamWriter)(nil)
	// _ io.WriterTo = (*GzipStreamWriter)(nil)
	// _ io.ByteWriter        = (*GzipStreamWriter)(nil)
	// _ io.StringWriter      = (*GzipStreamWriter)(nil)
	// _ CompressedBlobWriter = (*GzipStreamWriter)(nil)
	_ io.Closer      = (*GzipStreamWriter)(nil)
	_ io.WriteCloser = (*GzipStreamWriter)(nil)
)

// Everything from here down is inherited from the source:
