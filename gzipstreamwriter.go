package gzipstreamwriter

import "io"

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
type GzipBlobStream struct {
	buffers [][]byte
	writer  io.WriteCloser
	digest  uint32
	length  uint32
}

func NewGzipBlobStream(dest io.WriteCloser, source [][]byte) *GzipBlobStream {
	return &GzipBlobStream{buffers: source, writer: dest}
}

// Writes all blobs to the io.WriteCloser. Returns any errors.
func (g *GzipBlobStream) Flush() error {
}

func (g *GzipBlobStream) Reset(dest io.WriteCloser, source [][]byte) {
	g.buffers = source
	g.writer = dest
	g.digest = 0
}

// Appends a blob to the buffers list.
func (g *GzipBlobStream) Write(bs []byte) (n int, err error) {
}

// Flushes all available data to the output. Writes the accumulated trailer to the output.
func (g *GzipBlobStream) Close() error {
}
