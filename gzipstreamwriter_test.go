package gzipstreamwriter_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/philipaconrad/gzipstreamwriter"
)

var testGzipWriterPool = sync.Pool{
	New: func() any {
		writer := gzip.NewWriter(io.Discard)
		return writer
	},
}

var testGzipReaderPool = sync.Pool{
	New: func() any {
		reader := new(gzip.Reader)
		return reader
	},
}

func TestClose(t *testing.T) {
	t.Parallel()

	t.Run("closing writer is same as stdlib", func(t *testing.T) {
		t.Parallel()

		expBuffer := bytes.Buffer{}
		actBuffer := bytes.Buffer{}

		expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
		if !ok {
			t.Fatal("Could not get *gzip.Writer instance from the pool.")
		}
		defer expGzipWriter.Close() //nolint:errcheck
		defer testGzipWriterPool.Put(expGzipWriter)
		expGzipWriter.Reset(&expBuffer)
		_ = expGzipWriter.Close()

		actGzipWriter := gzipstreamwriter.NewGzipStreamWriter(&actBuffer)
		if err := actGzipWriter.Close(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestClose() single-close mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("double-close is same as stdlib", func(t *testing.T) {
		t.Parallel()

		expBuffer := bytes.Buffer{}
		actBuffer := bytes.Buffer{}

		expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
		if !ok {
			t.Fatal("Could not get *gzip.Writer instance from the pool.")
		}
		defer expGzipWriter.Close() //nolint:errcheck
		defer testGzipWriterPool.Put(expGzipWriter)
		expGzipWriter.Reset(&expBuffer)
		_ = expGzipWriter.Close()
		_ = expGzipWriter.Close()

		actGzipWriter := gzipstreamwriter.NewGzipStreamWriter(&actBuffer)
		_ = actGzipWriter.Close()
		_ = actGzipWriter.Close()

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestClose() double-close mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestFlush(t *testing.T) {
	t.Parallel()

	t.Run("flushing writer is same as stdlib", func(t *testing.T) {
		t.Parallel()

		expBuffer := bytes.Buffer{}
		actBuffer := bytes.Buffer{}

		expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
		if !ok {
			t.Fatal("Could not get *gzip.Writer instance from the pool.")
		}
		defer expGzipWriter.Close() //nolint:errcheck
		defer testGzipWriterPool.Put(expGzipWriter)
		expGzipWriter.Reset(&expBuffer)
		_ = expGzipWriter.Flush()

		actGzipWriter := gzipstreamwriter.NewGzipStreamWriter(&actBuffer)
		_ = actGzipWriter.Flush()

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestFlush() single-flush mismatch (-want +got):\n%s", diff)
		}

		// Close writers and ensure the trailers match.
		_ = expGzipWriter.Close()
		_ = actGzipWriter.Close()

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestFlush() single-flush mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("double-flush is same as stdlib", func(t *testing.T) {
		t.Parallel()

		expBuffer := bytes.Buffer{}
		actBuffer := bytes.Buffer{}

		expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
		if !ok {
			t.Fatal("Could not get *gzip.Writer instance from the pool.")
		}
		defer expGzipWriter.Close() //nolint:errcheck
		defer testGzipWriterPool.Put(expGzipWriter)
		expGzipWriter.Reset(&expBuffer)
		_ = expGzipWriter.Flush()
		_ = expGzipWriter.Flush()

		actGzipWriter := gzipstreamwriter.NewGzipStreamWriter(&actBuffer)
		_ = actGzipWriter.Flush()
		_ = actGzipWriter.Flush()

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestFlush() double-flush mismatch (-want +got):\n%s", diff)
		}

		// Close writers and ensure the trailers match.
		_ = expGzipWriter.Close()
		_ = actGzipWriter.Close()

		if diff := cmp.Diff(expBuffer.Bytes(), actBuffer.Bytes()); diff != "" {
			t.Fatalf("TestFlush() double-flush mismatch (-want +got):\n%s", diff)
		}
	})
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
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()

			expBuffer := bytes.Buffer{}
			actBuffer := bytes.Buffer{}

			expGzipWriter, ok := testGzipWriterPool.Get().(*gzip.Writer)
			if !ok {
				t.Fatal("Could not get *gzip.Writer instance from the pool.")
			}
			defer expGzipWriter.Close() //nolint:errcheck
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
			defer gzReader.Close() //nolint:errcheck
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
