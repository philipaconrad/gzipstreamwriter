// Copyright 2024, Philip Conrad.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package gzipstreamwriter provides a drop-in replacement for the stdlib
// gzip.Writer, as well as the ability to write multiple compressed gzip blobs
// to the same output stream, as if the were all written in one Write call.
package gzipstreamwriter

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"time"
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

// These constants are copied from the flate package, so that code that imports
// "philipaconrad/gzipstreamwriter" does not also have to import "compress/flate".
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

// The error types for the package.
var (
	ErrBlob                    = errors.New("gzip: invalid gzip blob")
	ErrHdrNonLatin1            = errors.New("gzip: non-Latin-1 header string")
	ErrHdrExtaDataTooLarge     = errors.New("gzip: extra data is too large")
	ErrInvalidCompressionLevel = errors.New("gzip: invalid compression level")
)

// CompressedBlobWriter is the interface for writing pre-compressed gzip blobs.
type CompressedBlobWriter interface {
	WriteCompressed(p []byte) (n int, err error)
}

// Design Goals:
// - Allow writing either raw bytes or compressed gzip blobs to a destination, resulting
//   in a valid, concatenated gzip blob at the destination, as if the blob had been written
//   "all at once" as a single gzip byte stream.
// - Avoid decompressing the compressed blobs.
// - Avoid excessive memory and CPU burn if possible.
// - Avoid exposing the awful complex parts to happy-path users. Still provide some support
//   scaffolding for hard-mode folks.

// Design Anti-Goals:
// - Best possible compression performance: We know that some usage patterns can result in a
//   less-than-optimal overall compression ratio.
// -

// - When it's time to ship off events for upload:
//   - We construct a chunk from a subslice of the event blobs
//   - Chop off the header/trailer from each blob. Save the header from the first one.
//   - Update each CRC32 from the trailer appropriately for its byte position, and save the
//     updated CRC32's in a slice.
//     - XOR the slice CRC32's together for the final CRC32.
//   - Write header to the byte stream.
//   - Write each blob to the byte stream.
//   - Write the trailer to the byte stream.
//   - We then return each []byte and gzip.Writer to the pool for later reuse.
//
// - The blob writer takes a list of gzip compressed blobs, and writes to an io.WriteCloser?
//   - This allows writing a "snapshot" of blobs, with minimal effort. The queue management is
//     separate then.

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

// GzipStreamWriter is a GZIP writer that can write multiple compressed gzip blobs to the same output stream.
type GzipStreamWriter struct {
	gzip.Header // written at first call to Write, Flush, or Close
	w           io.Writer
	compressor  *flate.Writer
	level       int
	err         error
	digest      uint32
	size        uint32

	// The stateFlags bitfield tracks
	// 0: Have we written the Gzip header yet?
	// 1: Has the stream been closed yet?
	// 2: Are we writing into the DEFLATE stream currently? (Negated when we write compressed blobs.)
	stateFlags uint32 // 0x1: wroteHeader, 0x2: closed, 0x4: activeDeflateStream
}

// NewGzipStreamWriter creates a new GzipStreamWriter with the default compression level.
func NewGzipStreamWriter(w io.Writer) *GzipStreamWriter {
	z, _ := NewGzipStreamWriterLevel(w, DefaultCompression)
	return z
}

// NewGzipStreamWriterLevel creates a new GzipStreamWriter with the specified compression level.
func NewGzipStreamWriterLevel(w io.Writer, level int) (*GzipStreamWriter, error) {
	if level < HuffmanOnly || level > BestCompression {
		return nil, fmt.Errorf("%w: %d", ErrInvalidCompressionLevel, level)
	}
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

func (z *GzipStreamWriter) setActiveDeflateStream(value bool) {
	flag := uint32(0)
	if value {
		flag = 1
	}
	z.stateFlags = (z.stateFlags & ^uint32(0x0004)) | (flag << 2)
}

func (z *GzipStreamWriter) checkWroteHeader() bool {
	flag := z.stateFlags & 0x1
	return flag == 1
}

func (z *GzipStreamWriter) checkClosed() bool {
	flag := (z.stateFlags & 0x2) >> 1
	return flag == 1
}

func (z *GzipStreamWriter) checkActiveDeflateStream() bool {
	flag := (z.stateFlags & 0x4) >> 2
	return flag == 1
}

func (z *GzipStreamWriter) writeHeader() (int, error) {
	// Write the GZIP header lazily.
	var n int
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
	// Note: Some libraries like github.com/klauspost/compress/gzip choose to
	// always write this field, which causes slight differences in header bytes
	// versus the stdlib gzip implementation.
	// Since this is a one-time cost for each GZIP stream, we go with the
	// stdlib approach for sake of compatibility.
	if z.ModTime.After(time.Unix(0, 0)) {
		// Section 2.3.1, the zero value for MTIME means that the
		// modified time is not set.
		binary.LittleEndian.PutUint32(buf[4:8], uint32(z.ModTime.Unix()))
	}
	switch z.level {
	case BestCompression:
		buf[8] = 2
	case BestSpeed:
		buf[8] = 4
	default:
		buf[8] = 0
	}
	buf[9] = z.OS
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
		return ErrHdrExtaDataTooLarge
	}
	var lengthPrefix [2]byte
	binary.LittleEndian.PutUint16(lengthPrefix[:2], uint16(len(b)))
	if _, err := z.w.Write(lengthPrefix[:2]); err != nil {
		return fmt.Errorf("gzip: failed to write length prefix: %w", err)
	}
	if _, err := z.w.Write(b); err != nil {
		return fmt.Errorf("gzip: failed to write bytes: %w", err)
	}
	return nil
}

// writeHeaderString writes a UTF-8 string s in GZIP's format to z.w.
// GZIP (RFC 1952) specifies that strings are NUL-terminated ISO 8859-1 (Latin-1).
func (z *GzipStreamWriter) writeHeaderString(s string) error {
	var err error
	// GZIP stores Latin-1 strings; error if non-Latin-1; convert if non-ASCII.
	needconv := false
	for _, v := range s {
		if v == 0 || v > 0xff {
			return ErrHdrNonLatin1
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
		return fmt.Errorf("gzip: failed to write header string: %w", err)
	}
	// GZIP strings are NUL-terminated.
	_, err = z.w.Write([]byte{0})
	if err != nil {
		return fmt.Errorf("gzip: failed to write null terminator for header string: %w", err)
	}
	return nil
}

// Write writes the byte slice to the Gzip output stream.
// This will trigger a Flush call on the underlying compressor, emitting a sync marker at a minimum.
func (z *GzipStreamWriter) Write(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}

	var n int
	if !z.checkWroteHeader() {
		if n, z.err = z.writeHeader(); z.err != nil {
			return n, z.err
		}
	}

	z.size += uint32(len(p))
	z.digest = crc32.Update(z.digest, crc32.IEEETable, p)

	z.setActiveDeflateStream(true)
	if n, z.err = z.compressor.Write(p); z.err != nil {
		return n, z.err
	}
	// Note: No forced flush here, we flush lazily instead.
	// z.err = z.compressor.Flush()
	return n, z.err
}

// WriteCompressed writes a compressed gzip byte blob through to the underlying writer.
func (z *GzipStreamWriter) WriteCompressed(p []byte) (int, error) {
	if z.err != nil {
		return 0, z.err
	}

	var n int
	if n, z.err = z.writeHeader(); z.err != nil {
		return n, z.err
	}

	// Flush the current deflate stream, if one was active.
	if z.checkActiveDeflateStream() {
		if z.err = z.compressor.Flush(); z.err != nil {
			return n, z.err
		}
		z.setActiveDeflateStream(false)
	}

	// Not a compliant Gzip blob. We can reject this up front.
	// This assumes header: 10 bytes, trailer: 8 bytes.
	if len(p) < 18 {
		return n, ErrBlob
	}
	trailerChecksum := binary.LittleEndian.Uint32(p[(len(p) - 8):(len(p) - 4)])
	trailerLength := binary.LittleEndian.Uint32(p[(len(p) - 4):])
	content, ok := getDeflateSlice(p)
	if !ok {
		return n, ErrBlob
	}

	z.size += trailerLength // uint32(len(p))

	z.digest = crc32Combine(z.digest, trailerChecksum, int(trailerLength))
	n, z.err = z.w.Write(content)

	// We would flush if we could here, but z.w is an io.Writer, and those do
	// not have to implement Flush().
	return n, z.err
}

// Combine 2x CRC32 checksums into a single checksum, using the XOR method.
func crc32Combine(front, back uint32, length int) uint32 {
	zeroes := make([]byte, length) // HACK: Naive version.
	// This is magic, but based on what I've been able to discern, it looks like
	// you have to do some extra XORs to get the "front" into a form that can be
	// XOR'd with the "back" checksum.
	front = crc32.Update(0xffffffff^front, crc32.IEEETable, zeroes) ^ 0xffffffff
	return front ^ back // crc32.Update(front, crc32.IEEETable, zeroes) ^ back
}

// Returns: updated slice + ok status.
// Returns false when not a valid gzip header.
func getDeflateSlice(gzblob []byte) ([]byte, bool) {
	headerLength := getHeaderLength(gzblob)
	if headerLength < 0 {
		return nil, false
	}

	// Safety.
	if len(gzblob) < (headerLength + 8) {
		return nil, false
	}

	return gzblob[headerLength:(len(gzblob) - 8)], true
}

// Walks the state machine for determining header length, without messing around with setting state.
// Returns a negative value on error? (Could do a boolean just as easily.)
func getHeaderLength(gzBlob []byte) int {
	headerLen := 10
	if len(gzBlob) < headerLen {
		return -1
	}

	// Valid header start bytes check.
	if gzBlob[0] != gzipID1 || gzBlob[1] != gzipID2 || gzBlob[2] != gzipDeflate {
		return -1
	}

	flag := gzBlob[3]
	// Scan over the "Extra" field, which is length-prefixed.
	if flag&flagExtra != 0 {
		// Safety
		headerLen += 2
		if len(gzBlob) < headerLen {
			return -1
		}
		extraFieldLength := binary.LittleEndian.Uint16(gzBlob[10:12])
		// Safety
		headerLen += int(extraFieldLength)
		if len(gzBlob) < headerLen {
			return -1
		}
	}
	// Scan over Name and Comment fields, which are zero-terminated.
	if flag&flagName != 0 {
		endField := bytes.IndexByte(gzBlob[headerLen:], byte(0))
		if endField < 0 {
			return -1 // Safety
		}
		// Safety
		headerLen += endField
		if len(gzBlob) < headerLen {
			return -1
		}
	}
	if flag&flagComment != 0 {
		endField := bytes.IndexByte(gzBlob[headerLen:], byte(0))
		if endField < 0 {
			return -1 // Safety
		}
		// Safety
		headerLen += endField
		if len(gzBlob) < headerLen {
			return -1
		}
	}

	// Scan over the Header CRC field.
	if flag&flagHdrCrc != 0 {
		// Safety
		headerLen += 2
		if len(gzBlob) < headerLen {
			return -1
		}
	}

	return headerLen
}

// func (z *GzipStreamWriter) WriteTo(w io.Writer) (n int64, err error)

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
		_, _ = z.Write(nil)
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
	z.setActiveDeflateStream(false)
	return z.err
}

// Reset resets the GzipStreamWriter's compressor and other internal state, and changes the output destination to the provided io.Writer.
func (z *GzipStreamWriter) Reset(w io.Writer) {
	z.init(w, z.level)
	z.setClosed(false)
	z.setWroteHeader(false)
	z.setActiveDeflateStream(false)
}

// Assertions for checking that we implemented the interfaces.
// The compiler will optimize all of these away.
var (
	_ io.Writer      = (*GzipStreamWriter)(nil)
	_ io.Closer      = (*GzipStreamWriter)(nil)
	_ io.WriteCloser = (*GzipStreamWriter)(nil)
	// _ io.WriterTo = (*GzipStreamWriter)(nil)
	_ CompressedBlobWriter = (*GzipStreamWriter)(nil)
)

// Everything from here down is inherited from the source:
