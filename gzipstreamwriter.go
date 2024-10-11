// Copyright 2024, Philip Conrad.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package gzipstreamwriter

import (
	"compress/flate"
	"compress/gzip"
)

const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.DefaultCompression
	HuffmanOnly        = flate.HuffmanOnly
)

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
	Header     gzip.Header // written at first call to Write, Flush, or Close
	compressor *flate.Writer
	err        error
	digest     uint32
	size       uint32
	header     [10]byte
}

func NewGzipBlobStream() {}

// func (z *GzipStreamWriter) Write(p []byte) (n int, err error) {
// 	z.length += uint32(len(p))
// 	z.digest = crc32.Update(z.digest, crc32.IEEETable, p)
// 	n, z.err = z.compressor.Write(p)
// }
// func (z *GzipStreamWriter) WriteTo(w io.Writer) (n int64, err error)
// func (z *GzipStreamWriter) WriteByte(c byte) error
// func (z *GzipStreamWriter) WriteRune(c rune) error
// func (z *GzipStreamWriter) WriteString(s string) (n int, err error)

// // Writes a compressed gzip byte blob through to the underlying writer.
// func (z *GzipStreamWriter) WriteCompressed(p []byte) (n int, err error) {
// 	z.length += uint32(len(p))
// 	z.digest = crc32.Update(z.digest, crc32.IEEETable, p)
// 	n, z.err = z.compressor.Write(p)
// }

// Close closes the [Writer] by flushing any unwritten data to the underlying
// [io.Writer] and writing the GZIP footer.
// It does not close the underlying [io.Writer].
func (z *GzipStreamWriter) Close() error {
	return nil
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
	return nil
}

func (z *GzipStreamWriter) Reset() error {
	return nil
}

// func (g *GzipStreamWriter)

// Assertions for checking that we implemented the interfaces.
// The compiler will optimize all of these away.
// var (
// 	_ io.Writer            = (*GzipStreamWriter)(nil)
// 	_ io.WriterTo          = (*GzipStreamWriter)(nil)
// 	_ io.ByteWriter        = (*GzipStreamWriter)(nil)
// 	_ io.StringWriter      = (*GzipStreamWriter)(nil)
// 	_ CompressedBlobWriter = (*GzipStreamWriter)(nil)
// 	_ io.Closer            = (*GzipStreamWriter)(nil)
// 	_ io.WriteCloser       = (*GzipStreamWriter)(nil)
// )

// Everything from here down is inherited from the source:
