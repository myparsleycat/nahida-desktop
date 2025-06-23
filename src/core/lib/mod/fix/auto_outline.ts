import fse from 'fs-extra';
import path from 'node:path';

export async function setOutline(dir: string, thickness: number) {
    await validateInputs(dir, thickness);

    const texcoords = await findFiles(dir, (name) => name.includes("Texcoord.buf"));
    const inis = await findFiles(dir, (name) => name.endsWith(".ini"));

    validateRequiredFiles(texcoords, inis);

    for (const texcoord of texcoords) {
        for (const ini of inis) {
            const stride = await extractStrideFromIni(ini, path.basename(texcoord));
            await processTexcoordFile(texcoord, ini, stride, thickness);
        }
    }

    console.log("All operations complete");
}

async function validateInputs(dir: string, thickness: number) {
    if (thickness < 0 || thickness > 255) {
        throw new Error("Thickness must be between 0 and 255");
    }

    if (!await fse.pathExists(dir)) {
        throw new Error(`Directory does not exist: ${dir}`);
    }
}

async function findFiles(dir: string, predicate: (filename: string) => boolean) {
    const results: string[] = [];

    async function searchRecursively(dir: string) {
        const items = await fse.readdir(dir);

        for (const item of items) {
            const fullPath = path.join(dir, item);
            const stat = await fse.stat(fullPath);

            if (stat.isDirectory()) {
                await searchRecursively(fullPath);
            } else if (predicate(item)) {
                results.push(fullPath);
            }
        }
    }

    await searchRecursively(dir);
    return results;
}

function validateRequiredFiles(texcoords: string[], inis: string[]) {
    if (texcoords.length === 0) {
        throw new Error("Unable to find texcoord file. Ensure you have 'Texcoord.buf' file in the directory or its subdirectories");
    }

    if (inis.length === 0) {
        throw new Error("Unable to find .ini file. Ensure you have a '.ini' file in the directory or its subdirectories");
    }
}

async function extractStrideFromIni(ini: string, texcoordFileName: string) {
    const iniContent = await fse.readFile(ini, 'utf8');
    const beforeTexcoord = iniContent.split(texcoordFileName)[0];
    const lines = beforeTexcoord.split('\n');
    const strideLine = lines[lines.length - 2];
    return parseInt(strideLine.split('=')[1].trim());
}

async function processTexcoordFile(texcoord: string, ini: string, stride: number, thickness: number) {
    console.log(`Processing - Texcoord: ${texcoord}, Ini: ${ini}, Stride: ${stride}`);

    const data = await fse.readFile(texcoord);
    const modifiableData = Buffer.from(data);

    for (let i = 0; i < modifiableData.length; i += stride) {
        modifiableData[i + 3] = thickness;
    }

    await fse.writeFile(texcoord, modifiableData);
}