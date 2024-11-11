package gzipstreamwriter_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/philipaconrad/gzipstreamwriter"
)

var testGzipWriterPool = sync.Pool{
	New: func() interface{} {
		writer := gzip.NewWriter(io.Discard)
		return writer
	},
}

var testGzipReaderPool = sync.Pool{
	New: func() interface{} {
		reader := new(gzip.Reader)
		return reader
	},
}

func TestWrite(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		note  string
		input []byte
	}{
		{
			note:  "nil input",
			input: nil,
		},
		{
			note:  "single byte",
			input: []byte("A"),
		},
		{
			note:  "many repeated bytes",
			input: bytes.Repeat([]byte("A"), 1000),
		},
	}

	for _, tc := range testcases {
		tc := tc // loop var copy. Not needed in Go 1.22+
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()

			expBuffer := bytes.Buffer{}
			actBuffer := bytes.Buffer{}

			expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
			if !ok {
				t.Fatal("Could not get *gzip.Writer instance from the pool.")
			}
			defer expGzipWriter.Close()
			defer testGzipWriterPool.Put(expGzipWriter)
			expGzipWriter.Reset(&expBuffer)
			actGzipWriter := gzipstreamwriter.NewGzipStreamWriter(&actBuffer)

			// Write input through both writers, check for errors.
			expWroteBytes, expErr := writeToBuffer(t, expGzipWriter, tc.input)
			actWroteBytes, actErr := writeToBuffer(t, actGzipWriter, tc.input)

			if expWroteBytes != actWroteBytes {
				t.Fatalf("expected %d bytes written, got %d bytes", expWroteBytes, actWroteBytes)
			}
			if !errors.Is(actErr, expErr) {
				t.Errorf("expected error %v, got %v", expErr, actErr)
			}

			// Compare decompressed contents for equality.
			gzReader, ok := testGzipReaderPool.Get().(*gzip.Reader)
			if !ok {
				t.Fatal("Could not get *gzip.Reader instance from the pool.")
			}
			defer gzReader.Close()
			defer testGzipReaderPool.Put(gzReader)

			var expResult []byte
			var actResult []byte
			var err error
			if expResult, err = decompressGzipBuffer(t, gzReader, &expBuffer); err != nil {
				t.Fatal(err)
			}
			if actResult, err = decompressGzipBuffer(t, gzReader, &actBuffer); err != nil {
				t.Fatal(err)
			}

			if slices.Compare(expResult, actResult) != 0 {
				t.Fatalf("expected %v, got %v", expResult, actResult)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func writeToBuffer(t *testing.T, gzWriter io.WriteCloser, data []byte) (int, error) {
	t.Helper()
	var n int
	var err error
	if n, err = gzWriter.Write(data); err != nil {
		return n, err
	}
	if err := gzWriter.Close(); err != nil {
		return n, err
	}
	return n, nil
}

func decompressGzipBuffer(t *testing.T, gzReader *gzip.Reader, buffer *bytes.Buffer) ([]byte, error) {
	t.Helper()
	if err := gzReader.Reset(buffer); err != nil {
		return nil, err
	}
	return io.ReadAll(gzReader)
}
