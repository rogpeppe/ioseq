package ioseq

import (
	"fmt"
	"io"
	"slices"
	"strings"
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

var seqFromReaderTests = []struct {
	testName string
	in       func() io.Reader
	want     []string
	wantErr  string
}{{
	testName: "OneByte",
	in: func() io.Reader {
		return iotest.OneByteReader(strings.NewReader("foo bar"))
	},

	want: strings.Split("foo bar", ""),
}, {
	testName: "DataErrEOF",
	in: func() io.Reader {
		return iotest.DataErrReader(strings.NewReader("foo bar"))
	},
	want: []string{"foo bar"},
}, {
	testName: "NonEOFError",
	in: func() io.Reader {
		return io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(fmt.Errorf("some error")))
	},
	want:    []string{"foo"},
	wantErr: "some error",
}, {
	testName: "NonDataErrNonEOF",
	in: func() io.Reader {
		return iotest.DataErrReader(
			io.MultiReader(strings.NewReader("foo"), iotest.ErrReader(fmt.Errorf("some error"))),
		)
	},
	want:    []string{"foo"},
	wantErr: "some error",
}}

func TestSeqFromReader(t *testing.T) {
	for _, test := range seqFromReaderTests {
		t.Run(test.testName, func(t *testing.T) {
			var got []string
			var gotErr error
			for data, err := range SeqFromReader(test.in(), 32*1024) {
				if err != nil {
					if gotErr != nil {
						t.Fatalf("got two error values (%v then %v)", gotErr, err)
					}
					gotErr = err
					continue
				}
				got = append(got, string(data))
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("unexpected results;\ngot %q\nwant %q", got, test.want)
			}
			if test.wantErr != "" {
				if gotErr == nil {
					t.Errorf("expected error %q but got none", test.wantErr)
				}
				if got, want := gotErr.Error(), test.wantErr; got != want {
					t.Errorf("unexpected error value; got %q want %q", got, want)
				}
			} else if gotErr != nil {
				t.Errorf("unexpected error: %v", gotErr)
			}
		})
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

func TestSeqWriterWillNotCallYieldAfterTermination(t *testing.T) {
	seq := func(yield func([]byte, error) bool) {
		w := SeqWriter(yield)
		w.Write([]byte("one"))
		// This will panic if the writer does not respect the yield result.
		w.Write([]byte("two"))
	}
	for range seq {
		break
	}
}

func TestSeqWriterClipsSlice(t *testing.T) {
	seq := func(yield func([]byte, error) bool) {
		buf := []byte("foobar")
		w := SeqWriter(yield)
		w.Write(buf[:3])
		if got, want := string(buf), "foobar"; got != want {
			t.Fatalf("slice was not clipped; want %q got %q", got, want)
		}
	}
	for data := range seq {
		_ = append(data, 'X')
	}
}
