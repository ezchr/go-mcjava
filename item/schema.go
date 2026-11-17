package item

import (
	"errors"
	"fmt"

	"github.com/ezchr/go-mcjava/wire"
)

// The wire form of every data component, as the 26.3 client reads it (the STREAM_CODEC each type
// is registered with in DataComponents), described as a small tree of nodes. One walker reads a
// value by its tree and can write it again as it goes, for the same version or the other one:
//
//   - a component Stack has no field for is read by its tree and kept as its bytes (Raw);
//   - for an older client the bytes are written again with that version's ids (items, sounds,
//     blocks, entity types... wherever the tree says an id is one of a registry) and in that
//     version's form where it differs (see the nodes onlyNew, oldNew and potDecor);
//   - what a client of an older version sends is turned into the newest form the same way.
//
// A value that names something the other version does not have (an id without a counterpart) is
// "lost": the component it is in is left out, the rest of the stack is kept.
//
// 26.2 differs from 26.3 in: consume effect teleport_randomly (26.3 has a flag more), an inline
// instrument (26.3 has durability damage), an inline trim material (asset name and overrides
// against a palette), pot_decorations (a list of item ids against four optional stacks), the
// component swing_animation (attack_animation in 26.3) and the components 26.3 added. Sources:
// the 26.3 client's classes, and MCProtocolLib's readers of both versions.

type kind uint8

const (
	kVarInt kind = iota + 1
	kInt32
	kInt64
	kFloat
	kDouble
	kBool
	kString
	kUUID
	kNBT  // one network NBT tag: a text component, a compound, a list
	kUnit // nothing
	kID   // a varint that is an id of registry reg
	kOpt  // a flag, then sub[0] when set
	kList // a count, then sub[0] that many times (at most max)
	kSeq  // sub in order
	kRep  // sub[0] max times, no count
	// kHolder is a holder of registry reg: 0 and the value itself (sub[0]), or the id + 1.
	kHolder
	// kHolderSet is a set of entries of registry reg: 0 and a tag name, or the count + 1 and the ids.
	kHolderSet
	kTemplate // an ItemStackTemplate: item, count, component patch
	kUnion    // a varint that picks one of sub
	kIf       // a flag: sub[0] when set, else sub[1]
	kCompType // a varint that is a data component type
	kExact    // a list of (component type, that component's value)
	// kOnlyNew is sub[0] in the newest version and nothing in an older one; def is what the
	// newest gets for a value that came from an older one.
	kOnlyNew
	// kOldNew is sub[0] in the newest version and sub[1] in an older one, with no way from one to
	// the other: the value is lost between versions.
	kOldNew
	kPotDecor // pot_decorations: four optional templates; in 26.2 a list of item ids
)

type node struct {
	k   kind
	reg string
	sub []*node
	max int
	def []byte
}

var (
	nVarInt = &node{k: kVarInt}
	nInt32  = &node{k: kInt32}
	nInt64  = &node{k: kInt64}
	nFloat  = &node{k: kFloat}
	nDouble = &node{k: kDouble}
	nBool   = &node{k: kBool}
	nString = &node{k: kString}
	nUUID   = &node{k: kUUID}
	nNBT    = &node{k: kNBT}
	nUnit   = &node{k: kUnit}
	nTmpl   = &node{k: kTemplate}
	nCompT  = &node{k: kCompType}
)

func id(reg string) *node            { return &node{k: kID, reg: "minecraft:" + reg} }
func opt(n *node) *node              { return &node{k: kOpt, sub: []*node{n}} }
func list(n *node) *node             { return &node{k: kList, sub: []*node{n}, max: maxList} }
func listMax(n *node, max int) *node { return &node{k: kList, sub: []*node{n}, max: max} }
func seq(n ...*node) *node           { return &node{k: kSeq, sub: n} }
func rep(n *node, times int) *node   { return &node{k: kRep, sub: []*node{n}, max: times} }
func holder(reg string, inline *node) *node {
	return &node{k: kHolder, reg: "minecraft:" + reg, sub: []*node{inline}}
}
func holderSet(reg string) *node         { return &node{k: kHolderSet, reg: "minecraft:" + reg} }
func union(n ...*node) *node             { return &node{k: kUnion, sub: n} }
func either(yes, no *node) *node         { return &node{k: kIf, sub: []*node{yes, no}} }
func onlyNew(n *node, def ...byte) *node { return &node{k: kOnlyNew, sub: []*node{n}, def: def} }
func oldNew(newest, old *node) *node     { return &node{k: kOldNew, sub: []*node{newest, old}} }

// Shared shapes.
var (
	// A sound event: a registry sound, or its name and an optional fixed range.
	nSound = holder("sound_event", seq(nString, opt(nFloat)))
	// MobEffectInstance.Details: amplifier, duration, ambient, particles, icon, a hidden effect.
	nEffectDetails = func() *node {
		n := &node{k: kSeq}
		n.sub = []*node{nVarInt, nVarInt, nBool, nBool, nBool, opt(n)}
		return n
	}()
	nEffect = seq(id("mob_effect"), nEffectDetails)
	// ConsumeEffect, by consume_effect_type: apply_effects, remove_effects, clear_all_effects,
	// teleport_randomly, play_sound.
	nConsumeEffect = union(
		seq(list(nEffect), nFloat),
		holderSet("mob_effect"),
		nUnit,
		seq(nFloat, onlyNew(nBool, 0)),
		nSound,
	)
	nFilterString = seq(nString, opt(nString)) // Filterable<String>: the text and a filtered one
	nFilterText   = seq(nNBT, opt(nNBT))
	nProperties   = listMax(seq(nString, nString, opt(nString)), 16)
	nExplosion    = seq(nVarInt, list(nInt32), list(nInt32), nBool, nBool)
	nEntityData   = seq(id("entity_type"), nNBT)
	// BlockPredicate: blocks, state properties (a value, or a range of two optional bounds), NBT,
	// component matchers (exact values, then predicates as NBT keyed by a predicate type or a
	// component type).
	nBlockPredicate = seq(
		opt(holderSet("block")),
		opt(list(seq(nString, either(nString, seq(opt(nString), opt(nString)))))),
		opt(nNBT),
		&node{k: kExact},
		listMax(seq(either(nVarInt, nCompT), nNBT), 64),
	)
	// An inline trim material: the palette and a description; 26.2 had an asset name, overrides
	// by armour material and the description.
	nTrimMaterial = holder("trim_material", oldNew(seq(nString, nNBT), seq(nString, list(seq(nString, nString)), nNBT)))
	nResInt       = either(nInt32, nString) // ResolvableInt: a constant or a reference
	nResFloat     = either(nFloat, nString)
	nSignText     = seq(rep(nNBT, 4), opt(rep(nNBT, 4)), nVarInt, nBool)
	nSwing        = seq(nVarInt, nVarInt)
	nKinetic      = seq(nVarInt, nFloat, nFloat)
)

// schemas is the wire form of each component, by its name without "minecraft:".
var schemas = map[string]*node{
	"custom_data":           nNBT,
	"max_stack_size":        nVarInt,
	"max_damage":            nVarInt,
	"damage":                nVarInt,
	"unbreakable":           nUnit,
	"use_effects":           seq(nBool, nBool, nFloat),
	"custom_name":           nNBT,
	"minimum_attack_charge": nFloat,
	"damage_type":           id("damage_type"),
	"item_name":             nNBT,
	"item_model":            nString,
	"lore":                  listMax(nNBT, 256),
	"rarity":                nVarInt,
	"enchantments":          list(seq(id("enchantment"), nVarInt)),
	"can_place_on":          list(nBlockPredicate),
	"can_break":             list(nBlockPredicate),
	// attribute, modifier (id, amount, operation), slot group, display (default, hidden, override
	// with a text).
	"attribute_modifiers":        list(seq(id("attribute"), nString, nDouble, nVarInt, nVarInt, union(nUnit, nUnit, nNBT))),
	"custom_model_data":          seq(list(nFloat), list(nBool), list(nString), list(nInt32)),
	"tooltip_display":            seq(nBool, list(nCompT)),
	"repair_cost":                nVarInt,
	"creative_slot_lock":         nUnit,
	"enchantment_glint_override": nBool,
	"intangible_projectile":      nNBT,
	"food":                       seq(nVarInt, nFloat, nBool),
	"consumable":                 seq(nFloat, nVarInt, nSound, nBool, list(nConsumeEffect)),
	"use_remainder":              nTmpl,
	"use_cooldown":               seq(nFloat, opt(nString)),
	"damage_resistant":           holderSet("damage_type"),
	"tool":                       seq(list(seq(holderSet("block"), opt(nFloat), opt(nBool))), nFloat, nVarInt, nBool),
	"weapon":                     seq(nVarInt, nFloat),
	"attack_range":               rep(nFloat, 6),
	"enchantable":                nVarInt,
	// slot, equip sound, asset, camera overlay, allowed entities, five flags, shearing sound.
	"equippable": seq(nVarInt, nSound, opt(nString), opt(nString), opt(holderSet("entity_type")),
		nBool, nBool, nBool, nBool, nBool, nSound),
	"repairable":       holderSet("item"),
	"glider":           nUnit,
	"tooltip_style":    nString,
	"death_protection": list(nConsumeEffect),
	// delay, cooldown scale, damage reductions, item damage (threshold, base, factor), bypassed
	// by, block sound, disable sound.
	"blocks_attacks": seq(nFloat, nFloat, list(seq(nFloat, opt(holderSet("damage_type")), nFloat, nFloat)),
		nFloat, nFloat, nFloat, opt(holderSet("damage_type")), opt(nSound), opt(nSound)),
	"piercing_weapon": seq(nBool, nBool, opt(nSound), opt(nSound)),
	"kinetic_weapon": seq(nVarInt, nVarInt, opt(nKinetic), opt(nKinetic), opt(nKinetic), nFloat, nFloat,
		opt(nSound), opt(nSound)),
	"attack_animation":        nSwing,
	"interact_animation":      nSwing,
	"additional_trade_cost":   nVarInt,
	"block_transformer":       id("block_transformer"),
	"villager_food":           nVarInt,
	"stored_enchantments":     list(seq(id("enchantment"), nVarInt)),
	"dye":                     nVarInt,
	"dyed_color":              nInt32,
	"map_id":                  nVarInt,
	"map_decorations":         nNBT,
	"map_post_processing":     nVarInt,
	"charged_projectiles":     listMax(nTmpl, 1024),
	"bundle_contents":         list(nTmpl),
	"potion_contents":         seq(opt(id("potion")), opt(nInt32), list(nEffect), opt(nString)),
	"potion_duration_scale":   nFloat,
	"suspicious_stew_effects": list(seq(id("mob_effect"), nVarInt)),
	"writable_book_content":   list(nFilterString),
	// title, author, generation, pages, resolved.
	"written_book_content": seq(nFilterString, nString, nVarInt, list(nFilterText), nBool),
	"trim":                 seq(nTrimMaterial, holder("trim_pattern", seq(nString, nNBT, nBool))),
	"debug_stick_state":    nNBT,
	"entity_data":          nEntityData,
	"bucket_entity_data":   nNBT,
	"block_entity_data":    seq(id("block_entity_type"), nNBT),
	// An inline instrument: sound, use duration, range, durability damage (not in 26.2), description.
	"instrument":               holder("instrument", seq(nSound, nFloat, nFloat, onlyNew(nVarInt, 0), nNBT)),
	"provides_trim_material":   nTrimMaterial,
	"ominous_bottle_amplifier": nVarInt,
	"jukebox_playable":         holder("jukebox_song", seq(nSound, nNBT, nFloat, nVarInt)),
	"provides_banner_patterns": holderSet("banner_pattern"),
	"recipes":                  nNBT,
	"lodestone_tracker":        seq(opt(seq(nString, nInt64)), nBool),
	"firework_explosion":       nExplosion,
	"fireworks":                seq(nVarInt, list(nExplosion)),
	// A whole game profile (id, name, properties) or a partial one (name, id, properties), then
	// the skin patch: body, cape and elytra textures and the model.
	"profile": seq(either(seq(nUUID, nString, nProperties), seq(opt(nString), opt(nUUID), nProperties)),
		opt(nString), opt(nString), opt(nString), opt(nBool)),
	"note_block_sound":    nString,
	"banner_patterns":     list(seq(holder("banner_pattern", seq(nString, nString)), nVarInt)),
	"base_color":          nVarInt,
	"pot_decorations":     {k: kPotDecor},
	"container":           listMax(opt(nTmpl), 256),
	"block_state":         list(seq(nString, nString)),
	"bees":                list(seq(nEntityData, nVarInt, nVarInt)),
	"sulfur_cube_content": nTmpl,
	"lock":                nNBT,
	"container_loot":      nNBT,
	"break_sound":         nSound,
	"compostable":         nResInt,
	"cooking_fuel":        seq(nResInt, nResFloat),
	"brewing_fuel":        seq(nResInt, nResFloat),
	"mob_visibility":      seq(holderSet("entity_type"), nFloat),

	"villager/variant":            id("villager_type"),
	"wolf/variant":                id("wolf_variant"),
	"wolf/sound_variant":          id("wolf_sound_variant"),
	"wolf/collar":                 nVarInt,
	"fox/variant":                 nVarInt,
	"salmon/size":                 nVarInt,
	"parrot/variant":              nVarInt,
	"tropical_fish/pattern":       nVarInt,
	"tropical_fish/base_color":    nVarInt,
	"tropical_fish/pattern_color": nVarInt,
	"mooshroom/variant":           nVarInt,
	"rabbit/variant":              nVarInt,
	"pig/variant":                 id("pig_variant"),
	"pig/sound_variant":           id("pig_sound_variant"),
	"cow/variant":                 id("cow_variant"),
	"cow/sound_variant":           id("cow_sound_variant"),
	"chicken/variant":             id("chicken_variant"),
	"chicken/sound_variant":       id("chicken_sound_variant"),
	"zombie_nautilus/variant":     id("zombie_nautilus_variant"),
	"frog/variant":                id("frog_variant"),
	"horse/variant":               nVarInt,
	// width, height, asset, title, author.
	"painting/variant":         holder("painting_variant", seq(nVarInt, nVarInt, nString, opt(nNBT), opt(nNBT))),
	"llama/variant":            nVarInt,
	"axolotl/variant":          nVarInt,
	"cat/variant":              id("cat_variant"),
	"cat/sound_variant":        id("cat_sound_variant"),
	"cat/collar":               nVarInt,
	"sheep/color":              nVarInt,
	"shulker/color":            nVarInt,
	"provides_pottery_pattern": id("decorated_pot_pattern"),
	"sign_text_front":          nSignText,
	"sign_text_back":           nSignText,
	"waxed":                    nUnit,
	"cushion/color":            nVarInt,
}

// compSchemas is schemas by component type.
var compSchemas = func() (t [ComponentTypes]*node) {
	for i, name := range componentNames {
		t[i] = schemas[name[len("minecraft:"):]]
	}
	return t
}()

// errLost marks a value the other version has no place for.
var errLost = errors.New("item: no counterpart in the other version")

const maxTemplateDepth = 64

// xcode walks values: it reads them from r in the wire form of version from (nil: the newest)
// and, when w is set, writes them to w in the form of version to.
type xcode struct {
	r        *wire.Reader
	w        *wire.Writer
	from, to *Proto
	lost     bool // something read has no counterpart in the other version
	depth    int
}

func (x *xcode) same() bool { return x.from == x.to }

// mapID turns an id of registry reg from the source version's into the target's.
func (x *xcode) mapID(reg string, v int32) int32 {
	if x.same() {
		return v
	}
	var t []int32
	if x.from == nil {
		t = x.to.registry(reg).out
	} else {
		t = x.from.registry(reg).in
	}
	if t == nil {
		return v // the same ids in both versions
	}
	o := mapID(t, v)
	if o < 0 {
		x.lost = true
		return 0
	}
	return o
}

// silent runs f without writing.
func (x *xcode) silent(f func()) {
	w, lost := x.w, x.lost
	x.w = nil
	f()
	x.w, x.lost = w, lost
}

func (x *xcode) varint(v int32) {
	if x.w != nil {
		x.w.VarInt(v)
	}
}

// copy writes what was read since start.
func (x *xcode) copy(start int) {
	if x.w != nil && x.r.Err == nil {
		x.w.Raw(x.r.B[start:x.r.Off])
	}
}

func (x *xcode) walk(n *node) {
	r := x.r
	if r.Err != nil {
		return
	}
	start := r.Off
	switch n.k {
	case kVarInt:
		r.VarInt()
		x.copy(start)
	case kInt32, kFloat:
		take(r, 4)
		x.copy(start)
	case kInt64, kDouble:
		take(r, 8)
		x.copy(start)
	case kBool:
		take(r, 1)
		x.copy(start)
	case kString:
		take(r, length(r, 32767*4, 1))
		x.copy(start)
	case kUUID:
		take(r, 16)
		x.copy(start)
	case kNBT:
		skipNBT(r)
		x.copy(start)
	case kUnit:
	case kID:
		x.varint(x.mapID(n.reg, r.VarInt()))
	case kCompType:
		x.varint(x.mapID("minecraft:data_component_type", r.VarInt()))
	case kOpt:
		if b := r.Byte(); r.Err == nil {
			x.copy(start)
			if b != 0 {
				x.walk(n.sub[0])
			}
		}
	case kList:
		c := length(r, n.max, 0)
		x.copy(start)
		for i := 0; i < c && r.Err == nil; i++ {
			before := r.Off
			x.walk(n.sub[0])
			if r.Off == before && c > 1024 {
				// Elements of no bytes: a count is all there is to them.
				fail(r, fmt.Errorf("%w: list of %d empty values", ErrInvalid, c))
			}
		}
	case kSeq:
		for _, s := range n.sub {
			x.walk(s)
		}
	case kRep:
		for range n.max {
			x.walk(n.sub[0])
		}
	case kHolder:
		v := r.VarInt()
		if v == 0 {
			x.varint(0)
			x.walk(n.sub[0])
		} else {
			x.varint(x.mapID(n.reg, v-1) + 1)
		}
	case kHolderSet:
		c := r.VarInt()
		if c == 0 {
			x.varint(0)
			s := r.Off
			take(r, length(r, 32767*4, 1))
			x.copy(s)
			return
		}
		if c < 0 || int(c-1) > r.Len() {
			fail(r, fmt.Errorf("%w: holder set of %d", ErrInvalid, c))
			return
		}
		x.varint(c)
		for i := int32(1); i < c && r.Err == nil; i++ {
			x.varint(x.mapID(n.reg, r.VarInt()))
		}
	case kTemplate:
		x.varint(x.mapID("minecraft:item", r.VarInt()))
		x.varint(r.VarInt())
		x.patch()
	case kUnion:
		d := r.VarInt()
		if r.Err != nil {
			return
		}
		if uint32(d) >= uint32(len(n.sub)) {
			fail(r, fmt.Errorf("%w: variant %d", ErrInvalid, d))
			return
		}
		x.varint(d)
		x.walk(n.sub[d])
	case kIf:
		b := r.Byte()
		x.copy(start)
		if b != 0 {
			x.walk(n.sub[0])
		} else {
			x.walk(n.sub[1])
		}
	case kExact:
		c := length(r, ComponentTypes, 1)
		x.varint(int32(c))
		for i := 0; i < c && r.Err == nil; i++ {
			x.component(r.VarInt())
		}
	case kOnlyNew:
		switch {
		case x.from != nil: // the source has no such value
			if x.to == nil && x.w != nil {
				x.w.Raw(n.def)
			}
		case x.to == nil:
			x.walk(n.sub[0])
		default:
			x.silent(func() { x.walk(n.sub[0]) })
		}
	case kOldNew:
		form := n.sub[0]
		if x.from != nil {
			form = n.sub[1]
		}
		if x.same() {
			x.walk(form)
		} else {
			x.silent(func() { x.walk(form) })
			x.lost = true
		}
	case kPotDecor:
		x.potDecorations()
	}
}

// component walks a component's type (as read, the source version's) and its value, inside
// another value: a type or a value the target version lacks makes the whole value lost.
func (x *xcode) component(t int32) {
	r := x.r
	if r.Err != nil {
		return
	}
	nt := t
	if x.from != nil {
		if nt = mapID(x.from.compsIn, t); nt < 0 {
			x.lost = true
			if !x.from.skipOld(r, t) {
				fail(r, fmt.Errorf("%w: old component type %d", ErrUnknownComponent, t))
			}
			return
		}
	}
	if uint32(nt) >= ComponentTypes || compSchemas[nt] == nil {
		fail(r, fmt.Errorf("%w: component type %d", ErrInvalid, t))
		return
	}
	ot := nt
	if x.to != nil {
		if ot = mapID(x.to.comps, nt); ot < 0 {
			x.lost = true
			ot = 0
		}
	}
	x.varint(ot)
	x.walk(compSchemas[nt])
}

// patch walks a component patch (of a template): the components the target version has no place
// for are left out of it.
func (x *xcode) patch() {
	r := x.r
	pos, neg := r.VarInt(), r.VarInt()
	if r.Err != nil {
		return
	}
	if pos < 0 || neg < 0 || pos > 2*ComponentTypes || neg > 2*ComponentTypes || int(pos)+int(neg) > r.Len() {
		fail(r, fmt.Errorf("%w: %d/%d components", ErrInvalid, pos, neg))
		return
	}
	if x.depth++; x.depth > maxTemplateDepth {
		fail(r, fmt.Errorf("%w: stacks nested too deep", ErrInvalid))
		return
	}
	defer func() { x.depth-- }()
	if x.w == nil || x.same() {
		// Nothing can be lost: straight through.
		x.varint(pos)
		x.varint(neg)
		for i := int32(0); i < pos && r.Err == nil; i++ {
			x.component(r.VarInt())
		}
		for i := int32(0); i < neg && r.Err == nil; i++ {
			x.varint(r.VarInt())
		}
		return
	}
	out, lost := x.w, x.lost
	var body wire.Writer
	x.w = &body
	kept := int32(0)
	for i := int32(0); i < pos && r.Err == nil; i++ {
		mark := len(body.B)
		x.lost = false
		x.component(r.VarInt())
		if x.lost {
			body.B = body.B[:mark]
		} else {
			kept++
		}
	}
	removed := int32(0)
	for i := int32(0); i < neg && r.Err == nil; i++ {
		x.lost = false
		if t := x.mapID("minecraft:data_component_type", r.VarInt()); !x.lost {
			body.VarInt(t)
			removed++
		}
	}
	x.w, x.lost = out, lost
	if r.Err != nil {
		return
	}
	x.w.VarInt(kept)
	x.w.VarInt(removed)
	x.w.Raw(body.B)
}

// potDecorations walks pot_decorations: the back, left, right and front of a decorated pot, each
// an optional stack; 26.2 has a list of up to four items, a brick for a plain side.
func (x *xcode) potDecorations() {
	r := x.r
	four := rep(opt(nTmpl), 4)
	old := listMax(id("item"), 4)
	switch {
	case x.same() && x.from == nil:
		x.walk(four)
	case x.same():
		x.walk(old)
	case x.from == nil: // to the old form
		brick, _ := ByName("minecraft:brick")
		x.varint(4)
		for range 4 {
			item := brick
			if r.Byte() != 0 {
				item = r.VarInt()
				r.VarInt()
				x.silent(x.patch)
			}
			x.varint(x.mapID("minecraft:item", item))
		}
	case x.w == nil:
		x.walk(old)
	default: // from the old form
		c := length(r, 4, 1)
		for i := range 4 {
			if i >= c {
				x.w.Byte(0)
				continue
			}
			x.w.Byte(1)
			x.varint(x.mapID("minecraft:item", r.VarInt()))
			x.w.VarInt(1)
			x.w.VarInt(0)
			x.w.VarInt(0)
		}
	}
}

// skipComponent skips the value of component t (a newest type) in the wire form of p's version.
// false: the package has no form for t.
func skipComponent(r *wire.Reader, t int32, p *Proto) bool {
	if uint32(t) >= ComponentTypes || compSchemas[t] == nil {
		return false
	}
	x := xcode{r: r, from: p, to: p}
	x.walk(compSchemas[t])
	return true
}

// ValidComponent reports whether data is exactly one value of component t in the newest wire
// form: what a RawComponent's Data has to be.
func ValidComponent(t int32, data []byte) bool {
	r := wire.Reader{B: data}
	return skipComponent(&r, t, nil) && r.Err == nil && r.Len() == 0
}

// convert writes the value of component t in data, in the form of version from, in the form of
// version to. false: the value is not valid or has no counterpart there.
func convert(t int32, data []byte, from, to *Proto) ([]byte, bool) {
	if uint32(t) >= ComponentTypes || compSchemas[t] == nil {
		return nil, false
	}
	r := wire.Reader{B: data}
	var w wire.Writer
	x := xcode{r: &r, w: &w, from: from, to: to}
	x.walk(compSchemas[t])
	if r.Err != nil || r.Len() != 0 || x.lost {
		return nil, false
	}
	if w.B == nil {
		w.B = []byte{} // a component without a value
	}
	return w.B, true
}

// rawToOld is the value of raw component t for the version (false: it has none).
func (p *Proto) rawToOld(t int32, data []byte) ([]byte, bool) {
	if mapID(p.comps, t) < 0 {
		return nil, false
	}
	return convert(t, data, nil, p)
}

// rawFromOld is the newest form of the version's value of component t (a newest type).
func (p *Proto) rawFromOld(t int32, data []byte) ([]byte, bool) { return convert(t, data, p, nil) }
