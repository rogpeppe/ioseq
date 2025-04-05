package ioseq

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// perflock go test -bench . -count 10 > /tmp/b
// # benchstat -filter '.name:/ReaderVsSeqFromReader/' -col=/kind -row .name /tmp/b
// benchstat '-col=/kind@(newNoWriterTo new)' -row .name /tmp/b

func BenchmarkPipeBase64(b *testing.B) {
	benchmarkPipe(b, newBase64Encoder)
}

func BenchmarkPipeNoop(b *testing.B) {
	benchmarkPipe(b, func(w io.Writer) io.WriteCloser {
		return nopCloser{w}
	})
}

func benchmarkPipe(b *testing.B, f func(w io.Writer) io.WriteCloser) {
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		r := ReaderFromSeq(func(yield func([]byte, error) bool) {
			w := f(SeqWriter(yield))
			defer w.Close()
			buf := make([]byte, 8192)
			for range b.N {
				if _, err := w.Write(buf); err != nil {
					b.Fatal(err)
				}
			}
		})
		defer r.Close()
		_, err := io.Copy(io.Discard, r)
		if err != nil {
			b.Fatal(err)
		}
	})
	b.Run("kind=old", func(b *testing.B) {
		b.SetBytes(8192)
		pr, pw := io.Pipe()
		w := f(pw)
		go func() {
			defer pw.Close()
			defer w.Close()
			buf := make([]byte, 8192)
			for range b.N {
				if _, err := w.Write(buf); err != nil {
					b.Fatal(err)
				}
			}
		}()
		_, err := io.Copy(io.Discard, pr)
		if err != nil {
			b.Fatal(err)
		}
	})
}

func BenchmarkReaderNoop(b *testing.B) {
	benchmarkReader(b, noop, noop)
}

func BenchmarkReaderFillIndex(b *testing.B) {
	benchmarkReader(b, fill, index)
}

func benchmarkReader(b *testing.B, produceWork, consumeWork func([]byte)) {
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		for data, err := range produceAndWork(b, produceWork) {
			if err != nil {
				b.Fatal(err)
			}
			consumeWork(data)
		}
	})
	b.Run("kind=old", func(b *testing.B) {
		b.SetBytes(8192)
		r := &workReader{
			work: produceWork,
			n:    b.N,
		}
		readAllAndWork(r, consumeWork)
	})
}

func BenchmarkReaderVsSeqFromReaderNoop(b *testing.B) {
	benchmarkReaderVsSeqFromReader(b, noop, noop)
}

func BenchmarkReaderVsSeqFromReaderFillIndex(b *testing.B) {
	benchmarkReaderVsSeqFromReader(b, fill, index)
}

func benchmarkReaderVsSeqFromReader(b *testing.B, produceWork, consumeWork func([]byte)) {
	b.Run("kind=old", func(b *testing.B) {
		b.SetBytes(8192)
		r := &workReader{
			work: produceWork,
			n:    b.N,
		}
		readAllAndWork(r, consumeWork)
	})
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		r := ReaderFromSeq(produceAndWork(b, produceWork))
		defer r.Close()
		readAllAndWork(r, consumeWork)
	})
}

func BenchmarkSeqFromWriterFuncBase64(b *testing.B) {
	f := SeqFromWriterFunc(newBase64Encoder)
	b.SetBytes(8192)
	buf := make([]byte, 8192)
	for range f(func(yield func([]byte, error) bool) {
		for range b.N {
			yield(buf, nil)
		}
	}) {
	}
}

func BenchmarkBase64HTTPPost(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
	}))
	defer srv.Close()
	b.Run("kind=old", func(b *testing.B) {
		b.SetBytes(8192)
		in := io.LimitReader(unlimited{}, int64(b.N*8192))
		pr, pw := io.Pipe()
		go func() {
			defer pw.Close()
			w := newBase64Encoder(pw)
			io.Copy(w, in)
		}()
		_, err := http.Post(srv.URL, "application/text", pr)
		if err != nil {
			b.Fatal(err)
		}
	})
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		in := io.LimitReader(unlimited{}, int64(b.N*8192))
		_, err := http.Post(srv.URL, "application/gzip", PipeThrough(in, newBase64Encoder, 8192))
		if err != nil {
			b.Fatal(err)
		}
	})
	b.Run("kind=newNoWriterTo", func(b *testing.B) {
		b.SetBytes(8192)
		in := io.LimitReader(unlimited{}, int64(b.N*8192))
		_, err := http.Post(srv.URL, "application/gzip", noWriterTo{PipeThrough(in, newBase64Encoder, 8192)})
		if err != nil {
			b.Fatal(err)
		}
	})
}

type noWriterTo struct{ io.Reader }

func nopEncoder(w io.Writer) io.WriteCloser { return nopCloser{w} }

type unlimited struct{}

func (unlimited) Read(buf []byte) (int, error) {
	fill(buf)
	return len(buf), nil
}

func readAllAndWork(r io.Reader, work func([]byte)) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return
		}
		work(buf[:n])
	}
}

func produceAndWork(b *testing.B, work func([]byte)) Seq {
	return func(yield func([]byte, error) bool) {
		buf := make([]byte, 8192)
		for range b.N {
			work(buf)
			if !yield(buf, nil) {
				return
			}
		}
	}
}

type workReader struct {
	work func([]byte)
	i    int
	n    int
}

func (r *workReader) Read(buf []byte) (int, error) {
	if r.i >= r.n {
		return 0, io.EOF
	}
	r.work(buf)
	r.i++
	return len(buf), nil
}

func noop(data []byte) {}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}

func index(data []byte) {
	bytes.IndexByte(data, 'y')
}

func newBase64Encoder(w io.Writer) io.WriteCloser {
	return base64.NewEncoder(base64.StdEncoding, w)
}

func fill(b []byte) {
	fillByte(b, 'x')
}

func fillByte(b []byte, value byte) {
	if len(b) == 0 {
		return
	}

	b[0] = value
	for i := 1; i < len(b); i *= 2 {
		copy(b[i:], b[:i])
	}
}
