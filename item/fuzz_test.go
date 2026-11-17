package item

import (
	"bytes"
	"testing"
	"time"

	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// Whatever bytes are read as a stack, in any form of either version: what is then written for the
// newest version and for 26.2 is read to its end again and written the same a second time. A stack
// a client made up is sent on to other clients, whose packet must not break on it.
func FuzzStackForms(f *testing.F) {
	p := For(version.V776)
	stone, _ := ByName("minecraft:stone")
	for seed := int64(0); seed < 4; seed++ {
		s := Stack{Count: 1, ID: stone}
		for ct := int32(0); ct < ComponentTypes; ct++ {
			if !Modeled.Has(ct) && (ct+int32(seed))%4 == 0 {
				s.Raw = append(s.Raw, RawComponent{ct, sampleValue(ct, seed)})
			}
		}
		var w wire.Writer
		s.Encode(&w)
		f.Add(w.B)
		w.Reset()
		s.EncodeUntrusted(&w)
		f.Add(w.B)
		w.Reset()
		s.EncodeFor(&w, p)
		f.Add(w.B)
		w.Reset()
		s.EncodeUntrustedFor(&w, p)
		f.Add(w.B)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		start := time.Now()
		defer func() {
			if d := time.Since(start); d > 5*time.Second {
				t.Fatalf("took %v", d)
			}
		}()
		for mode := 0; mode < 4; mode++ {
			var s Stack
			r := wire.NewReader(b)
			switch mode {
			case 0:
				s.Decode(r)
			case 1:
				s.DecodeUntrusted(r)
			case 2:
				s.DecodeFor(r, p)
			case 3:
				s.DecodeUntrustedFor(r, p)
			}
			if r.Err != nil {
				continue
			}
			for _, to := range []*Proto{nil, p} {
				var w wire.Writer
				s.EncodeFor(&w, to)
				var back Stack
				rr := wire.NewReader(w.B)
				back.DecodeFor(rr, to)
				if rr.Err != nil || rr.Len() != 0 {
					t.Fatalf("mode %d: written %x is not read back: %v, %d left", mode, w.B, rr.Err, rr.Len())
				}
				var w2 wire.Writer
				back.EncodeFor(&w2, to)
				if !bytes.Equal(w.B, w2.B) {
					t.Fatalf("mode %d: written %x, read and written again %x", mode, w.B, w2.B)
				}
			}
		}
	})
}
