// Registers Shixin's native components under the shixin_ prefix (DESIGN
// §3.8). The module is "shixin": the engine loads it explicitly
// (modules {"default", "shixin"}); librime's plugins loader stays off.
#include <rime/component.h>
#include <rime/registry.h>
#include <rime_api.h>

#include "rerank.h"
#include "select_character.h"
#include "translators.h"
#include "candidate_filters.h"

using namespace rime;

static void rime_shixin_initialize() {
  Registry& r = Registry::instance();
  r.Register("shixin_select_character", new Component<shixin::SelectCharacter>);
  r.Register("shixin_rerank", new Component<shixin::RerankFilter>);
  r.Register("shixin_rerank_processor", new Component<shixin::RerankProcessor>);
  r.Register("shixin_date_translator", new Component<shixin::DateTranslator>);
  r.Register("shixin_number_translator",
             new Component<shixin::NumberTranslator>);
  r.Register("shixin_unicode_translator",
             new Component<shixin::UnicodeTranslator>);
  r.Register("shixin_calc_translator", new Component<shixin::CalcTranslator>);
  r.Register("shixin_long_word_filter", new Component<shixin::LongWordFilter>);
  r.Register("shixin_english_demote_filter",
             new Component<shixin::EnglishDemoteFilter>);
  r.Register("shixin_v_filter", new Component<shixin::VFilter>);
  r.Register("shixin_autocap_filter", new Component<shixin::AutocapFilter>);
  r.Register("shixin_pin_filter", new Component<shixin::PinFilter>);
}

static void rime_shixin_finalize() {}

RIME_REGISTER_MODULE(shixin)
