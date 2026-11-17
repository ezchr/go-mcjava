package item

import (
	"math/bits"
	"sync"

	v776 "github.com/ezchr/go-mcjava/v776"
	v777 "github.com/ezchr/go-mcjava/v777"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

// Proto is the item side of a protocol version older than the newest (26.3): its ids of items, data
// component types and the registries stacks refer to (enchantments, potions, mob effects). Stacks
// always hold newest ids; the *For methods turn them into the version's ids when encoding and back
// when decoding, raw components included: their values are written in the version's wire form with
// its ids (schema.go). Components the other version lacks are left out, and so is a component
// whose value names something the other version does not have.
//
// A nil *Proto is the newest version: nothing is remapped.
type Proto struct {
	items, itemsIn       []int32 // newest -> version, version -> newest (-1: none)
	comps, compsIn       []int32
	effects, effectsIn   []int32
	potions, potionsIn   []int32
	enchants, enchantsIn []int32
	names                []string        // the version's item keys by its id
	oldKinds             map[int32]uint8 // wire form of the version's components the newest lacks
	v                    *version.Version
	regs                 sync.Map // registry name -> *registry
	// stackFix is, by newest item id, the item's default max_stack_size where the version's
	// stand-in has another one (0: same), sent as a max_stack_size component.
	stackFix []uint8
}

// older are the built-in and synchronised registries of the older versions.
var older = map[int32]struct {
	builtin map[string]map[string]int32
	synced  map[string][]string
}{
	776: {v776.Builtin, v776.Registries},
}

// renamedComps are components of older versions by the name the newest has them under, with the
// same wire form: 26.2's swing_animation is 26.3's attack_animation (26.3 split interact_animation
// off it).
var renamedComps = map[string]string{
	"minecraft:swing_animation": "minecraft:attack_animation",
}

// registry is the ids of one registry in the newest version and a Proto's: out[newest id] is the
// version's, in[version's id] the newest (-1: none). Both nil: the ids are the same.
type registry struct{ out, in []int32 }

// registry returns the id tables of a registry (by its key, "minecraft:sound_event").
func (p *Proto) registry(name string) *registry {
	if r, ok := p.regs.Load(name); ok {
		return r.(*registry)
	}
	r := p.newRegistry(name)
	p.regs.Store(name, r)
	return r
}

func (p *Proto) newRegistry(name string) *registry {
	switch name {
	case "minecraft:item":
		return &registry{p.items, p.itemsIn}
	case "minecraft:data_component_type":
		return &registry{p.comps, p.compsIn}
	case "minecraft:mob_effect":
		return &registry{p.effects, p.effectsIn}
	case "minecraft:potion":
		return &registry{p.potions, p.potionsIn}
	case "minecraft:enchantment":
		return &registry{p.enchants, p.enchantsIn}
	}
	old := older[p.v.Protocol]
	// The entries by id, of a built-in registry or of one sent in configuration (a registry can
	// be one in a version and the other in the next).
	names := func(builtin map[string]map[string]int32, synced map[string][]string) []string {
		if list, ok := synced[name]; ok {
			return list
		}
		m, ok := builtin[name]
		if !ok {
			return nil
		}
		list := make([]string, len(m))
		for n, id := range m {
			if int(id) < len(list) {
				list[id] = n
			}
		}
		return list
	}
	newest, version := names(v777.Builtin, v777.Registries), names(old.builtin, old.synced)
	if newest == nil && version == nil {
		return &registry{} // not a registry the tables have (an enum, villager types): the same ids
	}
	r := &registry{out: make([]int32, len(newest)), in: make([]int32, len(version))}
	byName := make(map[string]int32, len(version))
	for id, n := range version {
		byName[n] = int32(id)
	}
	newestID := make(map[string]int32, len(newest))
	for id, n := range newest {
		newestID[n] = int32(id)
		if o, ok := byName[n]; ok {
			r.out[id] = o
		} else {
			r.out[id] = -1
		}
	}
	for id, n := range version {
		if nid, ok := newestID[n]; ok {
			r.in[id] = nid
		} else {
			r.in[id] = -1
		}
	}
	// The version package's table knows stand-ins for entries the version lacks (a sound, an
	// entity type): better than leaving the component out.
	if t := p.v.BuiltinTable(name); len(t) == len(r.out) {
		r.out = t
	} else if t := p.v.SyncedTable(name); len(t) == len(r.out) {
		r.out = t
	}
	return r
}

// removedKinds is the wire form of components older versions have and the newest does not, so a
// decoder can skip them.
var removedKinds = map[string]uint8{
	"minecraft:map_color": kindInt32,
}

var (
	protosMu sync.Mutex
	protos   = map[*version.Version]*Proto{}
)

// For returns the item Proto of version v: nil for the newest version (and for nil).
func For(v *version.Version) *Proto {
	if v == nil || v.Native() {
		return nil
	}
	protosMu.Lock()
	defer protosMu.Unlock()
	if p, ok := protos[v]; ok {
		return p
	}
	p := newProto(v)
	protos[v] = p
	return p
}

func newProto(v *version.Version) *Proto {
	old, ok := older[v.Protocol]
	if !ok {
		panic("item: no registries of protocol " + v.Name)
	}
	p := &Proto{
		items:    v.BuiltinTable("minecraft:item"),
		comps:    v.BuiltinTable("minecraft:data_component_type"),
		effects:  v.BuiltinTable("minecraft:mob_effect"),
		potions:  v.BuiltinTable("minecraft:potion"),
		enchants: v.SyncedTable("minecraft:enchantment"),
		oldKinds: map[int32]uint8{},
		v:        v,
	}
	// Reverse tables by name: an id of the old version maps to the newest entry of the same name,
	// never to a newer entry that only falls back to it.
	reverse := func(registry string) []int32 {
		t := make([]int32, len(old.builtin[registry]))
		for i := range t {
			t[i] = -1
		}
		for name, id := range old.builtin[registry] {
			if nid, ok := v777.Builtin[registry][name]; ok {
				t[id] = nid
			}
		}
		return t
	}
	p.itemsIn = reverse("minecraft:item")
	p.compsIn = reverse("minecraft:data_component_type")
	p.effectsIn = reverse("minecraft:mob_effect")
	p.potionsIn = reverse("minecraft:potion")
	p.enchantsIn = make([]int32, len(old.synced["minecraft:enchantment"]))
	for i := range p.enchantsIn {
		p.enchantsIn[i] = -1
	}
	for nid, o := range p.enchants {
		if o >= 0 && int(o) < len(p.enchantsIn) {
			p.enchantsIn[o] = int32(nid)
		}
	}
	// A component that was renamed: the same value under another name.
	for oldName, newName := range renamedComps {
		oid, ok := old.builtin["minecraft:data_component_type"][oldName]
		nid, ok2 := v777.Builtin["minecraft:data_component_type"][newName]
		if ok && ok2 && int(nid) < len(p.comps) && p.comps[nid] < 0 && p.compsIn[oid] < 0 {
			p.comps = append([]int32(nil), p.comps...) // the version package's table is shared
			p.comps[nid], p.compsIn[oid] = oid, nid
		}
	}
	p.names = make([]string, len(p.itemsIn))
	for name, id := range old.builtin["minecraft:item"] {
		p.names[id] = name
	}
	for name, id := range old.builtin["minecraft:data_component_type"] {
		if p.compsIn[id] < 0 {
			p.oldKinds[id] = removedKinds[name]
		}
	}
	oldStack := oldMaxStack[v.Protocol]
	p.stackFix = make([]uint8, Items)
	for id := range p.stackFix {
		if o := mapID(p.items, int32(id)); o >= 0 && int(o) < len(oldStack) && oldStack[o] != defaultMaxStack[id] {
			p.stackFix[id] = defaultMaxStack[id]
		}
	}
	return p
}

// mapID maps id through t (out of range: -1).
func mapID(t []int32, id int32) int32 {
	if uint32(id) >= uint32(len(t)) {
		return -1
	}
	return t[id]
}

// Item is the version's id of a newest item id (-1: none). For a nil Proto it is id.
func (p *Proto) Item(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.items, id)
}

// ItemIn is the newest id of the version's item id (-1: none). For a nil Proto it is id.
func (p *Proto) ItemIn(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.itemsIn, id)
}

// ItemName is the key of the item the version shows for a newest item id: "minecraft:birch_planks"
// for poplar planks in 26.2; "" if none.
func (p *Proto) ItemName(id int32) string {
	if p == nil {
		return Name(id)
	}
	if o := mapID(p.items, id); o >= 0 && int(o) < len(p.names) {
		return p.names[o]
	}
	return ""
}

// Component is the version's id of a newest data component type (-1: the version lacks it).
func (p *Proto) Component(t int32) int32 {
	if p == nil {
		return t
	}
	return mapID(p.comps, t)
}

// Enchantment is the version's id of a newest minecraft:enchantment id (-1: none).
func (p *Proto) Enchantment(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.enchants, id)
}

// EnchantmentIn is the newest id of the version's minecraft:enchantment id (-1: none).
func (p *Proto) EnchantmentIn(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.enchantsIn, id)
}

// Effect is the version's id of a newest minecraft:mob_effect id (-1: none).
func (p *Proto) Effect(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.effects, id)
}

// EffectIn is the newest id of the version's minecraft:mob_effect id (-1: none).
func (p *Proto) EffectIn(id int32) int32 {
	if p == nil {
		return id
	}
	return mapID(p.effectsIn, id)
}

// skipOld skips the value of the version's component type ot that the newest version lacks.
func (p *Proto) skipOld(r *wire.Reader, ot int32) bool { return skipKind(r, p.oldKinds[ot]) }

// encodeOld is encode for an older version.
func (s *Stack) encodeOld(w *wire.Writer, delimited bool, p *Proto) {
	id := mapID(p.items, s.ID)
	if id < 0 {
		w.Byte(0)
		return
	}
	// An item sent as a stand-in with another stack size keeps its own.
	var fix int32
	if f := p.stackFix[s.ID]; f != 0 && !s.Added.Has(CompMaxStackSize) && !s.Removed.Has(CompMaxStackSize) &&
		mapID(p.comps, CompMaxStackSize) >= 0 {
		fix = int32(f)
	}
	// The raw components in the version's form (nil: it has no place for one).
	var conv [][]byte
	if len(s.Raw) > 0 {
		conv = make([][]byte, len(s.Raw))
		for i := range s.Raw {
			conv[i], _ = p.rawToOld(s.Raw[i].Type, s.Raw[i].Data)
		}
	}
	var buf [24]int32
	list := s.oldComponents(buf[:0], p, fix != 0, conv)
	removed := 0
	for wi, word := range s.Removed {
		for word != 0 {
			if mapID(p.comps, int32(wi*64+bits.TrailingZeros64(word))) >= 0 {
				removed++
			}
			word &= word - 1
		}
	}
	w.VarInt(s.Count)
	w.VarInt(id)
	w.VarInt(int32(len(list)))
	w.VarInt(int32(removed))
	added := s.Added.and(Modeled)
	for _, t := range list {
		w.VarInt(p.comps[t])
		start := len(w.B)
		switch {
		case added.Has(t):
			s.valueOld(w, t, p)
		case t == CompMaxStackSize && fix != 0:
			w.VarInt(fix)
		default:
			w.Raw(conv[s.rawIndex(t)])
		}
		if delimited {
			lengthPrefix(w, start)
		}
	}
	for wi, word := range s.Removed {
		for word != 0 {
			if o := mapID(p.comps, int32(wi*64+bits.TrailingZeros64(word))); o >= 0 {
				w.VarInt(o)
			}
			word &= word - 1
		}
	}
}

// oldComponents appends the added components (newest types) the version gets, in the order they are
// written: Order if set, else ascending by the version's id like vanilla. stackFix adds
// max_stack_size (see Proto.stackFix). conv is the raw components in the version's form.
func (s *Stack) oldComponents(list []int32, p *Proto, stackFix bool, conv [][]byte) []int32 {
	added := s.Added.and(Modeled)
	if len(s.Order) > 0 {
		for _, t := range s.Order {
			if added.Has(t) {
				if mapID(p.comps, t) >= 0 {
					list = append(list, t)
				}
			} else if i := s.rawIndex(t); i >= 0 && conv[i] != nil {
				list = append(list, t)
			}
		}
		if stackFix {
			list = append(list, CompMaxStackSize)
		}
		return list
	}
	if stackFix {
		list = append(list, CompMaxStackSize)
	}
	for wi, word := range added {
		for word != 0 {
			t := int32(wi*64 + bits.TrailingZeros64(word))
			word &= word - 1
			if mapID(p.comps, t) >= 0 {
				list = append(list, t)
			}
		}
	}
	for i := range s.Raw {
		if t := s.Raw[i].Type; conv[i] != nil && !added.Has(t) {
			list = append(list, t)
		}
	}
	// Insertion sort by the version's id (a handful of components).
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && p.comps[list[j]] < p.comps[list[j-1]]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list
}

// valueOld writes the value of modeled component t with the version's ids.
func (s *Stack) valueOld(w *wire.Writer, t int32, p *Proto) {
	switch t {
	case CompEnchantments:
		writeEnchantmentsOld(w, s.Enchantments, p)
	case CompStoredEnchantments:
		writeEnchantmentsOld(w, s.StoredEnchantments, p)
	case CompTooltipDisplay:
		w.Bool(s.TooltipDisplay.HideTooltip)
		n := 0
		for _, h := range s.TooltipDisplay.Hidden {
			if mapID(p.comps, h) >= 0 {
				n++
			}
		}
		w.VarInt(int32(n))
		for _, h := range s.TooltipDisplay.Hidden {
			if o := mapID(p.comps, h); o >= 0 {
				w.VarInt(o)
			}
		}
	case CompPotionContents:
		pc := &s.PotionContents
		potion := int32(-1)
		if pc.HasPotion {
			potion = mapID(p.potions, pc.Potion)
		}
		w.Bool(potion >= 0)
		if potion >= 0 {
			w.VarInt(potion)
		}
		w.Bool(pc.HasCustomColor)
		if pc.HasCustomColor {
			w.Int32(pc.CustomColor)
		}
		n := 0
		for i := range pc.Effects {
			if mapID(p.effects, pc.Effects[i].ID) >= 0 {
				n++
			}
		}
		w.VarInt(int32(n))
		for i := range pc.Effects {
			if o := mapID(p.effects, pc.Effects[i].ID); o >= 0 {
				w.VarInt(o)
				writeEffectDetails(w, &pc.Effects[i])
			}
		}
		w.Bool(pc.HasCustomName)
		if pc.HasCustomName {
			w.String(pc.CustomName)
		}
	default:
		s.value(w, t)
	}
}

func writeEnchantmentsOld(w *wire.Writer, e []Enchantment, p *Proto) {
	n := 0
	for _, en := range e {
		if mapID(p.enchants, en.ID) >= 0 {
			n++
		}
	}
	w.VarInt(int32(n))
	for _, en := range e {
		if o := mapID(p.enchants, en.ID); o >= 0 {
			w.VarInt(o)
			w.VarInt(en.Level)
		}
	}
}

// fromOld turns the version's ids in the value of modeled component t, just read, into the newest
// ids, leaving out entries the newest version lacks.
func (s *Stack) fromOld(t int32, p *Proto) {
	switch t {
	case CompEnchantments:
		s.Enchantments = enchantmentsFromOld(s.Enchantments, p)
	case CompStoredEnchantments:
		s.StoredEnchantments = enchantmentsFromOld(s.StoredEnchantments, p)
	case CompTooltipDisplay:
		h := s.TooltipDisplay.Hidden[:0]
		for _, c := range s.TooltipDisplay.Hidden {
			if n := mapID(p.compsIn, c); n >= 0 {
				h = append(h, n)
			}
		}
		s.TooltipDisplay.Hidden = h
	case CompPotionContents:
		pc := &s.PotionContents
		if pc.HasPotion {
			if pc.Potion = mapID(p.potionsIn, pc.Potion); pc.Potion < 0 {
				pc.HasPotion, pc.Potion = false, 0
			}
		}
		e := pc.Effects[:0]
		for _, ef := range pc.Effects {
			if ef.ID = mapID(p.effectsIn, ef.ID); ef.ID >= 0 {
				e = append(e, ef)
			}
		}
		pc.Effects = e
	}
}

func enchantmentsFromOld(e []Enchantment, p *Proto) []Enchantment {
	out := e[:0]
	for _, en := range e {
		if en.ID = mapID(p.enchantsIn, en.ID); en.ID >= 0 {
			out = append(out, en)
		}
	}
	return out
}
