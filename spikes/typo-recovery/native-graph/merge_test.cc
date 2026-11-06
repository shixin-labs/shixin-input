// Output-policy tests only. The fake scorer marks every recovery item
// corrected; engine integration tests separately verify real Phrase
// alignment/selection.
#include <array>
#include <gtest/gtest.h>
#include <rime/candidate.h>
#include <rime/translation.h>
#include <vector>

namespace merge_test {
using namespace rime;
using Phrase = SimpleCandidate;
class ScriptTranslation : public Translation {
public:
  explicit ScriptTranslation(std::vector<an<Candidate>> items)
      : items_(std::move(items)) {
    set_exhausted(items_.empty());
  }
  an<Candidate> Peek() override {
    ++peeks;
    return exhausted() ? nullptr : items_[position_];
  }
  bool Next() override {
    ++position_;
    set_exhausted(position_ >= items_.size());
    return !exhausted();
  }
  int SpikeCorrectionCount(const Phrase &) const { return 1; }
  size_t peeks = 0;

private:
  std::vector<an<Candidate>> items_;
  size_t position_ = 0;
};
#include "protected_translation.inc"

an<Candidate> item(const std::string &text, size_t span = 3) {
  return New<SimpleCandidate>("phrase", 0, span, text);
}
an<ScriptTranslation> ordinary() {
  std::vector<an<Candidate>> items;
  for (int n = 0; n < 8; n++)
    items.push_back(item("ordinary" + std::to_string(n)));
  return New<ScriptTranslation>(items);
}
std::vector<an<Candidate>> drain(Translation &t) {
  std::vector<an<Candidate>> result;
  while (!t.exhausted() && result.size() < 50) {
    result.push_back(t.Peek());
    t.Next();
  }
  return result;
}
TEST(ProtectedMerge, BroadStageSurvivesSpecificFullSpanQuota) {
  std::vector<an<Candidate>> specific;
  for (int n = 0; n < 10; n++)
    specific.push_back(item("specific" + std::to_string(n)));
  specific.insert(specific.begin() + 4, item("partial", 2));
  auto first = New<ScriptTranslation>(specific);
  auto broad =
      New<ScriptTranslation>(std::vector<an<Candidate>>{item("broad target")});
  SpikeProtectedTranslation merged(ordinary(), first, broad, 5, 3);
  auto result = drain(merged);
  size_t corrections = 0;
  bool found = false, partial = false;
  for (size_t i = 0; i < result.size(); i++) {
    if (i < 5)
      EXPECT_EQ(result[i]->text(), "ordinary" + std::to_string(i));
    if (result[i]->is_spike_correction())
      ++corrections;
    found |= result[i]->text() == "broad target";
    partial |= result[i]->text() == "partial";
  }
  EXPECT_TRUE(found);
  EXPECT_TRUE(partial);
  EXPECT_LE(corrections, 8);
}
TEST(ProtectedMerge, DuplicateScanCannotStarveBroadStage) {
  std::vector<an<Candidate>> duplicates(100, item("ordinary0"));
  auto first = New<ScriptTranslation>(duplicates);
  auto broad =
      New<ScriptTranslation>(std::vector<an<Candidate>>{item("broad target")});
  SpikeProtectedTranslation merged(ordinary(), first, broad, 5, 3);
  auto result = drain(merged);
  bool found = false;
  for (auto &c : result)
    found |= c->text() == "broad target";
  EXPECT_TRUE(found);
  EXPECT_LE(first->peeks, 32);
  EXPECT_LE(first->peeks + broad->peeks, 64);
}
TEST(ProtectedMerge, ReservesTwoSlotsWithinEightCandidateTotal) {
  std::vector<an<Candidate>> specific, broad_items;
  for (int n = 0; n < 10; n++)
    specific.push_back(item("specific" + std::to_string(n), n + 1));
  for (int n = 0; n < 4; n++)
    broad_items.push_back(item("broad" + std::to_string(n), 12));
  auto first = New<ScriptTranslation>(specific);
  auto broad = New<ScriptTranslation>(broad_items);
  SpikeProtectedTranslation merged(ordinary(), first, broad, 5, 12);
  auto result = drain(merged);
  size_t specific_count = 0, broad_count = 0;
  for (auto &c : result) {
    if (c->text().find("specific") == 0)
      ++specific_count;
    if (c->text().find("broad") == 0)
      ++broad_count;
  }
  EXPECT_EQ(specific_count, 6);
  EXPECT_EQ(broad_count, 2);
}
TEST(ProtectedMerge, SameTextDifferentRawSpanIsSelectable) {
  auto first =
      New<ScriptTranslation>(std::vector<an<Candidate>>{item("ordinary0", 2)});
  SpikeProtectedTranslation merged(ordinary(), first, nullptr, 5, 3);
  auto result = drain(merged);
  ASSERT_GT(result.size(), 5);
  EXPECT_EQ(result[0]->text(), result[5]->text());
  EXPECT_EQ(result[0]->end(), 3);
  EXPECT_EQ(result[5]->end(), 2);
  EXPECT_TRUE(result[5]->is_spike_correction());
}
TEST(ProtectedMerge, CacheFirstLeavesFourSpecificFullSpanSlots) {
  std::vector<an<Candidate>> cached, specific;
  for (int n = 0; n < 8; ++n) {
    cached.push_back(item("cached" + std::to_string(n)));
    specific.push_back(item("specific" + std::to_string(n)));
  }
  SpikeProtectedTranslation merged(ordinary(), New<ScriptTranslation>(cached),
                                   New<ScriptTranslation>(specific), 5, 3, 2);
  auto result = drain(merged);
  size_t cached_count = 0, specific_count = 0;
  for (size_t i = 0; i < result.size(); ++i) {
    if (i < 5)
      EXPECT_EQ(result[i]->text(), "ordinary" + std::to_string(i));
    cached_count += result[i]->text().find("cached") == 0;
    specific_count += result[i]->text().find("specific") == 0;
  }
  EXPECT_EQ(cached_count, 2);
  EXPECT_EQ(specific_count, 4);
}
} // namespace merge_test
