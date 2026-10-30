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
// that share #1's span, exports them, and yields them in the imported
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
    if (!c || c->start() != start || c->end() != end)
      break;
    buf_[n_] = c;
    items[n_] = Item{c->text(), c->type(), Weight(c)};
    n_++;
    inner_->Next();
  }
  rerank::Export(slot, rerank::Hash(input, start, end), abbrev ? rerank::kExportAbbrev : 0, items,
                 n_);
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

ProcessResult RerankProcessor::ProcessKeyEvent(const KeyEvent& key_event) {
  if (key_event.keycode() != kRefreshKeycode)
    return kNoop;
  if (!key_event.release())
    engine_->context()->RefreshNonConfirmedComposition();
  return kAccepted;
}

}  // namespace shixin
