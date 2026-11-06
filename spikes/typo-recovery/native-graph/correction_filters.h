#pragma once
#include <rime/filter.h>
#include <rime/translation.h>
namespace rime {
// Scratch-only consumer cap. It bounds values pulled through a filter, not the
// cost of one third-party conversion call.
class SpikeLimitedTranslation : public Translation {
public:
  SpikeLimitedTranslation(an<Translation> source, size_t limit)
      : source_(std::move(source)), left_(limit) {
    set_exhausted(!left_ || !source_ || source_->exhausted());
  }
  an<Candidate> Peek() override {
    return exhausted() ? nullptr : source_->Peek();
  }
  bool Next() override {
    if (exhausted())
      return false;
    if (!--left_) {
      set_exhausted(true);
      return false;
    }
    source_->Next();
    set_exhausted(source_->exhausted());
    return !exhausted();
  }

private:
  an<Translation> source_;
  size_t left_;
};
// Own the uniquifier's candidate history separately from the ordinary menu.
// Finish before publication: later duplicate merging cannot mutate a shown
// list.
class SpikeCorrectionFilterTranslation : public Translation {
public:
  explicit SpikeCorrectionFilterTranslation(an<Translation> source)
      : source_(New<SpikeLimitedTranslation>(std::move(source), 64)) {
    candidates_.reserve(8);
  }
  void AddFilter(Filter *filter) {
    if (finished_ || !filter || !filter->SupportsSpikeCorrections())
      return;
    source_ =
        New<SpikeLimitedTranslation>(filter->Apply(source_, &candidates_), 64);
  }
  void Finish() {
    if (finished_)
      return;
    finished_ = true;
    while (candidates_.size() < 8 && source_ && !source_->exhausted()) {
      if (auto candidate = source_->Peek())
        candidates_.push_back(candidate);
      source_->Next();
    }
    source_.reset();
    set_exhausted(candidates_.empty());
  }
  size_t candidate_count() const { return finished_ ? candidates_.size() : 0; }
  an<Candidate> candidate_at(size_t index) const {
    return finished_ && index < candidates_.size() ? candidates_[index]
                                                   : nullptr;
  }
  an<Candidate> Peek() override {
    return !finished_ || exhausted() ? nullptr : candidates_[position_];
  }
  bool Next() override {
    if (!finished_ || exhausted())
      return false;
    set_exhausted(++position_ == candidates_.size());
    return !exhausted();
  }

private:
  an<Translation> source_;
  CandidateList candidates_;
  size_t position_ = 0;
  bool finished_ = false;
};
} // namespace rime
