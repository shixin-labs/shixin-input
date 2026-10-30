// Rerank slots: how shixin_rerank and the engine exchange candidates and
// orders for 智能排序 (DESIGN §7.2). Each session has one slot in memory the
// engine maps outside the Go heap and registers with shixin_rerank_attach.
// The filter exports the first candidates of a segment into the slot; the
// engine imports a permutation for that export, and the filter applies it
// when the composition is refreshed.
//
// Only the engine thread touches a slot, and only between or inside its
// librime calls, so there are no locks. The layout is mirrored in Go
// (internal/rerank/slot.go); change both together.
//
// This file has no librime headers so it can be tested on its own.
#ifndef SHIXIN_RERANK_SLOT_H_
#define SHIXIN_RERANK_SLOT_H_

#include <cstddef>
#include <cstdint>
#include <string_view>

namespace shixin::rerank {

constexpr int kMaxEntries = 64;
constexpr int kArenaSize = 4096;

// Entry flags.
constexpr uint8_t kEntryFixed = 1 << 0;  // never moves: English, translators, custom phrases
constexpr uint8_t kEntryUser = 1 << 1;   // a learned user_phrase

// Export flags.
constexpr uint16_t kExportAbbrev = 1 << 0;  // the input is initials only (简拼)

struct Entry {
  uint16_t offset;  // text in the arena
  uint16_t length;
  uint8_t flags;
  uint8_t reserved[3];
  float weight;  // the dictionary's log weight (Phrase::weight), or log(quality)
};

struct Slot {
  // Written by the filter.
  uint32_t export_seq;  // changes with every new export; 0: none yet
  uint32_t input_hash;
  uint16_t count;
  uint16_t export_flags;
  uint32_t arena_used;
  Entry entries[kMaxEntries];
  char arena[kArenaSize];
  // Written by the engine.
  uint8_t enabled;  // 0: the filter passes candidates through
  uint8_t reserved[3];
  uint32_t import_seq;  // the export_seq perm is for; 0: none
  uint16_t import_count;
  uint8_t perm[kMaxEntries];  // perm[k]: the entry shown at position k
  uint16_t reserved2;
  // Counters written by the filter.
  uint32_t applied;   // menus built with an imported order
  uint32_t rejected;  // imported orders that weren't valid permutations
};

static_assert(sizeof(Entry) == 12, "Entry layout is shared with Go");
static_assert(offsetof(Slot, entries) == 16, "Slot layout is shared with Go");
static_assert(offsetof(Slot, arena) == 16 + 12 * kMaxEntries, "Slot layout is shared with Go");
static_assert(offsetof(Slot, enabled) == 16 + 12 * kMaxEntries + kArenaSize,
              "Slot layout is shared with Go");
static_assert(sizeof(Slot) == offsetof(Slot, enabled) + 84, "Slot layout is shared with Go");

// The slot of session index i, or nullptr if no slots are attached or i is
// out of range.
Slot* SlotAt(long i);

// Attach registers count slots of slot_size bytes at base (count 0
// detaches). It returns false, and keeps the old slots, if slot_size isn't
// sizeof(Slot): the engine and librime disagree on the layout.
bool Attach(void* base, uint32_t count, uint32_t slot_size);

// Item is one candidate offered for export.
struct Item {
  std::string_view text;
  std::string_view type;
  double weight;
};

// Limit is how many candidates of a segment to export: more for 简拼,
// where the right word is often on page 2–10.
constexpr int kLimit = 20;
constexpr int kLimitAbbrev = 50;

// IsAbbreviation reports whether input is initials only: letters with no
// vowel (a e i o u v), and apostrophes. "tw" is; "taiw" and "" aren't.
bool IsAbbreviation(std::string_view input);

// Hash identifies an export's input: the whole composition input and the
// span of its first candidate.
uint32_t Hash(std::string_view input, size_t start, size_t end);

// EntryFlags classifies a candidate. Only script_translator's Chinese
// phrases and sentences move.
uint8_t EntryFlags(std::string_view type, std::string_view text);

// Export writes items into slot. If the slot already holds the same
// export, it is kept with its sequence number, so an imported order still
// applies; otherwise the sequence changes. Items whose text doesn't fit
// the arena end the export.
void Export(Slot* slot, uint32_t hash, uint16_t flags, const Item* items, int n);

// Permutation returns the slot's imported order if it is for the current
// export and is a permutation of its n entries that leaves fixed entries
// in place, or nullptr.
const uint8_t* Permutation(Slot* slot, int n);

}  // namespace shixin::rerank

#endif  // SHIXIN_RERANK_SLOT_H_
