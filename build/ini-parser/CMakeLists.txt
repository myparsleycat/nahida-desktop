cmake_minimum_required(VERSION 3.10)
project(ini_parser)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Add source files
add_library(ini_parser SHARED main.cpp)

# Target Windows
if(WIN32)
    target_compile_definitions(ini_parser PRIVATE INI_PARSER_EXPORTS)
endif()

# Include directories (current dir for json.hpp)
target_include_directories(ini_parser PRIVATE ${CMAKE_CURRENT_SOURCE_DIR})
