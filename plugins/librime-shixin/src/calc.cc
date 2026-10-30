#include "calc.h"

#include <cmath>
#include <cstdio>

namespace shixin {

namespace {

constexpr double kPi = 3.14159265358979323846;
constexpr double kE = 2.71828182845904523536;

struct Function {
  const char* name;
  double (*fn)(double);
};

const Function kFunctions[] = {
    {"sqrt", [](double x) { return std::sqrt(x); }},   {"cbrt", [](double x) { return std::cbrt(x); }},
    {"abs", [](double x) { return std::fabs(x); }},    {"floor", [](double x) { return std::floor(x); }},
    {"ceil", [](double x) { return std::ceil(x); }},   {"round", [](double x) { return std::round(x); }},
    {"exp", [](double x) { return std::exp(x); }},     {"ln", [](double x) { return std::log(x); }},
    {"log", [](double x) { return std::log10(x); }}, {"log2", [](double x) { return std::log2(x); }},
    {"sin", [](double x) { return std::sin(x); }},     {"cos", [](double x) { return std::cos(x); }},
    {"tan", [](double x) { return std::tan(x); }},     {"asin", [](double x) { return std::asin(x); }},
    {"acos", [](double x) { return std::acos(x); }},   {"atan", [](double x) { return std::atan(x); }},
};

class Parser {
 public:
  explicit Parser(std::string_view s) : s_(s) {}

  bool Parse(double* result) {
    double v = Expr();
    Peek();
    if (i_ != s_.size())  // trailing input; Peek's 0 could be a NUL byte
      ok_ = false;
    if (!ok_ || !std::isfinite(v))
      return false;
    *result = v;
    return true;
  }

 private:
  char Peek() {
    while (i_ < s_.size() && s_[i_] == ' ')
      ++i_;
    return i_ < s_.size() ? s_[i_] : 0;
  }

  bool Eat(char c) {
    if (Peek() != c)
      return false;
    ++i_;
    return true;
  }

  double Fail() {
    ok_ = false;
    i_ = s_.size();  // stop: every caller sees the end of the input
    return 0;
  }

  double Expr() {
    double v = Term();
    while (ok_) {
      if (Eat('+'))
        v += Term();
      else if (Eat('-'))
        v -= Term();
      else
        break;
    }
    return v;
  }

  double Term() {
    double v = Unary();
    while (ok_) {
      if (Eat('*')) {
        v *= Unary();
      } else if (Eat('/')) {
        double d = Unary();
        if (d == 0)
          return Fail();
        v /= d;
      } else if (Eat('%')) {
        double d = Unary();
        if (d == 0)
          return Fail();
        v = std::fmod(v, d);
      } else {
        break;
      }
    }
    return v;
  }

  // Every recursive path goes through Unary, so its depth bounds the stack.
  double Unary() {
    if (++depth_ > kCalcMaxDepth)
      return Fail();
    double v;
    if (Eat('+'))
      v = Unary();
    else if (Eat('-'))
      v = -Unary();
    else
      v = Power();
    --depth_;
    return v;
  }

  double Power() {
    double base = Postfix();
    if (ok_ && Eat('^'))
      return std::pow(base, Unary());
    return base;
  }

  double Postfix() {
    double v = Primary();
    while (ok_ && Eat('!')) {
      if (v < 0 || v > 170 || v != std::floor(v))
        return Fail();
      double f = 1;
      for (int k = 2; k <= static_cast<int>(v); ++k)
        f *= k;
      v = f;
    }
    return v;
  }

  double Primary() {
    char c = Peek();
    if (c == '(') {
      ++i_;
      double v = Expr();
      if (!Eat(')'))
        return Fail();
      return v;
    }
    if ((c >= '0' && c <= '9') || c == '.')
      return Number();
    if (c >= 'a' && c <= 'z')
      return Name();
    return Fail();
  }

  double Number() {
    double v = 0, frac = 0, scale = 1;
    bool digits = false;
    while (i_ < s_.size() && s_[i_] >= '0' && s_[i_] <= '9') {
      v = v * 10 + (s_[i_++] - '0');
      digits = true;
    }
    if (i_ < s_.size() && s_[i_] == '.') {
      ++i_;
      while (i_ < s_.size() && s_[i_] >= '0' && s_[i_] <= '9') {
        frac = frac * 10 + (s_[i_++] - '0');
        scale *= 10;
        digits = true;
      }
    }
    if (!digits)
      return Fail();
    return v + frac / scale;
  }

  double Name() {
    size_t start = i_;
    while (i_ < s_.size() && ((s_[i_] >= 'a' && s_[i_] <= 'z') || (s_[i_] >= '0' && s_[i_] <= '9')))
      ++i_;
    std::string_view name = s_.substr(start, i_ - start);
    if (Peek() == '(') {
      for (const Function& f : kFunctions) {
        if (name == f.name) {
          ++i_;
          double v = Expr();
          if (!Eat(')'))
            return Fail();
          return f.fn(v);
        }
      }
      return Fail();
    }
    if (name == "pi")
      return kPi;
    if (name == "e")
      return kE;
    return Fail();
  }

  std::string_view s_;
  size_t i_ = 0;
  int depth_ = 0;
  bool ok_ = true;
};

}  // namespace

bool calc_eval(std::string_view expr, double* result) {
  if (expr.empty() || expr.size() > kCalcMaxLength)
    return false;
  return Parser(expr).Parse(result);
}

void calc_format(double v, std::string* out) {
  char buf[40];
  int n;
  // 15 significant digits are exact for any double and still hide binary
  // rounding (0.1+0.2 shows 0.3); integers print in full up to 10^16.
  if (v == 0)
    n = std::snprintf(buf, sizeof buf, "0");  // also -0
  else if (std::fabs(v) < 1e16 && v == std::floor(v))
    n = std::snprintf(buf, sizeof buf, "%.0f", v);
  else
    n = std::snprintf(buf, sizeof buf, "%.15g", v);
  for (int k = 0; k < n; ++k)
    if (buf[k] == ',')  // a decimal comma under some C locale
      buf[k] = '.';
  if (n > 0)
    out->append(buf, static_cast<size_t>(n) < sizeof buf ? static_cast<size_t>(n) : sizeof buf - 1);
}

}  // namespace shixin
