#include "candidate_filters.h"
#include <rime/config.h>
#include <rime/context.h>
#include <rime/engine.h>
#include <rime/schema.h>
#include <rime/segmentation.h>
#include <rime/translation.h>
#include <vector>

namespace shixin {
using namespace rime;
namespace {
struct PinRule {
  std::string code, text;
};

// Keep the genuine candidate attached: Rime's learning/deletion still sees
// its original dictionary entry when transformed English is selected.
class CaseCandidate : public ShadowCandidate {
 public:
  CaseCandidate()
      : ShadowCandidate(New<SimpleCandidate>("", 0, 0, ""),
                        std::string(32, ' ')) {
    text_.reserve(256);
  }
  void Reset(const an<Candidate>& c, const char* text) {
    item_ = c;
    text_.assign(text, c->text().size());
    set_type(c->type());
    set_start(c->start());
    set_end(c->end());
    set_quality(c->quality());
  }
  void Clear() { item_.reset(); }
};

class FilterTranslation : public Translation {
 public:
  explicit FilterTranslation(bool autocap) {
    if (autocap)
      for (auto& c : case_)
        c = New<CaseCandidate>();
  }
  bool Available() const {
    for (const auto& c : case_)
      if (c && c.use_count() != 1)
        return false;
    return true;
  }
  // Releases the previous menu's candidates. Idle slots are cleared on
  // every Apply, so an already released slot returns at once.
  void Clear() {
    if (!inner_)
      return;
    inner_.reset();
    for (size_t i = 0; i < n_; i++) {
      buffer_[i].reset();
      if (case_[i] && case_[i].use_count() == 1)
        case_[i]->Clear();
    }
    n_ = pos_ = 0;
  }
  void Reset(an<Translation> inner,
             FilterKind kind,
             std::string_view code,
             const std::vector<PinRule>& pins) {
    Clear();
    inner_ = std::move(inner);
    std::array<FilterItem, kFilterWindow> items;
    size_t start = 0, end = 0;
    while (n_ < kFilterWindow && !inner_->exhausted()) {
      an<Candidate> c = inner_->Peek();
      if (!c)
        break;
      if (!n_) {
        start = c->start();
        end = c->end();
      }
      if (start > end || end > code.size())
        break;
      // Never reorder a shorter segment ahead of a complete phrase.
      if (c->start() != start || c->end() != end)
        break;
      // Native translator results are exact readings, never word-filter input.
      if (c->type() == "date" || c->type() == "number" ||
          c->type() == "unicode" || c->type() == "calc")
        break;
      buffer_[n_] = c;
      items[n_] = {c->text(), c->type(), -1};
      if (kind == FilterKind::Pin && end == code.size()) {
        int rank = 0;
        for (const auto& p : pins) {
          if (p.code != code.substr(start))
            continue;
          if (p.text == c->text()) {
            items[n_].pin = rank;
            break;
          }
          rank++;
        }
      }
      if (kind == FilterKind::Autocap && c->type().size() <= 32) {
        char text[256];
        if (autocap_text(code.substr(start, end - start), c->text(), c->type(),
                         text, sizeof(text))) {
          case_[n_]->Reset(c, text);
          buffer_[n_] = case_[n_];
        }
      }
      n_++;
      inner_->Next();
    }
    filter_order(kind, items.data(), n_, &order_);
    set_exhausted(!n_ && inner_->exhausted());
  }
  bool Next() override {
    if (exhausted())
      return false;
    if (pos_ < n_)
      pos_++;
    else
      inner_->Next();
    set_exhausted(pos_ >= n_ && inner_->exhausted());
    return !exhausted();
  }
  an<Candidate> Peek() override {
    return pos_ < n_ ? buffer_[order_[pos_]] : inner_->Peek();
  }

 private:
  an<Translation> inner_;
  std::array<an<Candidate>, kFilterWindow> buffer_;
  std::array<an<CaseCandidate>, kFilterWindow> case_;
  std::array<size_t, kFilterWindow> order_;
  size_t n_ = 0, pos_ = 0;
};
}  // namespace

// Every selected segment keeps its menu, and so one of these translations,
// until the composition ends. The pool starts warm for ordinary typing and
// grows, rarely and only up to kMaxPool, for long piecewise selections.
constexpr size_t kWarmPool = 4, kMaxPool = 64;

struct CandidateFilter::Storage {
  FilterKind kind;
  bool enabled = false;
  std::vector<PinRule> pins;
  std::vector<an<FilterTranslation>> pool;
};

CandidateFilter::CandidateFilter(const Ticket& ticket, FilterKind kind)
    : Filter(ticket), storage_(new Storage) {
  auto& s = *storage_;
  s.kind = kind;
  if (!engine_ || !engine_->schema())
    return;
  auto config = engine_->schema()->config();
  if (!config)
    return;
  const char* key = kind == FilterKind::LongWord        ? "long_word"
                    : kind == FilterKind::EnglishDemote ? "demote_english"
                    : kind == FilterKind::VSymbols      ? "v_symbols"
                                                        : "autocap";
  if (kind == FilterKind::Pin) {
    if (auto list = config->GetList("shixin_filters/pins")) {
      for (size_t i = 0; i < list->size() && i < 256; i++) {
        auto v = list->GetValueAt(i);
        std::string value;
        if (!v || !v->GetString(&value))
          continue;
        auto tab = value.find('\t');
        if (tab == std::string::npos || tab == 0 || tab > 64 ||
            value.size() - tab - 1 > 256)
          continue;
        s.pins.push_back({value.substr(0, tab), value.substr(tab + 1)});
      }
    }
    s.enabled = !s.pins.empty();
  } else
    config->GetBool(std::string("shixin_filters/") + key, &s.enabled);
  if (s.enabled) {
    s.pool.reserve(kMaxPool);
    for (size_t i = 0; i < kWarmPool; i++)
      s.pool.push_back(New<FilterTranslation>(kind == FilterKind::Autocap));
  }
}
CandidateFilter::~CandidateFilter() = default;

an<Translation> CandidateFilter::Apply(an<Translation> input, CandidateList*) {
  auto& s = *storage_;
  if (!s.enabled || !input || input->exhausted())
    return input;
  const auto& code = engine_->context()->input();
  if (code.size() > 512)
    return input;
  if (s.kind == FilterKind::VSymbols && (code.size() != 2 || code[0] != 'v'))
    return input;
  if (s.kind == FilterKind::Autocap &&
      (code.size() < 2 || code[0] < 'A' || code[0] > 'Z'))
    return input;
  if (s.kind == FilterKind::Pin) {
    bool found = false;
    for (const auto& p : s.pins)
      if (p.code == code) {
        found = true;
        break;
      }
    if (!found)
      return input;
  }
  // Warmed pools avoid allocations on keys. If retained menus own every
  // slot, add one; at the bound, preserve upstream order instead.
  for (auto& p : s.pool)
    if (p.use_count() == 1)
      p->Clear();
  for (auto& p : s.pool) {
    if (p.use_count() != 1 || !p->Available())
      continue;
    p->Reset(std::move(input), s.kind, code, s.pins);
    return p;
  }
  if (s.pool.size() >= kMaxPool)
    return input;
  s.pool.push_back(New<FilterTranslation>(s.kind == FilterKind::Autocap));
  s.pool.back()->Reset(std::move(input), s.kind, code, s.pins);
  return s.pool.back();
}
bool CandidateFilter::AppliesToSegment(Segment* segment) {
  return storage_->kind == FilterKind::VSymbols || segment->HasTag("abc");
}
}  // namespace shixin
