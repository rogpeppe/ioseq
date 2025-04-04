package ioseq

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"
)

// perflock go test -bench . -count 10 > /tmp/b
//  benchstat -col=/kind -row .name /tmp/b

func BenchmarkPipeBase64(b *testing.B) {
	benchmarkPipe(b, func(w io.Writer) io.WriteCloser {
		return base64.NewEncoder(base64.StdEncoding, w)
	})
}

func BenchmarkPipeNoop(b *testing.B) {
	benchmarkPipe(b, func(w io.Writer) io.WriteCloser {
		return nopCloser{w}
	})
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}

func benchmarkPipe(b *testing.B, f func(w io.Writer) io.WriteCloser) {
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		r := ReaderFromSeq(func(yield func([]byte, error) bool) {
			w := f(SeqWriter(yield))
			defer w.Close()
			buf := make([]byte, 8192)
			for b.Loop() {
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
			for b.Loop() {
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
	benchmarkReader(b, func([]byte) {}, func([]byte) {})
}

func BenchmarkReaderFillIndex(b *testing.B) {
	benchmarkReader(b, func(data []byte) {
		for i := range data {
			data[i] = 'x'
		}
	}, func(data []byte) {
		bytes.IndexByte(data, 'y')
	})
}

func benchmarkReader(b *testing.B, produceWork, consumeWork func([]byte)) {
	b.Run("kind=new", func(b *testing.B) {
		b.SetBytes(8192)
		seq := func(yield func([]byte, error) bool) {
			buf := make([]byte, 8192)
			for b.Loop() {
				produceWork(buf)
				if !yield(buf, nil) {
					return
				}
			}
		}
		for data, err := range seq {
			if err != nil {
				b.Fatal(err)
			}
			consumeWork(data)
		}
	})
	b.Run("kind=old", func(b *testing.B) {
		b.SetBytes(8192)
		r := workReader{
			work: produceWork,
			n:    b.N,
		}
		buf := make([]byte, 8192)
		for {
			n, err := r.Read(buf)
			if err != nil {
				return
			}
			consumeWork(buf[:n])
		}
	})
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
