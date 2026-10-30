// A stand-in for libFuzzer where the toolchain has none (Apple clang): it
// feeds LLVMFuzzerTestOneInput random inputs: random bytes, strings of the
// characters the parsers know, and random expressions from the calculator's
// grammar (nested past its depth limit), so they get past the first token.
// Build with -fsanitize=address,undefined; usage: fuzz [iterations] [seed].
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <random>
#include <string>

extern "C" int LLVMFuzzerTestOneInput(const uint8_t* data, size_t size);

namespace {

const char* const kAtoms[] = {"0", "1", "2.5", ".5", "170", "171", "1e", "pi", "e", "999999999999999999"};
const char* const kOps[] = {"+", "-", "*", "/", "%", "^"};
const char* const kFns[] = {"sqrt(", "ln(", "log2(", "sin(", "acos(", "round(", "nope("};

void expr(std::mt19937& rng, int depth, std::string* s) {
  if (s->size() > 400)
    return;
  switch (depth > 80 ? 0 : rng() % 6) {
    case 0:
      s->append(kAtoms[rng() % 10]);
      break;
    case 1:
      expr(rng, depth + 1, s);
      s->append(kOps[rng() % 6]);
      expr(rng, depth + 1, s);
      break;
    case 2:
      s->push_back('(');
      expr(rng, depth + 1, s);
      s->push_back(')');
      break;
    case 3:
      s->push_back('-');
      expr(rng, depth + 1, s);
      break;
    case 4:
      s->append(kFns[rng() % 7]);
      expr(rng, depth + 1, s);
      s->push_back(')');
      break;
    case 5:
      expr(rng, depth + 1, s);
      s->push_back('!');
      break;
  }
}

}  // namespace

int main(int argc, char** argv) {
  long iterations = argc > 1 ? std::atol(argv[1]) : 1000000;
  unsigned seed = argc > 2 ? static_cast<unsigned>(std::atol(argv[2])) : std::random_device{}();
  std::printf("fuzz: %ld inputs, seed %u\n", iterations, seed);
  std::mt19937 rng(seed);
  static const char kTokens[] = "0123456789.+-*/%^!()  abcdefpisqrtlogn";
  std::string s;
  for (long i = 0; i < iterations; ++i) {
    s.clear();
    if (rng() % 2) {
      expr(rng, 0, &s);
    } else {
      size_t n = rng() % 300;
      bool bytes = rng() % 8 == 0;
      for (size_t k = 0; k < n; ++k)
        s.push_back(bytes ? static_cast<char>(rng()) : kTokens[rng() % (sizeof kTokens - 1)]);
    }
    LLVMFuzzerTestOneInput(reinterpret_cast<const uint8_t*>(s.data()), s.size());
  }
  return 0;
}
