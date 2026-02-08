const platform = process.platform;
const arch = process.arch;

let nativeBinding = null;
let localFileExisted = false;
let loadError = null;

switch (platform) {
    case "win32":
        switch (arch) {
            case "x64":
                localFileExisted = true;
                try {
                    nativeBinding = require("./ini-parser.win32-x64-msvc.node");
                } catch (e) {
                    loadError = e;
                }
                break;
            default:
                loadError = new Error(`Unsupported architecture on Windows: ${arch}`);
        }
        break;
    default:
        loadError = new Error(`Unsupported OS: ${platform}, architecture: ${arch}`);
}

if (!nativeBinding) {
    if (loadError) {
        throw loadError;
    }
    throw new Error(`Failed to load native binding`);
}

let binding = nativeBinding;
if (binding.default && Object.keys(binding).length === 1) {
    binding = binding.default;
}

const { ToggleKey, IniResult, processIniFiles } = binding;

module.exports.ToggleKey = ToggleKey;
module.exports.IniResult = IniResult;
module.exports.processIniFiles = processIniFiles;
