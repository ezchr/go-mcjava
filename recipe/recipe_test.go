package recipe

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/ezchr/go-mcjava/item"
	"github.com/ezchr/go-mcjava/version"
	"github.com/ezchr/go-mcjava/wire"
)

func id(t testing.TB, name string) int32 {
	t.Helper()
	i, ok := item.ByName("minecraft:" + name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return i
}

func varint(v int32) string { return hex.EncodeToString(wire.AppendVarInt(nil, v)) }

// str is a string as the wire has it: its length and its bytes.
func str(s string) string { return varint(int32(len(s))) + hex.EncodeToString([]byte(s)) }

// The registry ids are what the 26.3 client registers, in its order (SlotDisplays, RecipeDisplays
// and RecipeBookCategories bootstrap).
func TestRegistryIDs(t *testing.T) {
	if SlotItem != 4 || SlotItemStack != 5 || SlotTag != 6 || SlotComposite != 10 || slotKinds != 11 {
		t.Error("slot display ids")
	}
	if CraftingShaped != 1 || Smithing != 4 || displayKinds != 5 {
		t.Error("recipe display ids")
	}
	if CraftingMisc != 3 || Campfire != 12 || categories != 13 {
		t.Error("recipe book category ids")
	}
	if SlotWithRemainder.Name() != "minecraft:with_remainder" || Furnace.Name() != "minecraft:furnace" ||
		StonecutterCategory.Name() != "minecraft:stonecutter" || SlotKind(99).Name() != "" {
		t.Error("names")
	}
}

// TestShapedBytes: a shaped recipe's entry, byte for byte as the client's codecs read it
// (RecipeDisplayEntry: id, display, group, category, requirements; then the entry's flags).
func TestShapedBytes(t *testing.T) {
	planks, stick, table, pick := id(t, "oak_planks"), id(t, "stick"), id(t, "crafting_table"), id(t, "wooden_pickaxe")
	birch := id(t, "birch_planks")
	e := Entry{
		ID: 300,
		Display: Shaped(3, 3, []Slot{
			Tag("minecraft:planks"), Items(planks, birch), Item(planks),
			Empty(), Item(stick), Empty(),
			Empty(), Item(stick), Empty(),
		}, Item(pick), Item(table)),
		Group: 4, HasGroup: true,
		Category:     CraftingEquipment,
		Requirements: []HolderSet{{Tag: "minecraft:planks"}, {IDs: []int32{planks, birch}}, {IDs: []int32{stick}}},
		Flags:        FlagHighlight,
	}
	want := varint(300) + // id
		"01" + "03" + "03" + "09" + // crafting_shaped, 3 x 3, nine ingredients
		"06" + "00" + str("minecraft:planks") + // tag: a holder set that is a tag
		"0a" + "02" + "04" + varint(planks) + "04" + varint(birch) + // composite of two items
		"04" + varint(planks) +
		"00" + "04" + varint(stick) + "00" +
		"00" + "04" + varint(stick) + "00" +
		"04" + varint(pick) + // result
		"04" + varint(table) + // station
		"05" + // group 4
		"02" + // crafting_equipment
		"01" + "03" + // requirements: three
		"00" + str("minecraft:planks") +
		"03" + varint(planks) + varint(birch) +
		"02" + varint(stick) +
		"02" // flags
	w := &wire.Writer{}
	e.EncodeFor(w, nil)
	if got := hex.EncodeToString(w.B); got != want {
		t.Fatalf("entry:\n got  %s\n want %s", got, want)
	}

	// 26.2 writes a tag display as the tag's key, and its own item ids.
	p := item.For(version.V776)
	want262 := "06" + str("minecraft:planks")
	w.Reset()
	e.Display.Ingredients[0].EncodeFor(w, p)
	if got := hex.EncodeToString(w.B); got != want262 {
		t.Errorf("26.2 tag display: %s, want %s", got, want262)
	}
	w.Reset()
	s := Item(pick)
	s.EncodeFor(w, p)
	if got := hex.EncodeToString(w.B); got != "04"+varint(p.Item(pick)) {
		t.Errorf("26.2 item display: %s", got)
	}
	// A set of items in a tag display: 26.3 has it, 26.2 gets the items in turn.
	set := Slot{Kind: SlotTag, Set: HolderSet{IDs: []int32{planks, birch}}}
	w.Reset()
	set.EncodeFor(w, nil)
	if got := hex.EncodeToString(w.B); got != "06"+"03"+varint(planks)+varint(birch) {
		t.Errorf("set display: %s", got)
	}
	w.Reset()
	set.EncodeFor(w, p)
	if got := hex.EncodeToString(w.B); got != "0a"+"02"+"04"+varint(p.Item(planks))+"04"+varint(p.Item(birch)) {
		t.Errorf("26.2 set display: %s", got)
	}
}

// TestStackDisplay: an item_stack display is an ItemStackTemplate: item, count, patch.
func TestStackDisplay(t *testing.T) {
	paper := id(t, "paper")
	st := &item.Stack{ID: paper, Count: 4, ItemModel: "delta:ruby"}
	st.Add(item.CompItemModel)
	s := Stack(st)
	w := &wire.Writer{}
	s.EncodeFor(w, nil)
	want := "05" + varint(paper) + "04" + "01" + "00" + varint(item.CompItemModel) + str("delta:ruby")
	if got := hex.EncodeToString(w.B); got != want {
		t.Fatalf("stack display:\n got  %s\n want %s", got, want)
	}
	for _, p := range []*item.Proto{nil, item.For(version.V776)} {
		w.Reset()
		s.EncodeFor(w, p)
		// The same stack as a slot has the count first.
		full := &wire.Writer{}
		st.EncodeFor(full, p)
		fr := wire.NewReader(full.B)
		count, itemID := fr.VarInt(), fr.VarInt()
		head := append(wire.AppendVarInt([]byte{byte(SlotItemStack)}, itemID), wire.AppendVarInt(nil, count)...)
		if !bytes.Equal(w.B, append(head, full.B[fr.Off:]...)) {
			t.Errorf("protocol %d: template %x, stack %x", p.Protocol(), w.B, full.B)
		}
		var back Slot
		r := wire.NewReader(w.B)
		back.DecodeFor(r, p)
		if r.Err != nil || r.Len() != 0 || back.Stack == nil || back.Stack.ID != paper || back.Stack.Count != 4 || back.Stack.ItemModel != "delta:ruby" {
			t.Errorf("protocol %d: read back %+v, %v", p.Protocol(), back.Stack, r.Err)
		}
	}
	// No stack, or an empty one: an empty display.
	for _, s := range []Slot{{Kind: SlotItemStack}, Stack(&item.Stack{ID: paper})} {
		w.Reset()
		s.EncodeFor(w, nil)
		if hex.EncodeToString(w.B) != "00" {
			t.Errorf("empty stack display: %x", w.B)
		}
	}
}

func testEntries(t testing.TB) []Entry {
	planks, stick, table, pick := id(t, "oak_planks"), id(t, "stick"), id(t, "crafting_table"), id(t, "wooden_pickaxe")
	iron, bucket, milk, cake := id(t, "iron_ingot"), id(t, "bucket"), id(t, "milk_bucket"), id(t, "cake")
	st := &item.Stack{ID: stick, Count: 4}
	named := &item.Stack{ID: id(t, "paper"), Count: 1, ItemModel: "delta:ruby"}
	named.Add(item.CompItemModel)
	return []Entry{
		{ID: 0, Display: Shaped(1, 2, []Slot{Tag("minecraft:planks"), Tag("minecraft:planks")}, Stack(st), Item(table)),
			Category: CraftingMisc, Requirements: []HolderSet{{Tag: "minecraft:planks"}, {Tag: "minecraft:planks"}}},
		{ID: 1, Display: Shaped(3, 2, []Slot{Item(planks), Item(planks), Item(planks), Empty(), Item(stick), Empty()}, Item(pick), Item(table)),
			Group: 0, HasGroup: true, Category: CraftingEquipment, Requirements: []HolderSet{{IDs: []int32{planks}}, {IDs: []int32{stick}}},
			Flags: FlagNotification | FlagHighlight},
		{ID: 70000, Display: Shapeless([]Slot{WithRemainder(Item(milk), Item(bucket)), Items(iron, stick, planks), Stack(named)}, Item(cake), Item(table)),
			Category: CraftingBuildingBlocks},
		{ID: 3, Display: Display{Kind: Furnace, Ingredients: []Slot{Item(iron)}, Fuel: AnyFuel(), Duration: 200, Experience: 0.7,
			Result: Item(iron), Station: Item(id(t, "furnace"))}, Category: FurnaceMisc, Requirements: []HolderSet{}},
		{ID: 4, Display: Display{Kind: Stonecutter, Ingredients: []Slot{Item(planks)}, Result: Stack(st), Station: Item(id(t, "stonecutter"))},
			Category: StonecutterCategory},
		{ID: 5, Display: Display{Kind: Smithing, Ingredients: []Slot{Item(iron), Item(pick), Item(iron)},
			Result:  Slot{Kind: SlotSmithingTrim, Sub: []Slot{Item(pick), Item(iron)}, Pattern: 3},
			Station: Item(id(t, "smithing_table"))}, Category: SmithingCategory},
		{ID: 6, Display: Shapeless([]Slot{
			{Kind: SlotWithAnyPotion, Sub: []Slot{Item(id(t, "tipped_arrow"))}},
			{Kind: SlotOnlyWithComponent, Sub: []Slot{Item(id(t, "leather_boots"))}, Component: item.CompDyedColor},
			{Kind: SlotDyed, Sub: []Slot{Item(id(t, "red_dye")), Item(id(t, "leather_boots"))}},
		}, Empty(), Empty()), Category: CraftingMisc},
	}
}

// TestRoundTrip: what is written is read back the same, in both versions.
func TestRoundTrip(t *testing.T) {
	entries := testEntries(t)
	for _, p := range []*item.Proto{nil, item.For(version.V776)} {
		w := &wire.Writer{}
		WriteBookAdd(w, entries, true, p)
		r := wire.NewReader(w.B)
		got, replace := ReadBookAdd(r, p)
		if r.Err != nil || r.Len() != 0 || !replace {
			t.Fatalf("protocol %d: %v, %d bytes left, replace %v", p.Protocol(), r.Err, r.Len(), replace)
		}
		if len(got) != len(entries) {
			t.Fatalf("protocol %d: %d entries", p.Protocol(), len(got))
		}
		for i := range entries {
			// A stack read back differs from the one written in its empty slices only: entries
			// with one are compared by what is written of them.
			a, b := &wire.Writer{}, &wire.Writer{}
			got[i].EncodeFor(a, p)
			entries[i].EncodeFor(b, p)
			if !bytes.Equal(a.B, b.B) || (i != 0 && i != 2 && i != 4 && !reflect.DeepEqual(got[i], entries[i])) {
				t.Errorf("protocol %d: entry %d:\n got  %+v\n want %+v", p.Protocol(), i, got[i], entries[i])
			}
		}
		// And the bytes again.
		w2 := &wire.Writer{}
		WriteBookAdd(w2, got, true, p)
		if !bytes.Equal(w.B, w2.B) {
			t.Errorf("protocol %d: written again differs", p.Protocol())
		}

		w.Reset()
		WritePlaceGhost(w, 7, &entries[1].Display, p)
		r = wire.NewReader(w.B)
		container, d := ReadPlaceGhost(r, p)
		if r.Err != nil || r.Len() != 0 || container != 7 || !reflect.DeepEqual(d, entries[1].Display) {
			t.Errorf("protocol %d: ghost recipe %d %+v %v", p.Protocol(), container, d, r.Err)
		}
	}
}

func TestSmallPackets(t *testing.T) {
	w := &wire.Writer{}
	WriteBookRemove(w, []int32{1, 300, 2})
	if hex.EncodeToString(w.B) != "03"+"01"+varint(300)+"02" {
		t.Errorf("remove: %x", w.B)
	}
	if got := ReadBookRemove(wire.NewReader(w.B)); !reflect.DeepEqual(got, []int32{1, 300, 2}) {
		t.Errorf("remove read: %v", got)
	}

	s := Settings{Crafting: BookState{Open: true}, Smoker: BookState{Filtering: true}, BlastFurnace: BookState{Open: true, Filtering: true}}
	w.Reset()
	s.Write(w)
	if hex.EncodeToString(w.B) != "0100"+"0000"+"0101"+"0001" { // crafting, furnace, blast furnace, smoker
		t.Errorf("settings: %x", w.B)
	}
	var back Settings
	back.Read(wire.NewReader(w.B))
	if back != s {
		t.Errorf("settings read: %+v", back)
	}

	var pr PlaceRecipe
	r := wire.NewReader([]byte{0x05, 0xac, 0x02, 0x01}) // container 5, recipe 300, shift
	pr.Read(r)
	if r.Err != nil || r.Len() != 0 || pr != (PlaceRecipe{Container: 5, Recipe: 300, UseMax: true}) {
		t.Errorf("place_recipe: %+v %v", pr, r.Err)
	}
	w.Reset()
	pr.Write(w)
	if hex.EncodeToString(w.B) != "05ac0201" {
		t.Errorf("place_recipe written: %x", w.B)
	}
}

// TestUnshowable: what a version cannot show is an empty display, and a set loses the items the
// version does not have.
func TestUnshowable(t *testing.T) {
	p := item.For(version.V776)
	var only263 int32 = -1
	for i := int32(0); uint32(i) < item.Items; i++ {
		if p.Item(i) < 0 {
			only263 = i
			break
		}
	}
	w := &wire.Writer{}
	if only263 >= 0 {
		planks := id(t, "oak_planks")
		s := Item(only263)
		s.EncodeFor(w, p)
		st := Stack(&item.Stack{ID: only263, Count: 1})
		st.EncodeFor(w, p)
		e := Entry{Display: Shapeless(nil, Empty(), Empty()), Requirements: []HolderSet{{IDs: []int32{only263, planks}}}}
		e.EncodeFor(w, p)
		want := "00" + "00" + "00" + "00" + "00" + "00" + "00" + "00" + "03" + "01" + "01" + "02" + varint(p.Item(planks)) + "00"
		if got := hex.EncodeToString(w.B); got != want {
			t.Errorf("26.2 without the item:\n got  %s\n want %s", got, want)
		}
	}
	// A display without its parts, of no kind, or nested too deep.
	deep := Item(1)
	for i := 0; i < maxDepth+2; i++ {
		deep = Composite(deep)
	}
	for _, s := range []Slot{{Kind: SlotWithRemainder, Sub: []Slot{Item(1)}}, {Kind: 99}, {Kind: SlotItem, Item: -5}} {
		w.Reset()
		s.EncodeFor(w, nil)
		if hex.EncodeToString(w.B) != "00" {
			t.Errorf("%+v: %x", s, w.B)
		}
	}
	w.Reset()
	deep.EncodeFor(w, nil)
	var back Slot
	r := wire.NewReader(w.B)
	back.DecodeFor(r, nil)
	if r.Err != nil || r.Len() != 0 {
		t.Errorf("a display written too deep is not read back: %v", r.Err)
	}
}

// TestHostile: bytes no server writes give an error, not a panic or a huge allocation.
func TestHostile(t *testing.T) {
	for name, b := range map[string][]byte{
		"slot kind":       {0x0b},
		"item id":         {0x04, 0xff, 0xff, 0xff, 0xff, 0x07},
		"huge composite":  {0x0a, 0xff, 0xff, 0xff, 0xff, 0x07},
		"negative list":   {0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f},
		"short":           {0x09, 0x04},
		"inline pattern":  {0x08, 0x00, 0x00, 0x00},
		"endless nesting": bytes.Repeat([]byte{0x0a, 0x01}, 200),
	} {
		var s Slot
		r := wire.NewReader(b)
		s.DecodeFor(r, nil)
		if r.Err == nil {
			t.Errorf("%s: no error", name)
		}
		if name == "inline pattern" && !errors.Is(r.Err, ErrUnsupported) {
			t.Errorf("%s: %v", name, r.Err)
		}
	}
	for name, b := range map[string][]byte{
		"display kind":  {0x05},
		"huge book":     {0xff, 0xff, 0xff, 0xff, 0x07},
		"category":      {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0d, 0x00, 0x00},
		"huge set":      {0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x01, 0x01, 0xff, 0xff, 0xff, 0xff, 0x07},
		"cut off entry": {0x01, 0x00, 0x01, 0x03},
	} {
		r := wire.NewReader(b)
		if name == "display kind" {
			var d Display
			d.DecodeFor(r, nil)
		} else if name == "huge book" || name == "cut off entry" {
			ReadBookAdd(r, nil)
		} else {
			var e Entry
			e.DecodeFor(r, nil)
		}
		if r.Err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func FuzzDecode(f *testing.F) {
	w := &wire.Writer{}
	WriteBookAdd(w, testEntries(f), false, nil)
	f.Add(w.B)
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, p := range []*item.Proto{nil, item.For(version.V776)} {
			ReadBookAdd(wire.NewReader(b), p)
		}
	})
}
