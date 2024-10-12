package gzipstreamwriter

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
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
		tc := tc
		t.Run(tc.note, func(t *testing.T) {
			t.Parallel()
			expBuffer := bytes.Buffer{}
			actBuffer := bytes.Buffer{}

			expGzipWriter := testGzipWriterPool.Get().(*gzip.Writer)
			defer expGzipWriter.Close()
			defer testGzipWriterPool.Put(expGzipWriter)
			expGzipWriter.Reset(&expBuffer)

			// TODO: Replace with the real GzipStreamWriter.
			actGzipWriter := gzip.NewWriter(&actBuffer)

			// Write input through both writers, check for errors.
			expWroteBytes, expErr := expGzipWriter.Write(tc.input)
			actWroteBytes, actErr := actGzipWriter.Write(tc.input)

			if expWroteBytes != actWroteBytes {
				t.Fatalf("expected %d bytes written, got %d bytes", expWroteBytes, actWroteBytes)
			}
			if !errors.Is(actErr, expErr) {
				t.Errorf("expected error %v, got %v", expErr, actErr)
			}

			expErr = expGzipWriter.Close()
			actErr = actGzipWriter.Close()

			if !errors.Is(actErr, expErr) {
				t.Errorf("expected error %v, got %v", expErr, actErr)
			}

			// Compare decompressed contents for equality.
			gzReader := testGzipReaderPool.Get().(*gzip.Reader)
			defer gzReader.Close()
			defer testGzipReaderPool.Put(gzReader)

			var expResult []byte
			var actResult []byte
			var err error

			if err := gzReader.Reset(&expBuffer); err != nil {
				t.Fatal(err)
			}
			expResult, err = io.ReadAll(gzReader)
			if err != nil {
				t.Fatal(err)
			}

			if err := gzReader.Reset(&actBuffer); err != nil {
				t.Fatal(err)
			}
			actResult, err = io.ReadAll(gzReader)
			if err != nil {
				t.Fatal(err)
			}

			if slices.Compare(expResult, actResult) != 0 {
				t.Fatalf("expected %v, got %v", expResult, actResult)
			}
		})
	}
}

func TestStringWrite(t *testing.T) {
	// TODO
}
