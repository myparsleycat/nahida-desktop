import fse from 'fs-extra';
import path from 'node:path';

interface Replacement {
    old: string[];
    new: string;
}

interface ReplacementRule {
    line_prefix: string;
    replacements: Replacement[];
}

interface ComponentRemap {
    component_index: number;
    indices: Record<string, number>;
}

interface VertexRemapConfig {
    trigger_hash: string[];
    vertex_groups?: Record<string, number>;
    component_remap?: ComponentRemap[];
}

interface CharacterStates {
    injured?: Record<string, string>;
    wet?: Record<string, string>;
    wet_injured?: Record<string, string>;
}

interface CharacterConfig {
    main_hashes: Replacement[];
    texture_hashes?: Replacement[];
    checksum?: string;
    rules?: ReplacementRule[];
    vg_remaps?: VertexRemapConfig[];
    states?: CharacterStates;
}

interface WuwaConfig {
    characters: Record<string, CharacterConfig>;
}

const EARLY_CHARACTERS = [
    "RoverFemale", "RoverMale", "Yangyang", "Baizhi", "Chixia", "Jianxin",
    "Lingyang", "Encore", "Sanhua", "Verina", "Taoqi", "Calcharo",
    "Yuanwu", "Mortefi", "Aalto", "Jiyan", "Yinlin", "Jinhsi", "Changli"
];

class WuwaModFixer {
    private characters: Record<string, CharacterConfig>;
    private options: WuwaModFixOptions;
    private checksumRegex: RegExp;

    constructor(config: WuwaConfig, options: WuwaModFixOptions = {}) {
        this.characters = config.characters;
        this.options = options;
        this.checksumRegex = /checksum\s*=\s*\d+/g;
    }

    async processDirectory(dirPath: string): Promise<{ success: number; skipped: number }> {
        console.log(`모드 폴더 처리 시작: ${dirPath}`);

        let success = 0;
        let skipped = 0;

        const files = await this.getAllIniFiles(dirPath);

        for (const filePath of files) {
            try {
                const processed = await this.processFile(filePath);
                if (processed) {
                    success++;
                } else {
                    skipped++;
                }
            } catch (error) {
                console.error(`파일 처리 오류 ${filePath}:`, error);
                skipped++;
            }
            console.log('---------------------------------------------');
        }

        console.log(`폴더 처리 완료 - 성공: ${success}, 실패: ${skipped}`);
        return { success, skipped };
    }


    private escapeRegex(str: string): string {
        return str.replace(/[.*+?^${}()|[\]\\]/g, '\\');
    }

    private async getAllIniFiles(dirPath: string): Promise<string[]> {
        const files: string[] = [];
        const exclude = ["desktop", "ntuser", "disabled_backup", "disabled"];

        const processDir = async (currentPath: string) => {
            const entries = await fse.readdir(currentPath, { withFileTypes: true });

            for (const entry of entries) {
                const fullPath = path.join(currentPath, entry.name);

                if (entry.isDirectory()) {
                    await processDir(fullPath);
                } else if (this.isTargetFile(fullPath, exclude)) {
                    files.push(fullPath);
                }
            }
        };

        await processDir(dirPath);
        return files;
    }

    private isTargetFile(filePath: string, exclude: string[]): boolean {
        const fileName = path.basename(filePath).toLowerCase();
        const ext = path.extname(filePath);

        return ext === '.ini' && !exclude.some(keyword => fileName.includes(keyword));
    }

    private async processFile(filePath: string): Promise<boolean> {
        const content = await fse.readFile(filePath, 'utf-8');
        let modified = false;
        let iniModified = false;
        let newContent = content;

        console.log(`파일 처리 시작: ${filePath}`);

        // 각 캐릭터에 대해 해시 교체 처리
        for (const [charName, config] of Object.entries(this.characters)) {
            // vb0 해시 확인
            const vb0 = config.main_hashes[0];
            if (!vb0) continue;

            let matchOldMod = false;

            if (!this.anyMatch(content, vb0)) {
                if (!EARLY_CHARACTERS.includes(charName)) {
                    continue;
                }

                const shapeKeyHashes = config.main_hashes[1];
                if (shapeKeyHashes && this.anyMatch(content, shapeKeyHashes)) {
                    console.log("구버전 mod 발견");
                    matchOldMod = true;
                }

                if (!matchOldMod) {
                    continue;
                }
            }

            // RoverMale과 RoverFemale 구분
            if (charName === "RoverMale" && content.includes("FixAeroRoverFemale")) {
                continue;
            }

            console.log(`캐릭터 매칭: ${charName}`);

            // 메인 해시와 텍스처 해시 교체
            iniModified = this.replaceHashes(newContent, config.main_hashes).modified || iniModified;
            if (config.texture_hashes) {
                const textureResult = this.replaceHashes(newContent, config.texture_hashes);
                iniModified = textureResult.modified || iniModified;
                newContent = textureResult.content;
            }

            // 체크섬 교체
            if (config.checksum) {
                const checksumReplaced = newContent.replace(
                    this.checksumRegex,
                    `checksum = ${config.checksum}`
                );
                if (checksumReplaced !== newContent) {
                    newContent = checksumReplaced;
                    console.log(`체크섬 교체: ${charName} = ${config.checksum}`);
                    iniModified = true;
                }
            }

            // 인덱스 오프셋 카운트 교체
            if (config.rules) {
                const rulesResult = this.replaceIndexOffsetCount(newContent, config.rules);
                iniModified = rulesResult.modified || iniModified;
                newContent = rulesResult.content;
            }

            // 상태별 텍스처 오버라이드 처리
            if (this.options.textureOverride && config.states) {
                for (const [stateName, stateMap] of Object.entries(config.states)) {
                    const overrideResult = this.textureOverrideRedirection(newContent, stateMap, stateName);
                    iniModified = overrideResult.modified || iniModified;
                    newContent = overrideResult.content;
                }
            }

            // 버텍스 그룹 리맵핑 처리
            if (this.options.remap && config.vg_remaps) {
                const bufModified = await this.remaps(content, filePath, config.vg_remaps);
                modified = bufModified || modified;
            }

            // 특수 처리들
            if (charName === "RoverFemale") {
                // 풍주 여성 눈 수정 처리 (옵션으로 확장 가능)
                // const eyeFixed = await this.fixAeroRoverFemaleEyes(filePath, content);
                // modified = eyeFixed || modified;
            }

            if (charName === "Fleurdelys" && content.includes("618a230e")) {
                const fleurResult = await this.fixFleurdelys(newContent, filePath);
                iniModified = fleurResult.modified || iniModified;
                newContent = fleurResult.content;
                modified = fleurResult.bufModified || modified;
            }

            break;
        }

        if (iniModified) {
            await this.createBackup(filePath);
            await fse.writeFile(filePath, newContent);
            console.log(`파일 처리 완료: ${filePath}`);
        }

        modified = iniModified;

        if (!modified) {
            console.log("수정 필요 없음");
        }

        return modified;
    }

    private anyMatch(content: string, replacement: Replacement): boolean {
        return replacement.old.some(hash => content.includes(hash)) || content.includes(replacement.new);
    }

    private replaceHashes(content: string, hashes: Replacement[]): { content: string; modified: boolean } {
        let modified = false;
        let newContent = content;

        for (const hr of hashes) {
            for (let i = hr.old.length - 1; i >= 0; i--) {
                const oldHash = hr.old[i];
                if (oldHash !== hr.new && content.includes(`hash = ${oldHash}`)) {
                    const regex = new RegExp(`\\bhash\\s*=\\s*${this.escapeRegex(oldHash)}\\b`, 'g');
                    newContent = newContent.replace(regex, `hash = ${hr.new}`);
                    modified = true;
                    console.log(`${oldHash} -> ${hr.new}`);
                    break;
                }
            }
        }

        return { content: newContent, modified };
    }

    private replaceIndexOffsetCount(content: string, rules: ReplacementRule[]): { content: string; modified: boolean } {
        let modified = false;
        const lines = content.split('\n');

        for (let lineNum = 0; lineNum < lines.length; lineNum++) {
            let line = lines[lineNum];

            for (const rule of rules) {
                if (!line.trim().startsWith(rule.line_prefix)) {
                    continue;
                }

                for (const replacement of rule.replacements) {
                    for (let i = replacement.old.length - 1; i >= 0; i--) {
                        const oldVal = replacement.old[i];
                        const pos = line.indexOf(oldVal);
                        if (pos !== -1) {
                            line = line.substring(0, pos) + replacement.new + line.substring(pos + oldVal.length);
                            console.log(`[L${lineNum + 1}] ${oldVal} -> ${replacement.new}`);
                            modified = true;
                            break;
                        }
                    }
                }
            }

            lines[lineNum] = line;
        }

        return { content: lines.join('\n'), modified };
    }

    private textureOverrideRedirection(
        content: string,
        texOverrideMap: Record<string, string>,
        headerSuffix: string
    ): { content: string; modified: boolean } {
        const newFixSections: string[] = [];

        for (const [changedHash, originalHash] of Object.entries(texOverrideMap)) {
            if (!content.includes(originalHash) || content.includes(changedHash)) {
                continue;
            }

            const matchResult = this.getTextureOverrideContentAfterMatchPriority(originalHash, content);
            const cloneContent = matchResult.content.trim();

            if (cloneContent) {
                let sectionHeader = matchResult.section_header.trim();
                if (sectionHeader.startsWith('[') && sectionHeader.endsWith(']')) {
                    sectionHeader = sectionHeader.slice(1, -1);
                }

                console.log(`${changedHash} -> ${originalHash}: ${sectionHeader}`);
                const textureOverrideSection = `[${sectionHeader}_${headerSuffix}]`;
                const newSectionContent = `${textureOverrideSection}\nhash = ${changedHash}\nmatch_priority = 0\n${cloneContent}`;
                newFixSections.push(newSectionContent);
            }
        }

        if (newFixSections.length === 0) {
            return { content, modified: false };
        }

        const newContent = content + '\n' + newFixSections.join('\n\n') + '\n';
        return { content: newContent, modified: true };
    }

    private getTextureOverrideContentAfterMatchPriority(originalHash: string, content: string): {
        section_header: string;
        content: string;
    } {
        const lines = content.trim().split('\n');
        let foundSection = false;
        let matchPriorityFound = false;
        let sectionHeader = '';
        const contentLines: string[] = [];

        for (const line of lines) {
            const trimmedLine = line.trim();

            if (!matchPriorityFound && trimmedLine.startsWith('[TextureOverride')) {
                sectionHeader = trimmedLine;
                foundSection = false;
                continue;
            }

            if (foundSection) {
                if (trimmedLine.startsWith('match_priority')) {
                    matchPriorityFound = true;
                    continue;
                }

                if (matchPriorityFound) {
                    if (trimmedLine.startsWith('[')) {
                        break;
                    }
                    if (!trimmedLine.startsWith(';')) {
                        contentLines.push(trimmedLine);
                    }
                }
            }

            if (trimmedLine.includes(originalHash)) {
                foundSection = true;
            }
        }

        return {
            section_header: sectionHeader,
            content: contentLines.join('\n')
        };
    }

    private async createBackup(filePath: string): Promise<void> {
        const now = new Date();
        const datetime = now.toISOString().replace(/[:.]/g, '-').slice(0, 19);
        const ext = path.extname(filePath);
        const name = path.basename(filePath, ext);
        const dir = path.dirname(filePath);
        const backupName = `${name}_${datetime}.BAK`;
        const backupPath = path.join(dir, backupName);

        await fse.copyFile(filePath, backupPath);
        console.log(`백업 생성: ${backupPath}`);
    }

    private async remaps(content: string, filePath: string, vgRemaps: VertexRemapConfig[]): Promise<boolean> {
        let modified = false;
        const blendBufferPaths = this.parseResourceBufferPath(content, 'Blend', filePath);

        console.log('Blend buffer paths:', blendBufferPaths);

        const useMergedSkeleton = content.includes('[ResourceMergedSkeleton]');
        const multipleBlendFiles = blendBufferPaths.length > 1;

        for (const blendPath of blendBufferPaths) {
            try {
                const exists = await fse.access(blendPath).then(() => true).catch(() => false);
                if (!exists) {
                    console.warn(`${blendPath} not found`);
                    continue;
                }

                let matchFlag = false;
                let applyFlag = false;
                let blendData = await fse.readFile(blendPath);

                for (const vgRemap of vgRemaps) {
                    if (matchFlag || vgRemap.trigger_hash.some(h => content.includes(`hash = ${h}`))) {
                        try {
                            const remapResult = useMergedSkeleton
                                ? this.applyRemapMerged(blendData, vgRemap)
                                : this.applyRemapComponent(blendData, vgRemap, blendPath, content, multipleBlendFiles);

                            if (remapResult.success) {
                                blendData = remapResult.data;
                                applyFlag = true;
                            } else {
                                console.log(`remap 건너뜀: ${blendPath}`);
                            }
                        } catch (error) {
                            console.error('Remap error:', error);
                        }
                        matchFlag = true;
                    }
                }

                if (applyFlag) {
                    console.log('리맵핑 성공');
                    await this.createBackup(blendPath);
                    await fse.writeFile(blendPath, blendData);
                    modified = true;
                }
            } catch (error) {
                console.error(`Error processing ${blendPath}:`, error);
            }
        }

        return modified;
    }

    private parseResourceBufferPath(content: string, bufferType: string, iniPath: string): string[] {
        const paths: string[] = [];
        const regex = new RegExp(`\\[Resource${bufferType}Buffer[^\\]]*\\]([^\\[]+)`, 'gi');
        let match;

        while ((match = regex.exec(content)) !== null) {
            const section = match[1];
            const filenameMatch = section.match(/filename\s*=\s*(.+)/i);
            if (filenameMatch) {
                const filename = filenameMatch[1].trim();
                const fullPath = path.resolve(path.dirname(iniPath), filename);
                paths.push(fullPath);
            }
        }

        return paths;
    }

    private applyRemapMerged(data: Buffer, vgRemap: VertexRemapConfig): { success: boolean; data: Buffer } {
        if (!vgRemap.vertex_groups) {
            return { success: false, data };
        }

        const newData = Buffer.from(data);
        // 머지드 스켈레톤 리맵핑 로직 (4바이트씩 처리)
        for (let i = 0; i < newData.length; i += 4) {
            for (const [oldVg, newVg] of Object.entries(vgRemap.vertex_groups)) {
                const oldVgNum = parseInt(oldVg);
                const currentVg = newData.readUInt32LE(i);
                if (currentVg === oldVgNum) {
                    newData.writeUInt32LE(newVg, i);
                }
            }
        }

        return { success: true, data: newData };
    }

    private applyRemapComponent(
        data: Buffer,
        vgRemap: VertexRemapConfig,
        _blendPath: string,
        content: string,
        _multipleBlendFiles: boolean
    ): { success: boolean; data: Buffer } {
        if (!vgRemap.component_remap) {
            return { success: false, data };
        }

        // 컴포넌트 인덱스 파싱
        const componentIndices = this.parseComponentIndices(content);
        const newData = Buffer.from(data);

        for (const componentRemap of vgRemap.component_remap) {
            const componentIndex = componentRemap.component_index;
            const indices = componentIndices.get(componentIndex);

            if (!indices) continue;

            const [indexCount, indexOffset] = indices;
            const { start, end } = this.getByteRangeInBuffer(indexCount, indexOffset, data, 4);

            // 4바이트씩 리맵핑
            for (let i = start; i < end; i += 4) {
                const currentIndex = newData.readUInt32LE(i);
                const newIndex = componentRemap.indices[currentIndex.toString()];
                if (newIndex !== undefined) {
                    newData.writeUInt32LE(newIndex, i);
                }
            }
        }

        return { success: true, data: newData };
    }

    private parseComponentIndices(content: string): Map<number, [number, number]> {
        const indices = new Map<number, [number, number]>();
        const regex = /\[.*Component(\d+)\]([^\[]+)/gi;
        let match;

        while ((match = regex.exec(content)) !== null) {
            const componentIndex = parseInt(match[1]);
            const section = match[2];

            const countMatch = section.match(/match_index_count\s*=\s*(\d+)/i);
            const offsetMatch = section.match(/match_first_index\s*=\s*(\d+)/i);

            if (countMatch && offsetMatch) {
                const count = parseInt(countMatch[1]);
                const offset = parseInt(offsetMatch[1]);
                indices.set(componentIndex, [count, offset]);
            }
        }

        return indices;
    }

    private getByteRangeInBuffer(indexCount: number, indexOffset: number, data: Buffer, stride: number): { start: number; end: number } {
        const start = indexOffset * stride;
        const end = start + (indexCount * stride);
        return { start: Math.min(start, data.length), end: Math.min(end, data.length) };
    }

    private async fixFleurdelys(content: string, filePath: string): Promise<{ content: string; modified: boolean; bufModified: boolean }> {
        const blendBlockRegex = /\[ResourceBlendBuffer\][^\[]+/g;
        const strideRegex = /stride\s*=\s*8/g;

        const replacedContent = content.replace(blendBlockRegex, (match) => {
            return match.replace(strideRegex, 'stride = 16');
        });

        if (replacedContent === content) {
            return { content, modified: false, bufModified: false };
        }

        // blend 파일들 처리
        const blendPaths = this.parseResourceBufferPath(content, 'Blend', filePath);
        let bufModified = false;

        for (const blendPath of blendPaths) {
            try {
                const exists = await fse.access(blendPath).then(() => true).catch(() => false);
                if (!exists) continue;

                const blendData = await fse.readFile(blendPath);
                const expandedData = this.expandBlendStrideTo16(blendData);
                await this.createBackup(blendPath);
                await fse.writeFile(blendPath, expandedData);
                bufModified = true;
            } catch (error) {
                console.error(`Error processing blend file ${blendPath}:`, error);
            }
        }

        return { content: replacedContent, modified: true, bufModified };
    }

    private expandBlendStrideTo16(blendData: Buffer): Buffer {
        const BLEND_STRIDE = 8;
        const newData = Buffer.alloc(blendData.length * 2);
        let writePos = 0;

        for (let i = 0; i < blendData.length; i += BLEND_STRIDE) {
            const indices = blendData.subarray(i, i + 4);
            const weights = blendData.subarray(i + 4, i + 8);

            // 인덱스 + 패딩 + 웨이트 + 패딩
            indices.copy(newData, writePos);
            writePos += 4;
            newData.fill(0, writePos, writePos + 4); // 패딩
            writePos += 4;
            weights.copy(newData, writePos);
            writePos += 4;
            newData.fill(0, writePos, writePos + 4); // 패딩
            writePos += 4;
        }

        return newData;
    }
}

const WUWA_CONFIG: WuwaConfig = {
    "characters": {
        "RoverMale": {
            "main_hashes": [
                { "old": ["d53c2cc7", "c9db8418", "e18ca2cc"], "new": "b22dacf9" },
                { "old": ["f8375eb4", "3ab7c4d1"], "new": "f8375eb4" },
                { "old": ["6e2f48ba", "a4be44e5"], "new": "6e2f48ba" }
            ],
            "texture_hashes": [
                { "old": ["a1c0d97c"], "new": "f16d5dae" },
                { "old": ["7931ea8a"], "new": "fedfec0e" }
            ],
            "states": {
                "injured": {
                    "275382c9": "b4855e43",
                    "e7b350ce": "7bc718ed",
                    "fc2a5fb9": "65af60de",
                    "28b2e0ae": "db7ba06b"
                }
            }
        },
        "RoverFemale": {
            "main_hashes": [
                { "old": ["be84b775", "e8b5a730"], "new": "3533a957" },
                { "old": ["ac681fc1"], "new": "ac681fc1" }
            ],
            "texture_hashes": [
                { "old": ["2d5b41f6"], "new": "372bd73a" },
                { "old": ["c446a221"], "new": "99d33a32" },
                { "old": ["e18ca2cc"], "new": "b22dacf9" }
            ],
            "states": {
                "injured": {
                    "64f39763": "6aac203f",
                    "b42b8f11": "dcae957e",
                    "6bfc52b9": "99d33a32",
                    "b7f20fb3": "be8666c3",
                    "fae1dec5": "7bc718ed",
                    "9f464714": "cccc4663"
                }
            }
        },
        "Yangyang": {
            "main_hashes": [
                { "old": ["8ac49d65", "249200ff"], "new": "bb5dfeaf" },
                { "old": ["2ce54174", "d70fa623"], "new": "b43ab901" },
                { "old": ["502c1487", "1b1ed7be"], "new": "15a4a9b9" }
            ],
            "texture_hashes": [
                { "old": ["d25b9648"], "new": "77fe24ce" },
                { "old": ["3bd37212"], "new": "edf438d9" },
                { "old": ["fba3de34"], "new": "123bcc8e" },
                { "old": ["c3b2a42e"], "new": "bd8fff82" },
                { "old": ["8e27c9a2"], "new": "f42c4870" },
                { "old": ["1a9b9391"], "new": "69c48be0" },
                { "old": ["49b790e0"], "new": "92d4ad47" },
                { "old": ["ae886086"], "new": "16f9802d" },
                { "old": ["250c59b6"], "new": "fc40048f" },
                { "old": ["b487d389"], "new": "6905a9bd" },
                { "old": ["02e9009d"], "new": "1607589b" }
            ],
            "rules": [
                {
                    "line_prefix": "match_first_index",
                    "replacements": [
                        { "old": ["120018"], "new": "120021" }
                    ]
                },
                {
                    "line_prefix": "match_index_count",
                    "replacements": [
                        { "old": ["2085"], "new": "2088" }
                    ]
                }
            ],
            "vg_remaps": [
                {
                    "trigger_hash": ["502c1487", "1b1ed7be"],
                    "vertex_groups": {
                        "3": 5, "4": 3, "5": 4,
                        "36": 40, "37": 41, "38": 39, "39": 36, "40": 37, "41": 38, "48": 52, "49": 48, "50": 49, "51": 50, "52": 51,
                        "63": 69, "64": 70, "65": 71, "66": 72, "67": 68, "68": 65, "69": 66, "70": 67, "71": 75, "72": 76, "75": 63, "76": 64,
                        "94": 95, "95": 94, "102": 101, "103": 102, "113": 119, "114": 120, "116": 114, "117": 112, "118": 122, "119": 123, "120": 113,
                        "122": 116, "123": 117, "153": 154, "154": 153

                    },
                    "component_remap": [
                        {
                            "component_index": 0,
                            "indices": { "3": 5, "4": 3, "5": 4 }
                        },
                        {
                            "component_index": 3,
                            "indices": {
                                "3": 7, "4": 8, "5": 6, "6": 3, "7": 4, "8": 5, "15": 19, "16": 15, "17": 16, "18": 17, "19": 18, "30": 36, "31": 37,
                                "32": 38, "33": 39, "34": 35, "35": 32, "36": 33, "37": 34, "38": 42, "39": 43, "42": 30, "43": 31
                            }
                        },
                        {
                            "component_index": 4,
                            "indices": {
                                "15": 16, "16": 15, "22": 24, "23": 22, "24": 23, "33": 39, "34": 40, "35": 41, "37": 35, "38": 33, "39": 43, "40": 44,
                                "41": 34, "43": 37, "44": 38, "55": 56, "56": 55
                            }
                        },
                        {
                            "component_index": 5,
                            "indices": { "13": 14, "14": 13 }
                        }
                    ]
                }
            ],
            "states": {
                "injured": {
                    "a6fbf4d2": "fc40048f"
                }
            }
        },
        "Lingyang": {
            "main_hashes": [
                { "old": ["8b3c13f9"], "new": "9925d10e" },
                { "old": ["b239a59d"], "new": "d02c1cb1" },
                { "old": ["497106c3"], "new": "2e3de562" }
            ],
            "texture_hashes": [
                { "old": ["587edf05"], "new": "9925d10e" }
            ]
        },
        "Carlotta": {
            "main_hashes": [{ "old": ["4ba716ae"], "new": "4ba716ae" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "082598f8": "6deb3b31",
                    "2ddd54b4": "abea3993",
                    "fbda8c64": "90e7de37"
                }
            }
        },
        "Camellya": {
            "main_hashes": [{ "old": ["7748c1d8"], "new": "7748c1d8" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "10a2a706": "3a299182",
                    "65ee0c91": "830aecff",
                    "2231a66a": "faf8f2ae"
                }
            }
        },
        "Jinhsi": {
            "main_hashes": [
                { "old": ["243b7e59"], "new": "243b7e59" },
                { "old": ["8fb7baf7"], "new": "8fb7baf7" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "a658902c": "911dca0f",
                    "73a02c4e": "64bbdb18",
                    "d508a408": "4296f0e8",
                    "a2f121b4": "27f4ef91",
                    "e2ced28a": "3943dca3"
                }
            }
        },
        "JinhsiPeachBlossom": {
            "main_hashes": [
                { "old": ["493ff3b9", "35f167aa"], "new": "8a382bf1" },
                { "old": ["2ebd42ac"], "new": "5c9d8190" },
                { "old": ["1f3fe8c7"], "new": "cbe0b3ad" }
            ],
            "texture_hashes": [
                { "old": ["67b24870"], "new": "7fd16705" },
                { "old": ["e2165717"], "new": "ffe3eb56" },
                { "old": ["17f46e6f"], "new": "5523c61b" },
                { "old": ["ec98aed8"], "new": "d7753a6c" }
            ],
            "checksum": "2010",
            "states": {
                "injured": {
                    "4282f943": "ef3cbf99",
                    "c30c292f": "1ccb4d01",
                    "81b36a74": "43d2d155",
                    "b02ff93b": "ffe3eb56",
                    "f0e2b5d5": "5523c61b"
                }
            },
            "rules": [
                {
                    "line_prefix": "match_first_index",
                    "replacements": [
                        { "old": ["121599"], "new": "128598" },
                        { "old": ["215265"], "new": "222492" },
                        { "old": ["216813"], "new": "224040" },
                        { "old": ["217041"], "new": "224268" }
                    ]
                },
                {
                    "line_prefix": "match_index_count",
                    "replacements": [
                        { "old": ["48537"], "new": "55536" },
                        { "old": ["93666"], "new": "93894" }
                    ]
                }
            ],
            "vg_remaps": [
                {
                    "trigger_hash": ["1f3fe8c7"],
                    "vertex_groups": {
                        "74": 113, "75": 114, "76": 97, "77": 105, "78": 96, "79": 104, "80": 74, "81": 75, "82": 76, "83": 77, "84": 78, "85": 79,
                        "86": 80, "87": 81, "88": 82, "89": 83, "90": 84, "91": 93, "92": 101, "93": 91, "94": 92, "95": 99, "96": 94, "97": 95, "98": 85,
                        "99": 86, "101": 102, "102": 103, "103": 87, "104": 88, "105": 89, "106": 90, "107": 98, "108": 106, "109": 107, "110": 109, "111": 108,
                        "112": 110, "113": 112, "114": 111, "245": 246, "247": 246, "248": 246, "251": 246, "252": 246
                    },
                    "component_remap": [
                        {
                            "component_index": 3,
                            "indices": {
                                "46": 85, "47": 86, "48": 69, "49": 77, "50": 68, "51": 76, "52": 46, "53": 47, "54": 48, "55": 49, "56": 50, "57": 51, "58": 52,
                                "59": 53, "60": 54, "61": 55, "62": 56, "63": 65, "64": 73, "65": 63, "66": 64, "67": 71, "68": 66, "69": 67, "70": 57, "71": 58, "73": 74,
                                "74": 75, "75": 59, "76": 60, "77": 61, "78": 62, "79": 70, "80": 78, "81": 79, "82": 81, "83": 80, "84": 82, "85": 84, "86": 83
                            }
                        },
                        {
                            "component_index": 4,
                            "indices": { "125": 126, "126": 125, "130": 131, "131": 132, "132": 135, "133": 130, "134": 133, "135": 134 }
                        }
                    ]
                }
            ]
        },
        "Shorekeeper": {
            "main_hashes": [{ "old": ["0266ab77"], "new": "0266ab77" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "0518cc35": "33b7a4d5",
                    "cae4b4a7": "2cb33efd",
                    "ee8d84b9": "a2f6b3e4",
                    "b682c0e1": "be955444",
                    "2d692f90": "15d38062"
                }
            }
        },
        "Zhezhi": {
            "main_hashes": [
                { "old": ["c8b08afd"], "new": "b4525ff8" },
                { "old": ["4cdc5987"], "new": "a7ff2cab" },
                { "old": ["7ebe61e9"], "new": "2208a16a" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "b274962c": "e2ff72ff",
                    "70c13f4a": "9122d2f2",
                    "2052adaa": "dc383c2f",
                    "b42b8f11": "16bf75a8",
                    "806b0500": "2931bfa1",
                    "63df44b0": "26045936"
                }
            },
            "rules": [
                {
                    "line_prefix": "match_index_count",
                    "replacements": [
                        { "old": ["3420"], "new": "3078" }
                    ]
                }
            ]
        },
        "Encore": {
            "main_hashes": [
                { "old": ["592add5c"], "new": "592add5c" },
                { "old": ["388cecb3"], "new": "388cecb3" }
            ],
            "texture_hashes": [
                { "old": ["47c515ac"], "new": "a347d2bc" },
                { "old": ["b6021f06"], "new": "6ff2b9f1" }
            ],
            "states": {
                "injured": {
                    "d5d6d1f1": "e73ee6fb",
                    "efde5b45": "c26d7da2",
                    "4053249d": "34617fc8",
                    "1062e3a5": "beb2b10e",
                    "77d8a4de": "c44c3cb9",
                    "c93b8e26": "cd7ec1f3"
                }
            }
        },
        "Changli": {
            "main_hashes": [
                { "old": ["77975500", "5f8aac45"], "new": "d14bed8b" },
                { "old": ["d8decbca", "1ccb8008"], "new": "59f24b66" },
                { "old": ["fd9483ca", "060f5303"], "new": "277e18c9" }
            ],
            "texture_hashes": [
                { "old": ["8f067708"], "new": "f23483e3" },
                { "old": ["c79a4ccb"], "new": "225aad5a" }
            ],
            "rules": [
                {
                    "line_prefix": "match_first_index",
                    "replacements": [
                        { "old": ["152343"], "new": "153363" },
                        { "old": ["198855"], "new": "199875" },
                        { "old": ["283461"], "new": "284481" },
                        { "old": ["285489"], "new": "286509" }
                    ]
                },
                {
                    "line_prefix": "match_index_count",
                    "replacements": [
                        { "old": ["81513"], "new": "82533" }
                    ]
                }
            ],
            "vg_remaps": [
                {
                    "trigger_hash": ["fd9483ca"],
                    "component_remap": [
                        {
                            "component_index": 5,
                            "indices": {
                                "9": 23, "11": 28, "12": 10, "13": 11, "14": 12, "15": 13, "16": 14, "17": 15, "18": 16, "19": 17, "20": 18,
                                "21": 19, "22": 20, "23": 21, "24": 22, "25": 23, "26": 24, "27": 25, "28": 26, "29": 27, "30": 29, "31": 30,
                                "32": 31, "33": 32, "34": 33, "35": 34, "36": 35, "37": 36, "38": 37, "39": 38, "40": 39, "41": 40, "42": 41,
                                "43": 42, "44": 43, "45": 44, "46": 45, "47": 46
                            }
                        }
                    ]
                },
                {
                    "trigger_hash": ["060f5303"],
                    "vertex_groups": {
                        "135": 137,
                        "137": 135,
                        "123": 124,
                        "124": 125,
                        "125": 126,
                        "126": 123
                    },
                    "component_remap": [
                        {
                            "component_index": 3,
                            "indices": {
                                "64": 65, "65": 66, "66": 67, "67": 64, "76": 78, "78": 76
                            }
                        }
                    ]
                }
            ],
            "states": {
                "injured": {
                    "7072654c": "c02dbf56",
                    "628df960": "225aad5a",
                    "ccf96b54": "b54a043e",
                    "d36f54a5": "a260e7f7",
                    "a8e5e794": "a45bfe26",
                    "3bfaa05b": "d6f4003a"
                }
            }
        },
        "Danjin": {
            "main_hashes": [
                { "old": ["6aefd9b3"], "new": "6aefd9b3" },
                { "old": ["9f182348"], "new": "9f182348" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "6f6cd4bb": "1dd49aa4",
                    "b31e750c": "f6910c93",
                    "1e1108eb": "500d4413",
                    "429b6083": "e2321307",
                    "df6295e2": "b7758489"
                }
            }
        },
        "Sanhua": {
            "main_hashes": [
                { "old": ["33e4890f"], "new": "33e4890f" },
                { "old": ["a80e0e2b"], "new": "a80e0e2b" }
            ],
            "texture_hashes": [
                { "old": ["2584190a"], "new": "cef6494f" },
                { "old": ["28708ab8"], "new": "0bd3b5ab" },
                { "old": ["5efe7892"], "new": "89ba19a1" },
                { "old": ["11b9cadd"], "new": "f0713dc7" }
            ],
            "states": {
                "injured": {
                    "b222ae08": "2c0c2728"
                }
            }
        },
        "Verina": {
            "main_hashes": [
                { "old": ["83ced9f7"], "new": "82aa82e1" },
                { "old": ["15791c57"], "new": "15791c57" }
            ],
            "texture_hashes": [
                { "old": ["fd892e3c"], "new": "953daba7" },
                { "old": ["5981e400"], "new": "40f3c4ea" },
                { "old": ["63d590db"], "new": "c0ca0958" }
            ],
            "vg_remaps": [
                {
                    "trigger_hash": ["83ced9f7"],
                    "vertex_groups": {
                        "40": 41, "41": 40, "77": 78, "78": 79, "79": 80, "80": 81, "81": 82, "82": 83, "83": 84,
                        "84": 85, "85": 86, "86": 87, "87": 88, "88": 89, "89": 90, "90": 91, "91": 92, "92": 93, "93": 94, "94": 95, "95": 96,
                        "96": 97, "97": 98, "98": 99, "99": 100, "100": 101, "101": 102, "102": 103, "103": 104, "104": 105, "105": 106, "106": 107,
                        "107": 108, "108": 109, "109": 110, "110": 111, "111": 77, "112": 113, "114": 115, "174": 178, "175": 177, "176": 174, "177": 175, "178": 176
                    },
                    "component_remap": [
                        {
                            "component_index": 3,
                            "indices": {
                                "5": 6, "6": 5, "34": 33, "37": 46, "76": 42, "79": 80, "80": 79
                            }
                        },
                        {
                            "component_index": 4,
                            "indices": {
                                "39": 43, "40": 42, "41": 39, "42": 40, "43": 41
                            }
                        }
                    ]
                }
            ],
            "states": {
                "injured": {
                    "d8553fa9": "679ad2f5",
                    "fbdf715e": "fcc47274",
                    "4dd974c7": "ae7043eb",
                    "6bc44ed6": "c0ca0958",
                    "16261db1": "24c1883d"
                }
            }
        },
        "Baizhi": {
            "main_hashes": [
                { "old": ["4a2b2eca"], "new": "3cb91e7f" },
                { "old": ["d8d2286b"], "new": "d8d2286b" }
            ],
            "texture_hashes": [
                { "old": ["37bed36b"], "new": "718456ac" },
                { "old": ["52c9b804"], "new": "d7756134" },
                { "old": ["43ae1deb"], "new": "1a09bbb5" },
                { "old": ["fe4e4afe"], "new": "d755a4a9" }
            ],
            "states": {
                "injured": {
                    "c0e31ed3": "7404e947",
                    "1b8e24f0": "718456ac",
                    "83a57d50": "7c460f02",
                    "a3ea2aee": "804d32e9",
                    "7729d9e7": "d7756134"
                }
            },
            "vg_remaps": [
                {
                    "trigger_hash": ["37bed36b", "4a2b2eca", "43ae1deb"],
                    "vertex_groups": {
                        "17": 20, "18": 21, "19": 18, "20": 19, "21": 17,
                        "51": 53, "52": 51, "53": 52,
                        "84": 85, "85": 87, "86": 88, "87": 89, "88": 84, "89": 86,
                        "119": 122, "120": 121, "121": 119, "122": 120, "123": 128, "124": 129, "125": 126, "126": 125, "127": 123, "128": 124, "129": 127, "133": 136,
                        "134": 137, "135": 133, "136": 140, "137": 141, "138": 142, "139": 134, "140": 135, "141": 138, "142": 139
                    },
                    "component_remap": [
                        {
                            "component_index": 0,
                            "indices": { "17": 20, "18": 21, "19": 18, "20": 19, "21": 17 }
                        },
                        {
                            "component_index": 3,
                            "indices": {
                                "14": 16, "15": 14, "16": 15, "47": 48, "48": 50, "49": 51, "50": 52, "51": 47, "52": 49,
                                "82": 85, "83": 84, "84": 82, "85": 83, "86": 91, "87": 92, "88": 89, "89": 88, "90": 86, "91": 87, "92": 90,
                                "96": 99, "97": 100, "98": 96, "99": 103, "100": 104, "101": 105, "102": 97, "103": 98, "104": 101, "105": 102
                            }
                        },
                        {
                            "component_index": 4,
                            "indices": { "17": 21, "18": 19, "19": 17, "20": 18, "21": 20 }
                        }
                    ]
                }
            ]
        },
        "Chixia": {
            "main_hashes": [
                { "old": ["1606ae41"], "new": "1606ae41" },
                { "old": ["9b69878e"], "new": "9b69878e" }
            ],
            "texture_hashes": [
                { "old": ["ab72381e"], "new": "eee73787" },
                { "old": ["7988637b"], "new": "45e0cedb" },
                { "old": ["94afca13"], "new": "489b5f2a" },
                { "old": ["cb974015"], "new": "94d10f56" },
                { "old": ["873ca04e"], "new": "ba246036" },
                { "old": ["a7141c04"], "new": "4a2657d7" },
                { "old": ["8da682dd"], "new": "9497a1b9" }
            ],
            "states": {
                "injured": {
                    "064234d1": "8f423e37",
                    "8da682dd": "9497a1b9",
                    "7993434a": "ba246036",
                    "fd9dd557": "489b5f2a",
                    "f6c24f7d": "45e0cedb"
                }
            }
        },
        "Taoqi": {
            "main_hashes": [
                { "old": ["e2685889"], "new": "e2685889" },
                { "old": ["9f13087a"], "new": "9f13087a" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "921c63f7": "33a41964"
                }
            }
        },
        "Jianxin": {
            "main_hashes": [
                { "old": ["80f8caf9", "e9f8341f"], "new": "9b60ec42" },
                { "old": ["affc2fc3", "82d39ecb"], "new": "29ba85c5" },
                { "old": ["ead048c8", "068dd115"], "new": "bc9677ff" }
            ],
            "texture_hashes": [
                { "old": ["1d823cfb"], "new": "0862e376" }
            ],
            "checksum": "1876",
            "states": {
                "injured": {
                    "4bbdc3ad": "0862e376",
                    "e607b4a5": "6700cf35",
                    "21604370": "534c2615",
                    "1efc8bd1": "a7275cef",
                    "4080dfd4": "f095bfbc"
                }
            }
        },
        "Youhu": {
            "main_hashes": [{ "old": ["03eea2d0"], "new": "03eea2d0" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "6175988f": "9b11d340",
                    "05f67279": "7e251226",
                    "470dc9ae": "39ffdf33",
                    "f31f91ff": "eb5b96b2",
                    "2411e310": "baee6fc5",
                    "ffd57c26": "c1078929"
                }
            }
        },
        "XiangliYao": {
            "main_hashes": [{ "old": ["ed536543"], "new": "ed536543" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "e4fe65b0": "1ccddcaf",
                    "141badb9": "d966cc04",
                    "b4a8149d": "392cfad5",
                    "3f81c006": "580004ac",
                    "a4aceecb": "3865a786",
                    "6ada1c07": "7c17bb8d"
                }
            }
        },
        "Calcharo": {
            "main_hashes": [
                { "old": ["63e02559"], "new": "63e02559" },
                { "old": ["9c24e7eb"], "new": "9c24e7eb" }
            ],
            "texture_hashes": [
                { "old": ["8b43ad38"], "new": "f3e04a65" },
                { "old": ["cb23f0b5"], "new": "52197a16" },
                { "old": ["f657b0b8"], "new": "8a7d6de5" }
            ],
            "states": {
                "injured": {
                    "a105f6fb": "de0af803",
                    "fdd3e083": "3320efad",
                    "60628a85": "96cdca80",
                    "7b017cc9": "feff1922",
                    "4ead195a": "8a7d6de5"
                }
            }
        },
        "Yuanwu": {
            "main_hashes": [
                { "old": ["51c230c2"], "new": "51c230c2" },
                { "old": ["0b0fecc4"], "new": "0b0fecc4" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "f5040e5a": "a59c17cb",
                    "8c14e5f2": "389f5e85",
                    "bd954ba8": "2ba57c1b",
                    "44fa35d0": "c6025664"
                }
            }
        },
        "Mortefi": {
            "main_hashes": [
                { "old": ["7b29fedf"], "new": "7b29fedf" },
                { "old": ["7d919713"], "new": "7d919713" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "162848e0": "11b18904",
                    "5ffe24ad": "6d76b1cc",
                    "7f428c5a": "bedffbf4",
                    "052d9b73": "18fb0f57",
                    "033e7f72": "413c2f2e",
                    "afa20d6f": "f81811a7"
                }
            }
        },
        "Aalto": {
            "main_hashes": [
                { "old": ["93bbbdc0"], "new": "93bbbdc0" },
                { "old": ["4f0a1fba"], "new": "4f0a1fba" }
            ],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "6d8b824b": "41d712e3",
                    "05358c37": "83220aa9",
                    "e73a299b": "78616c21",
                    "e3ee8d85": "03265d9c"
                }
            }
        },
        "Yinlin": {
            "main_hashes": [
                { "old": ["42702199"], "new": "42702199" },
                { "old": ["41d656e8"], "new": "41d656e8" }
            ],
            "texture_hashes": [
                { "old": ["00120eee"], "new": "86b95122" },
                { "old": ["584b7755"], "new": "750390fa" },
                { "old": ["87bbb0c1"], "new": "30053482" },
                { "old": ["9ea4dc96"], "new": "76967821" },
                { "old": ["58b06268"], "new": "1f0f6dc8" },
                { "old": ["7d1b007a"], "new": "71525c2a" },
                { "old": ["e50849e0"], "new": "e56f82b1" },
                { "old": ["5f0fbdb9"], "new": "3271530d" },
                { "old": ["148a83c6"], "new": "33d00a20" },
                { "old": ["9ebf7cad"], "new": "5065eae3" }
            ],
            "states": {
                "injured": {
                    "8191eb28": "1f0f6dc8",
                    "9ca33205": "30053482",
                    "7a826f6c": "86b95122",
                    "17bd5877": "711af10e",
                    "b5054dd5": "76967821"
                }
            }
        },
        "Jiyan": {
            "main_hashes": [
                { "old": ["31b50c1d"], "new": "31b50c1d" },
                { "old": ["7759b7d3"], "new": "7759b7d3" }
            ],
            "texture_hashes": [
                { "old": ["b05fac63"], "new": "9631335c" },
                { "old": ["1b3a68de"], "new": "7741698a" },
                { "old": ["55f78d68"], "new": "9a8dc04e" }
            ],
            "states": {
                "injured": {
                    "a782abc1": "9631335c"
                }
            }
        },
        "Lumi": {
            "main_hashes": [{ "old": ["b441b9f6"], "new": "b441b9f6" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "976b780e": "0390820a",
                    "395e686d": "35c1c5cf",
                    "11dd9524": "2decd61b",
                    "f9934a03": "dda29ba9",
                    "1e55dd5a": "0249a0f6"
                }
            }
        },
        "Roccia": {
            "main_hashes": [{ "old": ["8d97c1bd"], "new": "8d97c1bd" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "2705d019": "87d73bf2",
                    "723f7244": "843152d9",
                    "5ba0e791": "86f266a5",
                    "59d4e7cf": "6f2fd0ce",
                    "9104e80a": "16558db2",
                    "21bfae92": "c9c9ae65"
                }
            }
        },
        "SanhuaExorcistic": {
            "main_hashes": [{ "old": ["b101dcf3"], "new": "b101dcf3" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "fb72762f": "464256d1",
                    "8ff044bc": "31a5f36a",
                    "b908eb42": "9522bbc7",
                    "41d44529": "fd078186",
                    "e8fdba2d": "168462a9"
                }
            }
        },
        "Phoebe": {
            "main_hashes": [
                { "old": ["93b4481e", "3a4bf877", "8ee2fb7c"], "new": "026f7616" },
                { "old": ["e304df53", "a284a970", "00b2a919"], "new": "8ba3d365" },
                { "old": ["3f411789", "baea13f2", "ecac2cc5"], "new": "2c8f9580" }
            ],
            "texture_hashes": [
                { "old": ["4a30f1df"], "new": "83bc4e8a" },
                { "old": ["2c8ff000"], "new": "e5034f55" }
            ],
            "states": {
                "injured": {
                    "e5034f55": "83bc4e8a",
                    "92ab1de5": "cbe73490",
                    "68055536": "a04d2480",
                    "cf6f91c1": "1641ec82",
                    "c464c8d5": "18b491c2"
                }
            }
        },
        "Brant": {
            "main_hashes": [{ "old": ["5943f99a"], "new": "5943f99a" }],
            "texture_hashes": [],
            "states": {
                "injured": {
                    "8a6d655d": "41be688c",
                    "b33059f7": "93f37a93",
                    "e096a650": "67175ee6",
                    "d86edd42": "2cf897c6",
                    "12c849c4": "fc28e018"
                }
            }
        },
        "Cantarella": {
            "main_hashes": [{ "old": ["5432f939"], "new": "5432f939" }],
            "texture_hashes": [
                { "old": ["787c92b8"], "new": "812ba53f" },
                { "old": ["35c01267", "b6deb6c2"], "new": "734ad993" },
                { "old": ["0fce3633", "0480d129"], "new": "59c7c460" },
                { "old": ["01c122b3"], "new": "e3127902" },
                { "old": ["354e8743", "801b4344"], "new": "abd93fa3" },
                { "old": ["28105b41", "884dc2c3"], "new": "3ea27f2c" },
                { "old": ["797bc768"], "new": "62a665e3" },
                { "old": ["8f3ebd32"], "new": "5cfb3cbc" },
                { "old": ["7423d5d0"], "new": "ad7c2fad" }
            ],
            "states": {
                "injured": {
                    "4a9b2a09": "d789c54e",
                    "ad7c2fad": "812ba53f",
                    "e3127902": "734ad993",
                    "25144e25": "dc14e209",
                    "2745d8b5": "c93ea25b"
                },
                "wet": {
                    "5e48f2a4": "d789c54e",
                    "5cfb3cbc": "812ba53f",
                    "abd93fa3": "734ad993",
                    "d67890aa": "dc14e209",
                    "ef3dab0e": "c93ea25b"
                },
                "wet_injured": {
                    "316457f4": "d789c54e",
                    "62a665e3": "812ba53f",
                    "3ea27f2c": "734ad993",
                    "f9f65a14": "dc14e209",
                    "a45cb30b": "c93ea25b"
                }
            }
        },
        "Cantarella_Umbrella": {
            "main_hashes": [{ "old": ["2005ef51"], "new": "2005ef51" }],
            "texture_hashes": [{ "old": ["52a54db3"], "new": "093cb526" }]
        },
        "Fleurdelys": {
            "main_hashes": [
                { "old": ["618a230e", "a74b1202"], "new": "6a4d2710" },
                { "old": ["b1151c81"], "new": "2f1d7ffd" },
                { "old": ["426cd36b"], "new": "2109de80" }
            ],
            "texture_hashes": [
                { "old": ["874ab04e", "afb0ea93"], "new": "cc3910e7" },
                { "old": ["c35ad2eb", "1036407c"], "new": "b08d5d57" },
                { "old": ["e385365e", "f079e2b8"], "new": "1028b6b7" }
            ]
        },
        "Zani_Interface": {
            "main_hashes": [{ "old": ["b9f4b0b4"], "new": "c2727ed7" }]
        },
        "Zani_Upper": {
            "main_hashes": [{ "old": ["eb7b28e3"], "new": "8acf0a92" }],
            "states": {
                "injured": {
                    "81693b04": "c91bc37a",
                    "ab3eb291": "bfaa82b4"
                }
            }
        },
        "Zani_Lower": {
            "main_hashes": [{ "old": ["12b90236"], "new": "07352718" }],
            "states": { "injured": { "3cc705ad": "988c2332" } }
        },
        "Zani_Liberupper": {
            "main_hashes": [{ "old": ["dbc0a3d1"], "new": "dbc0a3d1" }],
            "states": { "injured": { "f7694bcf": "c28372b3" } }
        },
        "Cartethyia_NPC": {
            "main_hashes": [{ "old": ["b1213fab"], "new": "b1213fab" }],
            "texture_hashes": [
                { "old": ["5ca9f916"], "new": "59d89437" },
                { "old": ["f953f385"], "new": "7ef7dba9" },
                { "old": ["a6ac08f5"], "new": "7f08cd4f" }
            ]
        },
        "Ciaccona": {
            "main_hashes": [{ "old": ["e57c9380"], "new": "e57c9380" }],
            "states": {
                "injured": {
                    "70a4acf0": "fe853fcf",
                    "987a6f0e": "cca1af56",
                    "064492c6": "852b2b72",
                    "1603d6fa": "24af21ae",
                    "bebbf828": "2f590d3f",
                    "17a8f8fa": "4c379f61",
                    "55dfd156": "1e072c24"
                }
            }
        }
    }
};

interface WuwaModFixOptions {
    textureOverride?: boolean;
    remap?: boolean;
}

export async function wuwaModFix(
    modPath: string,
    options: WuwaModFixOptions = {}
): Promise<{ success: number; skipped: number }> {
    const fixer = new WuwaModFixer(WUWA_CONFIG, options);
    return await fixer.processDirectory(modPath);
}

export default wuwaModFix;
export type { CharacterConfig, WuwaConfig, Replacement, ReplacementRule, VertexRemapConfig };