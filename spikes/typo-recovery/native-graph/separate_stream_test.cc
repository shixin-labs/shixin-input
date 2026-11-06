#include <gtest/gtest.h>
#include <cstdlib>
#include <filesystem>
#include <tuple>
#include <iostream>
#include <rime_api.h>
#include <rime/context.h>
#include <rime/menu.h>
#include <rime/service.h>
#include <rime/translation.h>
#include <rime/gear/translator_commons.h>
#include "correction_registry.h"
#include "correction_api.h"
#include "search_diagnostics.h"
#include <fstream>

TEST(SeparateStream, RealDictionaryOrdinaryPagesAndExplicitCommit) {
  const auto root_env = std::getenv("SHIXIN_TYPO_SPIKE_ROOT");
  if (!root_env)
    GTEST_SKIP();
  std::string root = root_env;
  auto data = root + "/rime-data", prebuilt = root + "/prebuilt";
  auto stage = root + "/native-graph/separate-generation-v1/build";
  if (const auto generation = std::getenv("SHIXIN_TYPO_SPIKE_GENERATION"))
    stage = std::string(generation) + "/build";
  char scratch[] = "/tmp/shixin-separate-stream-XXXXXX";
  ASSERT_NE(mkdtemp(scratch), nullptr);
  std::filesystem::create_directory_symlink(root + "/share/opencc",
                                            std::string(scratch) + "/opencc");
  auto api = rime_get_api();
  RIME_STRUCT(RimeTraits, traits);
  const char *modules[] = {"default", "shixin", nullptr};
  traits.shared_data_dir = data.c_str();
  traits.user_data_dir = scratch;
  traits.prebuilt_data_dir = prebuilt.c_str();
  traits.staging_dir = stage.c_str();
  traits.modules = modules;
  traits.app_name = "rime.shixin-separate-test";
  traits.min_log_level = 2;
  api->setup(&traits);
  api->initialize(&traits);
  rime::EnableSpikeSearchDiagnostics(true);
  struct Cleanup {
    RimeApi *api;
    const char *scratch;
    ~Cleanup() {
      api->finalize();
      std::filesystem::remove_all(scratch);
    }
  } cleanup{api, scratch};
  auto ordinary = api->create_session(), separate = api->create_session();
  ASSERT_NE(ordinary, 0u);
  ASSERT_NE(separate, 0u);
  ASSERT_TRUE(api->select_schema(ordinary, "rime_ice_control_warm"));
  ASSERT_TRUE(api->select_schema(separate, "rime_ice_warm"));
  ASSERT_EQ(shixin_spike_corrections_version(), 1u);
  ASSERT_EQ(shixin_spike_corrections_v1_configure(separate, 1), 1);
  EXPECT_EQ(shixin_spike_corrections_v1_configure(separate, 0), 0);
  EXPECT_EQ(shixin_spike_corrections_v1_read(
                separate, nullptr, SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE),
            -1);
  EXPECT_EQ(shixin_spike_corrections_v1_select(
                separate, nullptr, SHIXIN_SPIKE_CORRECTION_TOKEN_SIZE),
            0);
  using Row = std::tuple<std::string, std::string, size_t, size_t>;
  auto snapshot = [](rime::Context *c) {
    std::vector<Row> rows;
    if (c->composition().empty())
      return rows;
    auto menu = c->composition().back().menu;
    if (!menu)
      return rows;
    size_t count = menu->Prepare(100);
    for (size_t i = 0; i < count; ++i) {
      auto candidate = menu->GetCandidateAt(i);
      rows.emplace_back(candidate->text(), candidate->comment(),
                        candidate->start(), candidate->end());
    }
    return rows;
  };
  uint64_t last_revision = 0;
  for (int mode = 0; mode < 4; ++mode) {
    const bool traditional = mode & 1, emoji = mode & 2;
    SCOPED_TRACE("mode=" + std::to_string(mode));
    for (auto id : {ordinary, separate}) {
      api->clear_composition(id);
      api->set_option(id, "traditionalization", traditional);
      api->set_option(id, "emoji", emoji);
    }
    for (const char *raw : {"nihao", "nihoa", "niaho", "nihso", "ni'hso", "njh",
                            "zhongguo", "shijie", "nh", "xgr"}) {
      SCOPED_TRACE(raw);
      // Two traces exercise warmed recovery; this test does not assert cold
      // recall.
      for (int repeat = 0; repeat < 2; ++repeat) {
        for (auto id : {ordinary, separate}) {
          api->clear_composition(id);
          for (const char *k = raw; *k; ++k)
            api->process_key(id, *k, 0);
        }
        if (std::string(raw) == "niaho") {
          const auto diagnostics = rime::ReadSpikeSearchDiagnostics();
          std::cout << "diagnostic bytes=" << diagnostics.input_bytes
                    << " anchor=" << diagnostics.preferred_anchor;
          for (const auto& stage : diagnostics.stages)
            std::cout << " stage={evaluated:" << stage.evaluated
                      << ",found:" << stage.found << ",available:" << stage.available
                      << ",graph:" << stage.graph_complete << ",stop:" << int(stage.stop)
                      << ",phase:" << int(stage.phase) << ",work:" << stage.work_left << ",nodes:" << stage.nodes_left
                      << ",chunks:" << stage.chunks_left << "}";
          std::cout << "\n";
        }
        auto control =
            rime::Service::instance().GetSession(ordinary)->context();
        auto context =
            rime::Service::instance().GetSession(separate)->context();
        auto before = snapshot(context);
        ASSERT_FALSE(before.empty());
        EXPECT_EQ(snapshot(control), before);
        ASSERT_FALSE(context->composition().empty());
        auto stream = context->composition().back().spike_corrections;
        ASSERT_TRUE(stream);
        std::vector<rime::an<rime::Candidate>> recovered;
        while (!stream->exhausted()) {
          auto candidate = stream->Peek();
          ASSERT_TRUE(candidate);
          EXPECT_TRUE(candidate->is_spike_correction());
          EXPECT_TRUE(rime::As<rime::Phrase>(
              rime::Candidate::GetGenuineCandidate(candidate)));
          recovered.push_back(candidate);
          ASSERT_LE(recovered.size(), 8u);
          stream->Next();
        }
        if (std::getenv("SHIXIN_SPIKE_PRINT_CORRECTIONS")) {
          std::cout << "raw=" << raw << " candidates:";
          for (const auto& c:recovered) std::cout << " " << c->text();
          std::cout << "\n";
        }
        std::array<shixin_spike::CorrectionChoice, 8> choices;
        for (size_t i = 0; i < recovered.size(); ++i) {
          ASSERT_TRUE(shixin_spike::ChoiceFromNativeTrace(
              *context, recovered[i], &choices[i]));
        }
        ASSERT_EQ(context->SpikeCorrectionSize(), recovered.size());
        std::array<uint8_t, SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE + 16> wire;
        wire.fill(0xcc);
        EXPECT_EQ(shixin_spike_corrections_v1_read(
                      separate, wire.data() + 8,
                      SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE - 1),
                  -1);
        for (auto byte : wire)
          EXPECT_EQ(byte, 0xcc);
        ASSERT_EQ(shixin_spike_corrections_v1_read(
                      separate, wire.data() + 8,
                      SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE),
                  0);
        for (size_t i = 0; i < 8; ++i) {
          EXPECT_EQ(wire[i], 0xcc);
          EXPECT_EQ(wire[wire.size() - 1 - i], 0xcc);
        }
        EXPECT_EQ(wire[8], 1u);
        EXPECT_EQ(wire[16], recovered.size());
        if (mode == 0 && repeat == 1 && std::string(raw) == "nihso") {
          if (auto path = std::getenv("SHIXIN_SPIKE_EXPORT_FILE")) {
            std::ofstream output(path, std::ios::binary);
            output.write(reinterpret_cast<const char *>(wire.data() + 8),
                         SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE);
            ASSERT_TRUE(output.good());
          }
        }

        for (size_t i = 0; i < recovered.size(); ++i) {
          shixin_spike::CorrectionView view;
          ASSERT_TRUE(context->ReadSpikeCorrection(i, &view));
          EXPECT_EQ(view.text, recovered[i]->text());
          EXPECT_EQ(view.token.scope.generation, 1u);
          EXPECT_NE(view.token.scope.session, 0u);
          EXPECT_GT(view.token.scope.revision, last_revision);
        }
        if (!recovered.empty()) {
          shixin_spike::CorrectionView view;
          ASSERT_TRUE(context->ReadSpikeCorrection(0, &view));
          last_revision = view.token.scope.revision;
          EXPECT_FALSE(context->PublishSpikeCorrections());
          EXPECT_FALSE(control->SelectSpikeCorrection(view.token));
        }
        EXPECT_EQ(snapshot(context), before);
        std::cout << "mode=" << mode << " " << raw << " repeat=" << repeat
                  << " ordinary=" << before.size()
                  << " corrections=" << recovered.size() << "\n";
        if (repeat &&
            (std::string(raw) == "niaho" || std::string(raw) == "nihso" || std::string(raw) == "ni'hso" ||
             std::string(raw) == "xgr")) {
          const bool country = std::string(raw) == "xgr";
          const bool swap = std::string(raw) == "niaho";
          const std::string expected = country
                                           ? (traditional ? "中國人" : "中国人")
                                           : (emoji ? "👋" : "你好");
          auto found = std::find_if(
              recovered.begin(), recovered.end(),
              [&](const auto &c) { return c->text() == expected; });
          ASSERT_NE(found, recovered.end());
          auto trace = (*found)->spike_edit_trace();
          ASSERT_TRUE(trace);
          EXPECT_EQ(trace->kind, swap ? shixin_spike::EditTrace::Kind::Swap
                                     : shixin_spike::EditTrace::Kind::Neighbor);
          EXPECT_EQ(trace->begin,
                    swap ? 2u : country ? 0u : (std::string(raw) == "nihso" ? 3u : 4u));
          EXPECT_EQ(trace->end,
                    swap ? 4u : country ? 1u : (std::string(raw) == "nihso" ? 4u : 5u));
          EXPECT_EQ(trace->from[0], swap ? 'a' : country ? 'x' : 's');
          EXPECT_EQ(trace->to[0], swap ? 'h' : country ? 'z' : 'a');
          if (swap) {
            EXPECT_EQ(trace->from[1], 'h');
            EXPECT_EQ(trace->to[1], 'a');
            EXPECT_EQ(trace->cost, 8u);
          }
          auto native = rime::Candidate::GetGenuineCandidate(*found);
          ASSERT_TRUE(native);
          EXPECT_EQ(native->text(), country ? "中国人" : "你好");
          shixin_spike::CorrectionView view;
          ASSERT_TRUE(context->ReadSpikeCorrection(
              size_t(found - recovered.begin()), &view));
          std::array<uint8_t, SHIXIN_SPIKE_CORRECTION_TOKEN_SIZE> token;
          const auto record = 8 + SHIXIN_SPIKE_CORRECTION_HEADER_SIZE +
                              size_t(found - recovered.begin()) *
                                  SHIXIN_SPIKE_CORRECTION_RECORD_SIZE;
          std::copy_n(wire.data() + record, token.size(), token.data());
          auto invalid = token;
          invalid[32] = 8;
          EXPECT_EQ(shixin_spike_corrections_v1_select(separate, invalid.data(),
                                                       invalid.size()),
                    0);
          invalid = token;
          invalid[0] ^= 0x80;
          EXPECT_EQ(shixin_spike_corrections_v1_select(separate, invalid.data(),
                                                       invalid.size()),
                    0);
          invalid = token;
          invalid[39] = 1;
          EXPECT_EQ(shixin_spike_corrections_v1_select(separate, invalid.data(),
                                                       invalid.size()),
                    0);
          EXPECT_EQ(shixin_spike_corrections_v1_select(separate, token.data(),
                                                       token.size() - 1),
                    0);
          ASSERT_EQ(shixin_spike_corrections_v1_select(separate, token.data(),
                                                       token.size()),
                    1);
          EXPECT_EQ(shixin_spike_corrections_v1_select(separate, token.data(),
                                                       token.size()),
                    0);
          EXPECT_FALSE(context->SelectSpikeCorrection(view.token));
          RIME_STRUCT(RimeCommit, commit);
          ASSERT_TRUE(api->get_commit(separate, &commit));
          EXPECT_STREQ(commit.text, expected.c_str());
          api->free_commit(&commit);
          EXPECT_TRUE(context->composition().empty());
        }
        std::weak_ptr<rime::Translation> lifetime = stream;
        stream.reset();
        api->clear_composition(separate);
        EXPECT_TRUE(lifetime.expired());
      }
    }
  }
  // Live option changes keep raw input unchanged but must replace its scope.
  api->clear_composition(separate);
  for (char key : std::string("nihso"))
    api->process_key(separate, key, 0);
  auto *active = rime::Service::instance().GetSession(separate)->context();
  shixin_spike::CorrectionView old_view, new_view;
  ASSERT_TRUE(active->ReadSpikeCorrection(0, &old_view));
  const auto old_token = old_view.token;
  api->set_option(separate, "emoji", false);
  EXPECT_FALSE(active->SelectSpikeCorrection(old_token));
  ASSERT_TRUE(active->ReadSpikeCorrection(0, &new_view));
  EXPECT_GT(new_view.token.scope.revision, old_token.scope.revision);
  auto replaced = new_view.token;
  ASSERT_TRUE(active->ConfigureSpikeCorrections(2));
  EXPECT_FALSE(active->SelectSpikeCorrection(replaced));
  ASSERT_TRUE(active->PublishSpikeCorrections());
  ASSERT_TRUE(active->ReadSpikeCorrection(0, &new_view));
  EXPECT_EQ(new_view.token.scope.generation, 2u);
  replaced = new_view.token;
  active->InvalidateSpikeCorrections(); // Native hook for the future
                                        // focus/epoch bridge.
  EXPECT_EQ(active->SpikeCorrectionSize(), 0u);
  EXPECT_FALSE(active->SelectSpikeCorrection(replaced));
  std::array<uint8_t, SHIXIN_SPIKE_CORRECTION_SNAPSHOT_SIZE> dead;
  dead.fill(0xcc);
  ASSERT_TRUE(api->destroy_session(separate));
  EXPECT_EQ(
      shixin_spike_corrections_v1_read(separate, dead.data(), dead.size()), -2);
  for (auto byte : dead)
    EXPECT_EQ(byte, 0u);
  EXPECT_EQ(shixin_spike_corrections_v1_invalidate(separate), 0);
}
