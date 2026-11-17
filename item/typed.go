package item

import (
	"fmt"

	"github.com/ezchr/go-mcjava/wire"
)

// Typed values of the components Stack keeps raw. Each has a setter that encodes it into the
// stack (SetProfile, SetTrim...) and a getter that decodes it (Profile, Trim...; false when the
// stack has no such component or its value is of a form the type does not cover, an inline
// registry entry mostly). All are in the newest version's terms; EncodeFor turns them into an
// older one's.

// SetRaw sets component t to its encoded value (the newest wire form; see ValidComponent),
// replacing the one the stack has. It panics for a component Stack has fields for.
func (s *Stack) SetRaw(t int32, data []byte) {
	if Modeled.Has(t) {
		panic("item: " + ComponentName(t) + " has a field in Stack")
	}
	s.Removed.Del(t)
	if i := s.rawIndex(t); i >= 0 {
		s.Raw[i].Data = data
		return
	}
	s.Raw = append(s.Raw, RawComponent{t, data})
	if len(s.Order) > 0 {
		s.Order = append(s.Order, t)
	}
}

// RawData is the encoded value of raw component t.
func (s *Stack) RawData(t int32) ([]byte, bool) {
	if i := s.rawIndex(t); i >= 0 {
		return s.Raw[i].Data, true
	}
	return nil, false
}

// DelRaw takes raw component t out of the patch.
func (s *Stack) DelRaw(t int32) {
	i := s.rawIndex(t)
	if i < 0 {
		return
	}
	s.Raw = append(s.Raw[:i], s.Raw[i+1:]...)
	for j, o := range s.Order {
		if o == t {
			s.Order = append(s.Order[:j], s.Order[j+1:]...)
			break
		}
	}
}

// set encodes a value with f and sets it as component t.
func (s *Stack) set(t int32, f func(w *wire.Writer)) {
	var w wire.Writer
	f(&w)
	if w.B == nil {
		w.B = []byte{}
	}
	s.SetRaw(t, w.B)
}

// get decodes component t with f; false when the stack has none or f did not read all of it.
func (s *Stack) get(t int32, f func(r *wire.Reader)) bool {
	data, ok := s.RawData(t)
	if !ok {
		return false
	}
	r := wire.Reader{B: data}
	f(&r)
	return r.Err == nil && r.Len() == 0
}

func schemaKind(t int32) kind {
	if uint32(t) >= ComponentTypes || compSchemas[t] == nil {
		return 0
	}
	return compSchemas[t].k
}

// SetInt sets a component whose value is one number: an enum, a count or a registry id
// (base_color, rarity, enchantable, map_id, wolf/variant, cat/collar...). It panics for another
// kind of component.
func (s *Stack) SetInt(t int32, v int32) {
	if k := schemaKind(t); k != kVarInt && k != kID {
		panic("item: " + ComponentName(t) + " is not a number")
	}
	s.set(t, func(w *wire.Writer) { w.VarInt(v) })
}

// Int is the value of a component whose value is one number.
func (s *Stack) Int(t int32) (v int32, ok bool) {
	if k := schemaKind(t); k != kVarInt && k != kID {
		return 0, false
	}
	ok = s.get(t, func(r *wire.Reader) { v = r.VarInt() })
	return v, ok
}

// SetKey sets a component whose value is one resource key (note_block_sound, tooltip_style).
func (s *Stack) SetKey(t int32, v string) {
	if schemaKind(t) != kString {
		panic("item: " + ComponentName(t) + " is not a resource key")
	}
	s.set(t, func(w *wire.Writer) { w.String(v) })
}

// Key is the value of a component whose value is one resource key.
func (s *Stack) Key(t int32) (v string, ok bool) {
	if schemaKind(t) != kString {
		return "", false
	}
	ok = s.get(t, func(r *wire.Reader) { v = r.String(32767) })
	return v, ok
}

// SetFlag adds a component without a value (glider, creative_slot_lock, waxed).
func (s *Stack) SetFlag(t int32) {
	if schemaKind(t) != kUnit {
		panic("item: " + ComponentName(t) + " has a value")
	}
	s.SetRaw(t, []byte{})
}

// SetNBT sets a component whose value is one NBT tag (bucket_entity_data, lock, recipes...): nbt is
// the tag in network NBT, its type byte first.
func (s *Stack) SetNBT(t int32, nbt []byte) {
	if schemaKind(t) != kNBT {
		panic("item: " + ComponentName(t) + " is not NBT")
	}
	s.SetRaw(t, nbt)
}

// HolderSet is a set of registry entries: a tag ("minecraft:mineable/pickaxe", without the #)
// or ids.
type HolderSet struct {
	Tag string
	IDs []int32
}

func writeHolderSet(w *wire.Writer, h *HolderSet) {
	if h.Tag != "" {
		w.VarInt(0)
		w.String(h.Tag)
		return
	}
	w.VarInt(int32(len(h.IDs)) + 1)
	for _, id := range h.IDs {
		w.VarInt(id)
	}
}

func readHolderSet(r *wire.Reader) (h HolderSet) {
	n := r.VarInt()
	if n == 0 {
		h.Tag = r.String(32767)
		return h
	}
	if n < 0 || int(n-1) > r.Len() {
		fail(r, fmt.Errorf("%w: holder set of %d", ErrInvalid, n))
		return h
	}
	h.IDs = make([]int32, 0, n-1)
	for i := int32(1); i < n; i++ {
		h.IDs = append(h.IDs, r.VarInt())
	}
	return h
}

// SetHolderSet sets a component that is one set of registry entries: damage_resistant (damage
// types), repairable (items), provides_banner_patterns (banner patterns).
func (s *Stack) SetHolderSet(t int32, h HolderSet) {
	if schemaKind(t) != kHolderSet {
		panic("item: " + ComponentName(t) + " is not a holder set")
	}
	s.set(t, func(w *wire.Writer) { writeHolderSet(w, &h) })
}

// HolderSet is the value of a component that is one set of registry entries.
func (s *Stack) HolderSet(t int32) (h HolderSet, ok bool) {
	if schemaKind(t) != kHolderSet {
		return h, false
	}
	ok = s.get(t, func(r *wire.Reader) { h = readHolderSet(r) })
	return h, ok
}

// Sound is a sound event: one of the minecraft:sound_event registry by its id, or, when Name is
// set, any sound by name, with a fixed range to be heard in if HasRange.
type Sound struct {
	ID       int32
	Name     string
	HasRange bool
	Range    float32
}

func writeSound(w *wire.Writer, s *Sound) {
	if s.Name == "" {
		w.VarInt(s.ID + 1)
		return
	}
	w.VarInt(0)
	w.String(s.Name)
	w.Bool(s.HasRange)
	if s.HasRange {
		w.Float32(s.Range)
	}
}

func readSound(r *wire.Reader) (s Sound) {
	if n := r.VarInt(); n != 0 {
		s.ID = n - 1
		return s
	}
	s.Name = r.String(32767)
	if s.HasRange = r.Bool(); s.HasRange {
		s.Range = r.Float32()
	}
	return s
}

// ProfileProperty is a property of a game profile ("textures": the skin).
type ProfileProperty struct {
	Name, Value  string
	HasSignature bool
	Signature    string
}

// Profile is the profile component (a player head's owner). Complete: a whole profile, with id
// and name; else a partial one the client resolves, with a name, an id, or neither.
type Profile struct {
	Complete   bool
	HasID      bool
	ID         [16]byte
	Name       string
	Properties []ProfileProperty
	// Body, Cape and Elytra are texture assets that replace the profile's ("" for none).
	Body, Cape, Elytra string
	// Model is the skin's model when set: ModelWide or ModelSlim.
	HasModel bool
	Model    int8
}

// Skin models of a Profile.
const (
	ModelWide int8 = 0
	ModelSlim int8 = 1
)

// SetProfile sets the profile component.
func (s *Stack) SetProfile(p *Profile) {
	s.set(CompProfile, func(w *wire.Writer) {
		w.Bool(p.Complete)
		if p.Complete {
			w.UUID(p.ID)
			w.String(p.Name)
		} else {
			w.Bool(p.Name != "")
			if p.Name != "" {
				w.String(p.Name)
			}
			w.Bool(p.HasID)
			if p.HasID {
				w.UUID(p.ID)
			}
		}
		w.VarInt(int32(len(p.Properties)))
		for i := range p.Properties {
			pr := &p.Properties[i]
			w.String(pr.Name)
			w.String(pr.Value)
			w.Bool(pr.HasSignature)
			if pr.HasSignature {
				w.String(pr.Signature)
			}
		}
		for _, tex := range [3]string{p.Body, p.Cape, p.Elytra} {
			w.Bool(tex != "")
			if tex != "" {
				w.String(tex)
			}
		}
		w.Bool(p.HasModel)
		if p.HasModel {
			w.Bool(p.Model == ModelSlim)
		}
	})
}

// Profile is the stack's profile component.
func (s *Stack) Profile() (*Profile, bool) {
	p := &Profile{}
	ok := s.get(CompProfile, func(r *wire.Reader) {
		if p.Complete = r.Bool(); p.Complete {
			p.ID, p.HasID = r.UUID(), true
			p.Name = r.String(16)
		} else {
			if r.Bool() {
				p.Name = r.String(16)
			}
			if p.HasID = r.Bool(); p.HasID {
				p.ID = r.UUID()
			}
		}
		for n := length(r, 16, 2); n > 0 && r.Err == nil; n-- {
			pr := ProfileProperty{Name: r.String(64), Value: r.String(32767)}
			if pr.HasSignature = r.Bool(); pr.HasSignature {
				pr.Signature = r.String(1024)
			}
			p.Properties = append(p.Properties, pr)
		}
		for _, tex := range [3]*string{&p.Body, &p.Cape, &p.Elytra} {
			if r.Bool() {
				*tex = r.String(32767)
			}
		}
		if p.HasModel = r.Bool(); p.HasModel {
			if r.Bool() {
				p.Model = ModelSlim
			}
		}
	})
	return p, ok
}

// BannerLayer is one layer of a banner: a pattern of the minecraft:banner_pattern registry by
// its id, or, when AssetID is set, a pattern given here (its asset and translation key), in a
// dye colour.
type BannerLayer struct {
	Pattern        int32
	AssetID        string
	TranslationKey string
	Color          int32
}

// SetBannerPatterns sets the banner_patterns component.
func (s *Stack) SetBannerPatterns(layers []BannerLayer) {
	s.set(CompBannerPatterns, func(w *wire.Writer) {
		w.VarInt(int32(len(layers)))
		for i := range layers {
			l := &layers[i]
			if l.AssetID != "" {
				w.VarInt(0)
				w.String(l.AssetID)
				w.String(l.TranslationKey)
			} else {
				w.VarInt(l.Pattern + 1)
			}
			w.VarInt(l.Color)
		}
	})
}

// BannerPatterns is the stack's banner_patterns component.
func (s *Stack) BannerPatterns() (layers []BannerLayer, ok bool) {
	ok = s.get(CompBannerPatterns, func(r *wire.Reader) {
		for n := length(r, maxList, 2); n > 0 && r.Err == nil; n-- {
			var l BannerLayer
			if id := r.VarInt(); id != 0 {
				l.Pattern = id - 1
			} else {
				l.AssetID, l.TranslationKey = r.String(32767), r.String(32767)
			}
			l.Color = r.VarInt()
			layers = append(layers, l)
		}
	})
	return layers, ok
}

// Trim is the trim component: ids of the minecraft:trim_material and minecraft:trim_pattern
// registries.
type Trim struct{ Material, Pattern int32 }

// SetTrim sets the trim component.
func (s *Stack) SetTrim(t Trim) {
	s.set(CompTrim, func(w *wire.Writer) {
		w.VarInt(t.Material + 1)
		w.VarInt(t.Pattern + 1)
	})
}

// Trim is the stack's trim component (false too for a trim whose material or pattern is given
// inline, not by a registry id).
func (s *Stack) Trim() (t Trim, ok bool) {
	ok = s.get(CompTrim, func(r *wire.Reader) {
		t.Material, t.Pattern = r.VarInt()-1, r.VarInt()-1
	})
	return t, ok && t.Material >= 0 && t.Pattern >= 0
}

// FireworkExplosion is the firework_explosion component (a firework star) and one explosion of
// fireworks. Shape: 0 small ball, 1 large ball, 2 star, 3 creeper, 4 burst. Colours are RGB.
type FireworkExplosion struct {
	Shape              int32
	Colors, FadeColors []int32
	Trail, Twinkle     bool
}

func writeExplosion(w *wire.Writer, e *FireworkExplosion) {
	w.VarInt(e.Shape)
	for _, c := range [2][]int32{e.Colors, e.FadeColors} {
		w.VarInt(int32(len(c)))
		for _, v := range c {
			w.Int32(v)
		}
	}
	w.Bool(e.Trail)
	w.Bool(e.Twinkle)
}

func readExplosion(r *wire.Reader) (e FireworkExplosion) {
	e.Shape = r.VarInt()
	for _, c := range [2]*[]int32{&e.Colors, &e.FadeColors} {
		for n := length(r, maxList, 4); n > 0 && r.Err == nil; n-- {
			*c = append(*c, r.Int32())
		}
	}
	e.Trail, e.Twinkle = r.Bool(), r.Bool()
	return e
}

// SetFireworkExplosion sets the firework_explosion component.
func (s *Stack) SetFireworkExplosion(e FireworkExplosion) {
	s.set(CompFireworkExplosion, func(w *wire.Writer) { writeExplosion(w, &e) })
}

// FireworkExplosion is the stack's firework_explosion component.
func (s *Stack) FireworkExplosion() (e FireworkExplosion, ok bool) {
	ok = s.get(CompFireworkExplosion, func(r *wire.Reader) { e = readExplosion(r) })
	return e, ok
}

// Fireworks is the fireworks component (a rocket).
type Fireworks struct {
	FlightDuration int32
	Explosions     []FireworkExplosion
}

// SetFireworks sets the fireworks component.
func (s *Stack) SetFireworks(f Fireworks) {
	s.set(CompFireworks, func(w *wire.Writer) {
		w.VarInt(f.FlightDuration)
		w.VarInt(int32(len(f.Explosions)))
		for i := range f.Explosions {
			writeExplosion(w, &f.Explosions[i])
		}
	})
}

// Fireworks is the stack's fireworks component.
func (s *Stack) Fireworks() (f Fireworks, ok bool) {
	ok = s.get(CompFireworks, func(r *wire.Reader) {
		f.FlightDuration = r.VarInt()
		for n := length(r, 256, 4); n > 0 && r.Err == nil; n-- {
			f.Explosions = append(f.Explosions, readExplosion(r))
		}
	})
	return f, ok
}

// BookPage is a page of a book and quill: its text and, when HasFiltered, what a player with chat
// filtering on reads.
type BookPage struct {
	Text        string
	HasFiltered bool
	Filtered    string
}

func writeFilterString(w *wire.Writer, p *BookPage) {
	w.String(p.Text)
	w.Bool(p.HasFiltered)
	if p.HasFiltered {
		w.String(p.Filtered)
	}
}

func readFilterString(r *wire.Reader, max int) (p BookPage) {
	p.Text = r.String(max)
	if p.HasFiltered = r.Bool(); p.HasFiltered {
		p.Filtered = r.String(max)
	}
	return p
}

// SetWritableBook sets the writable_book_content component: the pages of a book and quill.
func (s *Stack) SetWritableBook(pages []BookPage) {
	s.set(CompWritableBookContent, func(w *wire.Writer) {
		w.VarInt(int32(len(pages)))
		for i := range pages {
			writeFilterString(w, &pages[i])
		}
	})
}

// WritableBook is the stack's writable_book_content component.
func (s *Stack) WritableBook() (pages []BookPage, ok bool) {
	ok = s.get(CompWritableBookContent, func(r *wire.Reader) {
		for n := length(r, maxList, 2); n > 0 && r.Err == nil; n-- {
			pages = append(pages, readFilterString(r, 32767))
		}
	})
	return pages, ok
}

// WrittenPage is a page of a written book: a text component and, when HasFiltered, the filtered
// one.
type WrittenPage struct {
	Text        Text
	HasFiltered bool
	Filtered    Text
}

// WrittenBook is the written_book_content component. Generation: 0 original, 1 copy of
// original, 2 copy of a copy, 3 tattered. Resolved: the pages' selectors and scores are final.
type WrittenBook struct {
	Title      BookPage // Text is the title
	Author     string
	Generation int32
	Pages      []WrittenPage
	Resolved   bool
}

// SetWrittenBook sets the written_book_content component.
func (s *Stack) SetWrittenBook(b *WrittenBook) {
	s.set(CompWrittenBookContent, func(w *wire.Writer) {
		writeFilterString(w, &b.Title)
		w.String(b.Author)
		w.VarInt(b.Generation)
		w.VarInt(int32(len(b.Pages)))
		for i := range b.Pages {
			p := &b.Pages[i]
			writeText(w, &p.Text)
			w.Bool(p.HasFiltered)
			if p.HasFiltered {
				writeText(w, &p.Filtered)
			}
		}
		w.Bool(b.Resolved)
	})
}

// WrittenBook is the stack's written_book_content component.
func (s *Stack) WrittenBook() (*WrittenBook, bool) {
	b := &WrittenBook{}
	ok := s.get(CompWrittenBookContent, func(r *wire.Reader) {
		b.Title = readFilterString(r, 32)
		b.Author = r.String(32767)
		b.Generation = r.VarInt()
		for n := length(r, maxList, 2); n > 0 && r.Err == nil; n-- {
			var p WrittenPage
			readText(r, &p.Text)
			if p.HasFiltered = r.Bool(); p.HasFiltered {
				readText(r, &p.Filtered)
			}
			b.Pages = append(b.Pages, p)
		}
		b.Resolved = r.Bool()
	})
	return b, ok
}

// encodeTemplate writes the stack as an ItemStackTemplate, the form of a stack inside a
// component: item, count, patch.
func (s *Stack) encodeTemplate(w *wire.Writer) {
	w.VarInt(s.ID)
	w.VarInt(s.Count)
	s.encodePatch(w, false)
}

func (s *Stack) decodeTemplate(r *wire.Reader) {
	s.Reset()
	s.ID = r.VarInt()
	s.Count = r.VarInt()
	if r.Err != nil {
		return
	}
	if uint32(s.ID) >= Items || s.Count <= 0 {
		fail(r, fmt.Errorf("%w: template of %d x item %d", ErrInvalid, s.Count, s.ID))
		return
	}
	s.decodePatch(r, false, nil)
}

func writeTemplates(w *wire.Writer, stacks []Stack) {
	w.VarInt(int32(len(stacks)))
	for i := range stacks {
		stacks[i].encodeTemplate(w)
	}
}

func readTemplates(r *wire.Reader, max int) (stacks []Stack) {
	for n := length(r, max, 2); n > 0 && r.Err == nil; n-- {
		stacks = append(stacks, Stack{})
		stacks[len(stacks)-1].decodeTemplate(r)
	}
	return stacks
}

// SetContainer sets the container component: the slots of a shulker box or another block with
// an inventory, an empty Stack for an empty slot.
func (s *Stack) SetContainer(slots []Stack) {
	s.set(CompContainer, func(w *wire.Writer) {
		w.VarInt(int32(len(slots)))
		for i := range slots {
			w.Bool(!slots[i].Empty())
			if !slots[i].Empty() {
				slots[i].encodeTemplate(w)
			}
		}
	})
}

// Container is the stack's container component.
func (s *Stack) Container() (slots []Stack, ok bool) {
	ok = s.get(CompContainer, func(r *wire.Reader) {
		for n := length(r, 256, 1); n > 0 && r.Err == nil; n-- {
			slots = append(slots, Stack{})
			if r.Bool() {
				slots[len(slots)-1].decodeTemplate(r)
			}
		}
	})
	return slots, ok
}

// SetBundleContents sets the bundle_contents component.
func (s *Stack) SetBundleContents(stacks []Stack) {
	s.set(CompBundleContents, func(w *wire.Writer) { writeTemplates(w, stacks) })
}

// BundleContents is the stack's bundle_contents component.
func (s *Stack) BundleContents() (stacks []Stack, ok bool) {
	ok = s.get(CompBundleContents, func(r *wire.Reader) { stacks = readTemplates(r, maxList) })
	return stacks, ok
}

// SetChargedProjectiles sets the charged_projectiles component (a loaded crossbow).
func (s *Stack) SetChargedProjectiles(stacks []Stack) {
	s.set(CompChargedProjectiles, func(w *wire.Writer) { writeTemplates(w, stacks) })
}

// ChargedProjectiles is the stack's charged_projectiles component.
func (s *Stack) ChargedProjectiles() (stacks []Stack, ok bool) {
	ok = s.get(CompChargedProjectiles, func(r *wire.Reader) { stacks = readTemplates(r, 1024) })
	return stacks, ok
}

// SetUseRemainder sets the use_remainder component: what the item turns into when used up.
func (s *Stack) SetUseRemainder(st *Stack) {
	s.set(CompUseRemainder, st.encodeTemplate)
}

// UseRemainder is the stack's use_remainder component.
func (s *Stack) UseRemainder() (*Stack, bool) {
	st := &Stack{}
	return st, s.get(CompUseRemainder, st.decodeTemplate)
}

// SetPotDecorations sets the pot_decorations component: the back, left, right and front of a
// decorated pot, an empty Stack for a plain side.
func (s *Stack) SetPotDecorations(sides *[4]Stack) {
	s.set(CompPotDecorations, func(w *wire.Writer) {
		for i := range sides {
			w.Bool(!sides[i].Empty())
			if !sides[i].Empty() {
				sides[i].encodeTemplate(w)
			}
		}
	})
}

// PotDecorations is the stack's pot_decorations component.
func (s *Stack) PotDecorations() (*[4]Stack, bool) {
	sides := &[4]Stack{}
	ok := s.get(CompPotDecorations, func(r *wire.Reader) {
		for i := range sides {
			if r.Bool() {
				sides[i].decodeTemplate(r)
			}
		}
	})
	return sides, ok
}

// AttributeModifier is one entry of attribute_modifiers. Attribute is a minecraft:attribute id;
// Operation 0 add value, 1 add multiplied base, 2 add multiplied total; Slot an equipment slot
// group (0 any, 1 main hand, 2 off hand, 3 hand, 4 feet, 5 legs, 6 chest, 7 head, 8 armor, 9
// body, 10 saddle); Display 0 default, 1 hidden, 2 the text in Override.
type AttributeModifier struct {
	Attribute int32
	ID        string
	Amount    float64
	Operation int32
	Slot      int32
	Display   int32
	Override  Text
}

// SetAttributeModifiers sets the attribute_modifiers component.
func (s *Stack) SetAttributeModifiers(mods []AttributeModifier) {
	s.set(CompAttributeModifiers, func(w *wire.Writer) {
		w.VarInt(int32(len(mods)))
		for i := range mods {
			m := &mods[i]
			w.VarInt(m.Attribute)
			w.String(m.ID)
			w.Float64(m.Amount)
			w.VarInt(m.Operation)
			w.VarInt(m.Slot)
			w.VarInt(m.Display)
			if m.Display == 2 {
				writeText(w, &m.Override)
			}
		}
	})
}

// AttributeModifiers is the stack's attribute_modifiers component.
func (s *Stack) AttributeModifiers() (mods []AttributeModifier, ok bool) {
	ok = s.get(CompAttributeModifiers, func(r *wire.Reader) {
		for n := length(r, maxList, 13); n > 0 && r.Err == nil; n-- {
			m := AttributeModifier{Attribute: r.VarInt(), ID: r.String(32767), Amount: r.Float64(),
				Operation: r.VarInt(), Slot: r.VarInt(), Display: r.VarInt()}
			switch m.Display {
			case 0, 1:
			case 2:
				readText(r, &m.Override)
			default:
				fail(r, fmt.Errorf("%w: attribute display %d", ErrInvalid, m.Display))
			}
			mods = append(mods, m)
		}
	})
	return mods, ok
}

// Food is the food component.
type Food struct {
	Nutrition    int32
	Saturation   float32
	CanAlwaysEat bool
}

// SetFood sets the food component.
func (s *Stack) SetFood(f Food) {
	s.set(CompFood, func(w *wire.Writer) {
		w.VarInt(f.Nutrition)
		w.Float32(f.Saturation)
		w.Bool(f.CanAlwaysEat)
	})
}

// Food is the stack's food component.
func (s *Stack) Food() (f Food, ok bool) {
	ok = s.get(CompFood, func(r *wire.Reader) {
		f = Food{r.VarInt(), r.Float32(), r.Bool()}
	})
	return f, ok
}

// Consume effect types (the minecraft:consume_effect_type registry).
const (
	ConsumeApplyEffects     int32 = 0
	ConsumeRemoveEffects    int32 = 1
	ConsumeClearAllEffects  int32 = 2
	ConsumeTeleportRandomly int32 = 3
	ConsumePlaySound        int32 = 4
)

// ConsumeEffect is what happens when an item is consumed (consumable, death_protection). By
// Type: apply effects (Effects with Probability), remove effects (Remove, mob effects), clear
// all effects, teleport randomly (Diameter; Flag is the flag 26.3 added to it), play a sound
// (Sound).
type ConsumeEffect struct {
	Type        int32
	Effects     []Effect
	Probability float32
	Remove      HolderSet
	Diameter    float32
	Flag        bool
	Sound       Sound
}

func writeConsumeEffects(w *wire.Writer, effects []ConsumeEffect) {
	w.VarInt(int32(len(effects)))
	for i := range effects {
		e := &effects[i]
		w.VarInt(e.Type)
		switch e.Type {
		case ConsumeApplyEffects:
			w.VarInt(int32(len(e.Effects)))
			for j := range e.Effects {
				w.VarInt(e.Effects[j].ID)
				writeEffectDetails(w, &e.Effects[j])
			}
			w.Float32(e.Probability)
		case ConsumeRemoveEffects:
			writeHolderSet(w, &e.Remove)
		case ConsumeTeleportRandomly:
			w.Float32(e.Diameter)
			w.Bool(e.Flag)
		case ConsumePlaySound:
			writeSound(w, &e.Sound)
		}
	}
}

func readConsumeEffects(r *wire.Reader) (effects []ConsumeEffect) {
	for n := length(r, maxList, 1); n > 0 && r.Err == nil; n-- {
		e := ConsumeEffect{Type: r.VarInt()}
		switch e.Type {
		case ConsumeApplyEffects:
			for m := length(r, maxList, 6); m > 0 && r.Err == nil; m-- {
				e.Effects = append(e.Effects, Effect{ID: r.VarInt()})
				readEffectDetails(r, &e.Effects[len(e.Effects)-1], 0)
			}
			e.Probability = r.Float32()
		case ConsumeRemoveEffects:
			e.Remove = readHolderSet(r)
		case ConsumeClearAllEffects:
		case ConsumeTeleportRandomly:
			e.Diameter, e.Flag = r.Float32(), r.Bool()
		case ConsumePlaySound:
			e.Sound = readSound(r)
		default:
			fail(r, fmt.Errorf("%w: consume effect %d", ErrInvalid, e.Type))
		}
		effects = append(effects, e)
	}
	return effects
}

// Consumable is the consumable component. Animation: 0 none, 1 eat, 2 drink, 3 block, 4 bow,
// 5 trident, 6 crossbow, 7 spyglass, 8 toot horn, 9 brush, 10 bundle, 11 spear.
type Consumable struct {
	Seconds   float32
	Animation int32
	Sound     Sound
	Particles bool
	Effects   []ConsumeEffect
}

// SetConsumable sets the consumable component.
func (s *Stack) SetConsumable(c *Consumable) {
	s.set(CompConsumable, func(w *wire.Writer) {
		w.Float32(c.Seconds)
		w.VarInt(c.Animation)
		writeSound(w, &c.Sound)
		w.Bool(c.Particles)
		writeConsumeEffects(w, c.Effects)
	})
}

// Consumable is the stack's consumable component.
func (s *Stack) Consumable() (*Consumable, bool) {
	c := &Consumable{}
	ok := s.get(CompConsumable, func(r *wire.Reader) {
		c.Seconds, c.Animation = r.Float32(), r.VarInt()
		c.Sound = readSound(r)
		c.Particles = r.Bool()
		c.Effects = readConsumeEffects(r)
	})
	return c, ok
}

// SetDeathProtection sets the death_protection component (a totem): what happens when it saves
// its holder.
func (s *Stack) SetDeathProtection(effects []ConsumeEffect) {
	s.set(CompDeathProtection, func(w *wire.Writer) { writeConsumeEffects(w, effects) })
}

// DeathProtection is the stack's death_protection component.
func (s *Stack) DeathProtection() (effects []ConsumeEffect, ok bool) {
	ok = s.get(CompDeathProtection, func(r *wire.Reader) { effects = readConsumeEffects(r) })
	return effects, ok
}

// UseCooldown is the use_cooldown component: Group is the cooldown group ("" for the item's own).
type UseCooldown struct {
	Seconds float32
	Group   string
}

// SetUseCooldown sets the use_cooldown component.
func (s *Stack) SetUseCooldown(c UseCooldown) {
	s.set(CompUseCooldown, func(w *wire.Writer) {
		w.Float32(c.Seconds)
		w.Bool(c.Group != "")
		if c.Group != "" {
			w.String(c.Group)
		}
	})
}

// UseCooldown is the stack's use_cooldown component.
func (s *Stack) UseCooldown() (c UseCooldown, ok bool) {
	ok = s.get(CompUseCooldown, func(r *wire.Reader) {
		c.Seconds = r.Float32()
		if r.Bool() {
			c.Group = r.String(32767)
		}
	})
	return c, ok
}

// ToolRule is a rule of the tool component: for Blocks (minecraft:block ids or a tag), a mining
// speed and whether the tool makes them drop, each only when its Has flag is set.
type ToolRule struct {
	Blocks          HolderSet
	HasSpeed        bool
	Speed           float32
	HasCorrect      bool
	CorrectForDrops bool
}

// Tool is the tool component.
type Tool struct {
	Rules                      []ToolRule
	DefaultMiningSpeed         float32
	DamagePerBlock             int32
	CanDestroyBlocksInCreative bool
}

// SetTool sets the tool component.
func (s *Stack) SetTool(t *Tool) {
	s.set(CompTool, func(w *wire.Writer) {
		w.VarInt(int32(len(t.Rules)))
		for i := range t.Rules {
			ru := &t.Rules[i]
			writeHolderSet(w, &ru.Blocks)
			w.Bool(ru.HasSpeed)
			if ru.HasSpeed {
				w.Float32(ru.Speed)
			}
			w.Bool(ru.HasCorrect)
			if ru.HasCorrect {
				w.Bool(ru.CorrectForDrops)
			}
		}
		w.Float32(t.DefaultMiningSpeed)
		w.VarInt(t.DamagePerBlock)
		w.Bool(t.CanDestroyBlocksInCreative)
	})
}

// Tool is the stack's tool component.
func (s *Stack) Tool() (*Tool, bool) {
	t := &Tool{}
	ok := s.get(CompTool, func(r *wire.Reader) {
		for n := length(r, maxList, 3); n > 0 && r.Err == nil; n-- {
			ru := ToolRule{Blocks: readHolderSet(r)}
			if ru.HasSpeed = r.Bool(); ru.HasSpeed {
				ru.Speed = r.Float32()
			}
			if ru.HasCorrect = r.Bool(); ru.HasCorrect {
				ru.CorrectForDrops = r.Bool()
			}
			t.Rules = append(t.Rules, ru)
		}
		t.DefaultMiningSpeed, t.DamagePerBlock, t.CanDestroyBlocksInCreative = r.Float32(), r.VarInt(), r.Bool()
	})
	return t, ok
}

// Weapon is the weapon component.
type Weapon struct {
	DamagePerAttack        int32
	DisableBlockingSeconds float32
}

// SetWeapon sets the weapon component.
func (s *Stack) SetWeapon(v Weapon) {
	s.set(CompWeapon, func(w *wire.Writer) {
		w.VarInt(v.DamagePerAttack)
		w.Float32(v.DisableBlockingSeconds)
	})
}

// Weapon is the stack's weapon component.
func (s *Stack) Weapon() (v Weapon, ok bool) {
	ok = s.get(CompWeapon, func(r *wire.Reader) { v = Weapon{r.VarInt(), r.Float32()} })
	return v, ok
}

// PropertyMatcher is a block state property a BlockPredicate asks for: a Value, or, when Range,
// a value between Min and Max (each only when its Has flag is set).
type PropertyMatcher struct {
	Name           string
	Value          string
	Range          bool
	HasMin, HasMax bool
	Min, Max       string
}

// BlockPredicate is one entry of can_break and can_place_on: the blocks it is about (minecraft:
// block ids or a tag; nil for any), state properties, and block entity NBT (a network NBT
// compound; nil for none). Matchers is its component matchers as they are encoded (nil: none).
type BlockPredicate struct {
	Blocks     *HolderSet
	Properties []PropertyMatcher
	NBT        []byte
	Matchers   []byte
}

// noMatchers is the encoding of no component matchers: no exact values, no predicates.
var noMatchers = []byte{0, 0}

var nMatchers = seq(&node{k: kExact}, listMax(seq(either(nVarInt, nCompT), nNBT), 64))

func writeBlockPredicates(w *wire.Writer, preds []BlockPredicate) {
	w.VarInt(int32(len(preds)))
	for i := range preds {
		p := &preds[i]
		w.Bool(p.Blocks != nil)
		if p.Blocks != nil {
			writeHolderSet(w, p.Blocks)
		}
		w.Bool(p.Properties != nil)
		if p.Properties != nil {
			w.VarInt(int32(len(p.Properties)))
			for j := range p.Properties {
				m := &p.Properties[j]
				w.String(m.Name)
				w.Bool(!m.Range)
				if !m.Range {
					w.String(m.Value)
					continue
				}
				w.Bool(m.HasMin)
				if m.HasMin {
					w.String(m.Min)
				}
				w.Bool(m.HasMax)
				if m.HasMax {
					w.String(m.Max)
				}
			}
		}
		w.Bool(p.NBT != nil)
		if p.NBT != nil {
			w.Raw(p.NBT)
		}
		if p.Matchers == nil {
			w.Raw(noMatchers)
		} else {
			w.Raw(p.Matchers)
		}
	}
}

func readBlockPredicates(r *wire.Reader) (preds []BlockPredicate) {
	for n := length(r, maxList, 5); n > 0 && r.Err == nil; n-- {
		var p BlockPredicate
		if r.Bool() {
			h := readHolderSet(r)
			p.Blocks = &h
		}
		if r.Bool() {
			p.Properties = []PropertyMatcher{}
			for m := length(r, maxList, 3); m > 0 && r.Err == nil; m-- {
				pm := PropertyMatcher{Name: r.String(32767)}
				if r.Bool() {
					pm.Value = r.String(32767)
				} else {
					pm.Range = true
					if pm.HasMin = r.Bool(); pm.HasMin {
						pm.Min = r.String(32767)
					}
					if pm.HasMax = r.Bool(); pm.HasMax {
						pm.Max = r.String(32767)
					}
				}
				p.Properties = append(p.Properties, pm)
			}
		}
		if r.Bool() {
			start := r.Off
			skipNBT(r)
			if r.Err == nil {
				p.NBT = append([]byte(nil), r.B[start:r.Off]...)
			}
		}
		start := r.Off
		x := xcode{r: r}
		x.walk(nMatchers)
		if r.Err == nil {
			if m := r.B[start:r.Off]; len(m) != len(noMatchers) || m[0] != 0 || m[1] != 0 {
				p.Matchers = append([]byte(nil), m...)
			}
		}
		preds = append(preds, p)
	}
	return preds
}

// SetCanBreak sets the can_break component: the blocks the item breaks in adventure mode.
func (s *Stack) SetCanBreak(preds []BlockPredicate) {
	s.set(CompCanBreak, func(w *wire.Writer) { writeBlockPredicates(w, preds) })
}

// CanBreak is the stack's can_break component.
func (s *Stack) CanBreak() (preds []BlockPredicate, ok bool) {
	ok = s.get(CompCanBreak, func(r *wire.Reader) { preds = readBlockPredicates(r) })
	return preds, ok
}

// SetCanPlaceOn sets the can_place_on component: the blocks the item is placed on in adventure
// mode.
func (s *Stack) SetCanPlaceOn(preds []BlockPredicate) {
	s.set(CompCanPlaceOn, func(w *wire.Writer) { writeBlockPredicates(w, preds) })
}

// CanPlaceOn is the stack's can_place_on component.
func (s *Stack) CanPlaceOn() (preds []BlockPredicate, ok bool) {
	ok = s.get(CompCanPlaceOn, func(r *wire.Reader) { preds = readBlockPredicates(r) })
	return preds, ok
}

// SetHolder sets a component that is one entry of a registry sent in configuration, by its id:
// jukebox_playable (minecraft:jukebox_song), instrument (minecraft:instrument),
// provides_trim_material (minecraft:trim_material), painting/variant (minecraft:painting_variant).
func (s *Stack) SetHolder(t int32, id int32) {
	if schemaKind(t) != kHolder {
		panic("item: " + ComponentName(t) + " is not a registry entry")
	}
	s.set(t, func(w *wire.Writer) { w.VarInt(id + 1) })
}

// Holder is the id of a component that is one registry entry (false too for one given inline).
func (s *Stack) Holder(t int32) (id int32, ok bool) {
	if schemaKind(t) != kHolder {
		return 0, false
	}
	ok = s.get(t, func(r *wire.Reader) { id = r.VarInt() - 1 })
	return id, ok && id >= 0
}

// LodestoneTracker is the lodestone_tracker component: a compass that points at a block of a
// dimension when HasTarget; Tracked: the target goes away when the lodestone is broken.
type LodestoneTracker struct {
	HasTarget bool
	Dimension string
	X, Y, Z   int
	Tracked   bool
}

// SetLodestoneTracker sets the lodestone_tracker component.
func (s *Stack) SetLodestoneTracker(l LodestoneTracker) {
	s.set(CompLodestoneTracker, func(w *wire.Writer) {
		w.Bool(l.HasTarget)
		if l.HasTarget {
			w.String(l.Dimension)
			w.Position(l.X, l.Y, l.Z)
		}
		w.Bool(l.Tracked)
	})
}

// LodestoneTracker is the stack's lodestone_tracker component.
func (s *Stack) LodestoneTracker() (l LodestoneTracker, ok bool) {
	ok = s.get(CompLodestoneTracker, func(r *wire.Reader) {
		if l.HasTarget = r.Bool(); l.HasTarget {
			l.Dimension = r.String(32767)
			l.X, l.Y, l.Z = r.Position()
		}
		l.Tracked = r.Bool()
	})
	return l, ok
}

// StewEffect is an effect of suspicious_stew_effects: a minecraft:mob_effect id and its duration
// in ticks.
type StewEffect struct{ Effect, Duration int32 }

// SetSuspiciousStewEffects sets the suspicious_stew_effects component.
func (s *Stack) SetSuspiciousStewEffects(effects []StewEffect) {
	s.set(CompSuspiciousStewEffects, func(w *wire.Writer) {
		w.VarInt(int32(len(effects)))
		for _, e := range effects {
			w.VarInt(e.Effect)
			w.VarInt(e.Duration)
		}
	})
}

// SuspiciousStewEffects is the stack's suspicious_stew_effects component.
func (s *Stack) SuspiciousStewEffects() (effects []StewEffect, ok bool) {
	ok = s.get(CompSuspiciousStewEffects, func(r *wire.Reader) {
		for n := length(r, maxList, 2); n > 0 && r.Err == nil; n-- {
			effects = append(effects, StewEffect{r.VarInt(), r.VarInt()})
		}
	})
	return effects, ok
}

// BlockProperty is a block state property of the block_state component ("facing", "north").
type BlockProperty struct{ Name, Value string }

// SetBlockState sets the block_state component: properties of the block the item places.
func (s *Stack) SetBlockState(props []BlockProperty) {
	s.set(CompBlockState, func(w *wire.Writer) {
		w.VarInt(int32(len(props)))
		for _, p := range props {
			w.String(p.Name)
			w.String(p.Value)
		}
	})
}

// BlockState is the stack's block_state component.
func (s *Stack) BlockState() (props []BlockProperty, ok bool) {
	ok = s.get(CompBlockState, func(r *wire.Reader) {
		for n := length(r, maxList, 2); n > 0 && r.Err == nil; n-- {
			props = append(props, BlockProperty{r.String(32767), r.String(32767)})
		}
	})
	return props, ok
}

// EntityData is the entity_data component (Type is a minecraft:entity_type id) and the
// block_entity_data component (a minecraft:block_entity_type id): the type and its data as a
// network NBT compound.
type EntityData struct {
	Type int32
	NBT  []byte
}

func (s *Stack) setEntityData(t int32, d EntityData) {
	s.set(t, func(w *wire.Writer) {
		w.VarInt(d.Type)
		if len(d.NBT) == 0 {
			w.Byte(nbtCompound)
			w.Byte(nbtEnd)
		} else {
			w.Raw(d.NBT)
		}
	})
}

func (s *Stack) entityData(t int32) (d EntityData, ok bool) {
	ok = s.get(t, func(r *wire.Reader) {
		d.Type = r.VarInt()
		start := r.Off
		skipNBT(r)
		if r.Err == nil {
			d.NBT = append([]byte(nil), r.B[start:r.Off]...)
		}
	})
	return d, ok
}

// SetEntityData sets the entity_data component.
func (s *Stack) SetEntityData(d EntityData) { s.setEntityData(CompEntityData, d) }

// EntityData is the stack's entity_data component.
func (s *Stack) EntityData() (EntityData, bool) { return s.entityData(CompEntityData) }

// SetBlockEntityData sets the block_entity_data component.
func (s *Stack) SetBlockEntityData(d EntityData) { s.setEntityData(CompBlockEntityData, d) }

// BlockEntityData is the stack's block_entity_data component.
func (s *Stack) BlockEntityData() (EntityData, bool) { return s.entityData(CompBlockEntityData) }
