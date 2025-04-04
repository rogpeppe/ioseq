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
// of the given size to do so.
func SeqFromReader(r io.Reader, bufSize int) Seq {
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
				if len(buf) > 0 && !yield(buf[:n], nil) {
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

// ReaderFromSeq converts an iterator into an io.ReadCloser.
// Close must be called after the caller is done with the reader.
func ReaderFromSeq(it Seq) io.ReadCloser {
	next, close := iter.Pull2(it)
	return &iterReader{
		next:  next,
		close: close,
	}
}

type iterReader struct {
	next  func() ([]byte, error, bool)
	close func()
	err   error
	data  []byte
}

func (r *iterReader) Read(buf []byte) (int, error) {
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
func SeqWriter(yield func([]byte, error) bool) io.Writer {
	return &seqWriter{yield: yield}
}

type seqWriter struct {
	yield  func([]byte, error) bool
	closed bool
}

var ErrSequenceTerminated = errors.New("sequence terminated")

func (w *seqWriter) Write(buf []byte) (int, error) {
	if w.closed || !w.yield(slices.Clip(buf), nil) {
		w.closed = true
		return 0, ErrSequenceTerminated
	}
	return len(buf), nil
}

func WriterWriterToReaderSeq(f func(w io.Writer) io.WriteCloser) func(io.Reader) Seq {
	return func(r io.Reader) Seq {
		return func(yield func([]byte, error) bool) {
			seqw := &seqWriter{yield: yield}
			w := f(seqw)

			_, err := io.CopyBuffer(f(w), r, make([]byte, 8192))
			if !seqw.closed && err != nil {
				if !yield(nil, err) {
					return
				}
			}
			if err := w.Close(); err != nil {
				yield(nil, err)
			}
		}
	}
}
