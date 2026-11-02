#ifndef SHIXIN_CANDIDATE_FILTERS_H_
#define SHIXIN_CANDIDATE_FILTERS_H_
#include <rime/filter.h>
#include <memory>
#include "filter_rules.h"
namespace shixin {
class CandidateFilter : public rime::Filter {
 public:
  CandidateFilter(const rime::Ticket& ticket, FilterKind kind);
  ~CandidateFilter() override;
  rime::an<rime::Translation> Apply(rime::an<rime::Translation>,
                                    rime::CandidateList*) override;
  bool AppliesToSegment(rime::Segment*) override;

 private:
  struct Storage;
  std::unique_ptr<Storage> storage_;
};
#define SHIXIN_FILTER_CLASS(Name, Kind)           \
  class Name : public CandidateFilter {           \
   public:                                        \
    explicit Name(const rime::Ticket& t)          \
        : CandidateFilter(t, FilterKind::Kind) {} \
  };
SHIXIN_FILTER_CLASS(LongWordFilter, LongWord)
SHIXIN_FILTER_CLASS(EnglishDemoteFilter, EnglishDemote)
SHIXIN_FILTER_CLASS(VFilter, VSymbols)
SHIXIN_FILTER_CLASS(AutocapFilter, Autocap)
SHIXIN_FILTER_CLASS(PinFilter, Pin)
#undef SHIXIN_FILTER_CLASS
}  // namespace shixin
#endif
