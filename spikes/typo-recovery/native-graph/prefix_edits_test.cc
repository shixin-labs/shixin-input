#include "prefix_edits.h"
#include <cassert>
#include <cstdlib>
#include <new>
#include <string>
static size_t allocations = 0;
void *operator new(size_t n) {
  allocations++;
  if (auto p = std::malloc(n ? n : 1))
    return p;
  throw std::bad_alloc();
}
void *operator new[](size_t n) { return ::operator new(n); }
void operator delete(void *p) noexcept { std::free(p); }
void operator delete[](void *p) noexcept { std::free(p); }
int main() {
  shixin_spike::Keyboard keyboard;
  for (auto raw : {"hoa", "hso", "nh", "ni'hao", "aaaaaaaa", "abcdefgh", ""}) {
    size_t count = 0;
    bool swap = false, neighbor = false;
    auto before = allocations;
    bool complete = shixin_spike::PrefixEdits(
        raw, keyboard, [&](std::string_view edit, int cost) {
          assert(edit.size() <= 8 && edit.find('\'') == std::string_view::npos);
          assert(edit != std::string_view(raw).substr(0, edit.size()));
          assert(cost == 8 || cost == 10);
          count++;
          if (edit == "hao" && cost == 8)
            swap = true;
          if (edit == "hao" && cost == 10)
            neighbor = true;
          return true;
        });
    assert(complete && count <= 256 && allocations == before);
    if (std::string_view(raw) == "hoa")
      assert(swap);
    if (std::string_view(raw) == "hso")
      assert(neighbor);
  }
  for (auto raw : {"hoa", "hso", "njh", "ni'hoa"}) {
    auto before = allocations;
    assert(shixin_spike::PrefixEditsWithTrace(
        raw, keyboard,
        [&](std::string_view edited, const shixin_spike::EditTrace &trace) {
          assert(trace.end <= edited.size() && trace.end > trace.begin);
          std::array<char, 8> applied{};
          for (size_t i = 0; i < edited.size(); ++i)
            applied[i] = raw[i];
          for (size_t i = trace.begin; i < trace.end; ++i) {
            assert(raw[i] == trace.from[i - trace.begin]);
            applied[i] = trace.to[i - trace.begin];
          }
          assert(std::string_view(applied.data(), edited.size()) == edited);
          assert(trace.cost ==
                 (trace.kind == shixin_spike::EditTrace::Kind::Swap ? 8 : 10));
          return true;
        }));
    assert(allocations == before);
  }
  size_t calls = 0;
  assert(!shixin_spike::PrefixEdits(
      "hao", keyboard,
      [&](auto, int) {
        calls++;
        return true;
      },
      0));
  assert(calls == 0);
  assert(!shixin_spike::PrefixEdits("hao", keyboard,
                                    [](auto, int) { return false; }));
}
