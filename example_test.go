package ioseq_test

import (
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/rogpeppe/ioseq"
)

func ExampleReaderFromSeq() {
	// The base64.NewEncoder API returns a io.Writer,
	// making it inconvenient to use on APIs that require a Reader.
	// Demonstrate how we'd use ReaderFromSeq to work around
	// that limitation without using io.Pipe.
	seq := func(yield func([]byte, error) bool) {
		w := base64.NewEncoder(base64.StdEncoding, ioseq.SeqWriter(yield))
		defer w.Close()
		fmt.Fprintf(w, "hello, world\n")
	}
	r := ioseq.ReaderFromSeq(seq)
	defer r.Close()
	io.Copy(os.Stdout, r)
	r.Close()

	// Output:
	// aGVsbG8sIHdvcmxkCg==
}

func ExampleWriterFuncToSeq() {
	compress := ioseq.WriterFuncToSeq(func(w io.Writer) io.WriteCloser {
		return gzip.NewWriter(w)
	})
	zeros := func(yield func([]byte, error) bool) {
		buf := make([]byte, 8192)
		for i := 0; i < 1000; i++ {
			if !yield(buf, nil) {
				return
			}
		}
	}
	n := 0
	for data, err := range compress(zeros) {
		if err != nil {
			panic(err)
		}
		n += len(data)
	}
	fmt.Println(n)
	// Output:
	// 7988
}
