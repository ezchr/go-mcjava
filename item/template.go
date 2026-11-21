package item

import (
	"fmt"

	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// EncodeTemplateFor writes the stack as an ItemStackTemplate for the protocol version of p (nil:
// the newest): item, count, component patch. It is the form of a stack that is never empty: a
// recipe display's item_stack slot, and stacks inside components. It writes nothing and reports
// false for an empty stack and for an item the version does not have.
func (s *Stack) EncodeTemplateFor(w *wire.Writer, p *Proto) bool {
	if s.Count <= 0 || uint32(s.ID) >= Items {
		return false
	}
	if p == nil {
		s.encodeTemplate(w)
		return true
	}
	if mapID(p.items, s.ID) < 0 {
		return false
	}
	// The version's stack is count, item, patch: the same bytes with the first two swapped.
	start := len(w.B)
	s.encodeOld(w, false, p)
	b := w.B[start:]
	r := wire.NewReader(b)
	count := r.VarInt()
	id := r.VarInt()
	var head [10]byte
	h := wire.AppendVarInt(head[:0], id)
	h = wire.AppendVarInt(h, count)
	copy(b[:r.Off], h)
	return true
}

// DecodeTemplateFor reads an ItemStackTemplate of the protocol version of p (nil: the newest). The
// stack gets newest ids (see DecodeFor).
func (s *Stack) DecodeTemplateFor(r *wire.Reader, p *Proto) {
	if p == nil {
		s.decodeTemplate(r)
		return
	}
	s.Reset()
	id := r.VarInt()
	count := r.VarInt()
	if r.Err != nil {
		return
	}
	nid := mapID(p.itemsIn, id)
	if uint32(nid) >= Items || count <= 0 {
		fail(r, fmt.Errorf("%w: template of %d x item %d", ErrInvalid, count, id))
		return
	}
	s.ID, s.Count = nid, count
	s.decodePatch(r, false, p)
}

// ComponentIn is the newest id of the version's data component type (-1: none). For a nil Proto
// it is t.
func (p *Proto) ComponentIn(t int32) int32 {
	if p == nil {
		return t
	}
	return mapID(p.compsIn, t)
}

// Protocol is the protocol number of the Proto's version (the newest's for a nil Proto).
func (p *Proto) Protocol() int32 {
	if p == nil {
		return version.Newest.Protocol
	}
	return p.v.Protocol
}
