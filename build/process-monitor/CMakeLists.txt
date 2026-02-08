cmake_minimum_required(VERSION 3.10)
project(process_monitor)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Add source files
add_library(process_monitor SHARED main.cpp)

# Target Windows
if(WIN32)
    target_compile_definitions(process_monitor PRIVATE PROCESS_MONITOR_EXPORTS)
    target_link_libraries(process_monitor wbemuuid ole32 oleaut32)
endif()

# Include directories
target_include_directories(process_monitor PRIVATE ${CMAKE_CURRENT_SOURCE_DIR})
