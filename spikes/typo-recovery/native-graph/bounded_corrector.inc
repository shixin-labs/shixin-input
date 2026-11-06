// Scratch-only adapter. The legacy Corrections container still allocates;
// this experiment does not claim an allocation-free native pipeline.
#include <rime/dict/corrector.h>
#include "prefix_edits.h"
class ShixinSpikeCorrector : public rime::Corrector {
  shixin_spike::Keyboard keyboard_;
  size_t anchor_ = size_t(-1);
  bool specific_ = false;
  bool abbreviated_ = false;
  bool targeted_ = false;
  shixin_spike::EditTrace target_{};
  mutable size_t position_ = 0;

public:
  const shixin_spike::EditTrace* SpikeTargetTrace() const override {
    return targeted_ ? &target_ : nullptr;
  }
  void SpikeTarget(const shixin_spike::EditTrace* trace) override {
    targeted_ = trace != nullptr;
    target_ = trace ? *trace : shixin_spike::EditTrace{};
  }
  bool SpikeExactAllowed(size_t begin, size_t length) const override {
    return !targeted_ || begin + length <= target_.begin || begin >= target_.end;
  }
  void SpikeAbbreviated(bool value) override { abbreviated_ = value; }
  bool SpikeAbbreviatedOnly() const override { return abbreviated_; }
  void SpikeSpecific(bool value) override { specific_ = value; }
  bool SpikeSpecificOnly() const override { return specific_; }
  void SpikeAnchor(size_t anchor) override { anchor_ = anchor; }
  bool SpikeAllows(size_t pos) const override {
    if (targeted_) {
      position_ = pos;
      return pos <= target_.begin && target_.end - pos <= 8;
    }
    return anchor_ == size_t(-1) || pos == anchor_;
  }
  bool SpikeBounded() const override { return true; }
  void ToleranceSearch(const rime::Prism &prism, const std::string &key,
                       rime::corrector::Corrections *results, size_t) override {
    if (key.size() > 32)
      return;
    if (targeted_) {
      if (position_ > target_.begin || target_.end-position_ > key.size()) return;
      auto trace=target_; trace.begin-=position_; trace.end-=position_;
      std::array<char,8> edited{};
      const auto limit=std::min(key.size(),edited.size());
      std::copy_n(key.data(),limit,edited.data());
      for(size_t i=trace.begin;i<trace.end;++i) edited[i]=trace.to[i-trace.begin];
      for(size_t length=trace.end;length<=limit;++length) {
        int id=prism.trie().exactMatchSearch<int>(edited.data(),length);
        if(id>=0) results->Alter(id,{trace.cost,id,length,trace});
      }
      return;
    }
    struct Match {
      int id;
      size_t length, cost;
      shixin_spike::EditTrace trace;
    };
    std::array<Match, 64> matches{};
    size_t count = 0;
    bool complete = shixin_spike::PrefixEditsWithTrace(
        key, keyboard_,
        [&](std::string_view edited, const shixin_spike::EditTrace &trace) {
          const auto cost = trace.cost;
          int id =
              prism.trie().exactMatchSearch<int>(edited.data(), edited.size());
          if (id < 0)
            return true;
          for (size_t i = 0; i < count; i++)
            if (matches[i].id == id && matches[i].length == edited.size()) {
              if (cost < matches[i].cost) {
                matches[i].cost = cost;
                matches[i].trace = trace;
              }
              return true;
            }
          if (count == matches.size())
            return false;
          matches[count++] = {id, edited.size(), size_t(cost), trace};
          return true;
        });
    if (!complete)
      return;
    for (size_t i = 0; i < count; i++)
      results->Alter(matches[i].id, {matches[i].cost, matches[i].id,
                                     matches[i].length, matches[i].trace});
  }
};
