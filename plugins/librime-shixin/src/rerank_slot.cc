#include "rerank_slot.h"

#include <cstring>

namespace shixin::rerank {

namespace {

Slot* g_slots = nullptr;
uint32_t g_count = 0;

bool IsVowel(char c) {
  return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u' || c == 'v';
}

bool HasNonASCII(std::string_view s) {
  for (char c : s)
    if (static_cast<unsigned char>(c) >= 0x80)
      return true;
  return false;
}

}  // namespace

Slot* SlotAt(long i) {
  if (!g_slots || i < 0 || static_cast<unsigned long>(i) >= g_count)
    return nullptr;
  return &g_slots[i];
}

bool Attach(void* base, uint32_t count, uint32_t slot_size) {
  if (count != 0 && (!base || slot_size != sizeof(Slot)))
    return false;
  g_slots = count ? static_cast<Slot*>(base) : nullptr;
  g_count = count;
  return true;
}

bool IsAbbreviation(std::string_view input) {
  bool letter = false;
  for (char c : input) {
    if (c == '\'')
      continue;
    if (c < 'a' || c > 'z' || IsVowel(c))
      return false;
    letter = true;
  }
  return letter;
}

uint32_t Hash(std::string_view input, size_t start, size_t end) {
  uint32_t h = 2166136261u;  // FNV-1a
  auto mix = [&h](unsigned char b) { h = (h ^ b) * 16777619u; };
  for (char c : input)
    mix(static_cast<unsigned char>(c));
  for (int i = 0; i < 4; i++)
    mix(static_cast<unsigned char>(start >> (8 * i)));
  for (int i = 0; i < 4; i++)
    mix(static_cast<unsigned char>(end >> (8 * i)));
  return h;
}

uint8_t EntryFlags(std::string_view type, std::string_view text) {
  if (type == "user_phrase")
    return HasNonASCII(text) ? kEntryUser : kEntryFixed;
  if ((type == "phrase" || type == "sentence") && HasNonASCII(text))
    return 0;
  return kEntryFixed;
}

void Export(Slot* slot, uint32_t hash, uint16_t flags, const Item* items, int n) {
  if (n > kMaxEntries)
    n = kMaxEntries;
  // Compare while writing: an unchanged export keeps its sequence number.
  bool same = slot->export_seq != 0 && slot->input_hash == hash && slot->export_flags == flags;
  uint32_t used = 0;
  int count = 0;
  for (; count < n; count++) {
    const Item& it = items[count];
    if (it.text.size() > kArenaSize - used)
      break;
    Entry e{};
    e.offset = static_cast<uint16_t>(used);
    e.length = static_cast<uint16_t>(it.text.size());
    e.flags = EntryFlags(it.type, it.text);
    e.weight = static_cast<float>(it.weight);
    Entry& old = slot->entries[count];
    same = same && count < slot->count && old.length == e.length && old.flags == e.flags &&
           old.weight == e.weight &&
           std::memcmp(slot->arena + old.offset, it.text.data(), e.length) == 0;
    // While every earlier entry matched, this one's old text starts at
    // `used` too, so it is compared before it is overwritten.
    std::memcpy(slot->arena + used, it.text.data(), e.length);
    old = e;
    used += e.length;
  }
  same = same && count == slot->count;
  slot->count = static_cast<uint16_t>(count);
  slot->arena_used = used;
  slot->input_hash = hash;
  slot->export_flags = flags;
  if (!same && ++slot->export_seq == 0)
    slot->export_seq = 1;
}

const uint8_t* Permutation(Slot* slot, int n) {
  if (!slot->enabled || slot->import_seq == 0 || slot->import_seq != slot->export_seq)
    return nullptr;
  if (n != slot->count || slot->import_count != n || n > kMaxEntries)
    return nullptr;
  uint64_t seen = 0;
  for (int k = 0; k < n; k++) {
    uint8_t i = slot->perm[k];
    uint64_t bit = i < n ? uint64_t{1} << i : 0;
    if (!bit || (seen & bit) || ((slot->entries[i].flags & kEntryFixed) && i != k)) {
      slot->rejected++;
      slot->import_seq = 0;
      return nullptr;
    }
    seen |= bit;
  }
  return slot->perm;
}

}  // namespace shixin::rerank
