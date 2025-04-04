package ioseq

import (
	"io"
	"testing"
	"testing/iotest"
)

func TestReaderFromSeqEarlyClose(t *testing.T) {
	var closeCalled bool

	input := func(yield func([]byte, error) bool) {
		for range 10 {
			if !yield([]byte("hello world"), nil) {
				break
			}
		}
		closeCalled = true
	}
	r := ReaderFromSeq(input)
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("unexpected read count %d", n)
	}
	if string(buf) != "hello" {
		t.Fatalf("unexpected content %q", buf)
	}
	if closeCalled {
		t.Fatalf("close called early")
	}
	r.Close()
	if !closeCalled {
		t.Fatalf("close not called")
	}
}

func TestReaderFromSeq(t *testing.T) {
	input := func(yield func([]byte, error) bool) {
		for _, data := range []string{
			"foo",
			"bar",
			"\n",
			"",
			"other",
		} {
			if !yield([]byte(data), nil) {
				return
			}
		}
	}
	data, err := io.ReadAll(iotest.OneByteReader(ReaderFromSeq(input)))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "foobar\nother"; got != want {
		t.Fatalf("unexpected result; got %q want %q", got, want)
	}
	data, err = io.ReadAll(ReaderFromSeq(input))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "foobar\nother"; got != want {
		t.Fatalf("unexpected result; got %q want %q", got, want)
	}

	r := ReaderFromSeq(input)
	n, err := r.Read(make([]byte, 10))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := n, 3; got != want {
		t.Fatalf("unexpected count; got %d want %d", got, want)
	}
	r.Close()
}
