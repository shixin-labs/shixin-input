#include "calc.h"

#include <cmath>
#include <string>
#include <string_view>

#include <gtest/gtest.h>

namespace {

std::string calc(const char* expr) {
  double v;
  if (!shixin::calc_eval(expr, &v))
    return "error";
  std::string s;
  shixin::calc_format(v, &s);
  return s;
}

TEST(Calc, Arithmetic) {
  EXPECT_EQ(calc("1+2*3"), "7");
  EXPECT_EQ(calc("(1+2)*3"), "9");
  EXPECT_EQ(calc("10/4"), "2.5");
  EXPECT_EQ(calc("7%3"), "1");
  EXPECT_EQ(calc("2^10"), "1024");
  EXPECT_EQ(calc("2^3^2"), "512");  // right-associative
  EXPECT_EQ(calc("-2^2"), "-4");
  EXPECT_EQ(calc("2^-1"), "0.5");
  EXPECT_EQ(calc("--3"), "3");
  EXPECT_EQ(calc("0.1+0.2"), "0.3");
  EXPECT_EQ(calc(".5*4"), "2");
  EXPECT_EQ(calc("1/3"), "0.333333333333333");
  EXPECT_EQ(calc("5!"), "120");
  EXPECT_EQ(calc("3!!"), "720");
  EXPECT_EQ(calc("1 + 2"), "3");
  EXPECT_EQ(calc("-0"), "0");
  EXPECT_EQ(calc("1e"), "error");
  EXPECT_EQ(calc("99999999999*99999999999"), "9.9999999998e+21");
  // Large amounts keep their cents.
  EXPECT_EQ(calc("100000000000+0.5"), "100000000000.5");
  EXPECT_EQ(calc("123456789012.5"), "123456789012.5");
  EXPECT_EQ(calc("1.1*1.1"), "1.21");
  EXPECT_EQ(calc("10^15"), "1000000000000000");
}

TEST(Calc, Functions) {
  EXPECT_EQ(calc("sqrt(16)"), "4");
  EXPECT_EQ(calc("log(1000)"), "3");
  EXPECT_EQ(calc("log2(8)"), "3");
  EXPECT_EQ(calc("ln(e)"), "1");
  EXPECT_EQ(calc("sin(pi/2)"), "1");
  EXPECT_EQ(calc("abs(-3)+floor(2.7)+ceil(0.2)+round(1.5)"), "8");
  EXPECT_EQ(calc("2*pi"), "6.28318530717959");
}

TEST(Calc, Errors) {
  for (const char* bad : {"", "1+", "(1", "1)", "*2", "1/0", "1%0", "sqrt(-1)", "foo(1)", "pi(2)", "x", "2pi", "1..2",
                          ".", "(-1)!", "2.5!", "171!", "1#2", "\xff"})
    EXPECT_EQ(calc(bad), "error") << bad;
  double v;
  EXPECT_FALSE(shixin::calc_eval(std::string_view("1\0+2", 4), &v));  // NUL isn't the end
  // Too deep and too long are errors, not crashes.
  std::string deep(200, '(');
  deep += "1";
  deep += std::string(200, ')');
  EXPECT_EQ(calc(deep.c_str()), "error");
  std::string minus(100, '-');
  EXPECT_EQ(calc((minus + "1").c_str()), "error");
  std::string nested;
  for (int i = 0; i < 30; ++i)
    nested += "(";
  nested += "1";
  for (int i = 0; i < 30; ++i)
    nested += ")";
  EXPECT_EQ(calc(nested.c_str()), "1");
  std::string sum = "1";
  for (int i = 0; i < 200; ++i)
    sum += "+1";
  EXPECT_EQ(calc(sum.c_str()), "error");  // longer than kCalcMaxLength
}

}  // namespace
