# GzipStreamWriter

*Ever wanted to merge gzipped `[]byte` blobs together without decompressing first? Now you can.*

This project exists to solve a very specific problem: efficiently concatenating gzip blobs together, as if they'd all been written as a single stream. This is trickier than it sounds, because we *don't* want to decompress the gzipped blobs while writing them!

## Design Goals:
 - Allow writing either raw bytes or compressed gzip blobs to a destination, resulting in a valid, concatenated gzip blob at the destination, as if the blob had been written "all at once" as a single gzip byte stream.
 - Avoid decompressing the compressed blobs.
 - Avoid excessive memory and CPU burn if possible.
 - Avoid exposing the awful complex parts to happy-path users. Still provide some support scaffolding for hard-mode folks.

## Design Anti-Goals:
 - Best possible compression performance: We know that some usage patterns can result in a less-than-optimal overall compression ratio.

## Algorithm

For writing compressed blobs:
 - Drop the header.
 - Extract CRC32 and uncompressed length fields from the trailer, and drop the trailer.
 - Write the blob to the stream.
 - Update the running CRC32 by the XOR trick from zlib.
 - Update the length field using the trailer length field.

For uncompressed `[]byte` writes:
 - Update the length field using `len(slice)`.
 - Compress into a gzip blob.
 - Extract the CRC32 from the trailer.
 - Drop header and trailer.
 - Write the blob to the stream.

This gives us a powerful abstraction that "does the right thing" behind the scenes, while being ridiculously cheaper to compute than decompressing and recompressing compressed gzip data.

## Go Version Support

I'm currently supporting the latest Go major version.
Previously, I'd aimed to support the current major version - 2, but there's a world of performance and features availble on Go 1.24+, and I'd like to have access to those things.
The larger open source project that this library was originally developed for has caught up to at least Go 1.23+, so this library should be usable for them soon.

