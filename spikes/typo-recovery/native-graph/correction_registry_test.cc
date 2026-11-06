#include <gtest/gtest.h>
#include <iostream>
#include <chrono>
#include <cstdlib>
#include <new>
#include <rime/menu.h>
#include <rime/translation.h>
#include "correction_registry.h"
#include "correction_filters.h"
using namespace shixin_spike;

static thread_local bool track_registry_allocations = false;
static thread_local size_t registry_allocations = 0;
void *operator new(size_t bytes) {
  if (track_registry_allocations)
    ++registry_allocations;
  if (auto *p = std::malloc(bytes ? bytes : 1))
    return p;
  throw std::bad_alloc();
}
void operator delete(void *p) noexcept { std::free(p); }
void *operator new[](size_t bytes) { return ::operator new(bytes); }
void operator delete[](void *p) noexcept { ::operator delete(p); }

static void PrepareContext(rime::Context &c, const std::string &raw = "nihoa") {
  c.set_input(raw);
  c.composition().Reset(raw);
  ASSERT_TRUE(c.composition().AddSegment(rime::Segment(0, raw.size())));
  auto &s = c.composition().back();
  s.status = rime::Segment::kGuess;
  s.menu = rime::New<rime::Menu>();
  auto ordinary = rime::New<rime::FifoTranslation>();
  ordinary->Append(
      rime::New<rime::SimpleCandidate>("ordinary", 0, raw.size(), "ordinary"));
  s.menu->AddTranslation(ordinary);
  ASSERT_EQ(s.menu->Prepare(1), 1u);
}
static rime::an<rime::Phrase> MakePhrase(const std::string &text = "你好") {
  auto entry = rime::New<rime::DictEntry>();
  entry->text = text;
  entry->code.push_back(1);
  entry->code.push_back(2);
  return rime::New<rime::Phrase>(nullptr, "phrase", 0, 5, entry);
}
static CorrectionChoice Choice(rime::an<rime::Candidate> c) {
  return {std::move(c), {EditKind::Swap, 3, 5, 1.0f}};
}
static const CorrectionScope scope{7, 11, 13};

TEST(CorrectionRegistry, ExplicitSelectionInvalidatesBeforeNotification) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry;
  auto phrase = MakePhrase();
  auto choice = Choice(phrase);
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  EXPECT_EQ(view.text, "你好");
  EXPECT_EQ(view.begin, 0u);
  EXPECT_EQ(view.end, 5u);
  EXPECT_EQ(view.edit.kind, EditKind::Swap);
  EXPECT_EQ(view.edit.cost, 1);
  auto ordinary = c.composition().back().GetCandidateAt(0);
  registry.Seal();
  size_t notifications = 0;
  auto connection = c.select_notifier().connect([&](rime::Context *context) {
    ++notifications;
    EXPECT_EQ(registry.size(), 0u);
    EXPECT_FALSE(registry.Select(*context, scope, view.token));
    EXPECT_EQ(context->GetSelectedCandidate(), phrase);
  });
  ASSERT_TRUE(registry.Select(c, scope, view.token));
  EXPECT_EQ(notifications, 1u);
  EXPECT_EQ(c.composition().back().GetCandidateAt(0), ordinary);
  EXPECT_EQ(c.composition().back().menu->Prepare(2), 1u);
  EXPECT_FALSE(registry.Select(c, scope, view.token));
}

TEST(CorrectionRegistry, ScopeRawSpanAndPublicationGuards) {
  rime::Context c, other;
  PrepareContext(c);
  PrepareContext(other);
  CorrectionRegistry registry;
  auto choice = Choice(MakePhrase());
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  const auto token = view.token;
  for (auto wrong : {CorrectionScope{8, 11, 13}, CorrectionScope{7, 12, 13},
                     CorrectionScope{7, 11, 14}}) {
    EXPECT_FALSE(registry.Read(c, wrong, 0, &view));
    EXPECT_FALSE(registry.Select(c, wrong, token));
  }
  EXPECT_FALSE(registry.Select(other, scope, token));
  auto bad = token;
  bad.slot = 8;
  EXPECT_FALSE(registry.Select(c, scope, bad));
  bad = token;
  ++bad.publication;
  EXPECT_FALSE(registry.Select(c, scope, bad));
  bad = token;
  ++bad.scope.revision;
  EXPECT_FALSE(registry.Select(c, scope, bad));
  c.set_input("nihso");
  EXPECT_FALSE(registry.Read(c, scope, 0, &view));
  EXPECT_FALSE(registry.Select(c, scope, token));
  c.set_input("nihoa");
  c.composition().back().end = 4;
  EXPECT_FALSE(registry.Select(c, scope, token));
  c.composition().back().end = 5;
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  EXPECT_FALSE(registry.Select(c, scope, token));
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  EXPECT_NE(view.token.publication, token.publication);
  EXPECT_TRUE(registry.Select(c, scope, view.token));
}

TEST(CorrectionRegistry, ShownSnapshotRejectsLateReplacement) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry;
  auto a = Choice(MakePhrase("你好")), b = Choice(MakePhrase("您好"));
  ASSERT_TRUE(registry.Publish(c, scope, &a, 1));
  registry.Seal();
  EXPECT_FALSE(registry.Publish(c, scope, &b, 1));
  CorrectionView old;
  ASSERT_TRUE(registry.Read(c, scope, 0, &old));
  EXPECT_EQ(old.text, "你好");
  auto next = scope;
  ++next.revision;
  ASSERT_TRUE(registry.Publish(c, next, &b, 1));
  EXPECT_FALSE(registry.Select(c, next, old.token));
  CorrectionView current;
  ASSERT_TRUE(registry.Read(c, next, 0, &current));
  EXPECT_EQ(current.text, "您好");
  registry.Clear();
  ASSERT_TRUE(registry.Publish(c, next, nullptr, 0));
  registry.Seal();
  EXPECT_FALSE(registry.Publish(c, next, &a, 1));
  EXPECT_EQ(registry.size(), 0u);
}

TEST(CorrectionRegistry, RejectsMutatedCandidateContent) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry;
  auto phrase = MakePhrase();
  auto choice = Choice(phrase);
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  const auto token = view.token;
  phrase->code()[0] = 9;
  EXPECT_FALSE(registry.Read(c, scope, 0, &view));
  EXPECT_FALSE(registry.Select(c, scope, token));
  phrase->code()[0] = 1;
  phrase->set_end(4);
  EXPECT_FALSE(registry.Select(c, scope, token));
  phrase->set_end(5);
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  // DictEntry text can be changed through another native owner.
  auto entry = rime::New<rime::DictEntry>();
  entry->text = "你好";
  entry->code.push_back(1);
  auto mutable_phrase = rime::New<rime::Phrase>(nullptr, "phrase", 0, 5, entry);
  choice = Choice(mutable_phrase);
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  entry->text = "您好";
  EXPECT_FALSE(registry.Read(c, scope, 0, &view));
  EXPECT_FALSE(registry.Select(c, scope, view.token));
}

TEST(CorrectionRegistry, BoundedValidationAndOwnership) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry;
  auto phrase = MakePhrase();
  auto valid = Choice(phrase);
  std::array<CorrectionChoice, 9> choices;
  choices.fill(valid);
  ASSERT_TRUE(registry.Publish(c, scope, choices.data(), 8));
  EXPECT_EQ(registry.size(), 8u);
  EXPECT_FALSE(registry.Publish(c, scope, choices.data(), 9));
  EXPECT_EQ(registry.size(), 0u);
  for (int which = 0; which < 8; ++which) {
    auto bad = valid;
    switch (which) {
    case 0:
      bad.edit.cost = -1;
      break;
    case 1:
      bad.edit.cost = std::numeric_limits<float>::quiet_NaN();
      break;
    case 2:
      bad.edit.end = 6;
      break;
    case 3:
      bad.edit.begin = 4;
      break;
    case 4:
      bad.edit.kind = static_cast<EditKind>(255);
      break;
    case 5:
      bad.candidate = MakePhrase(std::string(257, 'x'));
      break;
    case 6:
      bad.candidate.reset();
      break;
    case 7:
      bad.candidate = rime::New<rime::SimpleCandidate>("text", 0, 5, "你好");
      break;
    }
    ASSERT_TRUE(registry.Publish(c, scope, &valid, 1));
    EXPECT_FALSE(registry.Publish(c, scope, &bad, 1));
    EXPECT_EQ(registry.size(), 0u);
  }
  std::weak_ptr<rime::Candidate> lifetime;
  {
    auto owned = MakePhrase();
    lifetime = owned;
    auto item = Choice(owned);
    ASSERT_TRUE(registry.Publish(c, scope, &item, 1));
  }
  EXPECT_FALSE(lifetime.expired());
  registry.Clear();
  EXPECT_TRUE(lifetime.expired());
  std::cout << "REGISTRY_BYTES " << sizeof(CorrectionRegistry) << '\n';
}

TEST(CorrectionRegistry, PublicationCounterNeverWraps) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry(std::numeric_limits<uint64_t>::max() - 1);
  auto choice = Choice(MakePhrase());
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  EXPECT_EQ(view.token.publication, std::numeric_limits<uint64_t>::max());
  EXPECT_FALSE(registry.Publish(c, scope, &choice, 1));
  EXPECT_FALSE(registry.Select(c, scope, view.token));
  auto next = scope;
  ++next.generation;
  EXPECT_FALSE(registry.Publish(c, next, &choice, 1));
}

TEST(CorrectionRegistry, ExactRawTextAndCodeLimits) {
  rime::Context c;
  PrepareContext(c, std::string(32, 'a'));
  CorrectionRegistry registry;
  auto phrase = MakePhrase(std::string(256, 'x'));
  phrase->code().resize(32, 1);
  auto choice = Choice(phrase);
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  EXPECT_EQ(view.text.size(), 256u);
  phrase->code().resize(33, 1);
  EXPECT_FALSE(registry.Publish(c, scope, &choice, 1));
  phrase->code().clear();
  EXPECT_FALSE(registry.Publish(c, scope, &choice, 1));
  phrase->code().push_back(1);
  c.set_input(std::string(33, 'a'));
  c.composition().back().end = 33;
  EXPECT_FALSE(registry.Publish(c, scope, &choice, 1));
}

TEST(CorrectionRegistry, AbsoluteSpanAfterAnEarlierSelection) {
  rime::Context c;
  c.set_input("xxnihoa");
  c.composition().Reset(c.input());
  c.composition().emplace_back(0, 2);
  c.composition().back().status = rime::Segment::kSelected;
  c.composition().emplace_back(2, 7);
  c.composition().back().status = rime::Segment::kGuess;
  auto phrase = MakePhrase();
  phrase->set_start(2);
  phrase->set_end(7);
  auto choice = Choice(phrase);
  choice.edit.begin = 5;
  choice.edit.end = 7;
  CorrectionRegistry registry;
  ASSERT_TRUE(registry.Publish(c, scope, &choice, 1));
  CorrectionView view;
  ASSERT_TRUE(registry.Read(c, scope, 0, &view));
  EXPECT_EQ(view.begin, 2u);
  EXPECT_EQ(view.end, 7u);
  ASSERT_TRUE(registry.Select(c, scope, view.token));
  EXPECT_EQ(c.composition().front().end, 2u);
  EXPECT_EQ(c.GetSelectedCandidate(), phrase);
}

TEST(CorrectionRegistry, BoundedSnapshotsDoNotAllocateAfterWarmup) {
  rime::Context c;
  PrepareContext(c);
  CorrectionRegistry registry;
  std::array<CorrectionChoice, 8> choices;
  choices.fill(Choice(MakePhrase()));
  CorrectionScope current = scope;
  CorrectionView view;
  c.select_notifier()(
      &c); // Warm native notification machinery, not a selection.
  bool valid = true;
  registry_allocations = 0;
  const auto began = std::chrono::steady_clock::now();
  track_registry_allocations = true;
  for (size_t attempt = 0; attempt < 1000; ++attempt) {
    ++current.revision;
    c.composition().back().status = rime::Segment::kGuess;
    c.composition().back().selected_candidate_override.reset();
    valid &= registry.Publish(c, current, choices.data(), choices.size());
    for (size_t i = 0; i < choices.size(); ++i)
      valid &= registry.Read(c, current, i, &view);
    registry.Seal();
    valid &= registry.Select(c, current, view.token);
  }
  track_registry_allocations = false;
  const auto elapsed = std::chrono::steady_clock::now() - began;
  EXPECT_TRUE(valid);
  EXPECT_EQ(registry_allocations, 0u);
  std::cout
      << "REGISTRY_CYCLES count=1000 allocations=" << registry_allocations
      << " mean_ns="
      << std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count() /
             1000
      << '\n';
}

TEST(CorrectionRegistry, NativeTraceRequiresMatchingRawAndValidEditShape) {
  class Traced : public rime::ShadowCandidate {
  public:
    explicit Traced(rime::an<rime::Candidate> c)
        : ShadowCandidate(c, c->type()) {}
    EditTrace trace{EditTrace::Kind::Swap, 3, 5, 8, {'o', 'a'}, {'a', 'o'}};
    const EditTrace *spike_edit_trace() const override { return &trace; }
  };
  rime::Context c;
  PrepareContext(c, "nihoa");
  CorrectionChoice choice;
  auto phrase = MakePhrase();
  EXPECT_FALSE(ChoiceFromNativeTrace(c, phrase, &choice));
  auto traced = rime::New<Traced>(phrase);
  ASSERT_TRUE(ChoiceFromNativeTrace(c, traced, &choice));
  EXPECT_EQ(choice.edit.kind, EditKind::Swap);
  EXPECT_EQ(choice.edit.begin, 3u);
  EXPECT_EQ(choice.edit.end, 5u);
  EXPECT_EQ(choice.edit.cost, 8.0f);
  auto converted = rime::New<rime::ShadowCandidate>(traced, "phrase", "您好");
  auto unique = rime::New<rime::UniquifiedCandidate>(converted, "phrase");
  ASSERT_TRUE(ChoiceFromNativeTrace(c, unique, &choice));
  EXPECT_EQ(choice.candidate->text(), "您好");
  EXPECT_EQ(rime::Candidate::GetGenuineCandidate(choice.candidate), phrase);
  EXPECT_EQ(choice.edit.kind, EditKind::Swap);
  auto genuine_list = rime::Candidate::GetGenuineCandidates(unique);
  ASSERT_EQ(genuine_list.size(), 1u);
  EXPECT_EQ(genuine_list[0], phrase);
  CorrectionRegistry converted_registry;
  ASSERT_TRUE(converted_registry.Publish(c, scope, &choice, 1));
  CorrectionView converted_view;
  ASSERT_TRUE(converted_registry.Read(c, scope, 0, &converted_view));
  EXPECT_EQ(converted_view.text, "您好");
  rime::an<rime::Candidate> deep = phrase;
  for (int i = 0; i < 8; ++i)
    deep = rime::New<rime::ShadowCandidate>(deep, "phrase");
  EXPECT_EQ(rime::Candidate::GetGenuineCandidate(deep), phrase);
  deep = rime::New<rime::ShadowCandidate>(deep, "phrase");
  EXPECT_FALSE(rime::Candidate::GetGenuineCandidate(deep));
  c.set_input("nihao");
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, &choice));
  c.set_input("nihoa");
  traced->trace.end = 4;
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, &choice));
  traced->trace = {EditTrace::Kind::Neighbor, 3, 4, 10, {'o', 0}, {'i', 0}};
  EXPECT_TRUE(ChoiceFromNativeTrace(c, traced, &choice));
  traced->trace.to[0] = 'o';
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, &choice));
  traced->trace.to[0] = '\'';
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, &choice));
  traced->trace.to[0] = 'i';
  traced->trace.cost = 0;
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, &choice));
  EXPECT_FALSE(ChoiceFromNativeTrace(c, nullptr, &choice));
  EXPECT_FALSE(ChoiceFromNativeTrace(c, traced, nullptr));
}

class ContextTracedCandidate : public rime::ShadowCandidate {
public:
  explicit ContextTracedCandidate(rime::an<rime::Candidate> c)
      : ShadowCandidate(c, c->type()) {}
  EditTrace trace{EditTrace::Kind::Swap, 3, 5, 8, {'o', 'a'}, {'a', 'o'}};
  const EditTrace *spike_edit_trace() const override { return &trace; }
};
static void PublishContextChoice(rime::Context &context) {
  PrepareContext(context, "nihoa");
  auto source = rime::New<rime::FifoTranslation>();
  source->Append(rime::New<ContextTracedCandidate>(MakePhrase()));
  auto snapshot = rime::New<rime::SpikeCorrectionFilterTranslation>(source);
  snapshot->Finish();
  context.composition().back().spike_corrections = snapshot;
  ASSERT_TRUE(context.ConfigureSpikeCorrections(17));
  ASSERT_TRUE(context.PublishSpikeCorrections());
  ASSERT_EQ(context.SpikeCorrectionSize(), 1u);
}
TEST(ContextCorrectionRegistry, InvalidatesBeforeSynchronousCallbacks) {
  for (int operation = 0; operation < 8; ++operation) {
    SCOPED_TRACE(operation);
    rime::Context context;
    PublishContextChoice(context);
    CorrectionView view;
    ASSERT_TRUE(context.ReadSpikeCorrection(0, &view));
    auto token = view.token;
    size_t calls = 0;
    auto check = [&](rime::Context *c) {
      ++calls;
      EXPECT_EQ(c->SpikeCorrectionSize(), 0u);
      EXPECT_FALSE(c->SelectSpikeCorrection(token));
    };
    auto update = context.update_notifier().connect(check);
    auto select = context.select_notifier().connect(check);
    auto commit = context.commit_notifier().connect(check);
    auto deleted = context.delete_notifier().connect(check);
    auto option = context.option_update_notifier().connect(
        [&](auto *c, const auto &) { check(c); });
    auto property = context.property_update_notifier().connect(
        [&](auto *c, const auto &) { check(c); });
    switch (operation) {
    case 0:
      context.set_input("nihoa");
      break;
    case 1:
      context.set_caret_pos(4);
      break;
    case 2:
      context.set_option("traditionalization", true);
      break;
    case 3:
      context.set_property("fixture", "value");
      break;
    case 4:
      context.Clear();
      break;
    case 5:
      ASSERT_TRUE(context.Select(0));
      break;
    case 6:
      ASSERT_TRUE(context.DeleteCandidate(0));
      break;
    case 7:
      ASSERT_TRUE(context.Commit());
      break;
    }
    EXPECT_GT(calls, 0u);
    EXPECT_FALSE(context.ReadSpikeCorrection(0, &view));
    EXPECT_FALSE(context.SelectSpikeCorrection(token));
  }
}
TEST(ContextCorrectionRegistry, GenerationIncarnationAndFrozenPublication) {
  rime::Context first, other;
  PublishContextChoice(first);
  PublishContextChoice(other);
  CorrectionView a, b;
  ASSERT_TRUE(first.ReadSpikeCorrection(0, &a));
  ASSERT_TRUE(other.ReadSpikeCorrection(0, &b));
  EXPECT_NE(a.token.scope.session, b.token.scope.session);
  EXPECT_FALSE(other.SelectSpikeCorrection(a.token));
  EXPECT_FALSE(first.PublishSpikeCorrections());
  CorrectionView again;
  ASSERT_TRUE(first.ReadSpikeCorrection(0, &again));
  EXPECT_EQ(again.token.publication, a.token.publication);
  EXPECT_FALSE(first.ConfigureSpikeCorrections(0));
  ASSERT_TRUE(first.ConfigureSpikeCorrections(18));
  EXPECT_FALSE(first.SelectSpikeCorrection(a.token));
  ASSERT_TRUE(first.PublishSpikeCorrections());
  ASSERT_TRUE(first.ReadSpikeCorrection(0, &again));
  EXPECT_EQ(again.token.scope.generation, 18u);
  EXPECT_GT(again.token.scope.revision, a.token.scope.revision);
  size_t calls = 0;
  auto connection = first.select_notifier().connect([&](auto *c) {
    ++calls;
    EXPECT_EQ(c->SpikeCorrectionSize(), 0u);
    EXPECT_FALSE(c->SelectSpikeCorrection(again.token));
  });
  ASSERT_TRUE(first.SelectSpikeCorrection(again.token));
  EXPECT_EQ(calls, 1u);
}
TEST(ContextCorrectionRegistry, RecreatedContextCannotReuseOldTokens) {
  alignas(rime::Context) std::byte storage[sizeof(rime::Context)];
  auto *first = new (storage) rime::Context;
  PublishContextChoice(*first);
  CorrectionView view;
  ASSERT_TRUE(first->ReadSpikeCorrection(0, &view));
  auto token = view.token;
  first->~Context();
  auto *replacement = new (storage) rime::Context;
  PublishContextChoice(*replacement);
  EXPECT_FALSE(replacement->SelectSpikeCorrection(token));
  ASSERT_TRUE(replacement->ReadSpikeCorrection(0, &view));
  EXPECT_NE(view.token.scope.session, token.scope.session);
  replacement->~Context();
}

TEST(ContextCorrectionRegistry, PublicationUsesWarmStorage) {
  rime::Context context;
  PublishContextChoice(context);
  CorrectionView view;
  size_t failures = 0;
  registry_allocations = 0;
  track_registry_allocations = true;
  const auto begin = std::chrono::steady_clock::now();
  for (size_t i = 0; i < 1000; ++i) {
    context.InvalidateSpikeCorrections();
    if (!context.PublishSpikeCorrections() ||
        !context.ReadSpikeCorrection(0, &view))
      ++failures;
  }
  const auto elapsed = std::chrono::steady_clock::now() - begin;
  track_registry_allocations = false;
  EXPECT_EQ(failures, 0u);
  EXPECT_EQ(registry_allocations, 0u);
  std::cout
      << "CONTEXT_PUBLICATION count=1000 allocations=" << registry_allocations
      << " mean_ns="
      << std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count() /
             1000
      << "\n";
}
