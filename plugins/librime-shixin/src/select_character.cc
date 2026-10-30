#include "select_character.h"

#include <string>

#include <rime/candidate.h>
#include <rime/config.h>
#include <rime/context.h>
#include <rime/engine.h>
#include <rime/schema.h>

#include "utf8_edges.h"

namespace shixin {

using namespace rime;

SelectCharacter::SelectCharacter(const Ticket& ticket) : Processor(ticket) {
  if (!engine_ || !engine_->schema())
    return;
  Config* config = engine_->schema()->config();
  string repr;
  if (config->GetString("key_binder/select_first_character", &repr))
    has_first_ = first_key_.Parse(repr);
  if (config->GetString("key_binder/select_last_character", &repr))
    has_last_ = last_key_.Parse(repr);
}

ProcessResult SelectCharacter::ProcessKeyEvent(const KeyEvent& key_event) {
  // Common path: one or two comparisons, no allocation.
  if (key_event.release())
    return kNoop;
  bool first = has_first_ && key_event == first_key_;
  bool last = !first && has_last_ && key_event == last_key_;
  if (!first && !last)
    return kNoop;
  Context* ctx = engine_->context();
  if (!ctx->IsComposing() && !ctx->HasMenu())
    return kNoop;
  std::string_view text = ctx->input();
  an<Candidate> cand = ctx->GetSelectedCandidate();
  if (cand)
    text = cand->text();
  if (char_count(text) < 2)
    return kNoop;
  std::string_view ch = first ? first_char(text) : last_char(text);
  engine_->sink()(string(ch));
  ctx->Clear();
  return kAccepted;
}

}  // namespace shixin
