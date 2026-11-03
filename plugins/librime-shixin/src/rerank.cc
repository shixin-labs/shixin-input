#include "rerank.h"

#include <algorithm>
#include <cmath>
#include <cstdlib>
#include <string>

#include <rime/candidate.h>
#include <rime/context.h>
#include <rime/engine.h>
#include <rime/segmentation.h>
#include <rime/translation.h>
#include <rime/gear/translator_commons.h>
#include <rime_api.h>

#include "rerank_slot.h"

#ifdef _WIN32
#define SHIXIN_EXPORT extern "C" RIME_DLL
#else
#define SHIXIN_EXPORT extern "C" __attribute__((visibility("default")))
#endif

// shixin_rerank_attach registers the engine's rerank slots: count slots of
// slot_size bytes at base, which must stay mapped until they are detached
// with count 0. It returns 0 if slot_size doesn't match this build's
// layout. Called on the engine thread, which is the only one using slots.
SHIXIN_EXPORT int shixin_rerank_attach(void* base, uint32_t count, uint32_t slot_size) {
  return shixin::rerank::Attach(base, count, slot_size) ? 1 : 0;
}

namespace shixin {

using namespace rime;
using rerank::Item;
using rerank::kMaxEntries;
using rerank::Slot;

namespace {

// Weight is a candidate's prior for the policy: its dictionary entry's log
// weight. quality() is no use there: librime adds exp(weight) to the
// translator's initial quality and a length term.
double Weight(const an<Candidate>& c) {
  if (auto phrase = As<Phrase>(Candidate::GetGenuineCandidate(c)))
    return phrase->weight();
  return std::log(std::max(c->quality(), 1e-30));
}

long SlotIndex(Context* ctx) {
  // A short decimal string: no heap allocation.
  string p = ctx->get_property(kSlotProperty);
  if (p.empty())
    return -1;
  char* end = nullptr;
  long i = std::strtol(p.c_str(), &end, 10);
  return *end == '\0' ? i : -1;
}

// RerankTranslation buffers the first candidates of the inner translation
// that start at #1's position, exports them, and yields them in the imported
// order, then the rest unchanged. The buffer is filled on first use, when
// librime builds the menu.
class RerankTranslation : public Translation {
 public:
  RerankTranslation(an<Translation> inner, Context* ctx, long slot)
      : inner_(std::move(inner)), ctx_(ctx), slot_(slot) {
    set_exhausted(inner_->exhausted());
  }

  bool Next() override {
    Fill();
    if (exhausted())
      return false;
    if (pos_ < n_)
      pos_++;
    else
      inner_->Next();
    set_exhausted(pos_ >= n_ && inner_->exhausted());
    return true;
  }

  an<Candidate> Peek() override {
    Fill();
    if (pos_ < n_)
      return buf_[order_[pos_]];
    return inner_->exhausted() ? nullptr : inner_->Peek();
  }

 private:
  void Fill();

  an<Translation> inner_;
  Context* ctx_;
  long slot_;
  bool filled_ = false;
  int n_ = 0;
  int pos_ = 0;
  uint8_t order_[kMaxEntries];
  an<Candidate> buf_[kMaxEntries];
};

void RerankTranslation::Fill() {
  if (filled_)
    return;
  filled_ = true;
  Slot* slot = rerank::SlotAt(slot_);
  if (!slot || !slot->enabled || inner_->exhausted())
    return;
  an<Candidate> first = inner_->Peek();
  if (!first)
    return;
  const string& input = ctx_->input();
  size_t start = first->start(), end = first->end();
  if (start > end || end > input.size())
    return;
  bool abbrev = rerank::IsAbbreviation(std::string_view(input).substr(start, end - start));
  int limit = abbrev ? rerank::kLimitAbbrev : rerank::kLimit;
  Item items[kMaxEntries];
  while (n_ < limit && !inner_->exhausted()) {
    an<Candidate> c = inner_->Peek();
    // AI may prefer a shorter prefix to the full phrase. Keep each actual
    // candidate, including its span, so Rime still selects only that prefix.
    if (!c || c->start() != start || c->end() <= start || c->end() > end)
      break;
    buf_[n_] = c;
    items[n_] = Item{c->text(), c->type(), Weight(c), c->end()};
    n_++;
    inner_->Next();
  }
  rerank::Export(slot, rerank::Hash(input, start, end), abbrev ? rerank::kExportAbbrev : 0, items,
                 n_);
  // Menus are lazy. The export may be prepared for the active segment or
  // the immediately preceding Chinese segment while punctuation is added.
  const auto& comp = ctx_->composition();
  slot->menu_start = static_cast<uint32_t>(start);
  slot->menu_id = 0;
  for (size_t k = comp.size(), checked = 0; k > 0 && checked < 2; checked++) {
    const auto& seg = comp[--k];
    if (seg.start == start && seg.menu) {
      slot->menu_id = reinterpret_cast<uintptr_t>(seg.menu.get());
      break;
    }
  }
  const uint8_t* perm = n_ > 1 ? rerank::Permutation(slot, n_) : nullptr;
  for (int k = 0; k < n_; k++)
    order_[k] = perm ? perm[k] : static_cast<uint8_t>(k);
  if (perm)
    slot->applied++;
}

}  // namespace

an<Translation> RerankFilter::Apply(an<Translation> translation, CandidateList* candidates) {
  Context* ctx = engine_->context();
  long i = SlotIndex(ctx);
  Slot* slot = rerank::SlotAt(i);
  if (!slot || !slot->enabled || !translation)
    return translation;
  return New<RerankTranslation>(std::move(translation), ctx, i);
}

bool RerankFilter::AppliesToSegment(Segment* segment) {
  return segment->HasTag("abc");
}

RerankProcessor::RerankProcessor(const Ticket& ticket) : Processor(ticket) {
  Context* ctx = engine_->context();
  // Run before the engine closes/commits the selected segment. These
  // callbacks stay entirely in C++; Go only reads the aggregate counters.
  select_connection_ = ctx->select_notifier().connect(
      [this](Context* c) { OnSelection(c); }, boost::signals2::at_front);
  commit_connection_ = ctx->commit_notifier().connect(
      [this](Context* c) { OnSelection(c); }, boost::signals2::at_front);
  // The engine's update listener has already rebuilt the menu. Highlight
  // and page changes keep it; editing or clearing invalidates its rating.
  update_connection_ = ctx->update_notifier().connect(
      [this](Context* c) { OnUpdate(c); });
}

RerankProcessor::~RerankProcessor() {
  select_connection_.disconnect();
  commit_connection_.disconnect();
  update_connection_.disconnect();
}

void RerankProcessor::OnUpdate(Context* ctx) {
  auto* slot = rerank::SlotAt(SlotIndex(ctx));
  if (!slot)
    return;
  const auto& comp = ctx->composition();
  for (size_t k = comp.size(), checked = 0; k > 0 && checked < 2; checked++) {
    if (reinterpret_cast<uintptr_t>(comp[--k].menu.get()) == slot->menu_id)
      return;  // paging/highlight, or a retained prefix before punctuation
  }
  slot->rated_seq = 0;
  // A downstream filter may have forced the export while AddFilter ran,
  // before librime assigned the new menu to its segment.
  slot->menu_id = 0;
  for (size_t k = comp.size(), checked = 0; k > 0 && checked < 2; checked++) {
    const auto& seg = comp[--k];
    if (seg.start == slot->menu_start && seg.menu) {
      slot->menu_id = reinterpret_cast<uintptr_t>(seg.menu.get());
      break;
    }
  }
}

void RerankProcessor::OnSelection(Context* ctx) {
  auto* slot = rerank::SlotAt(SlotIndex(ctx));
  if (!slot || !slot->enabled || !slot->rated_seq)
    return;
  const auto& comp = ctx->composition();
  for (size_t k = comp.size(), checked = 0; k > 0 && checked < 2; checked++) {
    const auto& seg = comp[--k];
    if (seg.menu && reinterpret_cast<uintptr_t>(seg.menu.get()) == slot->menu_id && seg.GetSelectedCandidate()) {
      rerank::RecordSelection(slot, seg.selected_index);
      return;
    }
  }
}

ProcessResult RerankProcessor::ProcessKeyEvent(const KeyEvent& key_event) {
  if (key_event.keycode() != kRefreshKeycode)
    return kNoop;
  if (!key_event.release())
    engine_->context()->RefreshNonConfirmedComposition();
  return kAccepted;
}

}  // namespace shixin
