// Scratch-only adapter. The legacy Corrections container still allocates;
// this experiment does not claim an allocation-free native pipeline.
#include <rime/dict/corrector.h>
#include "prefix_edits.h"
class ShixinSpikeCorrector : public rime::Corrector {
  shixin_spike::Keyboard keyboard_;
  size_t anchor_ = size_t(-1);
  bool specific_ = false;
  bool abbreviated_ = false;

public:
  void SpikeAbbreviated(bool value) override { abbreviated_ = value; }
  bool SpikeAbbreviatedOnly() const override { return abbreviated_; }
  void SpikeSpecific(bool value) override { specific_ = value; }
  bool SpikeSpecificOnly() const override { return specific_; }
  void SpikeAnchor(size_t anchor) override { anchor_ = anchor; }
  bool SpikeAllows(size_t pos) const override { return anchor_ == size_t(-1) || pos == anchor_; }
  bool SpikeBounded() const override { return true; }
  void ToleranceSearch(const rime::Prism &prism, const std::string &key,
                       rime::corrector::Corrections *results, size_t) override {
    if (key.size() > 32)
      return;
    struct Match {
      int id;
      size_t length, cost;
    };
    std::array<Match, 64> matches{};
    size_t count = 0;
    bool complete = shixin_spike::PrefixEdits(
        key, keyboard_, [&](std::string_view edited, int cost) {
          int id =
              prism.trie().exactMatchSearch<int>(edited.data(), edited.size());
          if (id < 0)
            return true;
          for (size_t i = 0; i < count; i++)
            if (matches[i].id == id && matches[i].length == edited.size()) {
              matches[i].cost = std::min(matches[i].cost, size_t(cost));
              return true;
            }
          if (count == matches.size())
            return false;
          matches[count++] = {id, edited.size(), size_t(cost)};
          return true;
        });
    if (!complete)
      return;
    for (size_t i = 0; i < count; i++)
      results->Alter(matches[i].id,
                     {matches[i].cost, matches[i].id, matches[i].length});
  }
};
