// Registers Shixin's native components under the shixin_ prefix (DESIGN
// §3.8). The module is "shixin": the engine loads it explicitly
// (modules {"default", "shixin"}); librime's plugins loader stays off.
#include <rime/component.h>
#include <rime/registry.h>
#include <rime_api.h>

#include "rerank.h"
#include "select_character.h"
#include "translators.h"

using namespace rime;

static void rime_shixin_initialize() {
  Registry& r = Registry::instance();
  r.Register("shixin_select_character", new Component<shixin::SelectCharacter>);
  r.Register("shixin_rerank", new Component<shixin::RerankFilter>);
  r.Register("shixin_rerank_processor", new Component<shixin::RerankProcessor>);
  r.Register("shixin_date_translator", new Component<shixin::DateTranslator>);
  r.Register("shixin_number_translator", new Component<shixin::NumberTranslator>);
  r.Register("shixin_unicode_translator", new Component<shixin::UnicodeTranslator>);
  r.Register("shixin_calc_translator", new Component<shixin::CalcTranslator>);
}

static void rime_shixin_finalize() {}

RIME_REGISTER_MODULE(shixin)
