// Package recipe is the recipe book of the Java protocol: the recipe displays a server sends for
// the client's recipe book (recipe_book_add, recipe_book_remove, recipe_book_settings), the ghost
// recipe (place_ghost_recipe) and the client's click on a recipe (place_recipe).
//
// Since 1.21.2 a client knows no recipes: the server sends a display of each recipe the player
// has unlocked, made of slot displays (what a slot of the recipe shows: an item, a stack, a tag, a
// choice of several), and the client sends a display's id back when the player clicks it.
//
// Ids are those of the newest version (26.3); the *For functions take the item Proto of the
// client's version (nil: the newest), as package item does. The wire forms are the stream codecs
// of the 26.3 and 26.2 clients (net.minecraft.world.item.crafting.display.*, RecipeDisplayEntry,
// the packets of the same names). The two versions differ in one place: a tag slot display is a
// holder set in 26.3 and a tag key in 26.2. The ids of the three built-in registries involved
// (minecraft:slot_display, minecraft:recipe_display, minecraft:recipe_book_category) are the
// same in both and are the constants below.
package recipe

import (
	"errors"
	"fmt"

	"github.com/ezchr/go-mcjava/item"
	"github.com/ezchr/go-mcjava/wire"
)

// ErrInvalid is the reader's error for a value no client or server writes: an unknown display
// type, an id out of range, displays nested too deep.
var ErrInvalid = errors.New("recipe: invalid value")

// ErrUnsupported is the reader's error for the one value this package has no type for: a trim
// pattern given inline instead of by its registry id.
var ErrUnsupported = errors.New("recipe: inline trim pattern")

// SlotKind is a slot display type: its id in the minecraft:slot_display registry.
type SlotKind int32

const (
	SlotEmpty             SlotKind = iota // minecraft:empty
	SlotAnyFuel                           // minecraft:any_fuel
	SlotWithAnyPotion                     // minecraft:with_any_potion
	SlotOnlyWithComponent                 // minecraft:only_with_component
	SlotItem                              // minecraft:item
	SlotItemStack                         // minecraft:item_stack
	SlotTag                               // minecraft:tag
	SlotDyed                              // minecraft:dyed
	SlotSmithingTrim                      // minecraft:smithing_trim
	SlotWithRemainder                     // minecraft:with_remainder
	SlotComposite                         // minecraft:composite
	slotKinds
)

var slotNames = [slotKinds]string{"empty", "any_fuel", "with_any_potion", "only_with_component", "item", "item_stack",
	"tag", "dyed", "smithing_trim", "with_remainder", "composite"}

// Name is the kind's registry key ("minecraft:item").
func (k SlotKind) Name() string {
	if k < 0 || k >= slotKinds {
		return ""
	}
	return "minecraft:" + slotNames[k]
}

// HolderSet is a set of items: a tag by name ("minecraft:planks"), or the items themselves.
type HolderSet struct {
	Tag string
	IDs []int32
}

// Slot is a slot display: what one slot of a recipe shows.
//
// The fields used depend on Kind:
//
//	SlotEmpty, SlotAnyFuel   none
//	SlotItem                 Item
//	SlotItemStack            Stack (never empty)
//	SlotTag                  Set: a tag's name; items only reach a 26.3 client as a tag display
//	                         (a 26.2 client gets a composite of them)
//	SlotWithAnyPotion        Sub[0]: the display every potion is shown in
//	SlotOnlyWithComponent    Sub[0]: the source, Component: the data component type its stacks need
//	SlotDyed                 Sub[0]: the dye, Sub[1]: the target
//	SlotSmithingTrim         Sub[0]: the base, Sub[1]: the material, Pattern: the trim pattern's id
//	                         in minecraft:trim_pattern
//	SlotWithRemainder        Sub[0]: the input, Sub[1]: what is left of it
//	SlotComposite            Sub: the displays shown in turn
type Slot struct {
	Kind      SlotKind
	Item      int32
	Stack     *item.Stack
	Set       HolderSet
	Component int32
	Pattern   int32
	Sub       []Slot
}

// Empty is the display of a slot with nothing in it.
func Empty() Slot { return Slot{} }

// AnyFuel is the display of a furnace's fuel slot.
func AnyFuel() Slot { return Slot{Kind: SlotAnyFuel} }

// Item is the display of one item.
func Item(id int32) Slot { return Slot{Kind: SlotItem, Item: id} }

// Stack is the display of a stack: its count and components are shown.
func Stack(s *item.Stack) Slot { return Slot{Kind: SlotItemStack, Stack: s} }

// Tag is the display of the items of a tag ("minecraft:planks"), shown in turn.
func Tag(name string) Slot { return Slot{Kind: SlotTag, Set: HolderSet{Tag: name}} }

// Composite is several displays shown in turn.
func Composite(of ...Slot) Slot { return Slot{Kind: SlotComposite, Sub: of} }

// Items is the display of several items shown in turn; of one item it is Item.
func Items(ids ...int32) Slot {
	if len(ids) == 1 {
		return Item(ids[0])
	}
	s := Slot{Kind: SlotComposite, Sub: make([]Slot, len(ids))}
	for i, id := range ids {
		s.Sub[i] = Item(id)
	}
	return s
}

// WithRemainder is an input that leaves something in its slot (a bucket of milk, the bucket).
func WithRemainder(input, remainder Slot) Slot {
	return Slot{Kind: SlotWithRemainder, Sub: []Slot{input, remainder}}
}

// subs is how many displays a kind holds in Sub (-1: any number).
func (k SlotKind) subs() int {
	switch k {
	case SlotWithAnyPotion, SlotOnlyWithComponent:
		return 1
	case SlotDyed, SlotSmithingTrim, SlotWithRemainder:
		return 2
	case SlotComposite:
		return -1
	}
	return 0
}

// maxDepth is how deep slot displays may be nested, writing and reading.
const maxDepth = 32

// EncodeFor writes the slot display for the protocol version of p (nil: the newest). A display
// the version cannot show is written as an empty one: an item or a stack of an item it does not
// have, a stack that is empty, a display that lacks one of its parts or is nested too deep. Items
// a version does not have are left out of a set.
func (s *Slot) EncodeFor(w *wire.Writer, p *item.Proto) { s.encode(w, p, 0) }

func (s *Slot) encode(w *wire.Writer, p *item.Proto, depth int) {
	n := s.Kind.subs()
	if s.Kind < 0 || s.Kind >= slotKinds || depth >= maxDepth || (n > 0 && len(s.Sub) < n) {
		w.VarInt(int32(SlotEmpty))
		return
	}
	switch s.Kind {
	case SlotItem:
		id := p.Item(s.Item)
		if id < 0 || uint32(s.Item) >= item.Items {
			w.VarInt(int32(SlotEmpty))
			return
		}
		w.VarInt(int32(SlotItem))
		w.VarInt(id)
	case SlotItemStack:
		start := len(w.B)
		w.VarInt(int32(SlotItemStack))
		if s.Stack == nil || !s.Stack.EncodeTemplateFor(w, p) {
			w.B = w.B[:start]
			w.VarInt(int32(SlotEmpty))
		}
	case SlotTag:
		switch {
		case s.Set.Tag != "" && p != nil: // 26.2: a tag key
			w.VarInt(int32(SlotTag))
			w.String(s.Set.Tag)
		case s.Set.Tag != "" || p == nil:
			w.VarInt(int32(SlotTag))
			writeHolderSet(w, &s.Set, p)
		default: // 26.2 has no display of a set of items
			c := Items(s.Set.IDs...)
			c.encode(w, p, depth+1)
		}
	case SlotOnlyWithComponent:
		t := p.Component(s.Component)
		if t < 0 {
			w.VarInt(int32(SlotEmpty))
			return
		}
		w.VarInt(int32(s.Kind))
		s.Sub[0].encode(w, p, depth+1)
		w.VarInt(t)
	case SlotSmithingTrim:
		w.VarInt(int32(s.Kind))
		s.Sub[0].encode(w, p, depth+1)
		s.Sub[1].encode(w, p, depth+1)
		w.VarInt(s.Pattern + 1) // a holder: 0 is a pattern given inline
	case SlotComposite:
		w.VarInt(int32(s.Kind))
		w.VarInt(int32(len(s.Sub)))
		for i := range s.Sub {
			s.Sub[i].encode(w, p, depth+1)
		}
	default:
		w.VarInt(int32(s.Kind))
		for i := 0; i < n; i++ {
			s.Sub[i].encode(w, p, depth+1)
		}
	}
}

// DecodeFor reads a slot display of the protocol version of p (nil: the newest). Errors are in
// r.Err.
func (s *Slot) DecodeFor(r *wire.Reader, p *item.Proto) { s.decode(r, p, 0) }

func (s *Slot) decode(r *wire.Reader, p *item.Proto, depth int) {
	*s = Slot{}
	kind := SlotKind(r.VarInt())
	if r.Err != nil {
		return
	}
	if kind < 0 || kind >= slotKinds || depth > maxDepth {
		fail(r, fmt.Errorf("%w: slot display %d at depth %d", ErrInvalid, kind, depth))
		return
	}
	s.Kind = kind
	sub := func(n int) {
		s.Sub = make([]Slot, n)
		for i := 0; i < n && r.Err == nil; i++ {
			s.Sub[i].decode(r, p, depth+1)
		}
	}
	switch kind {
	case SlotItem:
		s.Item = readItem(r, p)
	case SlotItemStack:
		s.Stack = &item.Stack{}
		s.Stack.DecodeTemplateFor(r, p)
	case SlotTag:
		if p != nil {
			s.Set.Tag = r.String(32767)
		} else {
			readHolderSet(r, &s.Set, p)
		}
	case SlotOnlyWithComponent:
		sub(1)
		t := r.VarInt()
		if s.Component = p.ComponentIn(t); s.Component < 0 && r.Err == nil {
			fail(r, fmt.Errorf("%w: data component type %d", ErrInvalid, t))
		}
	case SlotSmithingTrim:
		sub(2)
		id := r.VarInt()
		if id == 0 && r.Err == nil {
			fail(r, ErrUnsupported)
		}
		s.Pattern = id - 1
	case SlotComposite:
		sub(length(r, 1))
	default:
		if n := kind.subs(); n > 0 {
			sub(n)
		}
	}
}

func fail(r *wire.Reader, err error) {
	if r.Err == nil {
		r.Err = err
	}
	r.Off = len(r.B)
}

// length reads a list's length: every element takes minElem bytes or more.
func length(r *wire.Reader, minElem int) int {
	n := int(r.VarInt())
	if r.Err != nil {
		return 0
	}
	if n < 0 || n*minElem > r.Len() {
		fail(r, fmt.Errorf("%w: list of %d", ErrInvalid, n))
		return 0
	}
	return n
}

func readItem(r *wire.Reader, p *item.Proto) int32 {
	id := r.VarInt()
	if r.Err != nil {
		return 0
	}
	nid := p.ItemIn(id)
	if uint32(nid) >= item.Items {
		fail(r, fmt.Errorf("%w: item %d", ErrInvalid, id))
		return 0
	}
	return nid
}

// writeHolderSet writes a set of items: 0 and the tag's name, or the count + 1 and the ids.
func writeHolderSet(w *wire.Writer, s *HolderSet, p *item.Proto) {
	if s.Tag != "" {
		w.VarInt(0)
		w.String(s.Tag)
		return
	}
	n := 0
	for _, id := range s.IDs {
		if uint32(id) < item.Items && p.Item(id) >= 0 {
			n++
		}
	}
	w.VarInt(int32(n) + 1)
	for _, id := range s.IDs {
		if uint32(id) < item.Items && p.Item(id) >= 0 {
			w.VarInt(p.Item(id))
		}
	}
}

func readHolderSet(r *wire.Reader, s *HolderSet, p *item.Proto) {
	*s = HolderSet{}
	n := int(r.VarInt())
	if r.Err != nil {
		return
	}
	if n == 0 {
		s.Tag = r.String(32767)
		return
	}
	if n < 0 || n-1 > r.Len() {
		fail(r, fmt.Errorf("%w: set of %d", ErrInvalid, n-1))
		return
	}
	s.IDs = make([]int32, 0, n-1)
	for i := 1; i < n && r.Err == nil; i++ {
		s.IDs = append(s.IDs, readItem(r, p))
	}
}

// DisplayKind is a recipe display type: its id in the minecraft:recipe_display registry.
type DisplayKind int32

const (
	CraftingShapeless DisplayKind = iota // minecraft:crafting_shapeless
	CraftingShaped                       // minecraft:crafting_shaped
	Furnace                              // minecraft:furnace
	Stonecutter                          // minecraft:stonecutter
	Smithing                             // minecraft:smithing
	displayKinds
)

var displayNames = [displayKinds]string{"crafting_shapeless", "crafting_shaped", "furnace", "stonecutter", "smithing"}

// Name is the kind's registry key ("minecraft:crafting_shaped").
func (k DisplayKind) Name() string {
	if k < 0 || k >= displayKinds {
		return ""
	}
	return "minecraft:" + displayNames[k]
}

// Display is a recipe display: how the recipe book and the ghost recipe show a recipe.
//
// Result is what the recipe makes and Station the block it is made in (a crafting table: the
// book's icon for it). The other fields used depend on Kind:
//
//	CraftingShapeless   Ingredients, in any order
//	CraftingShaped      Width, Height and Ingredients: Width * Height of them, row after row,
//	                    empty displays where the pattern has nothing
//	Furnace             Ingredients[0], Fuel, Duration (ticks) and Experience; also the display
//	                    of blast furnace, smoker and campfire recipes
//	Stonecutter         Ingredients[0]
//	Smithing            Ingredients[0..2]: the template, the base and the addition
type Display struct {
	Kind          DisplayKind
	Width, Height int32
	Ingredients   []Slot
	Fuel          Slot
	Duration      int32
	Experience    float32
	Result        Slot
	Station       Slot
}

// Shaped is the display of a shaped crafting recipe.
func Shaped(width, height int, ingredients []Slot, result, station Slot) Display {
	return Display{Kind: CraftingShaped, Width: int32(width), Height: int32(height), Ingredients: ingredients, Result: result, Station: station}
}

// Shapeless is the display of a shapeless crafting recipe.
func Shapeless(ingredients []Slot, result, station Slot) Display {
	return Display{Kind: CraftingShapeless, Ingredients: ingredients, Result: result, Station: station}
}

// fixed is how many ingredients a kind has (-1: a list).
func (k DisplayKind) fixed() int {
	switch k {
	case Furnace, Stonecutter:
		return 1
	case Smithing:
		return 3
	}
	return -1
}

// EncodeFor writes the display for the protocol version of p (nil: the newest). Ingredients a
// kind needs and the display lacks are written as empty ones; a kind there is none of is written
// as a shapeless recipe of nothing.
func (d *Display) EncodeFor(w *wire.Writer, p *item.Proto) {
	if d.Kind < 0 || d.Kind >= displayKinds {
		w.VarInt(int32(CraftingShapeless))
		w.VarInt(0)
		w.VarInt(int32(SlotEmpty))
		w.VarInt(int32(SlotEmpty))
		return
	}
	w.VarInt(int32(d.Kind))
	if n := d.Kind.fixed(); n >= 0 {
		for i := 0; i < n; i++ {
			if i < len(d.Ingredients) {
				d.Ingredients[i].EncodeFor(w, p)
			} else {
				w.VarInt(int32(SlotEmpty))
			}
		}
	} else {
		if d.Kind == CraftingShaped {
			w.VarInt(d.Width)
			w.VarInt(d.Height)
		}
		w.VarInt(int32(len(d.Ingredients)))
		for i := range d.Ingredients {
			d.Ingredients[i].EncodeFor(w, p)
		}
	}
	if d.Kind == Furnace {
		d.Fuel.EncodeFor(w, p)
	}
	d.Result.EncodeFor(w, p)
	d.Station.EncodeFor(w, p)
	if d.Kind == Furnace {
		w.VarInt(d.Duration)
		w.Float32(d.Experience)
	}
}

// DecodeFor reads a display of the protocol version of p (nil: the newest). Errors are in r.Err.
func (d *Display) DecodeFor(r *wire.Reader, p *item.Proto) {
	*d = Display{}
	kind := DisplayKind(r.VarInt())
	if r.Err != nil {
		return
	}
	if kind < 0 || kind >= displayKinds {
		fail(r, fmt.Errorf("%w: recipe display %d", ErrInvalid, kind))
		return
	}
	d.Kind = kind
	n := kind.fixed()
	if n < 0 {
		if kind == CraftingShaped {
			d.Width, d.Height = r.VarInt(), r.VarInt()
		}
		n = length(r, 1)
	}
	d.Ingredients = make([]Slot, n)
	for i := 0; i < n && r.Err == nil; i++ {
		d.Ingredients[i].DecodeFor(r, p)
	}
	if kind == Furnace {
		d.Fuel.DecodeFor(r, p)
	}
	d.Result.DecodeFor(r, p)
	d.Station.DecodeFor(r, p)
	if kind == Furnace {
		d.Duration = r.VarInt()
		d.Experience = r.Float32()
	}
}

// Category is a recipe book category: its id in the minecraft:recipe_book_category registry.
// The book's tabs are made of them (the crafting book: building blocks, redstone, equipment,
// misc).
type Category int32

const (
	CraftingBuildingBlocks Category = iota // minecraft:crafting_building_blocks
	CraftingRedstone                       // minecraft:crafting_redstone
	CraftingEquipment                      // minecraft:crafting_equipment
	CraftingMisc                           // minecraft:crafting_misc
	FurnaceFood                            // minecraft:furnace_food
	FurnaceBlocks                          // minecraft:furnace_blocks
	FurnaceMisc                            // minecraft:furnace_misc
	BlastFurnaceBlocks                     // minecraft:blast_furnace_blocks
	BlastFurnaceMisc                       // minecraft:blast_furnace_misc
	SmokerFood                             // minecraft:smoker_food
	StonecutterCategory                    // minecraft:stonecutter
	SmithingCategory                       // minecraft:smithing
	Campfire                               // minecraft:campfire
	categories
)

var categoryNames = [categories]string{"crafting_building_blocks", "crafting_redstone", "crafting_equipment", "crafting_misc",
	"furnace_food", "furnace_blocks", "furnace_misc", "blast_furnace_blocks", "blast_furnace_misc", "smoker_food",
	"stonecutter", "smithing", "campfire"}

// Name is the category's registry key ("minecraft:crafting_misc").
func (c Category) Name() string {
	if c < 0 || c >= categories {
		return ""
	}
	return "minecraft:" + categoryNames[c]
}

// Flags of an Entry.
const (
	// FlagNotification shows the "new recipes unlocked" toast for the entry.
	FlagNotification byte = 1
	// FlagHighlight makes the entry bounce in the book until the player has seen it.
	FlagHighlight byte = 2
)

// Entry is a recipe of the recipe book (recipe_book_add).
type Entry struct {
	// ID is the display's id: any number the server picks, sent back in place_recipe.
	ID      int32
	Display Display
	// Group puts recipes that make variants of a thing under one button of the book (the same
	// number for each); HasGroup false: a button of its own.
	Group    int32
	HasGroup bool
	Category Category
	// Requirements are what the recipe takes, an entry for every item it uses up (each a set of
	// the items that will do): the client works out from them whether the player's inventory has
	// the items (the book's "craftable" filter, and which recipes it shows as makeable). nil:
	// not given, the client never counts the recipe as craftable. An empty list is given as it is.
	Requirements []HolderSet
	// Flags are FlagNotification and FlagHighlight.
	Flags byte
}

// EncodeFor writes the entry with its flags, as recipe_book_add lists them, for the protocol
// version of p (nil: the newest).
func (e *Entry) EncodeFor(w *wire.Writer, p *item.Proto) {
	w.VarInt(e.ID)
	e.Display.EncodeFor(w, p)
	if e.HasGroup {
		w.VarInt(e.Group + 1)
	} else {
		w.VarInt(0)
	}
	c := e.Category
	if c < 0 || c >= categories {
		c = CraftingMisc
	}
	w.VarInt(int32(c))
	w.Bool(e.Requirements != nil)
	if e.Requirements != nil {
		w.VarInt(int32(len(e.Requirements)))
		for i := range e.Requirements {
			writeHolderSet(w, &e.Requirements[i], p)
		}
	}
	w.Byte(e.Flags)
}

// DecodeFor reads an entry and its flags. Errors are in r.Err.
func (e *Entry) DecodeFor(r *wire.Reader, p *item.Proto) {
	*e = Entry{}
	e.ID = r.VarInt()
	e.Display.DecodeFor(r, p)
	if g := r.VarInt(); g != 0 {
		e.Group, e.HasGroup = g-1, true
	}
	e.Category = Category(r.VarInt())
	if (e.Category < 0 || e.Category >= categories) && r.Err == nil {
		fail(r, fmt.Errorf("%w: recipe book category %d", ErrInvalid, e.Category))
		return
	}
	if r.Bool() {
		n := length(r, 1)
		e.Requirements = make([]HolderSet, n)
		for i := 0; i < n && r.Err == nil; i++ {
			readHolderSet(r, &e.Requirements[i], p)
		}
	}
	e.Flags = r.Byte()
}

// WriteBookAdd writes the body of recipe_book_add: recipes for the client's recipe book. replace:
// they are the whole book (what it had is dropped); else they are added to it.
func WriteBookAdd(w *wire.Writer, entries []Entry, replace bool, p *item.Proto) {
	w.VarInt(int32(len(entries)))
	for i := range entries {
		entries[i].EncodeFor(w, p)
	}
	w.Bool(replace)
}

// ReadBookAdd reads the body of recipe_book_add.
func ReadBookAdd(r *wire.Reader, p *item.Proto) (entries []Entry, replace bool) {
	n := length(r, 8)
	entries = make([]Entry, n)
	for i := 0; i < n && r.Err == nil; i++ {
		entries[i].DecodeFor(r, p)
	}
	return entries, r.Bool()
}

// WriteBookRemove writes the body of recipe_book_remove: the display ids taken out of the book.
func WriteBookRemove(w *wire.Writer, ids []int32) {
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
}

// ReadBookRemove reads the body of recipe_book_remove.
func ReadBookRemove(r *wire.Reader) []int32 {
	n := length(r, 1)
	ids := make([]int32, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		ids = append(ids, r.VarInt())
	}
	return ids
}

// BookState is one recipe book's state: whether it is open beside its screen and whether it
// shows only what the player can make now.
type BookState struct {
	Open, Filtering bool
}

// Settings is the state of the four recipe books (recipe_book_settings; the client sends a
// change of one as recipe_book_change_settings).
type Settings struct {
	Crafting, Furnace, BlastFurnace, Smoker BookState
}

// Write writes the body of recipe_book_settings.
func (s *Settings) Write(w *wire.Writer) {
	for _, b := range [...]BookState{s.Crafting, s.Furnace, s.BlastFurnace, s.Smoker} {
		w.Bool(b.Open)
		w.Bool(b.Filtering)
	}
}

// Read reads the body of recipe_book_settings.
func (s *Settings) Read(r *wire.Reader) {
	for _, b := range [...]*BookState{&s.Crafting, &s.Furnace, &s.BlastFurnace, &s.Smoker} {
		b.Open = r.Bool()
		b.Filtering = r.Bool()
	}
}

// WritePlaceGhost writes the body of place_ghost_recipe: the recipe is shown in the grid of the
// open window as faint items, which is the answer to a place_recipe the player lacks items for.
func WritePlaceGhost(w *wire.Writer, container int32, d *Display, p *item.Proto) {
	w.VarInt(container)
	d.EncodeFor(w, p)
}

// ReadPlaceGhost reads the body of place_ghost_recipe.
func ReadPlaceGhost(r *wire.Reader, p *item.Proto) (container int32, d Display) {
	container = r.VarInt()
	d.DecodeFor(r, p)
	return container, d
}

// PlaceRecipe is the client's place_recipe: the player clicked a recipe of the book of the open
// window. UseMax: with shift, for as many as the items allow.
type PlaceRecipe struct {
	Container int32
	Recipe    int32 // the Entry's ID
	UseMax    bool
}

// Read reads the body of place_recipe.
func (pr *PlaceRecipe) Read(r *wire.Reader) {
	pr.Container = r.VarInt()
	pr.Recipe = r.VarInt()
	pr.UseMax = r.Bool()
}

// Write writes the body of place_recipe.
func (pr *PlaceRecipe) Write(w *wire.Writer) {
	w.VarInt(pr.Container)
	w.VarInt(pr.Recipe)
	w.Bool(pr.UseMax)
}
