#include "packed_image.h"
#include <cstdio>
#include <random>

static void equivalent(const abbr::Result &a, const abbr::Result &b) {
  assert(a.clean == b.clean && a.corrected == b.corrected &&
         a.states == b.states && a.edges == b.edges &&
         a.peak_queue == b.peak_queue && a.exhausted == b.exhausted);
  for (int g = 0; g < 2; g++)
    for (int i = 0; i < (g ? a.corrected : a.clean); i++)
      assert(a.hits[g * 4 + i].word == b.hits[g * 4 + i].word &&
             a.hits[g * 4 + i].cost == b.hits[g * 4 + i].cost);
}
int main(int argc, char **argv) {
  assert(argc == 2);
  const char *path = argv[1];
  std::mt19937 rng(185);
  std::vector<std::string> syllables{"ni",  "hao", "zhong", "guo",
                                     "shi", "an",  "chao",  "lv"};
  for (int trial = 0; trial < 100; trial++) {
    abbr::Search plain;
    plain.add({"ni"}, "你", 1);
    plain.add({"ni", "hao"}, "你好", 50);
    for (int i = 0; i < 40; i++) {
      std::vector<std::string> parts;
      for (int k = 0; k < 1 + i % 6; k++)
        parts.push_back(syllables[rng() % syllables.size()]);
      plain.add(parts, "word" + std::to_string(i % 13),
                i + 1); // repeated texts across readings
    }
    plain.freeze();
    auto packed = plain;
    packed.compactPayloads();
    packed.packNodes();
    for (uint32_t i = 0; i < plain.nodes.size(); i++) {
      auto a = plain.nodeAt(i), b = packed.nodeAt(i);
      assert(a.words == b.words && a.edges == b.edges && a.bound == b.bound &&
             a.descendant_bound == b.descendant_bound &&
             a.min_tail == b.min_tail && a.max_tail == b.max_tail);
      if (a.words)
        assert(a.word == b.word);
      for (int j = 0; j < a.edges; j++) {
        auto x = plain.edgeAt(a, j), y = packed.edgeAt(b, j);
        assert(x.child == y.child && x.syllable == y.syllable);
      }
    }
    for (uint32_t i = 0; i < plain.words.size(); i++)
      assert(plain.text(i) == packed.text(i) &&
             plain.code(i) == packed.code(i) &&
             plain.words[i].prior == packed.words[i].prior);
    assert(abbr::savePackedImage(packed, path));
    abbr::Search loaded;
    assert(abbr::loadPackedImage(loaded, path));
    abbr::Workspace x, y, z;
    for (int j = 0; j < 100; j++) {
      std::string raw;
      for (int k = 0; k < 1 + j % 5; k++) {
        auto &s = syllables[rng() % syllables.size()];
        raw += rng() % 2 ? s : s.substr(0, 1);
      }
      if (j % 3 == 0)
        raw[rng() % raw.size()] = 'a' + rng() % 26;
      int states = j % 7 == 0 ? 2 : 2048, edges = j % 11 == 0 ? 3 : 8192;
      auto result = plain.query(raw, x, states, edges, j % 2);
      equivalent(result, packed.query(raw, y, states, edges, j % 2));
      equivalent(result, loaded.query(raw, z, states, edges, j % 2));
    }
    if (trial == 0) {
      std::ifstream input(path, std::ios::binary);
      std::string image((std::istreambuf_iterator<char>(input)), {});
      for (int t = 0; t < 100; t++) {
        auto bytes = image;
        if (t == 0)
          bytes[0] = '?';
        else if (t == 1)
          bytes.resize(20);
        else if (t == 2) {
          for (int b = 12; b < 16; b++)
            bytes[b] = char(255);
        } else
          bytes[rng() % bytes.size()] ^= char(1u << (rng() % 8));
        {
          std::ofstream output(path, std::ios::binary);
          output.write(bytes.data(), bytes.size());
        }
        abbr::Search bad;
        bool ok = abbr::loadPackedImage(bad, path);
        if (t < 3)
          assert(!ok);
        if (ok) {
          abbr::Workspace scratch;
          bad.query("nh", scratch);
          bad.query("nhong", scratch);
        }
      }
    }
  }
  std::remove(path);
}
