# librime-shixin: Shixin's native librime components, replacing rime-ice's
# Lua (DESIGN §3.8). Built merged into librime by scripts/build-librime.sh,
# which links this directory into librime's plugins/ as "shixin".
project(rime-shixin)
cmake_minimum_required(VERSION 3.10)

aux_source_directory(src shixin_src)

add_library(rime-shixin-objs OBJECT ${shixin_src})
target_compile_features(rime-shixin-objs PRIVATE cxx_std_17)
if(BUILD_SHARED_LIBS)
  set_target_properties(rime-shixin-objs PROPERTIES POSITION_INDEPENDENT_CODE ON)
endif()

set(plugin_name rime-shixin PARENT_SCOPE)
set(plugin_objs $<TARGET_OBJECTS:rime-shixin-objs> PARENT_SCOPE)
set(plugin_deps ${rime_library} PARENT_SCOPE)
set(plugin_modules "shixin" PARENT_SCOPE)

if(BUILD_TEST AND GTEST_FOUND)
  # The pure parts (no librime headers) are tested on their own.
  set(shixin_pure src/calc.cc src/dates.cc src/date_clock.cc src/numerals.cc src/rerank_slot.cc)
  add_executable(shixin_test test/utf8_test.cc test/numerals_test.cc test/dates_test.cc test/date_clock_test.cc
    test/calc_test.cc test/codepoint_test.cc test/rerank_slot_test.cc test/filter_rules_test.cc ${shixin_pure})
  target_include_directories(shixin_test PRIVATE src)
  target_compile_features(shixin_test PRIVATE cxx_std_17)
  target_link_libraries(shixin_test ${GTEST_MAIN_LIBRARIES} ${GTEST_LIBRARIES})
  add_test(NAME shixin_test COMMAND shixin_test)

  # Fuzzing (DESIGN §3.8): built with -DSHIXIN_FUZZ=ON and clang.
  if(SHIXIN_FUZZ)
    add_executable(shixin_fuzz_utf8 test/utf8_fuzz.cc)
    target_include_directories(shixin_fuzz_utf8 PRIVATE src)
    target_compile_options(shixin_fuzz_utf8 PRIVATE -fsanitize=fuzzer,address,undefined)
    target_link_options(shixin_fuzz_utf8 PRIVATE -fsanitize=fuzzer,address,undefined)
    add_executable(shixin_fuzz_parsers test/parsers_fuzz.cc ${shixin_pure})
    target_include_directories(shixin_fuzz_parsers PRIVATE src)
    target_compile_options(shixin_fuzz_parsers PRIVATE -fsanitize=fuzzer,address,undefined)
    target_link_options(shixin_fuzz_parsers PRIVATE -fsanitize=fuzzer,address,undefined)
  endif()
endif()
