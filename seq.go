package ioseq

import (
	"errors"
	"io"
	"iter"
	"slices"
)

// Seq represents a sequence of byte slices. It's somewhat equivalent to
// [Reader], although simpler in some respects.
// See [SeqFromReader] and [ReaderFromSeq] for a way to convert
// between [Seq] and [Reader].
//
// Each element in the sequence must have either a non-nil byte slice or
// a non-nil error; a producer should never produce either (nil, nil) or
// a non-nil slice and a non-nil error.
//
// The sequence always ends at the first error: if there are temporary
// errors, it's up to the producer to deal with them.
//
// The code ranging over the sequence must not use the slice outside of
// the loop or across iterations; that is, the receiver owns a slice
// until that particular iteration ends.
//
// Callers must not mutate the slice. [TODO perhaps it might be OK to
// allow callers to mutate, but not append to, the slice].
type Seq = iter.Seq2[[]byte, error]

// SeqFromReader returns a [Seq] that reads from r, allocating one buffer
// of the given size to do so unless r implements [WriterTo], in which
// case no buffer is needed.
func SeqFromReader(r io.Reader, bufSize int) Seq {
	if r, ok := r.(io.WriterTo); ok {
		return func(yield func([]byte, error) bool) {
			active := true
			_, err := r.WriteTo(writerFunc(func(data []byte) (int, error) {
				if !yield(data, nil) {
					active = false
					return 0, ErrSequenceTerminated
				}
				return len(data), nil
			}))
			if err != nil && active {
				yield(nil, err)
			}
		}
	}
	return func(yield func([]byte, error) bool) {
		buf := make([]byte, bufSize)
		for {
			n, err := r.Read(buf)
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				// Note: we _could_ call slices.Clip on the buffer
				// here, but there's no particular reason to do so:
				// if the rest of the buffer is overwritten by the
				// consumer, it doesn't make any difference.
				if n > 0 && !yield(buf[:n], nil) {
					return
				}
				if err != nil {
					yield(nil, err)
				}
				return
			}
			if n > 0 && !yield(buf[:n], nil) {
				return
			}
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(buf []byte) (int, error) {
	return f(buf)
}

// ReaderFromSeq converts an iterator into an io.ReadCloser.
// Close must be called after the caller is done with the reader.
func ReaderFromSeq(seq Seq) io.ReadCloser {
	return &iterReader{
		seq: seq,
	}
}

type iterReader struct {
	seq Seq

	next  func() ([]byte, error, bool)
	close func()
	err   error
	data  []byte
}

// WriteTo implements [WriterTo].
func (r *iterReader) WriteTo(w io.Writer) (int64, error) {
	if r.seq != nil {
		// Read hasn't been called yet, we can just use the
		// iterator directly, saving the cost of iter.Pull2.
		n, err := CopySeq(w, r.seq)
		// Subsequent reads should return EOF.
		r.seq = func(func([]byte, error) bool) {}
		return n, err
	}
	return io.Copy(w, r)
}

func (r *iterReader) Read(buf []byte) (int, error) {
	if r.seq != nil {
		r.next, r.close = iter.Pull2(r.seq)
		// Can't use the fast path in WriteTo any more.
		r.seq = nil
	}
	if r.err != nil {
		return 0, r.err
	}
	if len(r.data) == 0 {
		var ok bool
		r.data, r.err, ok = r.next()
		if !ok {
			r.err = io.EOF
		}
	}
	n := copy(buf, r.data)
	r.data = r.data[n:]
	if len(r.data) > 0 {
		return n, nil
	}
	return n, r.err
}

func (r *iterReader) Close() error {
	if r.close != nil {
		r.close()
		r.close = nil
		if r.err == nil {
			r.err = io.EOF
		}
	}
	return nil
}

// CopySeq is like [io.Copy] but reads over r writing
// all the data to w. It returns the total number of bytes
// read.
func CopySeq(w io.Writer, r Seq) (int64, error) {
	tot := int64(0)
	for data, err := range r {
		if err != nil {
			return tot, err
		}
		n, err := w.Write(data)
		tot += int64(n)
		if err != nil {
			return tot, err
		}
	}
	return tot, nil
}

// SeqWriter returns a [Writer] that operates on the yield
// function passed into a [Seq] iterator. Writes will succeed until
// the iteration is terminated, upon which Write will return
// [ErrSequenceTerminated].
//
// The returned Writer should not be used outside the scope
// of the iterator function, following the same rules as any yield
// function.
//
// If active is non-nil, it reflects the "active" status of the generator
// and its value should be (but does not have to be) true initially.
// yield will not be called when *active is false.
// If yield returns false, *active will be set to false.
//
// The caller can use the value of *active to find out whether
// the iterator is still active.
func SeqWriter(yield func([]byte, error) bool, active *bool) io.Writer {
	if active == nil {
		active = new(bool)
		*active = true
	}
	return seqWriter{
		yield:  yield,
		active: active,
	}
}

type seqWriter struct {
	yield  func([]byte, error) bool
	active *bool
}

var ErrSequenceTerminated = errors.New("sequence terminated")

func (w seqWriter) Write(buf []byte) (int, error) {
	if !*w.active {
		return 0, ErrSequenceTerminated
	}
	if !w.yield(slices.Clip(buf), nil) {
		*w.active = false
		return 0, ErrSequenceTerminated
	}
	return len(buf), nil
}

// PipeSeqThrough returns a Seq that iterates over the data written
// by the function f to its argument Writer. The Writer implementation
// that it returns will be written with the data read from seq.
//
// In other words, data read from seq will be "piped through" f,
// resulting in a new Seq.
func PipeSeqThrough[W io.WriteCloser](seq Seq, f func(w io.Writer) W) Seq {
	return func(yield func([]byte, error) bool) {
		send := func(w io.WriteCloser, seq Seq) error {
			if _, err := CopySeq(w, seq); err != nil {
				return err
			}
			return w.Close()
		}
		active := true
		w := f(SeqWriter(yield, &active))
		if err := send(w, seq); err != nil && active {
			yield(nil, err)
		}
	}
}

// PipeThrough calls f; all data written by f to its argument writer
// will be made available on the returned ReadCloser; all data read from
// f will be written to the writer implementation returned by f.
//
// In other words, it returns a reader that "pipes" the content from r
// through f.
func PipeThrough[W io.WriteCloser](r io.Reader, f func(io.Writer) W, bufSize int) io.ReadCloser {
	in := SeqFromReader(r, bufSize)
	out := PipeSeqThrough(in, f)
	return ReaderFromSeq(out)
}

// ReaderWithContent returns a [Reader] that calls the given function to generate the
// data to be read. If the function returns an error, that error will
// be returned from the reader.
func ReaderWithContent(generate func(w io.Writer) error) io.ReadCloser {
	return ReaderFromSeq(func(yield func([]byte, error) bool) {
		active := true
		if err := generate(SeqWriter(yield, &active)); err != nil && active {
			yield(nil, err)
		}
	})
}
