package item

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"reflect"
	"testing"

	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// Every component type of the newest version has a wire form, and the plain kinds the generator
// derives from Mojang's reports agree with it.
func TestSchemaCoversAll(t *testing.T) {
	for i, name := range componentNames {
		if compSchemas[i] == nil {
			t.Errorf("%s has no wire form", name)
		}
	}
	if len(schemas) != ComponentTypes {
		t.Errorf("%d wire forms for %d components", len(schemas), ComponentTypes)
	}
	plain := map[uint8]kind{kindVarInt: kVarInt, kindFloat: kFloat, kindBool: kBool, kindString: kString,
		kindUnit: kUnit, kindNBT: kNBT, kindInt32: kInt32}
	for i, k := range rawKinds {
		if k == kindNone {
			continue
		}
		got := compSchemas[i].k
		if got == kID {
			got = kVarInt // an id is a varint that is remapped
		}
		if got != plain[k] {
			t.Errorf("%s: wire form kind %d, the reports say %d", componentNames[i], compSchemas[i].k, k)
		}
	}
}

// sampler writes values of a wire form with random choices: which branch, how many elements.
type sampler struct {
	rnd *rand.Rand
	w   wire.Writer
}

func (g *sampler) value(n *node, depth int) {
	w := &g.w
	switch n.k {
	case kVarInt:
		w.VarInt(int32(g.rnd.Intn(300)))
	case kInt32:
		w.Int32(g.rnd.Int31())
	case kInt64:
		w.Int64(g.rnd.Int63())
	case kFloat:
		w.Float32(g.rnd.Float32())
	case kDouble:
		w.Float64(g.rnd.Float64())
	case kBool:
		w.Bool(g.rnd.Intn(2) == 0)
	case kString:
		w.String([]string{"minecraft:a", "", "é x"}[g.rnd.Intn(3)])
	case kUUID:
		for range 16 {
			w.Byte(byte(g.rnd.Intn(256)))
		}
	case kNBT:
		switch g.rnd.Intn(3) {
		case 0:
			w.Raw([]byte{nbtString, 0, 2, 'h', 'i'})
		case 1:
			w.Raw([]byte{nbtCompound, 1, 0, 1, 'a', 7, nbtEnd}) // {a: 7b}
		default:
			w.Raw([]byte{nbtCompound, nbtEnd})
		}
	case kUnit:
	case kID:
		w.VarInt(int32(g.rnd.Intn(3)))
	case kCompType:
		w.VarInt(CompDamage)
	case kOpt:
		if present := g.rnd.Intn(2) == 0; present {
			w.Bool(true)
			g.value(n.sub[0], depth)
		} else {
			w.Bool(false)
		}
	case kList:
		c := g.rnd.Intn(min(n.max, 3) + 1)
		w.VarInt(int32(c))
		for range c {
			g.value(n.sub[0], depth)
		}
	case kSeq:
		for _, s := range n.sub {
			g.value(s, depth)
		}
	case kRep:
		for range n.max {
			g.value(n.sub[0], depth)
		}
	case kHolder:
		if g.rnd.Intn(2) == 0 {
			w.VarInt(0)
			g.value(n.sub[0], depth)
		} else {
			w.VarInt(int32(g.rnd.Intn(3)) + 1)
		}
	case kHolderSet:
		if g.rnd.Intn(2) == 0 {
			w.VarInt(0)
			w.String("minecraft:some_tag")
		} else {
			c := g.rnd.Intn(3)
			w.VarInt(int32(c) + 1)
			for range c {
				w.VarInt(int32(g.rnd.Intn(3)))
			}
		}
	case kTemplate:
		w.VarInt(int32(g.rnd.Intn(3)) + 1) // stone and its neighbours
		w.VarInt(int32(g.rnd.Intn(64)) + 1)
		g.patch(depth)
	case kUnion:
		d := g.rnd.Intn(len(n.sub))
		w.VarInt(int32(d))
		g.value(n.sub[d], depth)
	case kIf:
		if g.rnd.Intn(2) == 0 {
			w.Bool(true)
			g.value(n.sub[0], depth)
		} else {
			w.Bool(false)
			g.value(n.sub[1], depth)
		}
	case kExact:
		c := g.rnd.Intn(3)
		w.VarInt(int32(c))
		for range c {
			g.component(depth)
		}
	case kOnlyNew, kOldNew:
		g.value(n.sub[0], depth)
	case kPotDecor:
		g.value(rep(opt(nTmpl), 4), depth)
	}
}

// component writes a random component type and a value of it.
func (g *sampler) component(depth int) {
	t := int32(g.rnd.Intn(ComponentTypes))
	if depth > 2 {
		t = CompDamage
	}
	g.w.VarInt(t)
	g.value(compSchemas[t], depth+1)
}

func (g *sampler) patch(depth int) {
	c := g.rnd.Intn(3)
	if depth > 2 {
		c = 0
	}
	types := g.rnd.Perm(ComponentTypes)[:c] // a patch has a type once
	removed := g.rnd.Intn(2)
	g.w.VarInt(int32(c))
	g.w.VarInt(int32(removed))
	for _, t := range types {
		g.w.VarInt(int32(t))
		g.value(compSchemas[t], depth+1)
	}
	for range removed {
		g.w.VarInt(CompLore)
	}
}

// sampleValue is a random value of component t.
func sampleValue(t int32, seed int64) []byte {
	g := sampler{rnd: rand.New(rand.NewSource(seed))}
	g.value(compSchemas[t], 0)
	if g.w.B == nil {
		return []byte{}
	}
	return g.w.B
}

// only263 are the components 26.2 does not have.
var only263 = map[string]bool{"minecraft:interact_animation": true, "minecraft:block_transformer": true,
	"minecraft:villager_food": true, "minecraft:compostable": true, "minecraft:cooking_fuel": true,
	"minecraft:brewing_fuel": true, "minecraft:mob_visibility": true, "minecraft:provides_pottery_pattern": true,
	"minecraft:sign_text_front": true, "minecraft:sign_text_back": true, "minecraft:waxed": true,
	"minecraft:cushion/color": true}

// Random values of every component: each is read to its end by its wire form, a stack with it is
// written again byte for byte in the server and the client-to-server forms, and for 26.2 it either
// has no counterpart or converts there and back to a value that converts to the same again.
func TestEveryComponentRoundTrips(t *testing.T) {
	p := For(version.V776)
	stone := mustID(t, "minecraft:stone")
	lostAll := map[string]bool{}
	for ct := int32(0); ct < ComponentTypes; ct++ {
		name := componentNames[ct]
		converted := 0
		for seed := int64(0); seed < 300; seed++ {
			data := sampleValue(ct, seed)
			if !ValidComponent(ct, data) {
				t.Fatalf("%s seed %d: %x is not read to its end", name, seed, data)
			}
			// One byte less is not a value (but for a component without one).
			if len(data) > 0 && ValidComponent(ct, data[:len(data)-1]) {
				t.Fatalf("%s seed %d: %x is read as a value without its last byte", name, seed, data)
			}
			if Modeled.Has(ct) {
				continue // Stack's own fields: the tests of item_test.go
			}
			s := Stack{Count: 3, ID: stone, Raw: []RawComponent{{ct, data}}}
			var w wire.Writer
			s.Encode(&w)
			var back Stack
			r := wire.NewReader(w.B)
			back.Decode(r)
			if r.Err != nil || r.Len() != 0 {
				t.Fatalf("%s seed %d: decode: %v, %d left", name, seed, r.Err, r.Len())
			}
			var w2 wire.Writer
			back.Encode(&w2)
			if !bytes.Equal(w.B, w2.B) || len(back.Raw) != 1 || !bytes.Equal(back.Raw[0].Data, data) {
				t.Fatalf("%s seed %d: %x came back as %x", name, seed, w.B, w2.B)
			}
			var u wire.Writer
			s.EncodeUntrusted(&u)
			r = wire.NewReader(u.B)
			back.DecodeUntrusted(r)
			w2.Reset()
			back.Encode(&w2)
			if r.Err != nil || r.Len() != 0 || !bytes.Equal(w.B, w2.B) {
				t.Fatalf("%s seed %d: client-to-server form: %v, %x came back as %x", name, seed, r.Err, w.B, w2.B)
			}

			// 26.2.
			old, ok := p.rawToOld(ct, data)
			if only263[name] {
				if ok {
					t.Fatalf("%s: 26.2 has no such component, got %x", name, old)
				}
				continue
			}
			if !ok {
				continue // names something 26.2 lacks
			}
			converted++
			or := wire.NewReader(old)
			if !skipComponent(or, ct, p) || or.Err != nil || or.Len() != 0 {
				t.Fatalf("%s seed %d: the 26.2 form %x is not read to its end (%v)", name, seed, old, or.Err)
			}
			newest, ok := p.rawFromOld(ct, old)
			if !ok || !ValidComponent(ct, newest) {
				t.Fatalf("%s seed %d: 26.2 form %x does not come back (%x)", name, seed, old, newest)
			}
			again, ok := p.rawToOld(ct, newest)
			if !ok || !bytes.Equal(again, old) {
				t.Fatalf("%s seed %d: 26.2 form %x, back and there again %x", name, seed, old, again)
			}
			// The whole stack for a 26.2 client, and what such a client sends back.
			w.Reset()
			s.EncodeFor(&w, p)
			r = wire.NewReader(w.B)
			back.DecodeFor(r, p)
			if r.Err != nil || r.Len() != 0 || len(back.Raw) != 1 || !bytes.Equal(back.Raw[0].Data, newest) {
				t.Fatalf("%s seed %d: 26.2 stack %x: %v, %d left, raw %v", name, seed, w.B, r.Err, r.Len(), back.Raw)
			}
			u.Reset()
			s.EncodeUntrustedFor(&u, p)
			r = wire.NewReader(u.B)
			back.DecodeUntrustedFor(r, p)
			if r.Err != nil || r.Len() != 0 || len(back.Raw) != 1 || !bytes.Equal(back.Raw[0].Data, newest) {
				t.Fatalf("%s seed %d: 26.2 client-to-server stack %x: %v, raw %v", name, seed, u.B, r.Err, back.Raw)
			}
		}
		if converted == 0 && !only263[name] && !Modeled.Has(ct) {
			lostAll[name] = true
		}
	}
	if len(lostAll) > 0 {
		t.Errorf("no value of these converts to 26.2: %v", lostAll)
	}
}

// A stack with every raw component at once.
func TestAllComponentsInOneStack(t *testing.T) {
	s := Stack{Count: 1, ID: mustID(t, "minecraft:stone")}
	for ct := int32(0); ct < ComponentTypes; ct++ {
		if !Modeled.Has(ct) {
			s.Raw = append(s.Raw, RawComponent{ct, sampleValue(ct, 7)})
		}
	}
	var w wire.Writer
	s.Encode(&w)
	var back Stack
	r := wire.NewReader(w.B)
	back.Decode(r)
	if r.Err != nil || r.Len() != 0 || len(back.Raw) != len(s.Raw) {
		t.Fatalf("decode: %v, %d left, %d of %d components", r.Err, r.Len(), len(back.Raw), len(s.Raw))
	}
	var w2 wire.Writer
	back.Encode(&w2)
	if !bytes.Equal(w.B, w2.B) {
		t.Fatal("not the same bytes")
	}
	// For 26.2: what is written is read to its end, and writing what was read gives it again.
	p := For(version.V776)
	w.Reset()
	s.EncodeFor(&w, p)
	r = wire.NewReader(w.B)
	back.DecodeFor(r, p)
	if r.Err != nil || r.Len() != 0 {
		t.Fatalf("26.2 decode: %v, %d left", r.Err, r.Len())
	}
	w2.Reset()
	back.EncodeFor(&w2, p)
	if !bytes.Equal(w.B, w2.B) {
		t.Fatalf("26.2: not the same bytes\n%x\n%x", w.B, w2.B)
	}
	if len(back.Raw) < 60 {
		t.Errorf("only %d raw components reach 26.2", len(back.Raw))
	}
}

func unhex(t testing.TB, s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// What differs in 26.2, byte by byte.
func TestForms262(t *testing.T) {
	p := For(version.V776)
	item := func(name string) int32 { return mustID(t, name) }
	old := func(name string) byte {
		id := p.Item(item(name))
		if id < 0 || id > 127 {
			t.Fatalf("%s: 26.2 id %d", name, id)
		}
		return byte(id)
	}
	stone, dirt := item("minecraft:stone"), item("minecraft:dirt")
	var brick wire.Writer
	brick.VarInt(p.Item(item("minecraft:brick")))

	// pot_decorations: four optional stacks; 26.2 has four item ids, a brick for a plain side.
	var s Stack
	s.SetPotDecorations(&[4]Stack{{Count: 1, ID: stone}, {}, {Count: 1, ID: dirt}, {}})
	data, _ := s.RawData(CompPotDecorations)
	if want := []byte{1, byte(stone), 1, 0, 0, 0, 1, byte(dirt), 1, 0, 0, 0}; !bytes.Equal(data, want) {
		t.Fatalf("pot decorations: %x, want %x", data, want)
	}
	got, ok := p.rawToOld(CompPotDecorations, data)
	want := append(append(append([]byte{4, old("minecraft:stone")}, brick.B...), old("minecraft:dirt")), brick.B...)
	if !ok || !bytes.Equal(got, want) {
		t.Errorf("pot decorations for 26.2: %x, want %x", got, want)
	}
	back, ok := p.rawFromOld(CompPotDecorations, []byte{2, old("minecraft:stone"), old("minecraft:dirt")})
	if want := []byte{1, byte(stone), 1, 0, 0, 1, byte(dirt), 1, 0, 0, 0, 0}; !ok || !bytes.Equal(back, want) {
		t.Errorf("pot decorations from 26.2: %x, want %x", back, want)
	}

	// consumable: teleport_randomly has a flag more in 26.3.
	s = Stack{}
	s.SetConsumable(&Consumable{Seconds: 1.6, Animation: 1, Sound: Sound{Name: "minecraft:x"}, Particles: true,
		Effects: []ConsumeEffect{{Type: ConsumeTeleportRandomly, Diameter: 16, Flag: true}, {Type: ConsumeClearAllEffects}}})
	data, _ = s.RawData(CompConsumable)
	head := unhex(t, "3fcccccd"+"01"+"00"+"0b6d696e6563726166743a78"+"00"+"01"+"02")
	if want := append(append([]byte(nil), head...), unhex(t, "03"+"41800000"+"01"+"02")...); !bytes.Equal(data, want) {
		t.Fatalf("consumable: %x, want %x", data, want)
	}
	got, ok = p.rawToOld(CompConsumable, data)
	if want := append(append([]byte(nil), head...), unhex(t, "03"+"41800000"+"02")...); !ok || !bytes.Equal(got, want) {
		t.Errorf("consumable for 26.2: %x, want %x", got, want)
	}
	back, ok = p.rawFromOld(CompConsumable, got)
	if want := append(append([]byte(nil), head...), unhex(t, "03"+"41800000"+"00"+"02")...); !ok || !bytes.Equal(back, want) {
		t.Errorf("consumable from 26.2: %x, want %x", back, want)
	}

	// An inline instrument: 26.3 has durability damage before the description.
	inst := unhex(t, "00"+"00"+"016100"+"40e00000"+"43800000"+"05"+"0800026869")
	got, ok = p.rawToOld(CompInstrument, inst)
	if want := unhex(t, "00"+"00"+"016100"+"40e00000"+"43800000"+"0800026869"); !ok || !bytes.Equal(got, want) {
		t.Errorf("instrument for 26.2: %x, want %x", got, want)
	}
	back, ok = p.rawFromOld(CompInstrument, got)
	if want := unhex(t, "00"+"00"+"016100"+"40e00000"+"43800000"+"00"+"0800026869"); !ok || !bytes.Equal(back, want) {
		t.Errorf("instrument from 26.2: %x, want %x", back, want)
	}

	// An inline trim material has another form: no way between the versions. By id it is kept.
	if _, ok := p.rawToOld(CompTrim, unhex(t, "00"+"0161"+"0800026869"+"01")); ok {
		t.Error("an inline trim material reached 26.2")
	}
	if got, ok := p.rawToOld(CompTrim, []byte{3, 5}); !ok || len(got) != 2 {
		t.Errorf("trim for 26.2: %x %v", got, ok)
	}

	// attack_animation is 26.2's swing_animation; interact_animation has no counterpart.
	swing := v776Comp(t, "minecraft:swing_animation")
	if p.Component(CompAttackAnimation) != swing || p.compsIn[swing] != CompAttackAnimation {
		t.Errorf("attack_animation: 26.2 id %d, swing_animation is %d", p.Component(CompAttackAnimation), swing)
	}
	if p.Component(CompInteractAnimation) >= 0 {
		t.Error("26.2 has interact_animation")
	}
	s = Stack{Count: 1, ID: stone, Raw: []RawComponent{{CompAttackAnimation, []byte{1, 6}}, {CompInteractAnimation, []byte{0, 6}}}}
	var w wire.Writer
	s.EncodeFor(&w, p)
	if want := []byte{1, old("minecraft:stone"), 1, 0, byte(swing), 1, 6}; !bytes.Equal(w.B, want) {
		t.Errorf("animations for 26.2: %x, want %x", w.B, want)
	}

	// Ids inside a value are the version's: a container's items, and their components.
	s = Stack{Count: 1, ID: item("minecraft:shulker_box")}
	sword := Stack{Count: 1, ID: item("minecraft:diamond_sword"), Damage: 5}
	sword.Add(CompDamage)
	s.SetContainer([]Stack{sword, {}, {Count: 2, ID: stone}})
	w.Reset()
	s.EncodeFor(&w, p)
	var back2 Stack
	r := wire.NewReader(w.B)
	back2.DecodeFor(r, p)
	slots, ok := back2.Container()
	if r.Err != nil || !ok || len(slots) != 3 || slots[0].ID != sword.ID || slots[0].Damage != 5 || !slots[1].Empty() ||
		slots[2].ID != stone || slots[2].Count != 2 {
		t.Errorf("container through 26.2: %v %v %+v", r.Err, ok, slots)
	}
	var sw wire.Writer
	sw.VarInt(p.Item(sword.ID))
	if !bytes.Contains(w.B, append(append([]byte{1}, sw.B...), 1, 1, 0, byte(p.Component(CompDamage)), 5)) {
		t.Errorf("the sword in the 26.2 container: %x", w.B)
	}
}

func v776Comp(t testing.TB, name string) int32 {
	id, ok := older[776].builtin["minecraft:data_component_type"][name]
	if !ok {
		t.Fatalf("26.2 has no %s", name)
	}
	return id
}

// The typed values: what is set is what is got, and the encoding is what the wire form reads.
func TestTypedComponents(t *testing.T) {
	stone := mustID(t, "minecraft:stone")
	cases := []struct {
		comp int32
		set  func(s *Stack)
		get  func(s *Stack) (any, bool)
		want any
		hex  string // the encoding, when it is worth pinning
	}{
		{CompProfile, func(s *Stack) { s.SetProfile(&Profile{Name: "Notch"}) },
			func(s *Stack) (any, bool) { return s.Profile() }, &Profile{Name: "Notch"},
			"00" + "01" + "054e6f746368" + "00" + "00" + "00000000"},
		{CompProfile, func(s *Stack) {
			s.SetProfile(&Profile{Complete: true, HasID: true, ID: [16]byte{1, 2, 3}, Name: "A",
				Properties: []ProfileProperty{{Name: "textures", Value: "e30="}, {Name: "x", Value: "y", HasSignature: true, Signature: "sig"}},
				Body:       "minecraft:b", HasModel: true, Model: ModelSlim})
		}, func(s *Stack) (any, bool) { return s.Profile() },
			&Profile{Complete: true, HasID: true, ID: [16]byte{1, 2, 3}, Name: "A",
				Properties: []ProfileProperty{{Name: "textures", Value: "e30="}, {Name: "x", Value: "y", HasSignature: true, Signature: "sig"}},
				Body:       "minecraft:b", HasModel: true, Model: ModelSlim}, ""},
		{CompBannerPatterns, func(s *Stack) {
			s.SetBannerPatterns([]BannerLayer{{Pattern: 4, Color: 14}, {AssetID: "a:b", TranslationKey: "k", Color: 0}})
		}, func(s *Stack) (any, bool) { return s.BannerPatterns() },
			[]BannerLayer{{Pattern: 4, Color: 14}, {AssetID: "a:b", TranslationKey: "k", Color: 0}},
			"02" + "05" + "0e" + "00" + "03613a62" + "016b" + "00"},
		{CompBaseColor, func(s *Stack) { s.SetInt(CompBaseColor, 11) }, func(s *Stack) (any, bool) { return s.Int(CompBaseColor) }, int32(11), "0b"},
		{CompTrim, func(s *Stack) { s.SetTrim(Trim{Material: 2, Pattern: 7}) }, func(s *Stack) (any, bool) { return s.Trim() },
			Trim{2, 7}, "0308"},
		{CompFireworkExplosion, func(s *Stack) {
			s.SetFireworkExplosion(FireworkExplosion{Shape: 2, Colors: []int32{0xff0000}, FadeColors: []int32{1, 2}, Twinkle: true})
		}, func(s *Stack) (any, bool) { return s.FireworkExplosion() },
			FireworkExplosion{Shape: 2, Colors: []int32{0xff0000}, FadeColors: []int32{1, 2}, Twinkle: true},
			"02" + "01" + "00ff0000" + "02" + "00000001" + "00000002" + "00" + "01"},
		{CompFireworks, func(s *Stack) {
			s.SetFireworks(Fireworks{FlightDuration: 3, Explosions: []FireworkExplosion{{Shape: 1, Colors: []int32{5}}}})
		}, func(s *Stack) (any, bool) { return s.Fireworks() },
			Fireworks{FlightDuration: 3, Explosions: []FireworkExplosion{{Shape: 1, Colors: []int32{5}}}},
			"03" + "01" + "01" + "01" + "00000005" + "00" + "00" + "00"},
		{CompWritableBookContent, func(s *Stack) {
			s.SetWritableBook([]BookPage{{Text: "one"}, {Text: "two", HasFiltered: true, Filtered: "t"}})
		}, func(s *Stack) (any, bool) { return s.WritableBook() },
			[]BookPage{{Text: "one"}, {Text: "two", HasFiltered: true, Filtered: "t"}},
			"02" + "036f6e65" + "00" + "0374776f" + "01" + "0174"},
		{CompWrittenBookContent, func(s *Stack) {
			s.SetWrittenBook(&WrittenBook{Title: BookPage{Text: "T"}, Author: "Me", Generation: 1,
				Pages: []WrittenPage{{Text: Text{Text: "p1"}}}, Resolved: true})
		}, func(s *Stack) (any, bool) { return s.WrittenBook() },
			&WrittenBook{Title: BookPage{Text: "T"}, Author: "Me", Generation: 1,
				Pages: []WrittenPage{{Text: Text{Text: "p1"}}}, Resolved: true},
			"0154" + "00" + "024d65" + "01" + "01" + "0800027031" + "00" + "01"},
		{CompBundleContents, func(s *Stack) { s.SetBundleContents([]Stack{{Count: 5, ID: stone}}) },
			func(s *Stack) (any, bool) {
				st, ok := s.BundleContents()
				return len(st) == 1 && st[0].Count == 5 && st[0].ID == stone, ok
			}, true, "01" + "01" + "05" + "00" + "00"},
		{CompChargedProjectiles, func(s *Stack) { s.SetChargedProjectiles([]Stack{{Count: 1, ID: stone}}) },
			func(s *Stack) (any, bool) {
				st, ok := s.ChargedProjectiles()
				return len(st) == 1 && st[0].Count == 1 && st[0].ID == stone, ok
			}, true, ""},
		{CompUseRemainder, func(s *Stack) { s.SetUseRemainder(&Stack{Count: 1, ID: stone}) },
			func(s *Stack) (any, bool) {
				st, ok := s.UseRemainder()
				return st.Count == 1 && st.ID == stone, ok
			}, true, "01" + "01" + "00" + "00"},
		{CompAttributeModifiers, func(s *Stack) {
			s.SetAttributeModifiers([]AttributeModifier{{Attribute: 2, ID: "a:b", Amount: 4, Operation: 0, Slot: 1},
				{Attribute: 3, ID: "c:d", Amount: -1, Operation: 2, Slot: 0, Display: 2, Override: Text{Text: "x"}}})
		}, func(s *Stack) (any, bool) { return s.AttributeModifiers() },
			[]AttributeModifier{{Attribute: 2, ID: "a:b", Amount: 4, Operation: 0, Slot: 1},
				{Attribute: 3, ID: "c:d", Amount: -1, Operation: 2, Slot: 0, Display: 2, Override: Text{Text: "x"}}},
			"02" + "02" + "03613a62" + "4010000000000000" + "00" + "01" + "00" +
				"03" + "03633a64" + "bff0000000000000" + "02" + "00" + "02" + "08000178"},
		{CompFood, func(s *Stack) { s.SetFood(Food{4, 0.3, true}) }, func(s *Stack) (any, bool) { return s.Food() },
			Food{4, 0.3, true}, "04" + "3e99999a" + "01"},
		{CompConsumable, func(s *Stack) {
			s.SetConsumable(&Consumable{Seconds: 1.6, Animation: 1, Sound: Sound{ID: 700}, Particles: true,
				Effects: []ConsumeEffect{{Type: ConsumeApplyEffects, Effects: []Effect{{ID: 1, Amplifier: 2, Duration: 100, ShowIcon: true}}, Probability: 1},
					{Type: ConsumeRemoveEffects, Remove: HolderSet{IDs: []int32{3, 4}}}, {Type: ConsumeClearAllEffects},
					{Type: ConsumeTeleportRandomly, Diameter: 16}, {Type: ConsumePlaySound, Sound: Sound{Name: "a:b", HasRange: true, Range: 8}}}})
		}, func(s *Stack) (any, bool) { return s.Consumable() },
			&Consumable{Seconds: 1.6, Animation: 1, Sound: Sound{ID: 700}, Particles: true,
				Effects: []ConsumeEffect{{Type: ConsumeApplyEffects, Effects: []Effect{{ID: 1, Amplifier: 2, Duration: 100, ShowIcon: true}}, Probability: 1},
					{Type: ConsumeRemoveEffects, Remove: HolderSet{IDs: []int32{3, 4}}}, {Type: ConsumeClearAllEffects},
					{Type: ConsumeTeleportRandomly, Diameter: 16}, {Type: ConsumePlaySound, Sound: Sound{Name: "a:b", HasRange: true, Range: 8}}}}, ""},
		{CompDeathProtection, func(s *Stack) { s.SetDeathProtection([]ConsumeEffect{{Type: ConsumeClearAllEffects}}) },
			func(s *Stack) (any, bool) { return s.DeathProtection() }, []ConsumeEffect{{Type: ConsumeClearAllEffects}}, "0102"},
		{CompUseCooldown, func(s *Stack) { s.SetUseCooldown(UseCooldown{1, "a:b"}) }, func(s *Stack) (any, bool) { return s.UseCooldown() },
			UseCooldown{1, "a:b"}, "3f800000" + "01" + "03613a62"},
		{CompTool, func(s *Stack) {
			s.SetTool(&Tool{Rules: []ToolRule{{Blocks: HolderSet{Tag: "minecraft:mineable/pickaxe"}, HasSpeed: true, Speed: 8, HasCorrect: true, CorrectForDrops: true},
				{Blocks: HolderSet{IDs: []int32{1}}}}, DefaultMiningSpeed: 1, DamagePerBlock: 1})
		}, func(s *Stack) (any, bool) { return s.Tool() },
			&Tool{Rules: []ToolRule{{Blocks: HolderSet{Tag: "minecraft:mineable/pickaxe"}, HasSpeed: true, Speed: 8, HasCorrect: true, CorrectForDrops: true},
				{Blocks: HolderSet{IDs: []int32{1}}}}, DefaultMiningSpeed: 1, DamagePerBlock: 1}, ""},
		{CompWeapon, func(s *Stack) { s.SetWeapon(Weapon{2, 5}) }, func(s *Stack) (any, bool) { return s.Weapon() }, Weapon{2, 5},
			"02" + "40a00000"},
		{CompCanBreak, func(s *Stack) {
			s.SetCanBreak([]BlockPredicate{{Blocks: &HolderSet{IDs: []int32{1, 2}}},
				{Properties: []PropertyMatcher{{Name: "facing", Value: "north"}, {Name: "age", Range: true, HasMin: true, Min: "2"}},
					NBT: []byte{nbtCompound, nbtEnd}}})
		}, func(s *Stack) (any, bool) { return s.CanBreak() },
			[]BlockPredicate{{Blocks: &HolderSet{IDs: []int32{1, 2}}},
				{Properties: []PropertyMatcher{{Name: "facing", Value: "north"}, {Name: "age", Range: true, HasMin: true, Min: "2"}},
					NBT: []byte{nbtCompound, nbtEnd}}},
			"02" + "01" + "03" + "01" + "02" + "00" + "00" + "0000" +
				"00" + "01" + "02" + "06666163696e67" + "01" + "056e6f727468" + "03616765" + "00" + "01" + "0132" + "00" + "01" + "0a00" + "0000"},
		{CompCanPlaceOn, func(s *Stack) { s.SetCanPlaceOn([]BlockPredicate{{Blocks: &HolderSet{Tag: "minecraft:dirt"}}}) },
			func(s *Stack) (any, bool) { return s.CanPlaceOn() }, []BlockPredicate{{Blocks: &HolderSet{Tag: "minecraft:dirt"}}}, ""},
		{CompJukeboxPlayable, func(s *Stack) { s.SetHolder(CompJukeboxPlayable, 5) },
			func(s *Stack) (any, bool) { return s.Holder(CompJukeboxPlayable) }, int32(5), "06"},
		{CompInstrument, func(s *Stack) { s.SetHolder(CompInstrument, 0) },
			func(s *Stack) (any, bool) { return s.Holder(CompInstrument) }, int32(0), "01"},
		{CompLodestoneTracker, func(s *Stack) {
			s.SetLodestoneTracker(LodestoneTracker{HasTarget: true, Dimension: "minecraft:overworld", X: 10, Y: -60, Z: -3, Tracked: true})
		}, func(s *Stack) (any, bool) { return s.LodestoneTracker() },
			LodestoneTracker{HasTarget: true, Dimension: "minecraft:overworld", X: 10, Y: -60, Z: -3, Tracked: true}, ""},
		{CompPotDecorations, func(s *Stack) { s.SetPotDecorations(&[4]Stack{{}, {Count: 1, ID: stone}, {}, {}}) },
			func(s *Stack) (any, bool) {
				d, ok := s.PotDecorations()
				return d[0].Empty() && d[1].ID == stone && d[2].Empty() && d[3].Empty(), ok
			}, true, "00" + "01" + "01" + "01" + "0000" + "00" + "00"},
		{CompSuspiciousStewEffects, func(s *Stack) { s.SetSuspiciousStewEffects([]StewEffect{{14, 160}}) },
			func(s *Stack) (any, bool) { return s.SuspiciousStewEffects() }, []StewEffect{{14, 160}}, "01" + "0e" + "a001"},
		{CompBlockState, func(s *Stack) { s.SetBlockState([]BlockProperty{{"lit", "true"}}) },
			func(s *Stack) (any, bool) { return s.BlockState() }, []BlockProperty{{"lit", "true"}}, "01" + "036c6974" + "0474727565"},
		{CompEntityData, func(s *Stack) {
			s.SetEntityData(EntityData{Type: 3, NBT: []byte{nbtCompound, 1, 0, 1, 'a', 7, nbtEnd}})
		},
			func(s *Stack) (any, bool) { return s.EntityData() }, EntityData{Type: 3, NBT: []byte{nbtCompound, 1, 0, 1, 'a', 7, nbtEnd}}, ""},
		{CompBlockEntityData, func(s *Stack) { s.SetBlockEntityData(EntityData{Type: 7}) },
			func(s *Stack) (any, bool) { return s.BlockEntityData() }, EntityData{Type: 7, NBT: []byte{nbtCompound, nbtEnd}}, "070a00"},
		{CompWolfVariant, func(s *Stack) { s.SetInt(CompWolfVariant, 3) }, func(s *Stack) (any, bool) { return s.Int(CompWolfVariant) }, int32(3), "03"},
		{CompNoteBlockSound, func(s *Stack) { s.SetKey(CompNoteBlockSound, "minecraft:a") },
			func(s *Stack) (any, bool) { return s.Key(CompNoteBlockSound) }, "minecraft:a", ""},
		{CompGlider, func(s *Stack) { s.SetFlag(CompGlider) }, func(s *Stack) (any, bool) { return true, s.Has(CompGlider) }, true, ""},
		{CompRepairable, func(s *Stack) { s.SetHolderSet(CompRepairable, HolderSet{IDs: []int32{stone}}) },
			func(s *Stack) (any, bool) { return s.HolderSet(CompRepairable) }, HolderSet{IDs: []int32{stone}}, "0201"},
		{CompDamageResistant, func(s *Stack) { s.SetHolderSet(CompDamageResistant, HolderSet{Tag: "minecraft:is_fire"}) },
			func(s *Stack) (any, bool) { return s.HolderSet(CompDamageResistant) }, HolderSet{Tag: "minecraft:is_fire"}, ""},
		{CompBucketEntityData, func(s *Stack) { s.SetNBT(CompBucketEntityData, []byte{nbtCompound, nbtEnd}) },
			func(s *Stack) (any, bool) { d, ok := s.RawData(CompBucketEntityData); return hex.EncodeToString(d), ok }, "0a00", ""},
	}
	for _, c := range cases {
		name := componentNames[c.comp]
		s := Stack{Count: 1, ID: stone}
		c.set(&s)
		data, ok := s.RawData(c.comp)
		if !ok || !ValidComponent(c.comp, data) {
			t.Errorf("%s: the encoding %x is not what its wire form reads", name, data)
			continue
		}
		if c.hex != "" && hex.EncodeToString(data) != c.hex {
			t.Errorf("%s: encoded %x, want %s", name, data, c.hex)
		}
		got, ok := c.get(&s)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v (%v), want %+v", name, got, ok, c.want)
		}
		// Through a packet, in both forms.
		var w wire.Writer
		s.EncodeUntrusted(&w)
		var back Stack
		r := wire.NewReader(w.B)
		back.DecodeUntrusted(r)
		got, ok = c.get(&back)
		if r.Err != nil || !ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s through a packet: %v, got %+v (%v)", name, r.Err, got, ok)
		}
		// A getter of another component says no.
		if _, ok := (&Stack{Count: 1, ID: stone}).RawData(c.comp); ok {
			t.Errorf("%s: an empty stack has it", name)
		}
	}
	// Setting again replaces; deleting takes it out; a component with a field is refused.
	s := Stack{Count: 1, ID: stone}
	s.SetInt(CompBaseColor, 1)
	s.SetInt(CompBaseColor, 2)
	if v, _ := s.Int(CompBaseColor); v != 2 || len(s.Raw) != 1 {
		t.Errorf("set twice: %d, %d raw", v, len(s.Raw))
	}
	s.DelRaw(CompBaseColor)
	if s.Has(CompBaseColor) {
		t.Error("deleted and still there")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("SetRaw took a component Stack has a field for")
			}
		}()
		s.SetRaw(CompDamage, []byte{1})
	}()
}

// A hashed stack (container_click) with hashes of any component, the newest ones too.
func TestHashedStackAllComponents(t *testing.T) {
	var w wire.Writer
	w.Bool(true)
	w.VarInt(mustID(t, "minecraft:diamond_sword"))
	w.VarInt(1)
	w.VarInt(ComponentTypes)
	for ct := int32(0); ct < ComponentTypes; ct++ {
		w.VarInt(ct)
		w.Int32(int32(ct) * 77)
	}
	w.VarInt(2)
	w.VarInt(CompWaxed)
	w.VarInt(CompAttributeModifiers)
	w.Byte(0xAB) // what follows the stack in the packet
	for _, p := range []*Proto{nil, For(version.V776)} {
		var h HashedStack
		r := wire.NewReader(w.B)
		h.DecodeFor(r, p)
		if r.Err != nil || r.Len() != 1 || h.Count != 1 {
			t.Errorf("hashed stack: %v, %d left, %+v", r.Err, r.Len(), h)
		}
	}
}

// What a client sends as a component has to be a value of it to be kept.
func TestUntrustedRawIsChecked(t *testing.T) {
	var w wire.Writer
	w.VarInt(1)
	w.VarInt(mustID(t, "minecraft:stone"))
	w.VarInt(2)
	w.VarInt(0)
	w.VarInt(CompFood) // varint, float, flag: 6 bytes, not 2
	w.VarInt(2)
	w.Raw([]byte{4, 1})
	w.VarInt(CompWeapon)
	w.VarInt(5)
	w.Raw([]byte{2, 0x40, 0xa0, 0, 0})
	var s Stack
	r := wire.NewReader(w.B)
	s.DecodeUntrusted(r)
	if r.Err != nil || len(s.Raw) != 1 || s.Raw[0].Type != CompWeapon {
		t.Fatalf("%v, raw %v", r.Err, s.Raw)
	}
	if v, ok := s.Weapon(); !ok || v != (Weapon{2, 5}) {
		t.Errorf("weapon %+v %v", v, ok)
	}
}
