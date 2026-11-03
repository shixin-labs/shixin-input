// shixin_rerank: 智能排序 inside librime (DESIGN §7.2).
//
//   filter     first in the filter list, on abc segments. It exports the
//              first candidates starting at #1's input position into the session's
//              rerank slot (rerank_slot.h) and, when the slot holds an
//              order imported for that export, shows them in that order.
//              Everything after them, and every candidate when the slot is
//              off, passes through unchanged.
//   processor  first in the processor list. It accepts only
//              kRefreshKeycode, which only the engine sends, and refreshes
//              the composition so the filter runs again.
//
// A session's slot index is the decimal context property kSlotProperty,
// set once by the engine.
#ifndef SHIXIN_RERANK_H_
#define SHIXIN_RERANK_H_

#include <rime/filter.h>
#include <rime/key_event.h>
#include <rime/processor.h>

namespace rime { class Context; }

namespace shixin {

// kRefreshKeycode is outside X11's keysyms and Unicode's range
// (0x01000000 + code point). The engine never forwards it from clients.
constexpr int kRefreshKeycode = 0x01fffffe;
constexpr const char* kSlotProperty = "shixin_rerank_slot";

class RerankFilter : public rime::Filter {
 public:
  explicit RerankFilter(const rime::Ticket& ticket) : Filter(ticket) {}
  rime::an<rime::Translation> Apply(rime::an<rime::Translation> translation,
                                    rime::CandidateList* candidates) override;
  bool AppliesToSegment(rime::Segment* segment) override;
};

class RerankProcessor : public rime::Processor {
 public:
  explicit RerankProcessor(const rime::Ticket& ticket);
  ~RerankProcessor() override;
  rime::ProcessResult ProcessKeyEvent(const rime::KeyEvent& key_event) override;

 private:
  void OnSelection(rime::Context* ctx);
  void OnUpdate(rime::Context* ctx);
  rime::connection select_connection_, commit_connection_, update_connection_;
};

}  // namespace shixin

#endif  // SHIXIN_RERANK_H_
