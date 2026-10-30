// Translators replacing rime-ice's Lua ones (DESIGN §3.8). They read
// rime-ice's own config (date_translator/*, recognizer/patterns/*), so its
// schemas need no changes. On the common path, a segment that isn't theirs,
// they compare a tag or a short string and return nothing: no allocation.
//
//   shixin_date_translator     rq sj xq dt ts rqzh rqen (date_translator/*)
//   shixin_number_translator   R123.45 → 壹佰贰拾叁点肆伍, amounts (tag number)
//   shixin_unicode_translator  U62fe → 拾 (tag unicode)
//   shixin_calc_translator     cC1+2*3 → 7 (tag calculator)
#ifndef SHIXIN_TRANSLATORS_H_
#define SHIXIN_TRANSLATORS_H_

#include <string>
#include <string_view>

#include <rime/translator.h>

#include "dates.h"

namespace shixin {

class DateTranslator : public rime::Translator {
 public:
  explicit DateTranslator(const rime::Ticket& ticket);
  rime::an<rime::Translation> Query(const std::string& input, const rime::Segment& segment) override;

 private:
  struct Code {
    std::string code;
    DateKind kind;
  };
  Code codes_[7];
  std::string tag_ = "abc";
};

// PatternTranslator handles the segments a recognizer pattern tags: the
// input is a literal prefix (the pattern's, as rime-ice's scripts derive
// it: "^U[a-f0-9]+" → "U") and an argument.
class PatternTranslator : public rime::Translator {
 public:
  PatternTranslator(const rime::Ticket& ticket, const std::string& tag, const std::string& prefix);

 protected:
  // Argument returns the input after the prefix, or false if the segment
  // isn't this translator's.
  bool Argument(const std::string& input, const rime::Segment& segment, std::string_view* arg) const;

  std::string tag_;
  std::string prefix_;
};

class NumberTranslator : public PatternTranslator {
 public:
  explicit NumberTranslator(const rime::Ticket& ticket);
  rime::an<rime::Translation> Query(const std::string& input, const rime::Segment& segment) override;
};

class UnicodeTranslator : public PatternTranslator {
 public:
  explicit UnicodeTranslator(const rime::Ticket& ticket);
  rime::an<rime::Translation> Query(const std::string& input, const rime::Segment& segment) override;
};

class CalcTranslator : public PatternTranslator {
 public:
  explicit CalcTranslator(const rime::Ticket& ticket);
  rime::an<rime::Translation> Query(const std::string& input, const rime::Segment& segment) override;
};

}  // namespace shixin

#endif  // SHIXIN_TRANSLATORS_H_
