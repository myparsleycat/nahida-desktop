#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <iostream>
#include <fstream>
#include <string>
#include <vector>
#include <filesystem>
#include <algorithm>
#include <future>
#include <sstream>
#include <optional>
#include <map>
#include <unordered_map>
#include <string_view>

#include "json.hpp"

using json = nlohmann::json;
namespace fs = std::filesystem;

#ifdef WIN32
    #define EXPORT __declspec(dllexport)
#else
    #define EXPORT
#endif

struct ToggleKey {
    std::string sectionName;
    std::string iniFileName;
    std::string key;
    std::string back;
    std::string type;
    std::string variable;
    std::vector<std::string> values;
    std::string currentValue;
};

bool iequals(std::string_view a, std::string_view b) {
    return std::equal(a.begin(), a.end(), b.begin(), b.end(),
                      [](char a, char b) {
                          return tolower(static_cast<unsigned char>(a)) == tolower(static_cast<unsigned char>(b));
                      });
}

bool istartsWith(std::string_view str, std::string_view prefix) {
    if (str.size() < prefix.size()) return false;
    return std::equal(prefix.begin(), prefix.end(), str.begin(),
                      [](char a, char b) {
                          return tolower(static_cast<unsigned char>(a)) == tolower(static_cast<unsigned char>(b));
                      });
}

std::string trim(const std::string& str) {
    size_t first = str.find_first_not_of(" \t\r\n");
    if (std::string::npos == first) return "";
    size_t last = str.find_last_not_of(" \t\r\n");
    return str.substr(first, (last - first + 1));
}

std::string getCaseInsensitive(const std::unordered_map<std::string, std::string>& data, const std::string& key) {
    for (const auto& pair : data) {
        if (iequals(pair.first, key)) {
            return pair.second;
        }
    }
    return "";
}

std::optional<ToggleKey> extractToggleKey(const std::string& sectionName, const std::unordered_map<std::string, std::string>& data, const std::string& iniFileName) {
    std::string variable;
    std::string valuesStr;

    bool foundVariable = false;
    for (const auto& pair : data) {
        if (!pair.first.empty() && pair.first[0] == '$') {
            variable = pair.first;
            valuesStr = pair.second;
            foundVariable = true;
            break;
        }
    }

    if (!foundVariable) return std::nullopt;

    std::vector<std::string> values;
    std::stringstream ss(valuesStr);
    std::string item;
    while (std::getline(ss, item, ',')) {
        values.push_back(trim(item));
    }

    std::string type = getCaseInsensitive(data, "type");
    if (values.size() < 2 && !iequals(type, "hold")) return std::nullopt;

    ToggleKey tk;
    tk.sectionName = sectionName;
    tk.iniFileName = iniFileName;
    tk.key = getCaseInsensitive(data, "key");
    tk.back = getCaseInsensitive(data, "back");
    tk.type = type;
    tk.variable = variable;
    tk.values = values;
    if (!values.empty()) tk.currentValue = values[0];

    return tk;
}

std::vector<ToggleKey> parseIni(const std::string& path) {
    std::vector<ToggleKey> toggleKeys;
    std::ifstream file(fs::u8path(path));
    if (!file.is_open()) return toggleKeys;

    std::string line;
    std::string currentSection;
    std::unordered_map<std::string, std::string> sectionData;
    std::string iniFileName = fs::path(fs::u8path(path)).filename().string();

    bool firstLine = true;

    while (std::getline(file, line)) {
        if (firstLine) {
            if (line.size() >= 3 && static_cast<unsigned char>(line[0]) == 0xEF && static_cast<unsigned char>(line[1]) == 0xBB && static_cast<unsigned char>(line[2]) == 0xBF) {
                line.erase(0, 3);
            }
            firstLine = false;
        }

        line = trim(line);
        if (line.empty() || line[0] == ';') continue;

        if (line.front() == '[' && line.back() == ']') {
            if (!currentSection.empty() && istartsWith(currentSection, "key")) {
                auto tk = extractToggleKey(currentSection, sectionData, iniFileName);
                if (tk) toggleKeys.push_back(*tk);
            }

            currentSection = line.substr(1, line.size() - 2);
            sectionData.clear();
            continue;
        }

        if (!currentSection.empty()) {
            size_t eqPos = line.find('=');
            if (eqPos != std::string::npos) {
                std::string key = trim(line.substr(0, eqPos));
                std::string value = trim(line.substr(eqPos + 1));
                sectionData[key] = value;
            }
        }
    }

    if (!currentSection.empty() && istartsWith(currentSection, "key")) {
        auto tk = extractToggleKey(currentSection, sectionData, iniFileName);
        if (tk) toggleKeys.push_back(*tk);
    }

    return toggleKeys;
}

struct IniResult {
    std::string name;
    std::string path;
    std::vector<ToggleKey> toggleKeys;
    bool hasToggleKey;
};

extern "C" {
    EXPORT const char* ProcessIniFiles(const char* pathsJsonStr) {
        try {
            auto paths = json::parse(pathsJsonStr).get<std::vector<std::string>>();
            std::vector<std::future<IniResult>> futures;
            
            futures.reserve(paths.size());

            for (const auto& path : paths) {
                futures.push_back(std::async(std::launch::async | std::launch::deferred, [path]() -> IniResult {
                    IniResult result;
                    result.path = path;
                    result.name = fs::path(fs::u8path(path)).filename().string();
                    result.toggleKeys = parseIni(path);
                    
                    std::stable_sort(result.toggleKeys.begin(), result.toggleKeys.end(), [](const ToggleKey& a, const ToggleKey& b) {
                        return !a.key.empty() && b.key.empty();
                    });

                    result.hasToggleKey = false;
                    for(const auto& tk : result.toggleKeys) {
                        if(!tk.key.empty()) {
                            result.hasToggleKey = true;
                            break;
                        }
                    }
                    return result;
                }));
            }

            json resultJson = json::array();
            for (auto& f : futures) {
                IniResult r = f.get();
                json entry;
                entry["name"] = r.name;
                entry["path"] = r.path;
                entry["hasToggleKey"] = r.hasToggleKey;
                
                json tks = json::array();
                for(const auto& tk : r.toggleKeys) {
                    json t;
                    t["sectionName"] = tk.sectionName;
                    t["iniFileName"] = tk.iniFileName;
                    if(!tk.key.empty()) t["key"] = tk.key;
                    if(!tk.back.empty()) t["back"] = tk.back;
                    if(!tk.type.empty()) t["type"] = tk.type;
                    t["variable"] = tk.variable;
                    t["values"] = tk.values;
                    if(!tk.currentValue.empty()) t["currentValue"] = tk.currentValue;
                    tks.push_back(t);
                }
                entry["toggleKeys"] = tks;
                resultJson.push_back(entry);
            }

            static thread_local std::string outputBuffer;
            outputBuffer = resultJson.dump();
            return outputBuffer.c_str();

        } catch (const std::exception& e) {
            static thread_local std::string errorBuffer;
            json error;
            error["error"] = e.what();
            errorBuffer = error.dump();
            return errorBuffer.c_str();
        } catch (...) {
            return "{\"error\":\"Unknown error\"}";
        }
    }
}