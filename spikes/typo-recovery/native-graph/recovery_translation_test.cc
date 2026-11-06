#include <gtest/gtest.h>
#include <array>
#include <rime/translation.h>
#include <rime/gear/translator_commons.h>
#include <rime/gear/uniquifier.h>
#include "correction_filters.h"

namespace rime {
// Exercise the production extractor with a deterministic recovery source.
// Dictionary lookup and spelling alignment are deliberately outside this test.
class ScriptTranslation : public FifoTranslation {
public:
  int SpikeCorrectionCount(const Phrase &phrase) const {
    return static_cast<int>(phrase.weight());
  }
  bool SpikeCorrectionTrace(const Phrase &phrase,
                            shixin_spike::EditTrace *trace) const {
    *trace = {
        shixin_spike::EditTrace::Kind::Neighbor, 0, 1, 10, {'s', 0}, {'a', 0}};
    return SpikeCorrectionCount(phrase) == 1;
  }
  bool Next() override {
    ++advances;
    return FifoTranslation::Next();
  }
  size_t advances = 0;
};
#include "protected_translation.inc"
} // namespace rime
using namespace rime;
static an<Phrase> Make(const std::string &text, size_t span = 5,
                       int edits = 1) {
  auto entry = New<DictEntry>();
  entry->text = text;
  entry->code.push_back(1);
  entry->code.push_back(2);
  entry->weight = edits;
  return New<Phrase>(nullptr, "phrase", 0, span, entry);
}
static std::vector<an<Candidate>> Drain(Translation &stream) {
  std::vector<an<Candidate>> result;
  while (!stream.exhausted()) {
    auto current = stream.Peek();
    EXPECT_EQ(stream.Peek(), current);
    result.push_back(current);
    stream.Next();
  }
  EXPECT_FALSE(stream.Next());
  EXPECT_FALSE(stream.Peek());
  return result;
}
TEST(RecoveryStream, IndependentOrdinaryCursorAndNativeIdentity) {
  auto ordinary = New<ScriptTranslation>();
  for (int i = 0; i < 40; ++i)
    ordinary->Append(Make("ordinary" + std::to_string(i)));
  auto recovery = New<ScriptTranslation>();
  auto native = Make("你好");
  recovery->Append(native);
  auto first = ordinary->Peek();
  SpikeRecoveryTranslation corrections(recovery, nullptr, 5);
  auto result = Drain(corrections);
  ASSERT_EQ(result.size(), 1u);
  EXPECT_TRUE(result[0]->is_spike_correction());
  EXPECT_EQ(Candidate::GetGenuineCandidate(result[0]), native);
  EXPECT_EQ(ordinary->advances, 0u);
  EXPECT_EQ(ordinary->Peek(), first);
  auto untouched = Drain(*ordinary);
  ASSERT_EQ(untouched.size(), 40u);
  for (size_t i = 0; i < untouched.size(); ++i)
    EXPECT_EQ(untouched[i]->text(), "ordinary" + std::to_string(i));
}
TEST(RecoveryStream, BoundedDedupAndPhaseQuotas) {
  auto first = New<ScriptTranslation>(), second = New<ScriptTranslation>();
  std::array<an<Candidate>, 10> exclusions{};
  exclusions[0] = Make("excluded");
  first->Append(exclusions[0]);
  first->Append(Make("zero", 5, 0));
  first->Append(Make("two", 5, 2));
  first->Append(Make("duplicate"));
  first->Append(Make("duplicate"));
  for (int i = 0; i < 20; ++i)
    first->Append(Make("full" + std::to_string(i)));
  for (int i = 0; i < 20; ++i)
    second->Append(Make("broad" + std::to_string(i)));
  second->Append(Make("prefix1", 3));
  second->Append(Make("prefix2", 3));
  second->Append(Make("prefix3", 3));
  SpikeRecoveryTranslation stream(first, second, 5, 4, exclusions);
  auto result = Drain(stream);
  ASSERT_EQ(result.size(), 8u);
  size_t full = 0, partial = 0;
  for (auto c : result) {
    EXPECT_NE(c->text(), "excluded");
    EXPECT_NE(c->text(), "zero");
    EXPECT_NE(c->text(), "two");
    if (c->end() == 5)
      ++full;
    else
      ++partial;
  }
  EXPECT_EQ(full, 6u);
  EXPECT_EQ(partial, 2u);
  EXPECT_LE(first->advances + second->advances, 64u);
}
TEST(RecoveryStream, ScanCapsAndAbsentFirstStage) {
  auto first = New<ScriptTranslation>(), second = New<ScriptTranslation>();
  for (int i = 0; i < 100; ++i) {
    first->Append(Make("invalid", 5, 0));
    second->Append(Make("invalid", 5, 2));
  }
  SpikeRecoveryTranslation exhausted(first, second, 5);
  EXPECT_TRUE(exhausted.exhausted());
  EXPECT_EQ(first->advances, 32u);
  EXPECT_EQ(second->advances, 32u);
  auto broad = New<ScriptTranslation>();
  broad->Append(Make("broad"));
  SpikeRecoveryTranslation broad_only(nullptr, broad, 5);
  ASSERT_EQ(Drain(broad_only).size(), 1u);
  SpikeRecoveryTranslation empty(nullptr, nullptr, 5);
  EXPECT_TRUE(empty.exhausted());
}
TEST(RecoveryStream, LegacyAdapterPreservesOrdinaryRelativeOrder) {
  auto ordinary = New<ScriptTranslation>(), recovery = New<ScriptTranslation>();
  for (int i = 0; i < 20; ++i)
    ordinary->Append(Make("ordinary" + std::to_string(i)));
  recovery->Append(Make("correction"));
  SpikeProtectedTranslation mixed(ordinary, recovery, nullptr, 5, 5);
  auto result = Drain(mixed);
  ASSERT_EQ(result.size(), 21u);
  EXPECT_EQ(result[5]->text(), "correction");
  for (size_t i = 0; i < 20; ++i)
    EXPECT_EQ(result[i < 5 ? i : i + 1]->text(),
              "ordinary" + std::to_string(i));
}

TEST(RecoveryStream, SeparateEnvelopeTransfersOnceEvenWithoutOrdinaryResults) {
  auto corrections = New<FifoTranslation>();
  auto native = Make("correction");
  corrections->Append(native);
  SpikeSeparateTranslation envelope(nullptr, corrections);
  EXPECT_TRUE(envelope.exhausted());
  EXPECT_FALSE(envelope.Peek());
  EXPECT_FALSE(envelope.Next());
  EXPECT_EQ(envelope.TakeSpikeCorrections(), corrections);
  EXPECT_FALSE(envelope.TakeSpikeCorrections());
  EXPECT_EQ(corrections->Peek(), native);
}

TEST(CorrectionFilters, IndependentHistoryAndUnauditedFiltersExcluded) {
  class Unaudited : public Filter {
  public:
    Unaudited() : Filter(Ticket{}) {}
    an<Translation> Apply(an<Translation> source, CandidateList *) override {
      ++calls;
      return source;
    }
    size_t calls = 0;
  } unaudited;
  Uniquifier unique(Ticket{});
  auto source = New<ScriptTranslation>();
  auto first = Make("same"), second = Make("same");
  source->Append(first);
  source->Append(second);
  SpikeCorrectionFilterTranslation pipeline(source);
  pipeline.AddFilter(&unaudited);
  pipeline.AddFilter(&unique);
  pipeline.Finish();
  EXPECT_EQ(unaudited.calls, 0u);
  auto result = Drain(pipeline);
  ASSERT_EQ(result.size(), 1u);
  EXPECT_EQ(result[0]->text(), "same");
  auto identities = Candidate::GetGenuineCandidates(result[0]);
  ASSERT_EQ(identities.size(), 2u);
  EXPECT_EQ(identities[0], first);
  EXPECT_EQ(identities[1], second);
}
TEST(CorrectionFilters, BoundedScanningAndFrozenEightChoiceSnapshot) {
  auto source = New<ScriptTranslation>();
  for (int i = 0; i < 100; ++i)
    source->Append(Make(std::to_string(i)));
  SpikeCorrectionFilterTranslation pipeline(source);
  pipeline.Finish();
  EXPECT_LE(source->advances, 8u);
  auto result = Drain(pipeline);
  ASSERT_EQ(result.size(), 8u);
  for (size_t i = 0; i < 8; ++i)
    EXPECT_EQ(result[i]->text(), std::to_string(i));
  auto advances = source->advances;
  pipeline.Finish();
  EXPECT_EQ(source->advances, advances);
  auto duplicates = New<ScriptTranslation>();
  for (int i = 0; i < 100; ++i)
    duplicates->Append(Make("same"));
  Uniquifier unique(Ticket{});
  SpikeCorrectionFilterTranslation bounded(duplicates);
  bounded.AddFilter(&unique);
  bounded.Finish();
  auto deduplicated = Drain(bounded);
  ASSERT_EQ(deduplicated.size(), 1u);
  EXPECT_LE(duplicates->advances, 64u);
  EXPECT_EQ(Candidate::GetGenuineCandidates(deduplicated[0]).size(), 64u);
}
