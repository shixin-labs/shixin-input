#include <gtest/gtest.h>
#include <rime/candidate.h>
#include <rime/context.h>
#include <rime/gear/translator_commons.h>
#include <rime/language.h>
#include <rime/menu.h>
#include <rime/translation.h>

static void Prepare(rime::Context &context, const std::string &raw) {
  context.set_input(raw);
  context.composition().Reset(raw);
  ASSERT_TRUE(context.composition().AddSegment(rime::Segment(0, raw.size())));
  auto &segment = context.composition().back();
  segment.status = rime::Segment::kGuess;
  segment.menu = rime::New<rime::Menu>();
  auto ordinary = rime::New<rime::FifoTranslation>();
  for (size_t i = 0; i < 16; ++i)
    ordinary->Append(rime::New<rime::SimpleCandidate>(
        "ordinary", 0, raw.size(), "ordinary-" + std::to_string(i)));
  segment.menu->AddTranslation(ordinary);
  ASSERT_EQ(segment.menu->Prepare(16), 16u);
}
static rime::an<rime::Phrase> Phrase(size_t end,
                                     const rime::Language *language = nullptr) {
  auto entry = rime::New<rime::DictEntry>();
  entry->text = "你好";
  entry->code.push_back(1);
  entry->code.push_back(2);
  entry->weight = 42;
  return rime::New<rime::Phrase>(language, "phrase", 0, end, entry);
}

TEST(ExplicitSelection, NativeIdentityWithoutOrdinaryMenuMutation) {
  rime::Context context;
  Prepare(context, "nihso");
  auto &segment = context.composition().back();
  std::array<rime::an<rime::Candidate>, 16> before;
  for (size_t i = 0; i < before.size(); ++i)
    before[i] = segment.GetCandidateAt(i);
  rime::Language language("fixture");
  auto hidden = Phrase(5, &language);
  size_t notified = 0;
  auto connection = context.select_notifier().connect([&](rime::Context *c) {
    ++notified;
    EXPECT_EQ(c->GetSelectedCandidate(), hidden);
    EXPECT_EQ(c->composition().back().status, rime::Segment::kSelected);
  });
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  EXPECT_EQ(notified, 1u);
  auto selected = rime::As<rime::Phrase>(context.GetSelectedCandidate());
  ASSERT_EQ(selected, hidden);
  EXPECT_EQ(selected->language(), &language);
  ASSERT_EQ(selected->code().size(), 2u);
  EXPECT_EQ(selected->code()[0], 1);
  EXPECT_EQ(selected->code()[1], 2);
  EXPECT_EQ(selected->weight(), 42);
  EXPECT_EQ(context.GetCommitText(), "你好");
  EXPECT_EQ(segment.menu->Prepare(17), 16u);
  for (size_t i = 0; i < before.size(); ++i)
    EXPECT_EQ(segment.GetCandidateAt(i), before[i]);
}

TEST(ExplicitSelection, PartialCloseReopenAndOrdinaryHighlight) {
  rime::Context context;
  Prepare(context, "nihsozhongguo");
  auto hidden = Phrase(5);
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  auto &segment = context.composition().back();
  segment.Close();
  EXPECT_EQ(segment.end, 5u);
  EXPECT_EQ(context.input().substr(segment.end), "zhongguo");
  EXPECT_TRUE(segment.HasTag("partial"));
  ASSERT_TRUE(segment.Reopen(context.input().size()));
  EXPECT_EQ(segment.end, context.input().size());
  EXPECT_EQ(segment.GetSelectedCandidate(), hidden);
  size_t updates = 0;
  auto connection =
      context.update_notifier().connect([&](rime::Context *) { ++updates; });
  ASSERT_TRUE(context.Highlight(segment.selected_index));
  EXPECT_EQ(updates, 1u);
  EXPECT_FALSE(segment.selected_candidate_override);
  EXPECT_EQ(segment.GetSelectedCandidate(), segment.GetCandidateAt(0));
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  segment.Close();
  ASSERT_TRUE(segment.Reopen(4));
  EXPECT_FALSE(segment.selected_candidate_override);
  segment.Clear();
  EXPECT_FALSE(segment.selected_candidate_override);
}

TEST(ExplicitSelection, ValidationDeletionAndOrdinarySelection) {
  rime::Context context;
  EXPECT_FALSE(context.SelectExplicitCandidate(Phrase(5)));
  Prepare(context, "nihso");
  EXPECT_FALSE(context.SelectExplicitCandidate(nullptr));
  EXPECT_FALSE(context.SelectExplicitCandidate(Phrase(0)));
  EXPECT_FALSE(context.SelectExplicitCandidate(Phrase(6)));
  EXPECT_FALSE(context.SelectExplicitCandidate(
      rime::New<rime::SimpleCandidate>("bad", 1, 5, "bad")));
  auto hidden = Phrase(5);
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  EXPECT_FALSE(context.SelectExplicitCandidate(hidden));
  rime::an<rime::Candidate> deleted;
  auto connection = context.delete_notifier().connect(
      [&](rime::Context *c) { deleted = c->GetSelectedCandidate(); });
  ASSERT_TRUE(context.DeleteCurrentSelection());
  EXPECT_EQ(deleted, hidden);
  EXPECT_FALSE(context.DeleteCandidate(99));
  EXPECT_EQ(context.GetSelectedCandidate(), hidden);
  ASSERT_TRUE(context.DeleteCandidate(1));
  EXPECT_EQ(deleted, context.composition().back().GetCandidateAt(1));
  EXPECT_FALSE(context.composition().back().selected_candidate_override);
  context.composition().back().status = rime::Segment::kGuess;
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  ASSERT_TRUE(context.Select(2));
  EXPECT_FALSE(context.composition().back().selected_candidate_override);
  EXPECT_EQ(context.GetSelectedCandidate(),
            context.composition().back().GetCandidateAt(2));
}

TEST(ExplicitSelection, CommitUsesNativeSelectionThenReleasesIt) {
  rime::Context context;
  Prepare(context, "nihso");
  auto hidden = Phrase(5);
  std::weak_ptr<rime::Candidate> retained = hidden;
  ASSERT_TRUE(context.SelectExplicitCandidate(hidden));
  hidden.reset();
  ASSERT_FALSE(retained.expired());
  std::string committed;
  auto connection = context.commit_notifier().connect(
      [&](rime::Context *c) { committed = c->GetCommitText(); });
  ASSERT_TRUE(context.Commit());
  EXPECT_EQ(committed, "你好");
  EXPECT_TRUE(context.input().empty());
  EXPECT_TRUE(context.composition().empty());
  EXPECT_TRUE(retained.expired());
}

TEST(ExplicitSelection, FilterWrapperKeepsGenuinePhrase) {
  rime::Context context;
  Prepare(context, "nihso");
  auto phrase = Phrase(5);
  auto displayed = rime::New<rime::ShadowCandidate>(phrase, "phrase", "妳好", "correction");
  ASSERT_TRUE(context.SelectExplicitCandidate(displayed));
  EXPECT_EQ(context.GetCommitText(), "妳好");
  EXPECT_EQ(rime::Candidate::GetGenuineCandidate(context.GetSelectedCandidate()), phrase);
  EXPECT_EQ(context.composition().back().menu->Prepare(17), 16u);
}
