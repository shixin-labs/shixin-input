// shixin_select_character: 以词定字. While composing, a key (rime-ice:
// key_binder/select_first_character, "bracketleft") commits the first
// character of the highlighted candidate, another (select_last_character,
// "bracketright") its last, and clears the composition. Replaces rime-ice's
// lua_processor@*select_character (DESIGN §3.8).
#ifndef SHIXIN_SELECT_CHARACTER_H_
#define SHIXIN_SELECT_CHARACTER_H_

#include <rime/key_event.h>
#include <rime/processor.h>

namespace shixin {

class SelectCharacter : public rime::Processor {
 public:
  explicit SelectCharacter(const rime::Ticket& ticket);
  rime::ProcessResult ProcessKeyEvent(const rime::KeyEvent& key_event) override;

 private:
  rime::KeyEvent first_key_;
  rime::KeyEvent last_key_;
  bool has_first_ = false;
  bool has_last_ = false;
};

}  // namespace shixin

#endif  // SHIXIN_SELECT_CHARACTER_H_
