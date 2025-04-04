package ioseq_test

import (
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
