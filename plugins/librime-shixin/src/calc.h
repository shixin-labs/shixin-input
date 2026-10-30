// The expression evaluator of shixin_calc_translator (cC1+2*3 → 7): a
// bounded recursive-descent parser over ASCII. Pure, no allocation while
// parsing, independent of the C locale. Fuzzed (test/parsers_fuzz.cc).
//
//   expr    := term (('+' | '-') term)*
//   term    := unary (('*' | '/' | '%') unary)*
//   unary   := ('+' | '-') unary | power
//   power   := postfix ('^' unary)?           right-associative; -2^2 = -4
//   postfix := primary '!'*                   factorial of 0–170
//   primary := number | '(' expr ')' | name '(' expr ')' | constant
//
// Names: sqrt cbrt abs floor ceil round exp ln log (base 10) log2 sin cos
// tan asin acos atan (radians); constants: pi, e.
#ifndef SHIXIN_CALC_H_
#define SHIXIN_CALC_H_

#include <cstddef>
#include <string>
#include <string_view>

namespace shixin {

// Longer expressions and deeper nesting are rejected, which bounds the
// parser's time and stack.
constexpr size_t kCalcMaxLength = 256;
constexpr int kCalcMaxDepth = 64;

// calc_eval evaluates expr. It returns false for a syntax error, an
// expression that is too long or deep, or a result that isn't finite.
bool calc_eval(std::string_view expr, double* result);

// calc_format appends v with up to 15 significant digits: 7, 0.3,
// 0.333333333333333, 100000000000.5, 1.5e+20.
void calc_format(double v, std::string* out);

}  // namespace shixin

#endif  // SHIXIN_CALC_H_
