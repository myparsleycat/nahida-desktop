import * as fs from 'fs';
import * as path from 'path';

const globalModifiedBuffers: Record<string, string[]> = {};

interface DefaultArgs {
    hash: string;
    ini: Ini;
    tabs: number;
    data: Record<string, string>;
}

interface ExecutionResult {
    touched?: boolean;
    failed?: boolean;
    signalBreak?: boolean;
    queueHashes?: string[];
    queueCommands?: Command[];
}

type Command = [new (...args: any[]) => CommandExecutor, any?];

interface CommandExecutor {
    execute(defaultArgs: DefaultArgs): ExecutionResult;
}

export async function processFolder(folderPath: string) {
    const files = fs.readdirSync(folderPath);

    for (const filename of files) {
        if (filename.toUpperCase().startsWith('DISABLED') && filename.toLowerCase().endsWith('.ini')) {
            continue;
        }
        if (filename.toUpperCase().startsWith('DESKTOP')) {
            continue;
        }

        const filepath = path.join(folderPath, filename);
        const stats = fs.statSync(filepath);

        if (stats.isDirectory()) {
            processFolder(filepath);
        } else if (filename.endsWith('.ini')) {
            console.log('.ini 파일 발견:', filepath);
            upgradeIni(filepath);
        }
    }
}

function upgradeIni(filepath: string): boolean {
    try {
        const ini = new Ini(filepath).upgrade();
        ini.save();
        return true;
    } catch (error) {
        console.log('오류 발생:', error);
        console.log(`${filepath}에 변경사항이 적용되지 않았습니다!`);
        console.log();
        console.error(error);
        console.log();
        return false;
    }
}

function getCriticalContent(section: string): {
    criticalContent: string;
    hash?: string;
    matchFirstIndex?: string;
} {
    let hash: string | undefined;
    let matchFirstIndex: string | undefined;
    const criticalLines: string[] = [];
    const pattern = /^\s*(.*?)\s*=\s*(.*?)\s*$/i;

    for (const line of section.split('\n')) {
        const lineMatch = line.match(pattern);

        if (line.trim().startsWith('[')) {
            continue;
        } else if (lineMatch && lineMatch[1].toLowerCase() === 'hash') {
            hash = lineMatch[2];
        } else if (lineMatch && lineMatch[1].toLowerCase() === 'match_first_index') {
            matchFirstIndex = lineMatch[2];
        } else {
            criticalLines.push(line);
        }
    }

    return {
        criticalContent: criticalLines.join('\n'),
        hash,
        matchFirstIndex
    };
}

function processCommandlist(iniContent: string, commandlist: string, target: string): string[] {
    const linePattern = new RegExp(`^\\s*(run|${target})\\s*=\\s*(.*)\\s*$`, 'gi');
    const resources: string[] = [];

    for (const line of commandlist.split('\n')) {
        const lineMatch = line.match(linePattern);
        if (!lineMatch) continue;

        if (lineMatch[1].toLowerCase() === target.toLowerCase()) {
            resources.push(lineMatch[2]);
        } else if (lineMatch[1].toLowerCase() === 'run') {
            const commandlistTitle = lineMatch[2];
            const pattern = getSectionTitlePattern(commandlistTitle);
            const commandlistMatch = pattern.exec(iniContent + '\n[');
            if (commandlistMatch) {
                const subResources = processCommandlist(iniContent, commandlistMatch[1], target);
                resources.push(...subResources);
            }
        }
    }

    return resources;
}

function getSectionHashPattern(hash: string): RegExp {
    return new RegExp(
        `^([ \\t]*?\\[(?:Texture|Shader)Override.*\\][ \\t]*(?:\\r?\\n(?![ \\t]*?(?:\\[|hash\\s*=)).*$)*?(?:\\r?\\n\\s*hash\\s*=\\s*${hash}[ \\t]*)(?:(?:\\r?\\n(?![ \\t]*?\\[).*$)*(?:\\r?\\n[\\t ]*?[\\$\\w].*$))?)\\s*`,
        'gim'
    );
}

function getSectionTitlePattern(title: string): RegExp {
    return new RegExp(
        `^([ \\t]*?\\[${title}\\](?:(?:\\r?\\n(?![ \\t]*?\\[).*$)*(?:\\r?\\n[\\t ]*?[\\$\\w].*$))?)\\s*`,
        'gim'
    );
}

class Ini {
    filepath: string;
    content: string;
    encoding: 'utf-8';
    private hashes: string[] = [];
    private touched = false;
    private doneHashes = new Set<string>();
    modifiedBuffers: Record<string, Buffer> = {};

    constructor(filepath: string) {
        this.filepath = filepath;

        try {
            this.content = fs.readFileSync(filepath, 'utf-8');
            this.encoding = 'utf-8';
        } catch (error) {
            throw error;
            // this.content = fs.readFileSync(filepath, 'gb2312');
            // this.encoding = 'gb2312';
        }

        const pattern = /\n\s*hash\s*=\s*([a-f0-9]*)/gi;
        const matches = this.content.matchAll(pattern);
        this.hashes = Array.from(matches, match => match[1]);
    }

    upgrade(): Ini {
        while (this.hashes.length > 0) {
            const hash = this.hashes.pop()!;
            if (!this.doneHashes.has(hash)) {
                if (hash in hashCommands) {
                    console.log(`\t${hash} 처리 중:`);
                    const defaultArgs: DefaultArgs = {
                        hash,
                        ini: this,
                        data: {},
                        tabs: 2
                    };
                    this.execute(hashCommands[hash], defaultArgs);
                } else {
                    console.log(`\t${hash} 건너뛰기: 사용 가능한 작업 없음`);
                }
            } else {
                console.log(`\t${hash} 건너뛰기: 이미 확인/처리됨`);
            }

            this.doneHashes.add(hash);
        }

        return this;
    }

    execute(commands: Command[], defaultArgs: DefaultArgs): DefaultArgs {
        for (const command of commands) {
            const [CommandClass, args] = command;

            let instance: CommandExecutor;
            if (CommandClass === Log && Array.isArray(args)) {
                instance = new CommandClass(...args);
            } else if (Array.isArray(args)) {
                instance = new CommandClass(...args);
            } else {
                instance = new CommandClass(args || {});
            }

            const result: ExecutionResult = instance.execute(defaultArgs);

            this.touched = this.touched || !!result.touched;

            if (result.failed) {
                console.log();
                return defaultArgs;
            }

            if (result.queueHashes) {
                const newHashes = result.queueHashes.filter(h => !this.doneHashes.has(h));
                this.hashes.push(...newHashes);
            }

            if (result.queueCommands) {
                this.execute(result.queueCommands, defaultArgs);
            }

            if (result.signalBreak) {
                return defaultArgs;
            }
        }

        return defaultArgs;
    }

    save(): void {
        if (this.touched) {
            const basename = path.basename(this.filepath).split('.ini')[0];
            const dirPath = path.dirname(path.resolve(this.filepath));
            const backupFilename = `DISABLED_BACKUP_${Date.now()}.${basename}.ini`;
            const backupFullpath = path.join(dirPath, backupFilename);

            fs.renameSync(this.filepath, backupFullpath);
            console.log(`백업 생성: ${backupFilename} at ${dirPath}`);

            fs.writeFileSync(this.filepath, this.content, this.encoding);

            if (Object.keys(this.modifiedBuffers).length > 0) {
                console.log('업데이트된 버퍼 쓰기');
                for (const [filepath, data] of Object.entries(this.modifiedBuffers)) {
                    fs.writeFileSync(filepath, data);
                    console.log(`\t저장됨: ${filepath}`);
                }
            }

            console.log('업데이트 적용됨');
        } else {
            console.log('변경사항 없음');
        }
        console.log();
    }

    hasHash(hash: string): boolean {
        return this.hashes.includes(hash) || this.doneHashes.has(hash);
    }
}

class Log implements CommandExecutor {
    private text: string[];

    constructor(...text: string[]) {
        this.text = text;
    }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { tabs } = defaultArgs;
        const [info, hash = '', title = '', ...rest] = this.text;

        let s = `${'\t'.repeat(tabs)}${info.padEnd(34)}`;
        if (hash) s += ` - ${hash.padEnd(8)}`;
        if (title) s += ` - ${title}`;
        if (rest.length) s += ` - ${rest.join(' - ')}`;

        console.log(s);

        return { touched: false, failed: false, signalBreak: false };
    }
}

class Zzz13RemapTexcoord implements CommandExecutor {
    constructor(
        private id: string,
        private oldFormat: string[], // ['4B','2e','2f','2e']
        private newFormat: string[]  // ['4B','2f','2f','2f']
    ) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash, tabs } = defaultArgs;

        if (this.oldFormat.length !== this.newFormat.length) {
            throw new Error('Old format과 new format의 길이가 다릅니다');
        }

        const oldStride = this.calculateStride(this.oldFormat);
        const newStride = this.calculateStride(this.newFormat);

        let offset = 0;
        const offsets = [0];
        for (const formatChunk of this.oldFormat) {
            offset += this.getFormatSize(formatChunk);
            offsets.push(offset);
        }

        const pattern = getSectionHashPattern(hash);
        const sectionMatch = pattern.exec(ini.content);
        if (!sectionMatch) return { touched: false };

        const resources = processCommandlist(ini.content, sectionMatch[1], 'vb1');

        const bufferFilenames = new Set<string>();
        const linePattern = /^\s*(filename|stride)\s*=\s*(.*)\s*$/i;

        for (const resource of resources) {
            const resourcePattern = getSectionTitlePattern(resource);
            const resourceSectionMatch = resourcePattern.exec(ini.content);
            if (!resourceSectionMatch) continue;

            const modifiedResourceSection: string[] = [];
            for (const line of resourceSectionMatch[1].split('\n')) {
                const lineMatch = line.match(linePattern);
                if (!lineMatch) {
                    modifiedResourceSection.push(line);
                } else if (lineMatch[1].toLowerCase() === 'filename') {
                    modifiedResourceSection.push(line);
                    bufferFilenames.add(lineMatch[2]);
                } else if (lineMatch[1].toLowerCase() === 'stride') {
                    const stride = parseInt(lineMatch[2]);
                    if (stride !== oldStride) {
                        console.log(`${'\t'.repeat(tabs)}X 경고 [${resource}]! 예상된 버퍼 stride ${oldStride}이지만 ${stride}를 받았습니다. 오버라이드하고 계속합니다.`);
                    }
                    modifiedResourceSection.push(`stride = ${newStride}`);
                    modifiedResourceSection.push(';' + line);
                }
            }

            const modifiedContent = modifiedResourceSection.join('\n');
            const [i, j] = [resourceSectionMatch.index!, resourceSectionMatch.index! + resourceSectionMatch[1].length];
            ini.content = ini.content.substring(0, i) + modifiedContent + ini.content.substring(j);
        }

        for (const bufferFilename of bufferFilenames) {
            const bufferFilepath = path.join(path.dirname(ini.filepath), bufferFilename);
            const bufferDictKey = path.resolve(bufferFilepath);

            if (!(bufferDictKey in globalModifiedBuffers)) {
                globalModifiedBuffers[bufferDictKey] = [];
            }
            const fixId = `${this.id}-texcoord_remap`;
            if (globalModifiedBuffers[bufferDictKey].includes(fixId)) continue;
            else globalModifiedBuffers[bufferDictKey].push(fixId);

            let buffer: Buffer;
            if (bufferDictKey in ini.modifiedBuffers) {
                buffer = ini.modifiedBuffers[bufferDictKey];
            } else {
                buffer = fs.readFileSync(bufferFilepath);
            }

            const vcount = Math.floor(buffer.length / oldStride);
            const newBuffer = Buffer.alloc(vcount * newStride);

            for (let i = 0; i < vcount; i++) {
                for (let j = 0; j < this.oldFormat.length; j++) {
                    const oldChunk = this.oldFormat[j];
                    const newChunk = this.newFormat[j];

                    if (offsets[j] < oldStride && offsets[j + 1] <= oldStride) {
                        if (oldChunk !== newChunk) {
                            if (j === 0 && oldChunk === '4B' && newChunk === '4f') {
                                // 4B -> 4f (byte to float)
                                for (let k = 0; k < 4; k++) {
                                    const byteValue = buffer.readUInt8(i * oldStride + offsets[j] + k);
                                    const floatValue = byteValue / 255.0;
                                    newBuffer.writeFloatLE(floatValue, i * newStride + offsets[j] + k * 4);
                                }
                            } else if (j === 0 && oldChunk === '4f' && newChunk === '4B') {
                                // 4f -> 4B (float to byte)
                                for (let k = 0; k < 4; k++) {
                                    const floatValue = buffer.readFloatLE(i * oldStride + offsets[j] + k * 4);
                                    const byteValue = Math.round(floatValue * 255);
                                    newBuffer.writeUInt8(byteValue, i * newStride + offsets[j] + k);
                                }
                            } else {
                                const sourceStart = i * oldStride + offsets[j];
                                const sourceEnd = i * oldStride + offsets[j + 1];
                                const targetStart = i * newStride + offsets[j];
                                buffer.copy(newBuffer, targetStart, sourceStart, sourceEnd);
                            }
                        } else {
                            const sourceStart = i * oldStride + offsets[j];
                            const sourceEnd = i * oldStride + offsets[j + 1];
                            const targetStart = i * newStride + offsets[j];
                            buffer.copy(newBuffer, targetStart, sourceStart, sourceEnd);
                        }
                    } else {
                        const formatSize = this.getFormatSize(newChunk);
                        const targetStart = i * newStride + offsets[j];
                        newBuffer.fill(0, targetStart, targetStart + formatSize);
                    }
                }
            }

            ini.modifiedBuffers[bufferDictKey] = newBuffer;
        }

        return { touched: true };
    }

    private calculateStride(format: string[]): number {
        return format.reduce((total, chunk) => total + this.getFormatSize(chunk), 0);
    }

    private getFormatSize(formatChunk: string): number {
        const formatMap: Record<string, number> = {
            'B': 1,   // unsigned char
            'b': 1,   // signed char
            'H': 2,   // unsigned short
            'h': 2,   // signed short
            'I': 4,   // unsigned int
            'i': 4,   // signed int
            'f': 4,   // float
            'd': 8,   // double
            'e': 2,   // half float (16-bit)
        };

        if (formatChunk.length === 2) {
            const count = parseInt(formatChunk[0]);
            const type = formatChunk[1];
            return count * (formatMap[type] || 0);
        } else if (formatChunk.length === 1) {
            return formatMap[formatChunk] || 0;
        }

        return 0;
    }
}

class UpdateHash implements CommandExecutor {
    constructor(private newHash: string) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash: activeHash } = defaultArgs;

        const pattern = new RegExp(`(\\r?\\n\\s*)(hash\\s*=\\s*${activeHash})`, 'gi');
        const replacement = `$1hash = ${this.newHash}\r?\n; $2`;
        const newContent = ini.content.replace(pattern, replacement);
        const subCount = (ini.content.match(pattern) || []).length;

        ini.content = newContent;
        defaultArgs.hash = this.newHash;

        return {
            touched: true,
            queueHashes: [this.newHash],
            queueCommands: [
                [Log, [`+ ${subCount}개 해시를 ${this.newHash}로 업데이트`]]
            ]
        };
    }
}

class AddIbCheckIfMissing implements CommandExecutor {
    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash } = defaultArgs;

        const pattern = getSectionHashPattern(hash);
        const matches = Array.from(ini.content.matchAll(pattern));

        let needsCheck = false;
        let newSections = '';
        let unindexedSection = '';

        for (const match of matches) {
            const section = match[1];

            if (!/\r?\n\s*match_first_index\s*=/i.test(section)) {
                unindexedSection = match[0];
                continue;
            }

            if (/\r?\n\s*run\s*=\s*CommandListSkinTexture/i.test(section)) {
                newSections += match[0];
                continue;
            }

            needsCheck = true;
            newSections += match[0].replace(
                /\r?\n\s*match_first_index\s*=.*?\r?\n/i,
                '$&run = CommandListSkinTexture\r?\n'
            );
        }

        if (unindexedSection && !newSections) {
            if (!/\r?\n\s*run\s*=\s*CommandListSkinTexture/i.test(unindexedSection)) {
                needsCheck = true;
                unindexedSection = unindexedSection.replace(
                    /\r?\n\s*hash\s*=.*?\r?\n/i,
                    '$&run = CommandListSkinTexture\r?\n'
                );
            }
        }

        newSections = unindexedSection + newSections;

        const commands: Command[] = needsCheck ? [
            [Log, ['+ `run = CommandListSkinTexture` 추가']],
            [RemoveIndexedSections, { capturePosition: '🌲' }],
            [CreateNewSection, { savedPosition: '🌲', sectionContent: newSections }]
        ] : [
            [Log, ['/ `run = CommandListSkinTexture` 추가 건너뛰기']]
        ];

        return { queueCommands: commands };
    }
}

class RemoveIndexedSections implements CommandExecutor {
    constructor(private options: {
        captureContent?: string;
        captureIndexedContent?: string;
        capturePosition?: string;
    }) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash, data } = defaultArgs;

        const pattern = getSectionHashPattern(hash);
        let newIniContent = '';
        let position = -1;
        let prevEnd = 0;

        const matches = Array.from(ini.content.matchAll(pattern));

        for (const match of matches) {
            const section = match[1];

            if (/\r?\n\s*match_first_index\s*=/i.test(section)) {
                if (this.options.captureIndexedContent) {
                    const { criticalContent, matchFirstIndex } = getCriticalContent(section);
                    const placeholder = `${this.options.captureIndexedContent}${matchFirstIndex}${this.options.captureIndexedContent}`;
                    data[placeholder] = criticalContent;
                }
            } else {
                if (this.options.captureContent) {
                    const { criticalContent } = getCriticalContent(section);
                    data[this.options.captureContent] = criticalContent;
                }
            }

            const [start, end] = [match.index!, match.index! + match[0].length];
            if (position === -1) {
                position = start;
            }

            newIniContent += ini.content.substring(prevEnd, start);
            prevEnd = end;
        }

        newIniContent += ini.content.substring(prevEnd);
        ini.content = newIniContent;

        if (this.options.capturePosition) {
            data[this.options.capturePosition] = position.toString();
        }

        return { touched: true };
    }
}

class CreateNewSection implements CommandExecutor {
    constructor(private options: {
        sectionContent: string;
        savedPosition?: string;
        capturePosition?: string;
    }) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, data } = defaultArgs;

        let pos = -1;
        if (this.options.savedPosition && this.options.savedPosition in data) {
            pos = parseInt(data[this.options.savedPosition]);
        }

        let sectionContent = this.options.sectionContent;

        for (const [placeholder, value] of Object.entries(data)) {
            if (placeholder.startsWith('_')) {
                continue;
            }
            sectionContent = sectionContent.replace(new RegExp(placeholder, 'g'), value);
        }

        const failureEmojis = ['🍰', '🌲', '🤍'];
        for (const emoji of failureEmojis) {
            if (sectionContent.includes(emoji)) {
                console.log('섹션 대체 실패');
                console.log(sectionContent);
                return { failed: true };
            }
        }

        if (this.options.capturePosition) {
            data[this.options.capturePosition] = (sectionContent.length + pos).toString();
        }

        ini.content = ini.content.substring(0, pos) + sectionContent + ini.content.substring(pos);

        return { touched: true };
    }
}

class CaptureSection implements CommandExecutor {
    constructor(private options: {
        captureContent?: string;
        capturePosition?: string;
    }) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash: activeHash, data } = defaultArgs;

        const pattern = getSectionHashPattern(activeHash);
        const match = pattern.exec(ini.content);
        if (!match) throw new Error('Bad regex');

        const end = match.index! + match[1].length;

        if (this.options.captureContent) {
            const { criticalContent } = getCriticalContent(match[1]);
            data[this.options.captureContent] = criticalContent;
        }
        if (this.options.capturePosition) {
            data[this.options.capturePosition] = (end + 1).toString();
        }

        return { touched: false };
    }
}

class MultiplySectionIfMissing implements CommandExecutor {
    constructor(private equivHashes: string | string[], private extraTitle: string) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini } = defaultArgs;

        const hashes = Array.isArray(this.equivHashes) ? this.equivHashes : [this.equivHashes];

        for (const equivHash of hashes) {
            if (ini.hasHash(equivHash)) {
                return {
                    queueCommands: [
                        [Log, ['/ 섹션 복제 건너뛰기', equivHash, `[...${this.extraTitle}]`]]
                    ]
                };
            }
        }

        const equivHash = hashes[0];
        const content = [
            '',
            `[TextureOverride${this.extraTitle}]`,
            `hash = ${equivHash}`,
            '🍰',
            ''
        ].join('\n');

        return {
            queueHashes: [equivHash],
            queueCommands: [
                [Log, ['+ 섹션 복제', equivHash, `[...${this.extraTitle}]`]],
                [CaptureSection, { captureContent: '🍰', capturePosition: '🌲' }],
                [CreateNewSection, { savedPosition: '🌲', sectionContent: content }]
            ]
        };
    }
}

class AddSectionIfMissing implements CommandExecutor {
    constructor(
        private equivHashes: string | string[],
        private sectionTitle: string,
        private sectionContent: string = ''
    ) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini } = defaultArgs;

        const hashes = Array.isArray(this.equivHashes) ? this.equivHashes : [this.equivHashes];

        for (const equivHash of hashes) {
            if (ini.hasHash(equivHash)) {
                return {
                    queueCommands: [
                        [Log, ['/ 섹션 추가 건너뛰기', equivHash, `[...${this.sectionTitle}]`]]
                    ]
                };
            }
        }

        const equivHash = hashes[0];
        const section = `\n[TextureOverride${this.sectionTitle}]\nhash = ${equivHash}\n${this.sectionContent}`;

        return {
            queueHashes: [equivHash],
            queueCommands: [
                [Log, ['+ 섹션 추가', equivHash, `[...${this.sectionTitle}]`]],
                [CaptureSection, { capturePosition: '🌲' }],
                [CreateNewSection, { savedPosition: '🌲', sectionContent: section }]
            ]
        };
    }
}

class UpdateBufferBlendIndices implements CommandExecutor {
    constructor(
        private hash: string,
        private oldIndices: number[],
        private newIndices: number[]
    ) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini } = defaultArgs;

        const pattern = getSectionHashPattern(this.hash);
        const sectionMatch = pattern.exec(ini.content);
        if (!sectionMatch) return { touched: false };

        const resources = processCommandlist(ini.content, sectionMatch[1], 'vb2');
        const bufferFilenames = new Set<string>();
        const linePattern = /^\s*(filename|stride)\s*=\s*(.*)\s*$/i;

        for (const resource of resources) {
            const resourcePattern = getSectionTitlePattern(resource);
            const resourceMatch = resourcePattern.exec(ini.content);
            if (!resourceMatch) continue;

            for (const line of resourceMatch[1].split('\n')) {
                const lineMatch = line.match(linePattern);
                if (lineMatch && lineMatch[1].toLowerCase() === 'filename') {
                    bufferFilenames.add(lineMatch[2]);
                }
            }
        }

        for (const bufferFilename of bufferFilenames) {
            const bufferFilepath = path.join(path.dirname(ini.filepath), bufferFilename);
            const bufferDictKey = path.resolve(bufferFilepath);

            let buffer: Buffer;
            if (bufferDictKey in ini.modifiedBuffers) {
                buffer = ini.modifiedBuffers[bufferDictKey];
            } else {
                buffer = fs.readFileSync(bufferFilepath);
            }

            const newBuffer = Buffer.alloc(buffer.length);
            const blendStride = 32;
            const vertexCount = Math.floor(buffer.length / blendStride);

            for (let i = 0; i < vertexCount; i++) {
                const offset = i * blendStride;

                buffer.copy(newBuffer, offset, offset, offset + 16);

                for (let j = 0; j < 4; j++) {
                    const indexOffset = offset + 16 + (j * 4);
                    const oldIndex = buffer.readUInt32LE(indexOffset);

                    const oldIndexPos = this.oldIndices.indexOf(oldIndex);
                    const newIndex = oldIndexPos !== -1 ? this.newIndices[oldIndexPos] : oldIndex;

                    newBuffer.writeUInt32LE(newIndex, indexOffset);
                }
            }

            ini.modifiedBuffers[bufferDictKey] = newBuffer;
        }

        return { touched: true };
    }
}

class Zzz12ShrinkTexcoordColor implements CommandExecutor {
    constructor(private id: string) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash, tabs } = defaultArgs;

        const pattern = getSectionHashPattern(hash);
        const sectionMatch = pattern.exec(ini.content);
        if (!sectionMatch) return { touched: false };

        const resources = processCommandlist(ini.content, sectionMatch[1], 'vb1');
        const bufferFilenames = new Set<string>();
        const linePattern = /^\s*(filename|stride)\s*=\s*(.*)\s*$/i;

        for (const resource of resources) {
            const resourcePattern = getSectionTitlePattern(resource);
            const resourceMatch = resourcePattern.exec(ini.content);
            if (!resourceMatch) continue;

            const modifiedResourceSection: string[] = [];
            let stride = 0;

            for (const line of resourceMatch[1].split('\n')) {
                const lineMatch = line.match(linePattern);
                if (!lineMatch) {
                    modifiedResourceSection.push(line);
                } else if (lineMatch[1].toLowerCase() === 'filename') {
                    modifiedResourceSection.push(line);
                    bufferFilenames.add(lineMatch[2]);
                } else if (lineMatch[1].toLowerCase() === 'stride') {
                    stride = parseInt(lineMatch[2]);
                    modifiedResourceSection.push(`stride = ${stride - 12}`);
                    modifiedResourceSection.push(';' + line);
                }
            }

            const modifiedContent = modifiedResourceSection.join('\n');
            const [i, j] = [resourceMatch.index!, resourceMatch.index! + resourceMatch[1].length];
            ini.content = ini.content.substring(0, i) + modifiedContent + ini.content.substring(j);

            console.log(`${'\t'.repeat(tabs)}버퍼 처리: stride ${stride} -> ${stride - 12}`);
        }

        return { touched: true };
    }
}

class TransferIndexedSections implements CommandExecutor {
    constructor(private options: {
        trgIndices: string[];
        srcIndices: string[];
    }) { }

    execute(defaultArgs: DefaultArgs): ExecutionResult {
        const { ini, hash } = defaultArgs;

        let title: string | null = null;
        const pattern = getSectionHashPattern(hash);
        const ibMatches = Array.from(ini.content.matchAll(pattern));
        let indexedIbCount = 0;

        for (const match of ibMatches) {
            if (/\r?\n\s*match_first_index\s*=/i.test(match[1])) {
                indexedIbCount++;
                if (!title) {
                    const titleMatch = match[1].match(/^\[TextureOverride(.*?)\]/i);
                    title = titleMatch ? titleMatch[1].slice(0, -1) : '';
                }
            } else {
                if (!title) {
                    const titleMatch = match[1].match(/^\[TextureOverride(.*?)\]/i);
                    title = titleMatch ? titleMatch[1].slice(0, -2) : '';
                }
            }
        }

        if (indexedIbCount === 0) {
            return {};
        }

        const unindexedIbContent = [
            `[TextureOverride${title}IB]`,
            `hash = ${hash}`,
            '🍰',
            '',
            ''
        ].join('\r?\n');

        const alpha = [
            'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J',
            'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T',
            'U', 'V', 'W', 'X', 'Y', 'Z'
        ];

        let content = '';
        for (let i = 0; i < this.options.trgIndices.length; i++) {
            const trgIndex = this.options.trgIndices[i];
            const srcIndex = this.options.srcIndices[i];

            content += [
                `[TextureOverride${title}${alpha[i]}]`,
                `hash = ${hash}`,
                `match_first_index = ${trgIndex}`,
                srcIndex !== '-1' ? `🤍${srcIndex}🤍` : 'ib = null',
                '',
                ''
            ].join('\r?\n');
        }

        const commands: Command[] = indexedIbCount < ibMatches.length ? [
            [RemoveIndexedSections, { captureContent: '🍰', captureIndexedContent: '🤍', capturePosition: '🌲' }],
            [CreateNewSection, { savedPosition: '🌲', sectionContent: content }],
            [CreateNewSection, { savedPosition: '🌲', sectionContent: unindexedIbContent }]
        ] : [
            [RemoveIndexedSections, { captureIndexedContent: '🤍', capturePosition: '🌲' }],
            [CreateNewSection, { savedPosition: '🌲', sectionContent: content }]
        ];

        return {
            touched: false,
            failed: false,
            signalBreak: false,
            queueHashes: undefined,
            queueCommands: commands
        };
    }
}

const hashCommands: Record<string, Command[]> = {
    // MARK: Anby
    '5c0240db': [[Log, ['1.0: Anby Hair IB Hash']], [AddIbCheckIfMissing]],
    '4816de84': [[Log, ['1.0: Anby Body IB Hash']], [AddIbCheckIfMissing]],
    '19df8e84': [[Log, ['1.0: Anby Head IB Hash']], [AddIbCheckIfMissing]],
    '39538886': [
        [Log, ['1.1 -> 1.2: Anby Hair Texcoord Hash']],
        [UpdateHash, '496a781d'],
        [Log, ['+ 텍스쳐 좌표 버퍼 리매핑']],
        [Zzz12ShrinkTexcoordColor, '1.2']
    ],
    'cc114f4f': [[Log, ['1.5 -> 1.6: Anby HeadA Diffuse 1024p Hash']], [UpdateHash, '692c6d2b']],
    // '692c6d2b': [
    //     [Log, ['1.6: Anby HeadA Diffuse 1024p Hash']],
    //     [MultiplySectionIfMissing, [['05d7b504', '2a29cb9b'], 'Anby.HeadA.Diffuse.2048']]
    // ],
    '2a29cb9b': [[Log, ['1.5 -> 1.6: Anby HeadA Diffuse 2048p Hash']], [UpdateHash, '05d7b504']],
    // '05d7b504': [
    //     [Log, ['1.6: Anby HeadA Diffuse 2048p Hash']],
    //     [MultiplySectionIfMissing, [['692c6d2b', 'cc114f4f'], 'Anby.HeadA.Diffuse.1024']]
    // ],
    '6ea0023c': [
        [Log, ['1.0: Anby HairA Diffuse 2048p Hash']],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7c7f96d2', 'Anby.HairA.Diffuse.1024']]
    ],
    '7c7f96d2': [
        [Log, ['1.0: Anby HairA Diffuse 1024p Hash']],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6ea0023c', 'Anby.HairA.Diffuse.2048']]
    ],
    'b54f2a3d': [
        [Log, ['1.0: Anby HairA LightMap 2048p Hash']],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9ceea795', 'Anby.HairA.LightMap.1024']]
    ],
    '9ceea795': [
        [Log, ['1.0: Anby HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b54f2a3d', 'Anby.HairA.LightMap.2048']],
    ],
    '20890a00': [
        [Log, ['1.0: Anby HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3101f0da', 'Anby.HairA.NormalMap.1024']],
    ],
    '3101f0da': [
        [Log, ['1.0: Anby HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['5c0240db', 'Anby.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['20890a00', 'Anby.HairA.NormalMap.2048']],
    ],

    'b37c3b4e': [[Log, ['1.5 -> 1.6: Anby BodyA Diffuse 2048p Hash',]], [UpdateHash, ['215ff74d',]]],
    '215ff74d': [
        [Log, ['1.6: Anby BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['8df45cb8', '8bd7966f'], 'Anby.BodyA.Diffuse.1024']],
    ],
    '8bd7966f': [[Log, ['1.5 -> 1.6: Anby BodyA Diffuse 1024p Hash',]], [UpdateHash, ['8df45cb8',]]],
    '8df45cb8': [
        [Log, ['1.6: Anby BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['215ff74d', 'b37c3b4e'], 'Anby.BodyA.Diffuse.2048']],
    ],
    '7c24acc9': [
        [Log, ['1.0: Anby BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9cddbf1e', 'Anby.BodyA.LightMap.1024']],
    ],
    '9cddbf1e': [
        [Log, ['1.0: Anby BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7c24acc9', 'Anby.BodyA.LightMap.2048']],
    ],
    'ccca3b8e': [
        [Log, ['1.0: Anby BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1115f163', 'Anby.BodyA.MaterialMap.1024']],
    ],
    '1115f163': [
        [Log, ['1.0: Anby BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ccca3b8e', 'Anby.BodyA.MaterialMap.2048']],
    ],
    '19226ead': [
        [Log, ['1.0: Anby BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6346d69d', 'Anby.BodyA.NormalMap.1024']],
    ],
    '6346d69d': [
        [Log, ['1.0: Anby BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['4816de84', 'Anby.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['19226ead', 'Anby.BodyA.NormalMap.2048']],
    ],

    // MARK: Anton
    '6b95c80d': [[Log, ['1.0: Anton Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '653fb27c': [[Log, ['1.0: Anton Body IB Hash',]], [AddIbCheckIfMissing,]],
    'a21fcee4': [[Log, ['1.0: Anton Jacket IB Hash',]], [AddIbCheckIfMissing,]],
    'a0201907': [[Log, ['1.0: Anton Head IB Hash',]], [AddIbCheckIfMissing,]],

    '15cb1aee': [
        [Log, ['1.0: Anton HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['a0201907', 'Anton.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['842119d6', 'Anton.HeadA.Diffuse.2048']],
    ],
    '654134c1': [
        [Log, ['1.0: Anton HeadA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['a0201907', 'Anton.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ac7fb2e2', 'Anton.HeadA.LightMap.2048']],
    ],
    '842119d6': [
        [Log, ['1.0: Anton HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['a0201907', 'Anton.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['15cb1aee', 'Anton.HeadA.Diffuse.1024']],
    ],
    'ac7fb2e2': [
        [Log, ['1.0: Anton HeadA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['a0201907', 'Anton.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['654134c1', 'Anton.HeadA.LightMap.1024']],
    ],

    '571aa398': [
        [Log, ['1.0: Anton HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d4c4c604', 'Anton.HairA.Diffuse.1024']],
    ],
    'd4c4c604': [
        [Log, ['1.0: Anton HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['571aa398', 'Anton.HairA.Diffuse.2048']],
    ],
    'ee06579e': [
        [Log, ['1.0: Anton HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['21ee9a3f', 'Anton.HairA.LightMap.1024']],
    ],
    '21ee9a3f': [
        [Log, ['1.0: Anton HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ee06579e', 'Anton.HairA.LightMap.2048']],
    ],
    '24caeb1f': [
        [Log, ['1.0: Anton HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6fc654e1', 'Anton.HairA.MaterialMap.1024']],
    ],
    '6fc654e1': [
        [Log, ['1.0: Anton HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['24caeb1f', 'Anton.HairA.MaterialMap.2048']],
    ],
    'b216f758': [
        [Log, ['1.0: Anton HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['77ae203f', 'Anton.HairA.NormalMap.1024']],
    ],
    '77ae203f': [
        [Log, ['1.0: Anton HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['6b95c80d', 'Anton.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b216f758', 'Anton.HairA.NormalMap.2048']],
    ],

    '00abcf22': [
        [Log, ['1.0: Anton BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['581a0958', 'Anton.BodyA.Diffuse.1024']],
    ],
    '581a0958': [
        [Log, ['1.0: Anton BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['00abcf22', 'Anton.BodyA.Diffuse.2048']],
    ],
    '17cf1b74': [
        [Log, ['1.0: Anton BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8e5ba7d0', 'Anton.BodyA.LightMap.1024']],
    ],
    '8e5ba7d0': [
        [Log, ['1.0: Anton BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['17cf1b74', 'Anton.BodyA.LightMap.2048']],
    ],
    '0238b0ff': [
        [Log, ['1.0: Anton BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b7ce5f0b', 'Anton.BodyA.MaterialMap.1024']],
    ],
    'b7ce5f0b': [
        [Log, ['1.0: Anton BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0238b0ff', 'Anton.BodyA.MaterialMap.2048']],
    ],
    '1b4ad5b7': [
        [Log, ['1.0: Anton BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5b2ab0e0', 'Anton.BodyA.NormalMap.1024']],
    ],
    '5b2ab0e0': [
        [Log, ['1.0: Anton BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['653fb27c', 'Anton.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1b4ad5b7', 'Anton.BodyA.NormalMap.2048']],
    ],

    'd4b15508': [
        [Log, ['1.0: Anton JacketA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f7831517', 'Anton.JacketA.Diffuse.1024']],
    ],
    'f7831517': [
        [Log, ['1.0: Anton JacketA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d4b15508', 'Anton.JacketA.Diffuse.2048']],
    ],
    '886a664a': [
        [Log, ['1.0: Anton JacketA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c42628a5', 'Anton.JacketA.LightMap.1024']],
    ],
    'c42628a5': [
        [Log, ['1.0: Anton JacketA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['886a664a', 'Anton.JacketA.LightMap.2048']],
    ],
    'd36a2f7a': [
        [Log, ['1.0: Anton JacketA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['75bccc40', 'Anton.JacketA.MaterialMap.1024']],
    ],
    '75bccc40': [
        [Log, ['1.0: Anton JacketA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d36a2f7a', 'Anton.JacketA.MaterialMap.2048']],
    ],
    'd7517d0e': [
        [Log, ['1.0: Anton JacketA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ae3d5fb8', 'Anton.JacketA.NormalMap.1024']],
    ],
    'ae3d5fb8': [
        [Log, ['1.0: Anton JacketA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['a21fcee4', 'Anton.Jacket.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d7517d0e', 'Anton.JacketA.NormalMap.2048']],
    ],

    // MARK: AstraYao
    '53cdac6c': [[Log, ['1.5: AstraYao Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '7a110804': [[Log, ['1.5: AstraYao Body IB Hash',]], [AddIbCheckIfMissing,]],
    '92f33156': [[Log, ['1.5: AstraYao Legs IB Hash',]], [AddIbCheckIfMissing,]],
    '51831437': [[Log, ['1.5: AstraYao Face IB Hash',]], [AddIbCheckIfMissing,]],

    '3cd13d03': [[Log, ['1.5 -> 1.6: AstraYao Body Blend Hash',]], [UpdateHash, ['9d35c352',]],],
    'f8b92870': [[Log, ['1.5 -> 1.6: AstraYao Hair Texcoord Hash',]], [UpdateHash, ['8ba0b335',]],],
    'da86a32e': [[Log, ['1.5 -> 1.6: AstraYao Legs Texcoord Hash',]], [UpdateHash, ['1433ee78',]],],


    '77670042': [[Log, ['1.5 -> 1.6: AstraYao Face Diffuse 1024p Hash',]], [UpdateHash, ['3283b8be',]]],
    '3283b8be': [
        [Log, ['1.6: AstraYao FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['51831437', 'AstraYao.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['c41341b2', '3a8d0dfc'], 'AstraYao.FaceA.Diffuse.2048']],
    ],
    '3a8d0dfc': [[Log, ['1.5 -> 1.6: AstraYao Face Diffuse 2048p Hash',]], [UpdateHash, ['c41341b2',]]],
    'c41341b2': [
        [Log, ['1.6: AstraYao FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['51831437', 'AstraYao.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['3283b8be', '77670042'], 'AstraYao.FaceA.Diffuse.1024']],
    ],

    'da673df0': [[Log, ['1.5A -> 1.5B: AstraYao HairA, LegsA Diffuse 2048p Hash',]], [UpdateHash, ['2daa2443',]]],
    '2daa2443': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA Diffuse 2048p Hash',]], [UpdateHash, ['e634238a',]]],
    'e634238a': [
        [Log, ['1.6: AstraYao HairA, LegsA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, [['56c71ea2', '4b1c1b47', '7a507e4a'], 'AstraYao.HairA.Diffuse.1024']],
    ],
    '34aad3b4': [[Log, ['1.5A -> 1.5B: AstraYao HairA, LegsA LightMap 2048p Hash',]], [UpdateHash, ['b085765e',]]],
    'b085765e': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA LightMap 2048p Hash',]], [UpdateHash, ['34f0706c',]]],
    '34f0706c': [
        [Log, ['1.6: AstraYao HairA, LegsA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, [['fd3ca2a6', 'c47a524a', 'e4a4f975'], 'AstraYao.HairA.LightMap.1024']],
    ],
    'b53b2e12': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA MaterialMap 2048p Hash',]], [UpdateHash, ['883a578f',]]],
    '883a578f': [
        [Log, ['1.6: AstraYao HairA, LegsA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, [['759c15e0', '0be99d44'], 'AstraYao.HairA.MaterialMap.1024']],
    ],

    '7a507e4a': [[Log, ['1.5A -> 1.5B: AstraYao HairA, LegsA Diffuse 1024p Hash',]], [UpdateHash, ['4b1c1b47',]]],
    '4b1c1b47': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA Diffuse 1024p Hash',]], [UpdateHash, ['56c71ea2',]]],
    '56c71ea2': [
        [Log, ['1.6: AstraYao HairA, LegsA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, [['e634238a', '2daa2443', 'da673df0'], 'AstraYao.HairA.Diffuse.2048']],
    ],
    'e4a4f975': [[Log, ['1.5A -> 1.5B: AstraYao HairA, LegsA LightMap 1024p Hash',]], [UpdateHash, ['c47a524a',]]],
    'c47a524a': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA LightMap 1024p Hash',]], [UpdateHash, ['fd3ca2a6',]]],
    'fd3ca2a6': [
        [Log, ['1.6: AstraYao HairA, LegsA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, [['34f0706c', 'b085765e', '34aad3b4'], 'AstraYao.HairA.LightMap.2048']],
    ],
    '0be99d44': [[Log, ['1.5 -> 1.6: AstraYao HairA, LegsA MaterialMap 1024p Hash',]], [UpdateHash, ['759c15e0',]]],
    '759c15e0': [
        [Log, ['1.6: AstraYao HairA, LegsA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, [['883a578f', 'b53b2e12'], 'AstraYao.HairA.MaterialMap.2048']],
    ],

    'd7f1c157': [
        [Log, ['1.5: AstraYao BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e523eb0f', 'AstraYao.BodyA.Diffuse.1024']],
    ],
    'dba7d767': [
        [Log, ['1.5: AstraYao BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3f9f0d8a', 'AstraYao.BodyA.LightMap.1024']],
    ],
    '21d5f5e3': [
        [Log, ['1.5: AstraYao BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c4248e2d', 'AstraYao.BodyA.MaterialMap.1024']],
    ],
    'e523eb0f': [
        [Log, ['1.5: AstraYao BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d7f1c157', 'AstraYao.BodyA.Diffuse.2048']],
    ],
    '3f9f0d8a': [
        [Log, ['1.5: AstraYao BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dba7d767', 'AstraYao.BodyA.LightMap.2048']],
    ],
    'c4248e2d': [
        [Log, ['1.5: AstraYao BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['7a110804', 'AstraYao.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['21d5f5e3', 'AstraYao.BodyA.MaterialMap.2048']],
    ],

    // MARK: AstraSkin
    '02d8a2cb': [[Log, ['1.5: AstraSkin Body IB Hash',]], [AddIbCheckIfMissing,]],

    '7301ca3a': [
        [Log, ['1.5: AstraSkin BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8212713f', 'AstraSkin.BodyA.Diffuse.1024']],
    ],
    '7ce9f1db': [
        [Log, ['1.5: AstraSkin BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['83ede428', 'AstraSkin.BodyA.LightMap.1024']],
    ],
    '56abc3a3': [[Log, ['1.5 -> 1.6: AstraSkin BodyA MaterialMap 2048p Hash',]], [UpdateHash, ['43a4d256',]]],
    '43a4d256': [
        [Log, ['1.6: AstraSkin BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['6da1b76a', '6989dc5a'], 'AstraSkin.BodyA.MaterialMap.1024']],
    ],
    '8212713f': [
        [Log, ['1.5: AstraSkin BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7301ca3a', 'AstraSkin.BodyA.Diffuse.2048']],
    ],
    '83ede428': [
        [Log, ['1.5: AstraSkin BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7ce9f1db', 'AstraSkin.BodyA.LightMap.2048']],
    ],
    '6989dc5a': [[Log, ['1.5 -> 1.6: AstraSkin BodyA MaterialMap 1024p Hash',]], [UpdateHash, ['6da1b76a',]]],
    '6da1b76a': [
        [Log, ['1.6: AstraSkin BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['02d8a2cb', 'AstraSkin.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['43a4d256', '56abc3a3'], 'AstraSkin.BodyA.MaterialMap.2048']],
    ],

    // MARK: Belle
    'bea4a483': [[Log, ['1.0: Belle Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '1817f3ca': [[Log, ['1.0: Belle Body IB Hash',]], [AddIbCheckIfMissing,]],
    '9a9780a7': [[Log, ['1.0: Belle Head IB Hash',]], [AddIbCheckIfMissing,]],

    'caf95576': [
        [Log, ['1.0 -> 1.1: Belle Body Texcoord Hash',]],
        [UpdateHash, ['801edbf4',]],
        [Log, ['1.0 -> 1.1: Belle Body Blend Remap',]],
        [UpdateBufferBlendIndices, [
            'd2844c01',
            [3, 4, 5, 6, 7, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 58, 59, 60, 61, 62, 63, 64, 65, 66, 95, 96, 97, 98, 99, 100, 101, 102, 103, 104, 126, 127],
            [6, 7, 3, 5, 4, 18, 9, 10, 11, 12, 13, 14, 15, 16, 17, 21, 25, 24, 20, 22, 23, 38, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 47, 48, 53, 56, 45, 46, 49, 50, 51, 52, 54, 55, 60, 61, 66, 58, 59, 62, 63, 64, 65, 104, 95, 96, 97, 98, 99, 100, 101, 102, 103, 127, 126],
        ]]
    ],

    '77eef7e8': [
        [Log, ['1.0: Belle HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['9a9780a7', 'Belle.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['75ec3614', 'Belle.HeadA.Diffuse.2048']],
    ],
    '75ec3614': [
        [Log, ['1.0: Belle HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['9a9780a7', 'Belle.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['77eef7e8', 'Belle.HeadA.Diffuse.1024']],
    ],

    '1ce58567': [
        [Log, ['1.0: Belle HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['08f04d95', 'Belle.HairA.Diffuse.1024']],
    ],
    '08f04d95': [
        [Log, ['1.0: Belle HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1ce58567', 'Belle.HairA.Diffuse.2048']],
    ],
    'f1ee2105': [
        [Log, ['1.0: Belle HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2e656f2f', 'Belle.HairA.LightMap.1024']],
    ],
    '2e656f2f': [
        [Log, ['1.0: Belle HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f1ee2105', 'Belle.HairA.LightMap.2048']],
    ],
    '24c47ca5': [[Log, ['1.4 -> 1.5: Belle HairA MaterialMap 2048p Hash',]], [UpdateHash, ['34bdb036',]]],
    '34bdb036': [
        [Log, ['1.5: Belle HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['7542ef4b', '4b6ef993'], 'Belle.HairA.MaterialMap.1024']],
    ],
    '4b6ef993': [[Log, ['1.4 -> 1.5: Belle HairA MaterialMap 1024p Hash',]], [UpdateHash, ['7542ef4b',]]],
    '7542ef4b': [
        [Log, ['1.5: Belle HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['34bdb036', '24c47ca5'], 'Belle.HairA.MaterialMap.2048']],
    ],
    '89b147ff': [
        [Log, ['1.0: Belle HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6b55c039', 'Belle.HairA.NormalMap.1024']],
    ],
    '6b55c039': [
        [Log, ['1.0: Belle HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['bea4a483', 'Belle.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['89b147ff', 'Belle.HairA.NormalMap.2048']],
    ],

    'd2960560': [[Log, ['1.4 -> 1.5: Belle BodyA Diffuse 2048p Hash',]], [UpdateHash, ['24639b77',]]],
    '24639b77': [
        [Log, ['1.5: Belle BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['b9c7f71b', '4454fb58'], 'Belle.BodyA.Diffuse.1024']],
    ],
    '4454fb58': [[Log, ['1.4 -> 1.5: Belle BodyA Diffuse 1024p Hash',]], [UpdateHash, ['b9c7f71b',]]],
    'b9c7f71b': [
        [Log, ['1.5: Belle BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['24639b77', 'd2960560'], 'Belle.BodyA.Diffuse.2048']],
    ],
    'bf286c84': [[Log, ['1.4 -> 1.5: Belle BodyA LightMap 2048p Hash',]], [UpdateHash, ['7947679c',]]],
    '7947679c': [
        [Log, ['1.5: Belle BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['a4d3687d', '2ed82c57'], 'Belle.BodyA.LightMap.1024']],
    ],
    '2ed82c57': [[Log, ['1.4 -> 1.5: Belle BodyA LightMap 1024p Hash',]], [UpdateHash, ['a4d3687d',]]],
    'a4d3687d': [
        [Log, ['1.5: Belle BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['7947679c', 'bf286c84'], 'Belle.BodyA.LightMap.2048']],
    ],
    '33f28c6d': [
        [Log, ['1.0: Belle BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b1abe877', 'Belle.BodyA.MaterialMap.1024']],
    ],
    'b1abe877': [
        [Log, ['1.0: Belle BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['33f28c6d', 'Belle.BodyA.MaterialMap.2048']],
    ],
    'f04f7ab9': [
        [Log, ['1.0: Belle BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c0bd8516', 'Belle.BodyA.NormalMap.1024']],
    ],
    'c0bd8516': [
        [Log, ['1.0: Belle BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['1817f3ca', 'Belle.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f04f7ab9', 'Belle.BodyA.NormalMap.2048']],
    ],

    // MARK: Ben
    '9c4f1a9a': [[Log, ['1.0: Ben Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '94288cca': [[Log, ['1.0: Ben Body IB Hash',]], [AddIbCheckIfMissing,]],

    '00002f2c': [
        [Log, ['1.0: Ben HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8d83daba', 'Ben.HairA.Diffuse.1024']],
    ],
    '8d83daba': [
        [Log, ['1.0: Ben HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['00002f2c', 'Ben.HairA.Diffuse.2048']],
    ],
    'cc195dc5': [
        [Log, ['1.0: Ben HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1439d2b9', 'Ben.HairA.LightMap.1024']],
    ],
    '1439d2b9': [
        [Log, ['1.0: Ben HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cc195dc5', 'Ben.HairA.LightMap.2048']],
    ],
    '0bbceea0': [
        [Log, ['1.0: Ben HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d665246d', 'Ben.HairA.MaterialMap.1024']],
    ],
    'd665246d': [
        [Log, ['1.0: Ben HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0bbceea0', 'Ben.HairA.MaterialMap.2048']],
    ],
    '894ea737': [
        [Log, ['1.0: Ben HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ba809960', 'Ben.HairA.NormalMap.1024']],
    ],
    'ba809960': [
        [Log, ['1.0: Ben HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['9c4f1a9a', 'Ben.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['894ea737', 'Ben.HairA.NormalMap.2048']],
    ],

    '0313ed95': [
        [Log, ['1.0: Ben BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d8dc4645', 'Ben.BodyA.Diffuse.1024']],
    ],
    'd8dc4645': [
        [Log, ['1.0: Ben BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0313ed95', 'Ben.BodyA.Diffuse.2048']],
    ],
    'cb84ed5e': [
        [Log, ['1.0: Ben BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6a80c2d8', 'Ben.BodyA.LightMap.1024']],
    ],
    '6a80c2d8': [
        [Log, ['1.0: Ben BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cb84ed5e', 'Ben.BodyA.LightMap.2048']],
    ],
    '3f4f6bc0': [
        [Log, ['1.0: Ben BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['decc28c5', 'Ben.BodyA.MaterialMap.1024']],
    ],
    'decc28c5': [
        [Log, ['1.0: Ben BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3f4f6bc0', 'Ben.BodyA.MaterialMap.2048']],
    ],
    '1b79fa5c': [
        [Log, ['1.0: Ben BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f6ecc618', 'Ben.BodyA.NormalMap.1024']],
    ],
    'f6ecc618': [
        [Log, ['1.0: Ben BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['94288cca', 'Ben.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1b79fa5c', 'Ben.BodyA.NormalMap.2048']],
    ],

    // MARK: Billy
    '21e98aeb': [[Log, ['1.0: Billy Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '3371580a': [[Log, ['1.0: Billy Body IB Hash',]], [AddIbCheckIfMissing,]],
    'dc7978f3': [[Log, ['1.0: Billy Head IB Hash',]], [AddIbCheckIfMissing,]],


    'a1d68c9e': [
        [Log, ['1.0: Billy HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6f8a9cdb', 'Billy.HeadA.Diffuse.2048']],
    ],
    'eed0cd5f': [
        [Log, ['1.0: Billy HeadA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e5f2fc35', 'Billy.HeadA.NormalMap.2048']],
    ],
    '877e1a0d': [
        [Log, ['1.0: Billy HeadA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9f02ef2b', 'Billy.HeadA.LightMap.2048']],
    ],
    'dc2f2dd2': [
        [Log, ['1.0: Billy HeadA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d166c3e5', 'Billy.HeadA.MaterialMap.2048']],
    ],
    '6f8a9cdb': [
        [Log, ['1.0: Billy HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a1d68c9e', 'Billy.HeadA.Diffuse.1024']],
    ],
    'e5f2fc35': [
        [Log, ['1.0: Billy HeadA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['eed0cd5f', 'Billy.HeadA.NormalMap.1024']],
    ],
    '9f02ef2b': [
        [Log, ['1.0: Billy HeadA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['877e1a0d', 'Billy.HeadA.LightMap.1024']],
    ],
    'd166c3e5': [
        [Log, ['1.0: Billy HeadA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['dc7978f3', 'Billy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dc2f2dd2', 'Billy.HeadA.MaterialMap.1024']],
    ],

    '0475db07': [
        [Log, ['1.0: Billy HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c0360c81', 'Billy.HairA.Diffuse.1024']],
    ],
    'c0360c81': [
        [Log, ['1.0: Billy HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0475db07', 'Billy.HairA.Diffuse.2048']],
    ],
    '4817b1bc': [
        [Log, ['1.0: Billy HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d269a0a1', 'Billy.HairA.LightMap.1024']],
    ],
    'd269a0a1': [
        [Log, ['1.0: Billy HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4817b1bc', 'Billy.HairA.LightMap.2048']],
    ],
    '47bbe297': [
        [Log, ['1.0: Billy HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['27185819', 'Billy.HairA.NormalMap.1024']],
    ],
    '27185819': [
        [Log, ['1.0: Billy HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['21e98aeb', 'Billy.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['47bbe297', 'Billy.HairA.NormalMap.2048']],
    ],

    '399d9865': [
        [Log, ['1.0: Billy BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['af07a583', 'Billy.BodyA.Diffuse.1024']],
    ],
    'af07a583': [
        [Log, ['1.0: Billy BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['399d9865', 'Billy.BodyA.Diffuse.2048']],
    ],
    '789b054e': [
        [Log, ['1.0: Billy BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0d5d374f', 'Billy.BodyA.LightMap.1024']],
    ],
    '0d5d374f': [
        [Log, ['1.0: Billy BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['789b054e', 'Billy.BodyA.LightMap.2048']],
    ],
    '9cb20fa9': [
        [Log, ['1.0: Billy BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b3cabf65', 'Billy.BodyA.MaterialMap.1024']],
    ],
    'b3cabf65': [
        [Log, ['1.0: Billy BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9cb20fa9', 'Billy.BodyA.MaterialMap.2048']],
    ],
    '56b5953e': [
        [Log, ['1.0: Billy BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['71d95d5d', 'Billy.BodyA.NormalMap.1024']],
    ],
    '71d95d5d': [
        [Log, ['1.0: Billy BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['3371580a', 'Billy.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['56b5953e', 'Billy.BodyA.NormalMap.2048']],
    ],

    // MARK: Burnice
    'f779fb81': [[Log, ['1.2: Burnice Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'af63e974': [[Log, ['1.2: Burnice Body IB Hash',]], [AddIbCheckIfMissing,]],
    'b3f6fcb3': [[Log, ['1.2: Burnice Head IB Hash',]], [AddIbCheckIfMissing,]],

    'c9c87bb1': [[Log, ['1.3 -> 1.4: Burnice HeadA Diffuse 1024p Hash',]], [UpdateHash, ['68f0fb19',]],],
    '68f0fb19': [
        [Log, ['1.4: Burnice HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['b3f6fcb3', 'Burnice.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['c4b6bb10', 'e338bb82'], 'Burnice.HeadA.Diffuse.2048']],
    ],
    'e338bb82': [[Log, ['1.3 -> 1.4: Burnice HeadA Diffuse 2048p Hash',]], [UpdateHash, ['c4b6bb10',]],],
    'c4b6bb10': [
        [Log, ['1.4: Burnice HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['b3f6fcb3', 'Burnice.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['68f0fb19', 'c9c87bb1'], 'Burnice.HeadA.Diffuse.1024']],
    ],

    '609b50a9': [
        [Log, ['1.2: Burnice HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4568c6b3', 'Burnice.HairA.Diffuse.1024']],
    ],
    '4568c6b3': [
        [Log, ['1.2: Burnice HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['609b50a9', 'Burnice.HairA.Diffuse.2048']],
    ],
    'bf0042b9': [
        [Log, ['1.2: Burnice HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['08770e8c', 'Burnice.HairA.LightMap.1024']],
    ],
    '08770e8c': [
        [Log, ['1.2: Burnice HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bf0042b9', 'Burnice.HairA.LightMap.2048']],
    ],
    '5f2840f1': [
        [Log, ['1.2: Burnice HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3ae3ea20', 'Burnice.HairA.MaterialMap.1024']],
    ],
    '3ae3ea20': [
        [Log, ['1.2: Burnice HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5f2840f1', 'Burnice.HairA.MaterialMap.2048']],
    ],
    '438cf629': [
        [Log, ['1.2: Burnice HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0050e0d2', 'Burnice.HairA.NormalMap.1024']],
    ],
    '0050e0d2': [
        [Log, ['1.2: Burnice HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['f779fb81', 'Burnice.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['438cf629', 'Burnice.HairA.NormalMap.2048']],
    ],

    '50bf6521': [
        [Log, ['1.2: Burnice BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f0e67001', 'Burnice.BodyA.Diffuse.1024']],
    ],
    'f0e67001': [
        [Log, ['1.2: Burnice BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['50bf6521', 'Burnice.BodyA.Diffuse.2048']],
    ],
    'f4e05ee7': [
        [Log, ['1.2: Burnice BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0a3ba8ac', 'Burnice.BodyA.LightMap.1024']],
    ],
    '0a3ba8ac': [
        [Log, ['1.2: Burnice BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f4e05ee7', 'Burnice.BodyA.LightMap.2048']],
    ],
    'c321481d': [
        [Log, ['1.2: Burnice BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e37e7622', 'Burnice.BodyA.MaterialMap.1024']],
    ],
    'e37e7622': [
        [Log, ['1.2: Burnice BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c321481d', 'Burnice.BodyA.MaterialMap.2048']],
    ],
    '0f2c69e2': [
        [Log, ['1.2: Burnice BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0c4f338a', 'Burnice.BodyA.NormalMap.1024']],
    ],
    '0c4f338a': [
        [Log, ['1.2: Burnice BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['af63e974', 'Burnice.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0f2c69e2', 'Burnice.BodyA.NormalMap.2048']],
    ],



    // MARK: Caesar
    '7a8fa826': [[Log, ['1.2: Caesar Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '92061e5e': [[Log, ['1.2: Caesar Body IB Hash',]], [AddIbCheckIfMissing,]],
    '6caaeb53': [[Log, ['1.2: Caesar Head IB Hash',]], [AddIbCheckIfMissing,]],

    'af291513': [
        [Log, ['1.2 -> 1.3: Caesar Hair Texcoord Hash',]],
        [UpdateHash, ['72537fa3',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz13RemapTexcoord, [
            '13_Caesar_hair',
            ['4B', '2e', '2f', '2e'],
            ['4B', '2f', '2f', '2f']
        ]],
    ],
    '3b2a70a5': [
        [Log, ['1.2 -> 1.3: Caesar Body Texcoord Hash',]],
        [UpdateHash, ['0ca81129',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz13RemapTexcoord, [
            '13_Caesar_body',
            ['4B', '2e', '2f', '2e', '2e'],
            ['4B', '2f', '2f', '2f', '2f']
        ]],
    ],

    '84d53514': [
        [Log, ['1.2: Caesar HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['6caaeb53', 'Caesar.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['13098244', 'Caesar.HeadA.Diffuse.2048']],
    ],
    '13098244': [
        [Log, ['1.2: Caesar HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['6caaeb53', 'Caesar.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['84d53514', 'Caesar.HeadA.Diffuse.1024']],
    ],

    '9ce3e80c': [
        [Log, ['1.2: Caesar HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b004ab49', 'Caesar.HairA.Diffuse.1024']],
    ],
    'b004ab49': [
        [Log, ['1.2: Caesar HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9ce3e80c', 'Caesar.HairA.Diffuse.2048']],
    ],
    'bf19954f': [
        [Log, ['1.2: Caesar HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c7115c4b', 'Caesar.HairA.LightMap.1024']],
    ],
    'c7115c4b': [
        [Log, ['1.2: Caesar HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bf19954f', 'Caesar.HairA.LightMap.2048']],
    ],
    '350b827e': [
        [Log, ['1.2: Caesar HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2204f89a', 'Caesar.HairA.MaterialMap.1024']],
    ],
    '2204f89a': [
        [Log, ['1.2: Caesar HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['350b827e', 'Caesar.HairA.MaterialMap.2048']],
    ],
    '10af3807': [
        [Log, ['1.2: Caesar HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e17b3529', 'Caesar.HairA.NormalMap.1024']],
    ],
    'e17b3529': [
        [Log, ['1.2: Caesar HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['7a8fa826', 'Caesar.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['10af3807', 'Caesar.HairA.NormalMap.2048']],
    ],

    '5e2cea1a': [
        [Log, ['1.2: Caesar BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f4b78da0', 'Caesar.BodyA.Diffuse.1024']],
    ],
    'f4b78da0': [
        [Log, ['1.2: Caesar BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5e2cea1a', 'Caesar.BodyA.Diffuse.2048']],
    ],
    '6296d481': [
        [Log, ['1.2: Caesar BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a9e24ba0', 'Caesar.BodyA.LightMap.1024']],
    ],
    'a9e24ba0': [
        [Log, ['1.2: Caesar BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6296d481', 'Caesar.BodyA.LightMap.2048']],
    ],
    'd5d89d5b': [
        [Log, ['1.2: Caesar BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['328bc108', 'Caesar.BodyA.MaterialMap.1024']],
    ],
    '328bc108': [
        [Log, ['1.2: Caesar BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d5d89d5b', 'Caesar.BodyA.MaterialMap.2048']],
    ],

    'c1f1e12f': [[Log, ['1.3 -> 1.4: Caesar BodyA NormalMap 2048p Hash',]], [UpdateHash, ['36f39b49',]],],
    'f1c6c309': [[Log, ['1.4B -> 1.4C: Caesar BodyA NormalMap 2048p Hash',]], [UpdateHash, ['36f39b49',]],],
    '36f39b49': [
        [Log, ['1.4: Caesar BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['a8abff9d', '8cdf95d0'], 'Caesar.BodyA.NormalMap.1024']],
    ],
    '8cdf95d0': [[Log, ['1.3 -> 1.4: Caesar BodyA NormalMap 1024p Hash',]], [UpdateHash, ['a8abff9d',]],],
    'a8abff9d': [
        [Log, ['1.4: Caesar BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['92061e5e', 'Caesar.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['36f39b49', 'f1c6c309', 'c1f1e12f'], 'Caesar.BodyA.NormalMap.2048']],
    ],



    // MARK: Corin
    '5a839fb2': [[Log, ['1.0: Corin Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'e74620b5': [[Log, ['1.0: Corin Body IB Hash',]], [AddIbCheckIfMissing,]],
    '5f803336': [[Log, ['1.0: Corin Bear IB Hash',]], [AddIbCheckIfMissing,]],
    'a0c80593': [[Log, ['1.0: Corin Head IB Hash',]], [AddIbCheckIfMissing,]],


    '8d999156': [[Log, ['1.3 -> 1.4: Corin Hair Blend Hash',]], [UpdateHash, ['5fa50113',]],],
    '2cf242f4': [[Log, ['1.3 -> 1.4: Corin Hair Texcoord Hash',]], [UpdateHash, ['abc95b03',]],],

    '97022d3c': [
        [Log, ['1.0: Corin HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['a0c80593', 'Corin.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6d662824', 'Corin.HeadA.Diffuse.2048']],
    ],
    '6d662824': [
        [Log, ['1.0: Corin HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['a0c80593', 'Corin.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['97022d3c', 'Corin.HeadA.Diffuse.1024']],
    ],

    '60526444': [
        [Log, ['1.0: Corin HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['651e96f8', 'Corin.HairA.Diffuse.1024']],
    ],
    '651e96f8': [
        [Log, ['1.0: Corin HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['60526444', 'Corin.HairA.Diffuse.2048']],
    ],
    '929aca42': [
        [Log, ['1.0: Corin HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['edff2372', 'Corin.HairA.LightMap.1024']],
    ],
    'edff2372': [
        [Log, ['1.0: Corin HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['929aca42', 'Corin.HairA.LightMap.2048']],
    ],
    // '23b4c60d': [
    //     [Log,                           ['1.0: Corin HairA MaterialMap 2048p Hash',]],
    //     [AddSectionIfMissing,        ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
    //     [MultiplySectionIfMissing,   ['1b88e01e', 'Corin.HairA.MaterialMap.1024']],
    // ],
    // '1b88e01e': [
    //     [Log,                           ['1.0: Corin HairA MaterialMap 1024p Hash',]],
    //     [AddSectionIfMissing,        ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
    //     [MultiplySectionIfMissing,   ['23b4c60d', 'Corin.HairA.MaterialMap.2048']],
    // ],
    '4a68ef99': [
        [Log, ['1.0: Corin HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ab8956c8', 'Corin.HairA.NormalMap.1024']],
    ],
    'ab8956c8': [
        [Log, ['1.0: Corin HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['5a839fb2', 'Corin.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4a68ef99', 'Corin.HairA.NormalMap.2048']],
    ],


    'af9d845a': [
        [Log, ['1.0: Corin BodyA, BearA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['681f5162', 'Corin.BodyA.Diffuse.1024']],
    ],
    '681f5162': [
        [Log, ['1.0: Corin BodyA, BearA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['af9d845a', 'Corin.BodyA.Diffuse.2048']],
    ],
    '75e05cdc': [
        [Log, ['1.0: Corin BodyA, BearA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['af7eda82', 'Corin.BodyA.LightMap.1024']],
    ],
    'af7eda82': [
        [Log, ['1.0: Corin BodyA, BearA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['75e05cdc', 'Corin.BodyA.LightMap.2048']],
    ],
    '50a0faea': [
        [Log, ['1.0: Corin BodyA, BearA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['9dc9c0f6', 'Corin.BodyA.MaterialMap.1024']],
    ],
    '9dc9c0f6': [
        [Log, ['1.0: Corin BodyA, BearA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['50a0faea', 'Corin.BodyA.MaterialMap.2048']],
    ],
    '289f4c58': [
        [Log, ['1.0: Corin BodyA, BearA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['640141d4', 'Corin.BodyA.NormalMap.1024']],
    ],
    '640141d4': [
        [Log, ['1.0: Corin BodyA, BearA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['289f4c58', 'Corin.BodyA.NormalMap.2048']],
    ],



    // MARK: Ellen
    'd44a8015': [[Log, ['1.1: Ellen Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'e30fae03': [[Log, ['1.1: Ellen Body IB Hash',]], [AddIbCheckIfMissing,]],
    'f6ef8f3a': [[Log, ['1.1: Ellen Head IB Hash',]], [AddIbCheckIfMissing,]],

    '9c7fac5a': [[Log, ['1.0 -> 1.1: Ellen Head IB Hash',]], [UpdateHash, ['f6ef8f3a',]]],
    '7f89a2b3': [[Log, ['1.0 -> 1.1: Ellen Hair IB Hash',]], [UpdateHash, ['d44a8015',]]],
    'a72cfb34': [[Log, ['1.0 -> 1.1: Ellen Body IB Hash',]], [UpdateHash, ['e30fae03',]]],


    '83dfd744': [[Log, ['1.0 -> 1.1: Ellen Head Texcoord Hash',]], [UpdateHash, ['8744badf',]]],


    'd59a5fec': [[Log, ['1.0 -> 1.1: Ellen Hair Draw Hash',]], [UpdateHash, ['77ac5f85',]]],
    'a5448398': [[Log, ['1.0 -> 1.1: Ellen Hair Position Hash',]], [UpdateHash, ['ba0fe600',]]],
    '9cddb082': [
        [Log, ['1.0 -> 1.1: Ellen Hair Texcoord Hash',]],
        [UpdateHash, ['5c33833e',]],
        [Log, ['+ Remapping texcoord buffer from stride 24 to 36',]],
        [Zzz13RemapTexcoord, ['11_Ellen_Hair', ['4B', '2e', '2f', '2e', '2e'], ['4f', '2e', '2f', '2e', '2e']]], // attention
    ],

    '5c33833e': [
        [Log, ['1.1 -> 1.2: Ellen Hair Texcoord Hash',]],
        [UpdateHash, ['a27a8e1a',]],
        [Log, ['+ Remapping texcoord buffer from stride 36 to 24',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],
    '52188576': [
        [Log, ['1.3 -> 1.4: Ellen Hair Blend Remap',]],
        [UpdateBufferBlendIndices, [
            '52188576',
            [34, 35, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 49, 50],
            [39, 34, 40, 35, 38, 42, 43, 44, 45, 46, 47, 41, 50, 49],
        ]],
        [UpdateHash, ['e91c93e0',]],
    ],


    '7bd3f8c2': [[Log, ['1.0 -> 1.1: Ellen Body Draw Hash',]], [UpdateHash, ['cdce1fc2',]]],
    '89d5fba4': [[Log, ['1.0 -> 1.1: Ellen Body Position Hash',]], [UpdateHash, ['b78f3616',]]],
    '26966844': [[Log, ['1.0 -> 1.1: Ellen Body Texcoord Hash',]], [UpdateHash, ['5ac6d5ee',]]],
    '89589539': [[Log, ['1.5 -> 1.6: Ellen Body Blend Hash',]], [UpdateHash, ['ed9cb852',]]],


    '09d55bce': [[Log, ['1.0 -> 1.1: Ellen HeadA Diffuse 2048p Hash',]], [UpdateHash, ['465a66eb',]]],
    '465a66eb': [
        [Log, ['1.1: Ellen HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['f6ef8f3a', '9c7fac5a'], 'Ellen.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['4808d050', 'e6b27e31'], 'Ellen.HeadA.Diffuse.1024']],
    ],
    'e6b27e31': [[Log, ['1.0 -> 1.1: Ellen HeadA Diffuse 1024p Hash',]], [UpdateHash, ['4808d050',]]],
    '4808d050': [
        [Log, ['1.1: Ellen HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['f6ef8f3a', '9c7fac5a'], 'Ellen.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['465a66eb', '09d55bce'], 'Ellen.HeadA.Diffuse.2048']],
    ],


    '81ccd2e2': [
        [Log, ['1.0: Ellen HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1440e534', 'Ellen.HairA.Diffuse.1024']],
    ],
    '1440e534': [
        [Log, ['1.0: Ellen HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['81ccd2e2', 'Ellen.HairA.Diffuse.2048']],
    ],
    'dc9d8b6e': [
        [Log, ['1.0: Ellen HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8c835faa', 'Ellen.HairA.LightMap.1024']],
    ],
    '8c835faa': [
        [Log, ['1.0: Ellen HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dc9d8b6e', 'Ellen.HairA.LightMap.2048']],
    ],
    '01bb8189': [
        [Log, ['1.0: Ellen HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b21b8370', 'Ellen.HairA.MaterialMap.1024']],
    ],
    'b21b8370': [
        [Log, ['1.0: Ellen HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['01bb8189', 'Ellen.HairA.MaterialMap.2048']],
    ],
    'aaadca31': [
        [Log, ['1.0: Ellen HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d6715e09', 'Ellen.HairA.NormalMap.1024']],
    ],
    'd6715e09': [
        [Log, ['1.0: Ellen HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['d44a8015', '7f89a2b3'], 'Ellen.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['aaadca31', 'Ellen.HairA.NormalMap.2048']],
    ],


    'cf5f5fed': [
        [Log, ['1.0: -> 1.1: Ellen BodyA Diffuse 2048p Hash',]],
        [UpdateHash, ['163e2559',]],
    ],
    '163e2559': [
        [Log, ['1.1: Ellen BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['22fa0cd6', '94c15986'], 'Ellen.BodyA.Diffuse.1024']],
    ],
    '94c15986': [
        [Log, ['1.0: -> 1.1: Ellen BodyA Diffuse 1024p Hash',]],
        [UpdateHash, ['22fa0cd6',]],
    ],
    '22fa0cd6': [
        [Log, ['1.1: Ellen BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['163e2559', 'cf5f5fed'], 'Ellen.BodyA.Diffuse.2048']],
    ],
    'ff26fb83': [
        [Log, ['1.0: Ellen BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cea7516a', 'Ellen.BodyA.LightMap.1024']],
    ],
    'cea7516a': [
        [Log, ['1.0: Ellen BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ff26fb83', 'Ellen.BodyA.LightMap.2048']],
    ],
    'f4487235': [
        [Log, ['1.0: Ellen BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['30dc14d7', 'Ellen.BodyA.MaterialMap.1024']],
    ],
    '30dc14d7': [
        [Log, ['1.0: Ellen BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f4487235', 'Ellen.BodyA.MaterialMap.2048']],
    ],
    '798c3a51': [
        [Log, ['1.0: Ellen BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['590880e5', 'Ellen.BodyA.NormalMap.1024']],
    ],
    '590880e5': [
        [Log, ['1.0: Ellen BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['e30fae03', 'a72cfb34'], 'Ellen.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['798c3a51', 'Ellen.BodyA.NormalMap.2048']],
    ],



    // MARK: Evelyn
    '10a5bde2': [[Log, ['1.5: Evelyn Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '04b53ecd': [[Log, ['1.5: Evelyn Body IB Hash',]], [AddIbCheckIfMissing,]],
    'bb6d1023': [[Log, ['1.5: Evelyn Jacket IB Hash',]], [AddIbCheckIfMissing,]],
    'b3eaedb0': [[Log, ['1.5: Evelyn Shoulders IB Hash',]], [AddIbCheckIfMissing,]],
    'ddf4efa6': [[Log, ['1.5: Evelyn Face IB Hash',]], [AddIbCheckIfMissing,]],

    '8e1d1a6f': [
        [Log, ['1.5: Evelyn FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['ddf4efa6', 'Evelyn.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bc090438', 'Evelyn.FaceA.Diffuse.1024']],
    ],
    'bc090438': [
        [Log, ['1.5: Evelyn FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['ddf4efa6', 'Evelyn.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8e1d1a6f', 'Evelyn.FaceA.Diffuse.2048']],
    ],


    '0e5c3c97': [
        [Log, ['1.5: Evelyn Hair, Jacket Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['65a7592d', 'Evelyn.Hair.Diffuse.1024']],
    ],
    'e1434e0d': [
        [Log, ['1.5: Evelyn Hair, Jacket LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['eb414a98', 'Evelyn.Hair.LightMap.1024']],
    ],
    'b2718585': [
        [Log, ['1.5: Evelyn Hair, Jacket MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['e680f0c7', 'Evelyn.Hair.MaterialMap.1024']],
    ],
    '65a7592d': [
        [Log, ['1.5: Evelyn Hair, Jacket Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['0e5c3c97', 'Evelyn.Hair.Diffuse.2048']],
    ],
    'eb414a98': [
        [Log, ['1.5: Evelyn Hair, Jacket LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['e1434e0d', 'Evelyn.Hair.LightMap.2048']],
    ],
    'e680f0c7': [
        [Log, ['1.5: Evelyn Hair, Jacket MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['b2718585', 'Evelyn.Hair.MaterialMap.2048']],
    ],

    'a59b14c0': [
        [Log, ['1.5: Evelyn Body, Shoulder Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['93033898', 'Evelyn.Body.Diffuse.1024']],
    ],
    'd022d32c': [
        [Log, ['1.5: Evelyn Body, Shoulder LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['16aab2ab', 'Evelyn.Body.LightMap.1024']],
    ],
    '8624e4e4': [
        [Log, ['1.5: Evelyn Body, Shoulder MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['716561f0', 'Evelyn.Body.MaterialMap.1024']],
    ],
    '93033898': [
        [Log, ['1.5: Evelyn Body, Shoulder Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['a59b14c0', 'Evelyn.Body.Diffuse.2048']],
    ],
    '16aab2ab': [
        [Log, ['1.5: Evelyn Body, Shoulder LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['d022d32c', 'Evelyn.Body.LightMap.2048']],
    ],
    '716561f0': [
        [Log, ['1.5: Evelyn Body, Shoulder MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['8624e4e4', 'Evelyn.Body.MaterialMap.2048']],
    ],



    // MARK: Grace
    '89299f56': [[Log, ['1.0: Grace Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '8b240678': [[Log, ['1.2: Grace Body IB Hash',]], [AddIbCheckIfMissing,]],
    '4d60568b': [[Log, ['1.0: Grace Head IB Hash',]], [AddIbCheckIfMissing,]],


    // reverted in 1.2
    // '89d903ba': [
    //     [Log, ['1.0: -> 1.1: Grace Hair Texcoord Hash',]],
    //     [UpdateHash, ['d21f32ad',]],
    //     [Log, ['+ Remapping texcoord buffer from stride 20 to 32',]],
    //     [update_buffer_element_width, [['BBBB', 'ee', 'ff', 'ee'], ['ffff', 'ee', 'ff', 'ee'], '1.1']],
    //     [Log, ['+ Setting texcoord vcolor alpha to 1',]],
    //     [update_buffer_element_value, [['ffff', 'ee', 'ff', 'ee'], ['xxx1', 'xx', 'xx', 'xx'], '1.1']]
    // ],

    'd21f32ad': [
        [Log, ['1.1 -> 1.2: Grace Hair Texcoord Hash',]],
        [UpdateHash, ['89d903ba',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],

    'e5e04f6f': [[Log, ['1.1 -> 1.2: Grace Body Draw Hash',]], [UpdateHash, ['f1cba806',]]],
    '26ffa186': [
        [Log, ['1.1 -> 1.2: Grace Body Position Hash',]],
        [UpdateHash, ['8855c5cf',]],
        [Log, ['1.1 -> 1.2: Grace Body Blend Remap',]],
        [UpdateBufferBlendIndices, [
            '8855c5cf',
            [35, 34, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85, 86, 87, 88, 89],
            [34, 35, 80, 85, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 51, 47, 48, 49, 50, 52, 54, 53, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 66, 65, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 89, 78, 79, 81, 82, 83, 84, 86, 87, 88],
        ]]
    ],
    'e536af35': [[Log, ['1.1 -> 1.2: Grace Body Texcoord Hash',]], [UpdateHash, ['4bb45448',]]],
    '0f82a13e': [
        [Log, ['1.1 -> 1.2: Grace Body IB Hash',]],
        [UpdateHash, ['8b240678',]],
        [TransferIndexedSections, {
            'src_indices': ['0', '42885'],
            'trg_indices': ['0', '42927'],
        }]
    ],

    'e75590cb': [
        [Log, ['1.0: Grace HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['4d60568b', 'Grace.HeadA.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7459ecf4', 'Grace.HeadA.Diffuse.2048']],
    ],
    '7459ecf4': [
        [Log, ['1.0: Grace HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['4d60568b', 'Grace.HeadA.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e75590cb', 'Grace.HeadA.Diffuse.1024']],
    ],


    'a87d2822': [
        [Log, ['1.0: Grace HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['94d04401', 'Grace.HairA.Diffuse.1024']],
    ],
    '94d04401': [
        [Log, ['1.0: Grace HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a87d2822', 'Grace.HairA.Diffuse.2048']],
    ],
    '8eddd041': [
        [Log, ['1.0: Grace HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['26bf1588', 'Grace.HairA.LightMap.1024']],
    ],
    '26bf1588': [
        [Log, ['1.0: Grace HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8eddd041', 'Grace.HairA.LightMap.2048']],
    ],
    '3a38f6f9': [
        [Log, ['1.0: Grace HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e1cb3739', 'Grace.HairA.MaterialMap.1024']],
    ],
    'e1cb3739': [
        [Log, ['1.0: Grace HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3a38f6f9', 'Grace.HairA.MaterialMap.2048']],
    ],
    '846fab9a': [
        [Log, ['1.0: Grace HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1c4079f7', 'Grace.HairA.NormalMap.1024']],
    ],
    '1c4079f7': [
        [Log, ['1.0: Grace HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['89299f56', 'Grace.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['846fab9a', 'Grace.HairA.NormalMap.2048']],
    ],


    '6d6ac4f4': [
        [Log, ['1.0: Grace BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['397a8aed', 'Grace.BodyA.Diffuse.1024']],
    ],
    '397a8aed': [
        [Log, ['1.0: Grace BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6d6ac4f4', 'Grace.BodyA.Diffuse.2048']],
    ],
    '993fe3e1': [
        [Log, ['1.0: Grace BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['59dd8899', 'Grace.BodyA.LightMap.1024']],
    ],
    '59dd8899': [
        [Log, ['1.0: Grace BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['993fe3e1', 'Grace.BodyA.LightMap.2048']],
    ],
    'e8345f2c': [
        [Log, ['1.0: Grace BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a6c8c203', 'Grace.BodyA.MaterialMap.1024']],
    ],
    'a6c8c203': [
        [Log, ['1.0: Grace BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e8345f2c', 'Grace.BodyA.MaterialMap.2048']],
    ],
    '1e794b69': [
        [Log, ['1.0: Grace BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9abd7824', 'Grace.BodyA.NormalMap.1024']],
    ],
    '9abd7824': [
        [Log, ['1.0: Grace BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1e794b69', 'Grace.BodyA.NormalMap.2048']],
    ],
    '210b3ebf': [[Log, ['1.3 -> 1.4: Grace BodyB Diffuse 2048p Hash',]], [UpdateHash, ['9c7057e8',]]],
    '9c7057e8': [
        [Log, ['1.4: Grace BodyB Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['ac361185', '21794bd6'], 'Grace.BodyB.Diffuse.1024']],
    ],
    '21794bd6': [[Log, ['1.3 -> 1.4: Grace BodyB Diffuse 1024p Hash',]], [UpdateHash, ['ac361185',]]],
    'ac361185': [
        [Log, ['1.4: Grace BodyB Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['9c7057e8', '210b3ebf'], 'Grace.BodyB.Diffuse.2048']],
    ],
    '08082f5f': [
        [Log, ['1.0: Grace BodyB LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a60162a0', 'Grace.BodyB.LightMap.1024']],
    ],
    'a60162a0': [
        [Log, ['1.0: Grace BodyB LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['08082f5f', 'Grace.BodyB.LightMap.2048']],
    ],
    'f176398a': [
        [Log, ['1.0: Grace BodyB MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b5b88a3f', 'Grace.BodyB.MaterialMap.1024']],
    ],
    'b5b88a3f': [
        [Log, ['1.0: Grace BodyB MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f176398a', 'Grace.BodyB.MaterialMap.2048']],
    ],
    '06cb1413': [
        [Log, ['1.0: Grace BodyB NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c5f703be', 'Grace.BodyB.NormalMap.1024']],
    ],
    'c5f703be': [
        [Log, ['1.0: Grace BodyB NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['8b240678', '0f82a13e'], 'Grace.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['06cb1413', 'Grace.BodyB.NormalMap.2048']],
    ],



    // MARK: Harumasa
    '78bea30d': [[Log, ['1.4 -> 1.5: Harumasa Body IB Hash',]], [UpdateHash, ['79679a10',]]],

    '6324de38': [[Log, ['1.4: Harumasa Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '79679a10': [[Log, ['1.4: Harumasa Body IB Hash',]], [AddIbCheckIfMissing,]],
    'aa7ba2dc': [[Log, ['1.4: Harumasa Legs IB Hash',]], [AddIbCheckIfMissing,]],
    'b0688334': [[Log, ['1.4: Harumasa Face IB Hash',]], [AddIbCheckIfMissing,]],


    'cafffd37': [[Log, ['1.4 -> 1.5: Harumasa Body Draw Hash',]], [UpdateHash, ['1fb92e46',]]],
    '3fa41462': [[Log, ['1.4 -> 1.5: Harumasa Body Position Hash',]], [UpdateHash, ['0899751e',]]],
    'c0b32d17': [[Log, ['1.4 -> 1.5: Harumasa Body Blend Hash',]], [UpdateHash, ['347a0e9d',]]],
    '95ee1030': [[Log, ['1.4 -> 1.5: Harumasa Body Texcoord Hash',]], [UpdateHash, ['e14fbc30',]]],


    '4394c0b2': [
        [Log, ['1.4: Harumasa FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['b0688334', 'Harumasa.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c5596262', 'Harumasa.FaceA.Diffuse.1024']],
    ],
    'c5596262': [
        [Log, ['1.4: Harumasa FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['b0688334', 'Harumasa.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4394c0b2', 'Harumasa.FaceA.Diffuse.2048']],
    ],

    'b8f268ee': [
        [Log, ['1.4: Harumasa HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5700ced5', 'Harumasa.HairA.Diffuse.1024']],
    ],
    'd4838b9d': [
        [Log, ['1.4: Harumasa HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a1310b4f', 'Harumasa.HairA.LightMap.1024']],
    ],
    '7217c146': [
        [Log, ['1.4: Harumasa HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c2c9ad2d', 'Harumasa.HairA.MaterialMap.1024']],
    ],
    '5700ced5': [
        [Log, ['1.4: Harumasa HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b8f268ee', 'Harumasa.HairA.Diffuse.2048']],
    ],
    'a1310b4f': [
        [Log, ['1.4: Harumasa HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d4838b9d', 'Harumasa.HairA.LightMap.2048']],
    ],
    'c2c9ad2d': [
        [Log, ['1.4: Harumasa HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['6324de38', 'Harumasa.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7217c146', 'Harumasa.HairA.MaterialMap.2048']],
    ],

    'ba52ac92': [[Log, ['1.4 -> 1.5: Harumasa BodyA Diffuse 2048p Hash',]], [UpdateHash, ['49f8aaf6',]]],
    '49f8aaf6': [
        [Log, ['1.4: Harumasa BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['999ec526', 'e0b0c6eb'], 'Harumasa.BodyA.Diffuse.1024']],
    ],
    'cc51476a': [
        [Log, ['1.4: Harumasa BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2b1230cf', 'Harumasa.BodyA.LightMap.1024']],
    ],
    'cd1e0187': [[Log, ['1.4 -> 1.5: Harumasa BodyA MaterialMap 2048p Hash',]], [UpdateHash, ['6d105f7e',]]],
    '6d105f7e': [
        [Log, ['1.4: Harumasa BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['c90264db', '2b0017d5'], 'Harumasa.BodyA.MaterialMap.1024']],
    ],

    'e0b0c6eb': [[Log, ['1.4 -> 1.5: Harumasa BodyA Diffuse 1024p Hash',]], [UpdateHash, ['999ec526',]]],
    '999ec526': [
        [Log, ['1.4: Harumasa BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['49f8aaf6', 'ba52ac92'], 'Harumasa.BodyA.Diffuse.2048']],
    ],
    '2b1230cf': [
        [Log, ['1.4: Harumasa BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cc51476a', 'Harumasa.BodyA.LightMap.2048']],
    ],
    '2b0017d5': [[Log, ['1.4 -> 1.5: Harumasa BodyA MaterialMap 1024p Hash',]], [UpdateHash, ['c90264db',]]],
    'c90264db': [
        [Log, ['1.4: Harumasa BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['79679a10', '78bea30d'], 'Harumasa.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['6d105f7e', 'cd1e0187'], 'Harumasa.BodyA.MaterialMap.2048']],
    ],

    '44d74a1a': [
        [Log, ['1.4: Harumasa LegsA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['897c74d5', 'Harumasa.LegsA.Diffuse.1024']],
    ],
    '4b4d0ff6': [
        [Log, ['1.4: Harumasa LegsA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['822ec07f', 'Harumasa.LegsA.LightMap.1024']],
    ],
    'ba8e396b': [
        [Log, ['1.4: Harumasa LegsA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bdbf66a1', 'Harumasa.LegsA.MaterialMap.1024']],
    ],
    '897c74d5': [
        [Log, ['1.4: Harumasa LegsA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['44d74a1a', 'Harumasa.LegsA.Diffuse.2048']],
    ],
    '822ec07f': [
        [Log, ['1.4: Harumasa LegsA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4b4d0ff6', 'Harumasa.LegsA.LightMap.2048']],
    ],
    'bdbf66a1': [
        [Log, ['1.4: Harumasa LegsA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['aa7ba2dc', 'Harumasa.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ba8e396b', 'Harumasa.LegsA.MaterialMap.2048']],
    ],



    // MARK: Hugo
    '45ae7079': [[Log, ['1.7: Hugo Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'b4765894': [[Log, ['1.7: Hugo Body IB Hash',]], [AddIbCheckIfMissing,]],
    'ed26c53d': [[Log, ['1.7: Hugo Coat IB Hash',]], [AddIbCheckIfMissing,]],
    '5db95af3': [[Log, ['1.7: Hugo Badge IB Hash',]], [AddIbCheckIfMissing,]],
    '66b936fc': [[Log, ['1.7: Hugo Face IB Hash',]], [AddIbCheckIfMissing,]],

    // Face
    'a3064b0e': [
        [Log, ['1.7: Hugo FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['66b936fc', 'Hugo.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0f344a22', 'Hugo.FaceA.Diffuse.1024']],
    ],
    '0f344a22': [
        [Log, ['1.7: Hugo FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['66b936fc', 'Hugo.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a3064b0e', 'Hugo.FaceA.Diffuse.2048']],
    ],

    // Hair
    'f50ebb37': [
        [Log, ['1.7: Hugo HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bab642c6', 'Hugo.HairA.Diffuse.1024']],
    ],
    'bab642c6': [
        [Log, ['1.7: Hugo HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f50ebb37', 'Hugo.HairA.Diffuse.2048']],
    ],
    '94daa8f7': [
        [Log, ['1.7: Hugo HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dcf7c209', 'Hugo.HairA.LightMap.1024']],
    ],
    'dcf7c209': [
        [Log, ['1.7: Hugo HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['94daa8f7', 'Hugo.HairA.LightMap.2048']],
    ],
    '9614f191': [
        [Log, ['1.7: Hugo HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['144c15d0', 'Hugo.HairA.MaterialMap.1024']],
    ],
    '144c15d0': [
        [Log, ['1.7: Hugo HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['45ae7079', 'Hugo.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9614f191', 'Hugo.HairA.MaterialMap.2048']],
    ],

    // Body
    '7fa5eb2e': [
        [Log, ['1.7: Hugo BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2841b582', 'Hugo.BodyA.Diffuse.1024']],
    ],
    '2841b582': [
        [Log, ['1.7: Hugo BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7fa5eb2e', 'Hugo.BodyA.Diffuse.2048']],
    ],
    'f9911f83': [
        [Log, ['1.7: Hugo BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9fd99d99', 'Hugo.BodyA.LightMap.1024']],
    ],
    '9fd99d99': [
        [Log, ['1.7: Hugo BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f9911f83', 'Hugo.BodyA.LightMap.2048']],
    ],
    'c6fa84c9': [
        [Log, ['1.7: Hugo BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e2333ede', 'Hugo.BodyA.MaterialMap.1024']],
    ],
    'e2333ede': [
        [Log, ['1.7: Hugo BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c6fa84c9', 'Hugo.BodyA.MaterialMap.2048']],
    ],

    // Coat
    '348bc40f': [
        [Log, ['1.7: Hugo CoatA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['481e8fe0', 'Hugo.CoatA.Diffuse.1024']],
    ],
    '481e8fe0': [
        [Log, ['1.7: Hugo CoatA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['348bc40f', 'Hugo.CoatA.Diffuse.2048']],
    ],
    '0db80414': [
        [Log, ['1.7: Hugo CoatA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a951a0cf', 'Hugo.CoatA.LightMap.1024']],
    ],
    'a951a0cf': [
        [Log, ['1.7: Hugo CoatA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0db80414', 'Hugo.CoatA.LightMap.2048']],
    ],
    '25b33389': [
        [Log, ['1.7: Hugo CoatA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ec648dcb', 'Hugo.CoatA.MaterialMap.1024']],
    ],
    'ec648dcb': [
        [Log, ['1.7: Hugo CoatA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['b4765894', 'Hugo.Coat.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['25b33389', 'Hugo.CoatA.MaterialMap.2048']],
    ],



    // MARK: Jane Doe
    '9268a5af': [[Log, ['1.4: Jane Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'ba4255a5': [[Log, ['1.4: Jane Body IB Hash',]], [AddIbCheckIfMissing,]],
    'ef86fc9f': [[Log, ['1.1: Jane Head IB Hash',]], [AddIbCheckIfMissing,]],

    'c8ad344e': [
        [Log, ['1.1 -> 1.2: Jane Hair Texcoord Hash',]],
        [UpdateHash, ['257a90d6',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],

    '5721e4e7': [[Log, ['1.3 -> 1.4: Jane Hair Draw Hash',]], [UpdateHash, ['2d06e785',]],],
    '24323bf9': [[Log, ['1.3 -> 1.4: Jane Hair Position Hash',]], [UpdateHash, ['e7a3b7dc',]],],
    '0a10c747': [[Log, ['1.3 -> 1.4: Jane Hair Blend Hash',]], [UpdateHash, ['8721477f',]],],
    '257a90d6': [[Log, ['1.3 -> 1.4: Jane Hair Texcoord Hash',]], [UpdateHash, ['acec29f8',]],],
    '7b16a708': [[Log, ['1.3 -> 1.4: Jane Hair IB Hash',]], [UpdateHash, ['9268a5af',]],],

    'd1aa4b85': [[Log, ['1.3 -> 1.4: Jane Body Draw Hash',]], [UpdateHash, ['0e1c6740',]],],
    '06f9bc49': [[Log, ['1.3 -> 1.4: Jane Body Position Hash',]], [UpdateHash, ['10050266',]],],
    '9727a184': [[Log, ['1.3 -> 1.4: Jane Body Blend Hash',]], [UpdateHash, ['e27f398e',]],],
    '8b85c03e': [[Log, ['1.3 -> 1.4: Jane Body Texcoord Hash',]], [UpdateHash, ['949549de',]],],
    'e2c0144e': [[Log, ['1.3 -> 1.4: Jane Body IB Hash',]], [UpdateHash, ['ba4255a5',]],],

    '689639a5': [[Log, ['1.3 -> 1.4: Jane HeadA Diffuse 1024p Hash',]], [UpdateHash, ['d823ac80',]],],
    'd823ac80': [
        [Log, ['1.1: Jane HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['ef86fc9f', 'Jane.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['3b75aa2c', '8974fb74'], 'Jane.HeadA.Diffuse.2048']],
    ],
    '8974fb74': [[Log, ['1.3 -> 1.4: Jane HeadA Diffuse 2048p Hash',]], [UpdateHash, ['3b75aa2c',]],],
    '3b75aa2c': [
        [Log, ['1.1: Jane HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['ef86fc9f', 'Jane.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['d823ac80', '689639a5'], 'Jane.HeadA.Diffuse.1024']],
    ],

    'f7ef1a53': [
        [Log, ['1.1: Jane HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b33a9770', 'Jane.HairA.Diffuse.1024']],
    ],
    'b33a9770': [
        [Log, ['1.1: Jane HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f7ef1a53', 'Jane.HairA.Diffuse.2048']],
    ],
    '9ec4cd4f': [
        [Log, ['1.1: Jane HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5e12acc1', 'Jane.HairA.LightMap.1024']],
    ],
    '5e12acc1': [
        [Log, ['1.1: Jane HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9ec4cd4f', 'Jane.HairA.LightMap.2048']],
    ],
    '5e34e275': [
        [Log, ['1.1: Jane HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['40fca454', 'Jane.HairA.MaterialMap.1024']],
    ],
    '40fca454': [
        [Log, ['1.1: Jane HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5e34e275', 'Jane.HairA.MaterialMap.2048']],
    ],
    '4aa12b36': [
        [Log, ['1.1: Jane HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f0aded31', 'Jane.HairA.NormalMap.1024']],
    ],
    'f0aded31': [
        [Log, ['1.1: Jane HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['9268a5af', '7b16a708'], 'Jane.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4aa12b36', 'Jane.HairA.NormalMap.2048']],
    ],

    'd1f56c7d': [
        [Log, ['1.1: Jane BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e62ae3b5', 'Jane.BodyA.Diffuse.1024']],
    ],
    'e62ae3b5': [
        [Log, ['1.1: Jane BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d1f56c7d', 'Jane.BodyA.Diffuse.2048']],
    ],
    '3087f82a': [
        [Log, ['1.1: Jane BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['52fa9861', 'Jane.BodyA.LightMap.1024']],
    ],
    '52fa9861': [
        [Log, ['1.1: Jane BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3087f82a', 'Jane.BodyA.LightMap.2048']],
    ],
    '99eae42e': [
        [Log, ['1.1: Jane BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5dce2408', 'Jane.BodyA.MaterialMap.1024']],
    ],
    '5dce2408': [
        [Log, ['1.1: Jane BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['99eae42e', 'Jane.BodyA.MaterialMap.2048']],
    ],
    '0165f71c': [
        [Log, ['1.1: Jane BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['387dfc9f', 'Jane.BodyA.NormalMap.1024']],
    ],
    '387dfc9f': [
        [Log, ['1.1: Jane BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['ba4255a5', 'e2c0144e'], 'Jane.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0165f71c', 'Jane.BodyA.NormalMap.2048']],
    ],



    // MARK: Koleda
    '242a8d48': [[Log, ['1.0: Koleda Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '3afb3865': [[Log, ['1.0: Koleda Body IB Hash',]], [AddIbCheckIfMissing,]],
    '0e74656e': [[Log, ['1.0: Koleda Head IB Hash',]], [AddIbCheckIfMissing,]],


    '1a9b182a': [
        [Log, ['1.2 -> 1.3: Koleda Hair Texcoord Hash',]],
        [UpdateHash, ['e35571a9',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz13RemapTexcoord, [
            '13_koleda_hair',
            ['4B', '2e', '2f', '2e'],
            ['4B', '2f', '2f', '2f']
        ]],
    ],
    'e3021a32': [
        [Log, ['1.2 -> 1.3: Koleda Body Texcoord Hash',]],
        [UpdateHash, ['38b31082',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz13RemapTexcoord, [
            '13_koleda_body',
            ['4B', '2e', '2f', '2e'],
            ['4B', '2f', '2f', '2f']
        ]],
    ],

    'f1045670': [
        [Log, ['1.0: Koleda HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['0e74656e', 'Koleda.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['200db5c4', 'Koleda.HeadA.Diffuse.2048']],
    ],
    '200db5c4': [
        [Log, ['1.0: Koleda HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['0e74656e', 'Koleda.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f1045670', 'Koleda.HeadA.Diffuse.1024']],
    ],


    'e8e89f00': [
        [Log, ['1.0: Koleda HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b0046e5a', 'Koleda.HairA.Diffuse.1024']],
    ],
    'b0046e5a': [
        [Log, ['1.0: Koleda HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e8e89f00', 'Koleda.HairA.Diffuse.2048']],
    ],
    '8042506d': [
        [Log, ['1.0: Koleda HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['144ab293', 'Koleda.HairA.LightMap.1024']],
    ],
    '144ab293': [
        [Log, ['1.0: Koleda HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8042506d', 'Koleda.HairA.LightMap.2048']],
    ],
    'd1aac666': [
        [Log, ['1.0: Koleda HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7a46b52a', 'Koleda.HairA.NormalMap.1024']],
    ],
    '7a46b52a': [
        [Log, ['1.0: Koleda HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['242a8d48', 'Koleda.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d1aac666', 'Koleda.HairA.NormalMap.2048']],
    ],


    '337fd6a2': [
        [Log, ['1.0: Koleda BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ce10237d', 'Koleda.BodyA.Diffuse.1024']],
    ],
    'ce10237d': [
        [Log, ['1.0: Koleda BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['337fd6a2', 'Koleda.BodyA.Diffuse.2048']],
    ],
    '78e0f9f5': [
        [Log, ['1.0: Koleda BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['db58787e', 'Koleda.BodyA.LightMap.1024']],
    ],
    'db58787e': [
        [Log, ['1.0: Koleda BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['78e0f9f5', 'Koleda.BodyA.LightMap.2048']],
    ],
    '6f34885f': [
        [Log, ['1.0: Koleda BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['02e6cb95', 'Koleda.BodyA.MaterialMap.1024']],
    ],
    '02e6cb95': [
        [Log, ['1.0: Koleda BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6f34885f', 'Koleda.BodyA.MaterialMap.2048']],
    ],
    'e71d134f': [
        [Log, ['1.0: Koleda BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0914d3d3', 'Koleda.BodyA.NormalMap.1024']],
    ],
    '0914d3d3': [
        [Log, ['1.0: Koleda BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['3afb3865', 'Koleda.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e71d134f', 'Koleda.BodyA.NormalMap.2048']],
    ],


    // MARK: Lighter
    '542b8aa9': [[Log, ['1.3: Lighter Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '8899e0fd': [[Log, ['1.3: Lighter Body IB Hash',]], [AddIbCheckIfMissing,]],
    '018b03f0': [[Log, ['1.3: Lighter Arm IB Hash',]], [AddIbCheckIfMissing,]],

    '039f30cf': [[Log, ['1.3 -> 1.4: Lighter Face IB Hash',]], [UpdateHash, ['dcc7bb78',]]],
    'dcc7bb78': [[Log, ['1.4: Lighter Face IB Hash',]], [AddIbCheckIfMissing,]],

    '0baec6b7': [[Log, ['1.3 -> 1.4: Lighter Body Position Hash',]], [UpdateHash, ['5e461440',]]],
    '710bca71': [[Log, ['1.3 -> 1.4: Lighter Body Texcoord Hash',]], [UpdateHash, ['25ad7289',]]],
    'af2e48a6': [[Log, ['1.3 -> 1.4: Lighter Arm Texcoord Hash',]], [UpdateHash, ['88aecee2',]]],

    '5e461440': [[Log, ['1.5 -> 1.6: Lighter Body Position Hash',]], [UpdateHash, ['f6bbabb5',]]],
    '25ad7289': [[Log, ['1.5 -> 1.6: Lighter Body Texcoord Hash',]], [UpdateHash, ['e1ae7f38',]]],

    '8ec33dd0': [
        [Log, ['1.3: Lighter FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['dcc7bb78', '039f30cf'], 'Lighter.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4524e91a', 'Lighter.FaceA.Diffuse.2048']],
    ],
    '4524e91a': [
        [Log, ['1.3: Lighter FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['dcc7bb78', '039f30cf'], 'Lighter.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8ec33dd0', 'Lighter.FaceA.Diffuse.1024']],
    ],

    '1cd2d442': [
        [Log, ['1.3: Lighter HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c5d60a1d', 'Lighter.HairA.Diffuse.2048']],
    ],
    '62ec7f01': [
        [Log, ['1.3: Lighter HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6d3f91bc', 'Lighter.HairA.LightMap.2048']],
    ],
    '8687f7b8': [
        [Log, ['1.3: Lighter HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d5ba9ea6', 'Lighter.HairA.MaterialMap.2048']],
    ],
    'c5d60a1d': [
        [Log, ['1.3: Lighter HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1cd2d442', 'Lighter.HairA.Diffuse.1024']],
    ],
    '6d3f91bc': [
        [Log, ['1.3: Lighter HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['62ec7f01', 'Lighter.HairA.LightMap.1024']],
    ],
    'd5ba9ea6': [
        [Log, ['1.3: Lighter HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['542b8aa9', 'Lighter.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8687f7b8', 'Lighter.HairA.MaterialMap.1024']],
    ],

    'be46890b': [
        [Log, ['1.3: Lighter BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5ed96bf2', 'Lighter.BodyA.Diffuse.2048']],
    ],
    '5b828635': [
        [Log, ['1.3: Lighter BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['da6f4dc0', 'Lighter.BodyA.LightMap.2048']],
    ],
    '65f3bb7c': [
        [Log, ['1.3: Lighter BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['94aebd7e', 'Lighter.BodyA.MaterialMap.2048']],
    ],
    '5ed96bf2': [
        [Log, ['1.3: Lighter BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['be46890b', 'Lighter.BodyA.Diffuse.1024']],
    ],
    'da6f4dc0': [
        [Log, ['1.3: Lighter BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5b828635', 'Lighter.BodyA.LightMap.1024']],
    ],
    '94aebd7e': [
        [Log, ['1.3: Lighter BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['8899e0fd', 'Lighter.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['65f3bb7c', 'Lighter.BodyA.MaterialMap.1024']],
    ],

    '6506987b': [
        [Log, ['1.3: Lighter ArmA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8b854866', 'Lighter.ArmA.Diffuse.2048']],
    ],
    '939a2e18': [
        [Log, ['1.3: Lighter ArmA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['547cbcd8', 'Lighter.ArmA.LightMap.2048']],
    ],
    '1684d3e4': [
        [Log, ['1.3: Lighter ArmA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3617c303', 'Lighter.ArmA.MaterialMap.2048']],
    ],
    '8b854866': [
        [Log, ['1.3: Lighter ArmA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6506987b', 'Lighter.ArmA.Diffuse.1024']],
    ],
    '547cbcd8': [
        [Log, ['1.3: Lighter ArmA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['939a2e18', 'Lighter.ArmA.LightMap.1024']],
    ],
    '3617c303': [
        [Log, ['1.3: Lighter ArmA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['018b03f0', 'Lighter.Arm.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1684d3e4', 'Lighter.ArmA.MaterialMap.1024']],
    ],



    // MARK: Lucy
    '69ad9d08': [[Log, ['1.3: Lucy Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '272dd7f6': [[Log, ['1.0: Lucy Snout IB Hash',]], [AddIbCheckIfMissing,]],
    '9b6370f6': [[Log, ['1.0: Lucy Belt IB Hash',]], [AddIbCheckIfMissing,]],
    'be5f4c7d': [[Log, ['1.3: Lucy Body IB Hash',]], [AddIbCheckIfMissing,]],
    '1fe6e084': [[Log, ['1.0: Lucy RedCloth IB Hash',]], [AddIbCheckIfMissing,]],
    'a0ed04de': [[Log, ['1.0: Lucy Helmet IB Hash',]], [AddIbCheckIfMissing,]],
    'df3e3965': [[Log, ['1.3: Lucy Head IB Hash',]], [AddIbCheckIfMissing,]],

    '5315f036': [[Log, ['1.2 -> 1.3: Lucy Hair Blend Hash',]], [UpdateHash, ['a37c7537',]]],
    '751e21a5': [[Log, ['1.2 -> 1.3: Lucy Hair Texcoord Hash',]], [UpdateHash, ['c8810832',]]],
    '198e99d7': [
        [Log, ['1.2 -> 1.3: Lucy Hair IB Hash',]],
        [UpdateHash, ['69ad9d08',]],
        [TransferIndexedSections, {
            'src_indices': ['0', '-1'],
            'trg_indices': ['0', '5253'],
        }]
    ],

    '5da9dafc': [[Log, ['1.2 -> 1.3: Lucy Body Position Hash',]], [UpdateHash, ['246b93e2',]]],
    'b94b02e8': [[Log, ['1.2 -> 1.3: Lucy Body Blend Hash',]], [UpdateHash, ['66948a0f',]]],
    '00f11ea6': [[Log, ['1.2 -> 1.3: Lucy Body Texcoord Hash',]], [UpdateHash, ['f60dbb9e',]]],
    'e0ad50ed': [[Log, ['1.2 -> 1.3: Lucy Body IB Hash',]], [UpdateHash, ['be5f4c7d',]]],

    'fca15ccb': [[Log, ['1.2 -> 1.3: Lucy Head IB Hash',]], [UpdateHash, ['df3e3965',]]],


    '483b418a': [[Log, ['1.2 -> 1.3: Lucy HeadA Diffuse 1024p Hash',]], [UpdateHash, ['2578d35b',]]],
    '2a6df536': [[Log, ['1.2 -> 1.3: Lucy HeadA Diffuse 1024p Hash',]], [UpdateHash, ['4e2d5baa',]]],

    '2578d35b': [
        [Log, ['1.3: Lucy HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['df3e3965', 'fca15ccb'], 'Lucy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['4e2d5baa', '2a6df536'], 'Lucy.HeadA.Diffuse.2048']],
    ],
    '4e2d5baa': [
        [Log, ['1.3: Lucy HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['df3e3965', 'fca15ccb'], 'Lucy.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['2578d35b', '483b418a'], 'Lucy.HeadA.Diffuse.1024']],
    ],


    'b50eb71c': [[Log, ['1.2 -> 1.3: Lucy HairA, SnoutA, BeltA Diffuse 1024p Hash',]], [UpdateHash, ['753baa45',]]],
    'd1241cfc': [[Log, ['1.2 -> 1.3: Lucy HairA, SnoutA, BeltA MaterialMap 1024p Hash',]], [UpdateHash, ['368f931c',]]],

    'aa513afa': [[Log, ['1.2 -> 1.3: Lucy HairA, SnoutA, BeltA Diffuse 2048p Hash',]], [UpdateHash, ['0fa60fe1',]]],
    '919b608c': [[Log, ['1.2 -> 1.3: Lucy HairA, SnoutA, BeltA MaterialMap 2048p Hash',]], [UpdateHash, ['068aba7f',]]],

    '0fa60fe1': [
        [Log, ['1.3: Lucy HairA, SnoutA, BeltA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, [['753baa45', 'b50eb71c'], 'Lucy.HairA.Diffuse.1024']],
    ],
    '753baa45': [
        [Log, ['1.3: Lucy HairA, SnoutA, BeltA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, [['0fa60fe1', 'aa513afa'], 'Lucy.HairA.Diffuse.2048']],
    ],
    '1a3b30ba': [
        [Log, ['1.0: Lucy HairA, SnoutA, BeltA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['810c0878', 'Lucy.HairA.LightMap.1024']],
    ],
    '810c0878': [
        [Log, ['1.0: Lucy HairA, SnoutA, BeltA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['1a3b30ba', 'Lucy.HairA.LightMap.2048']],
    ],
    '068aba7f': [
        [Log, ['1.3: Lucy HairA, SnoutA, BeltA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, [['368f931c', 'd1241cfc'], 'Lucy.HairA.MaterialMap.1024']],
    ],
    '368f931c': [
        [Log, ['1.3: Lucy HairA, SnoutA, BeltA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, [['068aba7f', '919b608c'], 'Lucy.HairA.MaterialMap.2048']],
    ],
    'edcb9661': [
        [Log, ['1.0: Lucy HairA, SnoutA, BeltA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['9114c7c7', 'Lucy.HairA.NormalMap.1024']],
    ],
    '9114c7c7': [
        [Log, ['1.0: Lucy HairA, SnoutA, BeltA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['edcb9661', 'Lucy.HairA.NormalMap.2048']],
    ],
    '474c7aa2': [
        [Log, ['1.0: Lucy BodyA, RedClothA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['f810e7ac', 'Lucy.BodyA.Diffuse.1024']],
    ],
    'f810e7ac': [
        [Log, ['1.0: Lucy BodyA, RedClothA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['474c7aa2', 'Lucy.BodyA.Diffuse.2048']],
    ],
    '855d9fa3': [
        [Log, ['1.0: Lucy BodyA, RedClothA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['e89f7814', 'Lucy.BodyA.LightMap.1024']],
    ],
    'e89f7814': [
        [Log, ['1.0: Lucy BodyA, RedClothA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['855d9fa3', 'Lucy.BodyA.LightMap.2048']],
    ],
    '1fd24fd8': [
        [Log, ['1.0: Lucy BodyA, RedClothA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['86ca6cfd', 'Lucy.BodyA.MaterialMap.1024']],
    ],
    '86ca6cfd': [
        [Log, ['1.0: Lucy BodyA, RedClothA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['1fd24fd8', 'Lucy.BodyA.MaterialMap.2048']],
    ],
    '463b4f55': [
        [Log, ['1.0: Lucy BodyA, RedClothA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['1711cafd', 'Lucy.BodyA.NormalMap.1024']],
    ],
    '1711cafd': [
        [Log, ['1.0: Lucy BodyA, RedClothA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['463b4f55', 'Lucy.BodyA.NormalMap.2048']],
    ],
    'a0be0ed3': [
        [Log, ['1.0: Lucy HelmetA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['919ab7e5', 'Lucy.HelmetA.Diffuse.1024']],
    ],
    '919ab7e5': [
        [Log, ['1.0: Lucy HelmetA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a0be0ed3', 'Lucy.HelmetA.Diffuse.2048']],
    ],
    '8d9a16c7': [
        [Log, ['1.0: Lucy HelmetA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6a8fca92', 'Lucy.HelmetA.LightMap.1024']],
    ],
    '6a8fca92': [
        [Log, ['1.0: Lucy HelmetA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8d9a16c7', 'Lucy.HelmetA.LightMap.2048']],
    ],
    'b3013a33': [
        [Log, ['1.0: Lucy HelmetA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4227db77', 'Lucy.HelmetA.MaterialMap.1024']],
    ],
    '4227db77': [
        [Log, ['1.0: Lucy HelmetA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b3013a33', 'Lucy.HelmetA.MaterialMap.2048']],
    ],
    'ca5fd23a': [
        [Log, ['1.0: Lucy HelmetA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f4d44970', 'Lucy.HelmetA.NormalMap.1024']],
    ],
    'f4d44970': [
        [Log, ['1.0: Lucy HelmetA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['a0ed04de', 'Lucy.Helmet.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ca5fd23a', 'Lucy.HelmetA.NormalMap.2048']],
    ],



    // MARK: Lycaon
    '060bc1ad': [[Log, ['1.0: Lycaon Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '395572dc': [[Log, ['1.3 -> 1.4: Lycaon Hair Texcoord Hash',]], [UpdateHash, ['b092c043',]]],

    '25196b7a': [[Log, ['1.3 -> 1.4: Lycaon Body IB Hash',]], [UpdateHash, ['6749b6e7',]]],
    '6749b6e7': [[Log, ['1.4: Lycaon Body IB Hash',]], [AddIbCheckIfMissing,]],

    '2a340ed5': [[Log, ['1.3 -> 1.4: Lycaon Body Draw Hash',]], [UpdateHash, ['25418598',]]],
    '949e688a': [[Log, ['1.3 -> 1.4: Lycaon Body Texcoord Hash',]], [UpdateHash, ['b950fda5',]]],
    'b68056b4': [
        [Log, ['1.3 -> 1.4: Lycaon Body Position Hash',]],
        [UpdateHash, ['8c7775ae',]],
        [Log, ['1.3 -> 1.4: Lycaon Body Blend Remap',]],
        [UpdateBufferBlendIndices, [
            '8c7775ae',
            [50, 51, 89, 90, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110],
            [51, 50, 90, 89, 669, 669, 669, 669, 669, 669, 669, 669, 669, 98, 99, 100, 101]
        ]]
    ],
    'a485180e': [
        [Log, ['1.3 -> 1.4: Lycaon Body Blend Remap',]],
        [UpdateHash, ['f2d1a929',]],
        [UpdateBufferBlendIndices, [
            'f2d1a929',
            [50, 51, 89, 90, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110],
            [51, 50, 90, 89, 669, 669, 669, 669, 669, 669, 669, 669, 669, 98, 99, 100, 101]
        ]],
    ],

    '5e710f36': [[Log, ['1.0: Lycaon Mask IB Hash',]], [AddIbCheckIfMissing,]],
    '22a1347b': [[Log, ['1.0: Lycaon Legs IB Hash',]], [AddIbCheckIfMissing,]],
    '6ffdfccb': [[Log, ['1.6: Lycaon Head IB Hash',]], [AddIbCheckIfMissing,]],

    '7074f97e': [[Log, ['1.5 -> 1.6: Lycaon Head Draw Hash',]], [UpdateHash, ['44277f65',]]],
    '4a666a39': [[Log, ['1.5 -> 1.6: Lycaon Head Position Hash',]], [UpdateHash, ['7e35ec22',]]],
    'c862a611': [[Log, ['1.5 -> 1.6: Lycaon Head Blend Hash',]], [UpdateHash, ['e2d4c532',]]],
    '6902f441': [[Log, ['1.? -> 1.?: Lycaon Head Texcoord Hash',]], [UpdateHash, ['b1edaf35',]]],
    'b1edaf35': [[Log, ['1.? -> 1.6: Lycaon Head Texcoord Hash',]], [UpdateHash, ['3adaebb3',]]],
    '7341e07b': [[Log, ['1.5 -> 1.6: Lycaon Head IB Hash',]], [UpdateHash, ['6ffdfccb',]]],

    '4f098897': [[Log, ['1.5 -> 1.6: Lycaon Head Diffuse 1024p Hash',]], [UpdateHash, ['2cc208a7',]]],
    '2cc208a7': [
        [Log, ['1.6: Lycaon HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['6ffdfccb', '7341e07b'], 'Lycaon.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['7077ebb1', 'd14f3284'], 'Lycaon.HeadA.Diffuse.2048']],
    ],
    'd14f3284': [[Log, ['1.5 -> 1.6: Lycaon HeadA Diffuse 2048p Hash',]], [UpdateHash, ['7077ebb1',]]],
    '7077ebb1': [
        [Log, ['1.6: Lycaon HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['6ffdfccb', '7341e07b'], 'Lycaon.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['2cc208a7', '4f098897'], 'Lycaon.HeadA.Diffuse.1024']],
    ],

    '61aaace5': [
        [Log, ['1.0: Lycaon HairA, MaskA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['3bd1b7e6', 'Lycaon.HairA.Diffuse.1024']],
    ],
    '3bd1b7e6': [
        [Log, ['1.0: Lycaon HairA, MaskA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['61aaace5', 'Lycaon.HairA.Diffuse.2048']],
    ],
    '3d6eb388': [[Log, ['1.3 -> 1.4: Lycaon HairA, MaskA LightMap 2048p Hash',]], [UpdateHash, ['04d061fe',]]],
    '04d061fe': [
        [Log, ['1.4: Lycaon HairA, MaskA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, [['4d878953', '4d4e8986'], 'Lycaon.HairA.LightMap.1024']],
    ],
    '4d4e8986': [[Log, ['1.3 -> 1.4: Lycaon HairA, MaskA LightMap 1024p Hash',]], [UpdateHash, ['4d878953',]]],
    '4d878953': [
        [Log, ['1.4: Lycaon HairA, MaskA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, [['04d061fe', '3d6eb388'], 'Lycaon.HairA.LightMap.2048']],
    ],
    '02bfcc69': [
        [Log, ['1.0: Lycaon HairA, MaskA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['ba0f8320', 'Lycaon.HairA.MaterialMap.1024']],
    ],
    'ba0f8320': [
        [Log, ['1.0: Lycaon HairA, MaskA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['02bfcc69', 'Lycaon.HairA.MaterialMap.2048']],
    ],
    '5817e801': [
        [Log, ['1.0: Lycaon HairA, MaskA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['71925b2f', 'Lycaon.HairA.NormalMap.1024']],
    ],
    '71925b2f': [
        [Log, ['1.0: Lycaon HairA, MaskA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['5817e801', 'Lycaon.HairA.NormalMap.2048']],
    ],

    '7169ec86': [
        [Log, ['1.0: Lycaon BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['82ad0c28', 'Lycaon.BodyA.Diffuse.1024']],
    ],
    '82ad0c28': [
        [Log, ['1.0: Lycaon BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7169ec86', 'Lycaon.BodyA.Diffuse.2048']],
    ],
    '565aa8be': [[Log, ['1.3 -> 1.4: Lycaon Body LightMap 2048p Hash',]], [UpdateHash, ['814db5bf',]]],
    '814db5bf': [
        [Log, ['1.0: Lycaon BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['122c655e', '7ea75154'], 'Lycaon.BodyA.LightMap.1024']],
    ],
    '7ea75154': [[Log, ['1.3 -> 1.4: Lycaon Body LightMap 1024p Hash',]], [UpdateHash, ['122c655e',]]],
    '122c655e': [
        [Log, ['1.0: Lycaon BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['814db5bf', '565aa8be'], 'Lycaon.BodyA.LightMap.2048']],
    ],
    '5a321eae': [
        [Log, ['1.0: Lycaon BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7cca7d7e', 'Lycaon.BodyA.MaterialMap.1024']],
    ],
    '7cca7d7e': [
        [Log, ['1.0: Lycaon BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5a321eae', 'Lycaon.BodyA.MaterialMap.2048']],
    ],
    'c8fd1702': [
        [Log, ['1.0: Lycaon BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bac2b2e2', 'Lycaon.BodyA.NormalMap.1024']],
    ],
    'bac2b2e2': [
        [Log, ['1.0: Lycaon BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['6749b6e7', '25196b7a'], 'Lycaon.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c8fd1702', 'Lycaon.BodyA.NormalMap.2048']],
    ],

    'd947066b': [
        [Log, ['1.0: Lycaon LegsA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['89bd4d58', 'Lycaon.LegsA.Diffuse.1024']],
    ],
    '89bd4d58': [
        [Log, ['1.0: Lycaon LegsA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d947066b', 'Lycaon.LegsA.Diffuse.2048']],
    ],
    '072e6786': [
        [Log, ['1.0: Lycaon LegsA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3dfdab95', 'Lycaon.LegsA.LightMap.1024']],
    ],
    '3dfdab95': [
        [Log, ['1.0: Lycaon LegsA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['072e6786', 'Lycaon.LegsA.LightMap.2048']],
    ],
    '4a4ea6dc': [
        [Log, ['1.0: Lycaon LegsA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['288e7fbd', 'Lycaon.LegsA.MaterialMap.1024']],
    ],
    '288e7fbd': [
        [Log, ['1.0: Lycaon LegsA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4a4ea6dc', 'Lycaon.LegsA.MaterialMap.2048']],
    ],
    '72f53876': [
        [Log, ['1.0: Lycaon LegsA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a6efc854', 'Lycaon.LegsA.NormalMap.1024']],
    ],
    'a6efc854': [
        [Log, ['1.0: Lycaon LegsA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['22a1347b', 'Lycaon.Legs.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['72f53876', 'Lycaon.LegsA.NormalMap.2048']],
    ],



    // MARK: Miyabi
    '4faabaac': [[Log, ['1.4: Miyabi Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '981c1a1e': [[Log, ['1.4: Miyabi Body IB Hash',]], [AddIbCheckIfMissing,]],
    'd8003df3': [[Log, ['1.4: Miyabi Legs IB Hash',]], [AddIbCheckIfMissing,]],
    'dbd59d30': [[Log, ['1.4: Miyabi Face IB Hash',]], [AddIbCheckIfMissing,]],

    '1d487fd5': [
        [Log, ['1.4: Miyabi FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['dbd59d30', 'Miyabi.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['92599e94', 'Miyabi.FaceA.Diffuse.1024']],
    ],
    '92599e94': [
        [Log, ['1.4: Miyabi FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['dbd59d30', 'Miyabi.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1d487fd5', 'Miyabi.FaceA.Diffuse.2048']],
    ],

    '012e84e9': [
        [Log, ['1.4: Miyabi HairA, LegsA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['ed6b94f7', 'Miyabi.HairA.Diffuse.1024']],
    ],
    'a6ea6d83': [
        [Log, ['1.4: Miyabi HairA, LegsA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['8b5708f4', 'Miyabi.HairA.LightMap.1024']],
    ],
    'd5462e37': [
        [Log, ['1.4: Miyabi HairA, LegsA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['a84d9003', 'Miyabi.HairA.MaterialMap.1024']],
    ],
    'ed6b94f7': [
        [Log, ['1.4: Miyabi HairA, LegsA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['012e84e9', 'Miyabi.HairA.Diffuse.2048']],
    ],
    '8b5708f4': [
        [Log, ['1.4: Miyabi HairA, LegsA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['a6ea6d83', 'Miyabi.HairA.LightMap.2048']],
    ],
    'a84d9003': [
        [Log, ['1.4: Miyabi HairA, LegsA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['d5462e37', 'Miyabi.HairA.MaterialMap.2048']],
    ],

    '09a2bbd1': [
        [Log, ['1.4: Miyabi BodyA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['1a3644e7', 'Miyabi.BodyA.Diffuse.1024']],
    ],
    'fd289380': [
        [Log, ['1.4: Miyabi BodyA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['0492f64a', 'Miyabi.BodyA.LightMap.1024']],
    ],
    '450770fd': [
        [Log, ['1.4: Miyabi BodyA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['168b1df9', 'Miyabi.BodyA.MaterialMap.1024']],
    ],
    '1a3644e7': [
        [Log, ['1.4: Miyabi BodyA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['09a2bbd1', 'Miyabi.BodyA.Diffuse.2048']],
    ],
    '0492f64a': [
        [Log, ['1.4: Miyabi BodyA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['fd289380', 'Miyabi.BodyA.LightMap.2048']],
    ],
    '168b1df9': [
        [Log, ['1.4: Miyabi BodyA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['450770fd', 'Miyabi.BodyA.MaterialMap.2048']],
    ],



    // MARK: Nekomata
    'da11fd85': [[Log, ['1.0: Nekomata Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '26a487ff': [[Log, ['1.0: Nekomata Body IB Hash',]], [AddIbCheckIfMissing,]],
    '74688145': [[Log, ['1.0: Nekomata Swords IB Hash',]], [AddIbCheckIfMissing,]],
    '37119851': [[Log, ['1.0: Nekomata Head IB Hash',]], [AddIbCheckIfMissing,]],


    'd9370c84': [[Log, ['1.0 -> 1.1: Nekomata HeadA Diffuse 1024p Hash',]], [UpdateHash, ['0834f635',]]],
    '0834f635': [
        [Log, ['1.1: Nekomata HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['37119851', 'Nekomata.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['ba411d22', 'fed3abbe'], 'Nekomata.HeadA.Diffuse.2048']],
    ],

    'fed3abbe': [[Log, ['1.0 -> 1.1: Nekomata HeadA Diffuse 2048p Hash',]], [UpdateHash, ['ba411d22',]]],
    'ba411d22': [
        [Log, ['1.1: Nekomata HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['37119851', 'Nekomata.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['0834f635', 'd9370c84'], 'Nekomata.HeadA.Diffuse.1024']],
    ],


    '2c317dda': [[Log, ['1.0 -> 1.1: Nekomata Body Position Hash',]], [UpdateHash, ['eaad1408',]]],
    'b5a4c084': [[Log, ['1.0 -> 1.1: Nekomata Body Texcoord Hash',]], [UpdateHash, ['f589a51f',]]],

    '6abb714e': [[Log, ['1.0 -> 1.1: Nekomata Swords Position Hash',]], [UpdateHash, ['3c4015fd',]]],
    '70f4875e': [[Log, ['1.0 -> 1.1: Nekomata Swords Texcoord Hash',]], [UpdateHash, ['2a4f8c9e',]]],


    '25f3ae9b': [
        [Log, ['1.0: Nekomata HairA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['aed3d8bd', 'Nekomata.HairA.Diffuse.1024']],
    ],
    'aed3d8bd': [
        [Log, ['1.0: Nekomata HairA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['25f3ae9b', 'Nekomata.HairA.Diffuse.2048']],
    ],
    '548c7f7d': [
        [Log, ['1.0: Nekomata HairA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['f8accad8', 'Nekomata.HairA.LightMap.1024']],
    ],
    'f8accad8': [
        [Log, ['1.0: Nekomata HairA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['548c7f7d', 'Nekomata.HairA.LightMap.2048']],
    ],
    '4ca5efc6': [
        [Log, ['1.0: Nekomata HairA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['0c22352c', 'Nekomata.HairA.MaterialMap.1024']],
    ],
    '0c22352c': [
        [Log, ['1.0: Nekomata HairA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['4ca5efc6', 'Nekomata.HairA.MaterialMap.2048']],
    ],
    '799eb07d': [
        [Log, ['1.0: Nekomata HairA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['c936ea68', 'Nekomata.HairA.NormalMap.1024']],
    ],
    'c936ea68': [
        [Log, ['1.0: Nekomata HairA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['799eb07d', 'Nekomata.HairA.NormalMap.2048']],
    ],
    'd3f67c0d': [
        [Log, ['1.0: -> 1.1: Nekomata HairB, BodyA, SwordsA Diffuse 2048p Hash',]],
        [UpdateHash, ['207b8e63',]],
    ],
    '207b8e63': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, [['60687646', '37d3154d'], 'Nekomata.HairB.Diffuse.1024']],
    ],
    '37d3154d': [
        [Log, ['1.0: -> 1.1: Nekomata HairB, BodyA, SwordsA Diffuse 1024p Hash',]],
        [UpdateHash, ['60687646',]],
    ],
    '60687646': [
        [Log, ['1.1 Nekomata HairB, BodyA, SwordsA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, [['207b8e63', 'd3f67c0d'], 'Nekomata.HairB.Diffuse.2048']],
    ],
    'fc53fc6f': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['4f3f7df0', 'Nekomata.HairB.LightMap.1024']],
    ],
    '4f3f7df0': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['fc53fc6f', 'Nekomata.HairB.LightMap.2048']],
    ],
    'f26828bd': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA MaterialMap 2048p Hash',]],
        [UpdateHash, ['b3286755',]],
    ],
    'b3286755': [
        [Log, ['1.1: Nekomata HairB, BodyA, SwordsA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, [['a5529690', '424da647'], 'Nekomata.HairB.MaterialMap.1024']],
    ],
    '424da647': [
        [Log, ['1.0 -> 1.1: Nekomata HairB, BodyA, SwordsA MaterialMap 1024p Hash',]],
        [UpdateHash, ['a5529690',]],
    ],
    'a5529690': [
        [Log, ['1.1: Nekomata HairB, BodyA, SwordsA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, [['b3286755', 'f26828bd'], 'Nekomata.HairB.MaterialMap.2048']],
    ],
    'ecaef71c': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['c1933b38', 'Nekomata.HairB.NormalMap.1024']],
    ],
    'c1933b38': [
        [Log, ['1.0: Nekomata HairB, BodyA, SwordsA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['ecaef71c', 'Nekomata.HairB.NormalMap.2048']],
    ],



    // MARK: Nicole
    '6847bbbd': [[Log, ['1.0: Nicole Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '5a4c1ef3': [[Log, ['1.0: Nicole Body IB Hash',]], [AddIbCheckIfMissing,]],
    '7435fc0e': [[Log, ['1.0: Nicole Head IB Hash',]], [AddIbCheckIfMissing,]],


    '6abd3dd3': [
        [Log, ['1.0: Nicole HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['7435fc0e', 'Nicole.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d1e84a34', 'Nicole.HeadA.Diffuse.2048']],
    ],
    'd1e84a34': [
        [Log, ['1.0: Nicole HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['7435fc0e', 'Nicole.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6abd3dd3', 'Nicole.HeadA.Diffuse.1024']],
    ],


    '6d3868f9': [
        [Log, ['1.0: Nicole HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7a45adcd', 'Nicole.HairA.Diffuse.1024']],
    ],
    '7a45adcd': [
        [Log, ['1.0: Nicole HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6d3868f9', 'Nicole.HairA.Diffuse.2048']],
    ],
    '1dfd9e16': [
        [Log, ['1.0: Nicole HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9adc04ed', 'Nicole.HairA.LightMap.1024']],
    ],
    '9adc04ed': [
        [Log, ['1.0: Nicole HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1dfd9e16', 'Nicole.HairA.LightMap.2048']],
    ],
    'bffb4a66': [
        [Log, ['1.0: Nicole HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b8db0209', 'Nicole.HairA.NormalMap.1024']],
    ],
    'b8db0209': [
        [Log, ['1.0: Nicole HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['6847bbbd', 'Nicole.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bffb4a66', 'Nicole.HairA.NormalMap.2048']],
    ],
    'f86ffe2c': [
        [Log, ['1.0: Nicole BodyA, BangbooA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9ee9b402', 'Nicole.BodyA.Diffuse.1024']],
    ],
    '9ee9b402': [
        [Log, ['1.0: Nicole BodyA, BangbooA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f86ffe2c', 'Nicole.BodyA.Diffuse.2048']],
    ],


    '80855e0f': [
        [Log, ['1.0: Nicole BodyA, BangbooA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2b5aa784', 'Nicole.BodyA.LightMap.1024']],
    ],
    '2b5aa784': [
        [Log, ['1.0: Nicole BodyA, BangbooA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['80855e0f', 'Nicole.BodyA.LightMap.2048']],
    ],
    '95cabef3': [
        [Log, ['1.0: Nicole BodyA, BangbooA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bb33129d', 'Nicole.BodyA.MaterialMap.1024']],
    ],
    'bb33129d': [
        [Log, ['1.0: Nicole BodyA, BangbooA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['95cabef3', 'Nicole.BodyA.MaterialMap.2048']],
    ],
    '8cf23419': [
        [Log, ['1.0: Nicole BodyA, BangbooA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['580df52d', 'Nicole.BodyA.NormalMap.1024']],
    ],
    '580df52d': [
        [Log, ['1.0: Nicole BodyA, BangbooA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['5a4c1ef3', 'Nicole.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8cf23419', 'Nicole.BodyA.NormalMap.2048']],
    ],



    // MARK: Piper
    '940454ef': [[Log, ['1.0: Piper Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '585da98b': [[Log, ['1.0: Piper Body IB Hash',]], [AddIbCheckIfMissing,]],
    'e11baad9': [[Log, ['1.0: Piper Head IB Hash',]], [AddIbCheckIfMissing,]],


    '4b06ffe6': [[Log, ['1.1 -> 1.2: Piper Face Diffuse 1024p Hash',]], [UpdateHash, ['f1c8f946',]]],
    'f1c8f946': [
        [Log, ['1.2: Piper HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['e11baad9', 'Piper.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['3b2eb1d9', '97a7862e'], 'Piper.HeadA.Diffuse.2048']],
    ],

    '97a7862e': [[Log, ['1.1 -> 1.2: Piper Face Diffuse 2048p Hash',]], [UpdateHash, ['3b2eb1d9',]]],
    '3b2eb1d9': [
        [Log, ['1.2: Piper HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['e11baad9', 'Piper.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['f1c8f946', '4b06ffe6'], 'Piper.HeadA.Diffuse.1024']],
    ],

    // Reverted in 1.2
    // '8b6b17f8': [
    //     [Log, ['1.0: -> 1.1: Piper Hair Texcoord Hash',]],
    //     [UpdateHash, ['fd1b9c29',]],
    //     [Log, ['+ Remapping texcoord buffer from stride 20 to 32',]],
    //     [update_buffer_element_width, [['BBBB', 'ee', 'ff', 'ee'], ['ffff', 'ee', 'ff', 'ee'], '1.1']],
    //     [Log, ['+ Setting texcoord vcolor alpha to 1',]],
    //     [update_buffer_element_value, [['ffff', 'ee', 'ff', 'ee'], ['xxx1', 'xx', 'xx', 'xx'], '1.1']]
    // ],

    'fd1b9c29': [
        [Log, ['1.1 -> 1.2: Piper Hair Texcoord Hash',]],
        [UpdateHash, ['8b6b17f8',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],
    '8b6b17f8': [[Log, ['1.3 -> 1.4: Piper Hair Texcoord Hash',]], [UpdateHash, ['1c6d41af',]],],

    'b2f3e6aa': [[Log, ['1.1 -> 1.2: Piper Body Position Hash',]], [UpdateHash, ['ffe8fea7',]],],
    'a0d146b3': [[Log, ['1.1 -> 1.2: Piper Body Texcoord Hash',]], [UpdateHash, ['a011f94e',]],],
    'a011f94e': [[Log, ['1.2 -> 1.3: Piper Body Texcoord Hash',]], [UpdateHash, ['6357b120',]],],
    '764276de': [[Log, ['1.2 -> 1.3: Piper Body Blend Hash',]], [UpdateHash, ['3d329807',]],],

    '69ed4d11': [
        [Log, ['1.0: Piper HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9b743eab', 'Piper.HairA.Diffuse.1024']],
    ],
    '9b743eab': [
        [Log, ['1.0: Piper HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['69ed4d11', 'Piper.HairA.Diffuse.2048']],
    ],
    '79953d32': [
        [Log, ['1.0: Piper HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['92acb4d4', 'Piper.HairA.LightMap.1024']],
    ],
    '92acb4d4': [
        [Log, ['1.0: Piper HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['79953d32', 'Piper.HairA.LightMap.2048']],
    ],
    'b3034dff': [
        [Log, ['1.0: Piper HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['78c42c66', 'Piper.HairA.MaterialMap.1024']],
    ],
    '78c42c66': [
        [Log, ['1.0: Piper HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b3034dff', 'Piper.HairA.MaterialMap.2048']],
    ],
    '7ca957d8': [
        [Log, ['1.0: Piper HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['db7dccbf', 'Piper.HairA.NormalMap.1024']],
    ],
    'db7dccbf': [
        [Log, ['1.0: Piper HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['940454ef', 'Piper.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7ca957d8', 'Piper.HairA.NormalMap.2048']],
    ],


    '621564e5': [[Log, ['1.2 -> 1.3: Piper BodyA Diffuse 1024p Hash',]], [UpdateHash, ['b450949d',]]],
    'b4b74e7e': [[Log, ['1.2 -> 1.3: Piper BodyA Diffuse 2048p Hash',]], [UpdateHash, ['fed40302',]]],

    'fed40302': [
        [Log, ['1.3: Piper BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['b450949d', '621564e5'], 'Piper.BodyA.Diffuse.1024']],
    ],
    'b450949d': [
        [Log, ['1.3: Piper BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['fed40302', 'b4b74e7e'], 'Piper.BodyA.Diffuse.2048']],
    ],
    '9cc2aaa0': [
        [Log, ['1.0: Piper BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['db9c7abf', 'Piper.BodyA.LightMap.1024']],
    ],
    'db9c7abf': [
        [Log, ['1.0: Piper BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9cc2aaa0', 'Piper.BodyA.LightMap.2048']],
    ],
    '7fdee30d': [
        [Log, ['1.0: Piper BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['73e72a1e', 'Piper.BodyA.MaterialMap.1024']],
    ],
    '73e72a1e': [
        [Log, ['1.0: Piper BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7fdee30d', 'Piper.BodyA.MaterialMap.2048']],
    ],
    '51f1ec36': [
        [Log, ['1.0: Piper BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['73a61e88', 'Piper.BodyA.NormalMap.1024']],
    ],
    '73a61e88': [
        [Log, ['1.0: Piper BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['585da98b', 'Piper.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['51f1ec36', 'Piper.BodyA.NormalMap.2048']],
    ],



    // MARK: Pulchra
    'bd385763': [[Log, ['1.6: Pulchra Hair Body IB Hash',]], [AddIbCheckIfMissing,]],
    '5b30f4da': [[Log, ['1.6: Pulchra Mask IB Hash',]], [AddIbCheckIfMissing,]],
    '62de5837': [[Log, ['1.6: Pulchra Face IB Hash',]], [AddIbCheckIfMissing,]],

    // Face
    '1626aafe': [
        [Log, ['1.6: Pulchra FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['62de5837', 'Pulchra.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['32f923f1', 'Pulchra.FaceA.Diffuse.1024']],
    ],
    '32f923f1': [
        [Log, ['1.6: Pulchra FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['62de5837', 'Pulchra.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1626aafe', 'Pulchra.FaceA.Diffuse.2048']],
    ],

    // Hair
    '57be79d6': [
        [Log, ['1.6: Pulchra HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fb0a816a', 'Pulchra.HairA.Diffuse.1024']],
    ],
    'fb0a816a': [
        [Log, ['1.6: Pulchra HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['57be79d6', 'Pulchra.HairA.Diffuse.2048']],
    ],
    '12c44063': [
        [Log, ['1.6: Pulchra HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f475e822', 'Pulchra.HairA.LightMap.1024']],
    ],
    'f475e822': [
        [Log, ['1.6: Pulchra HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['12c44063', 'Pulchra.HairA.LightMap.2048']],
    ],
    'a553df20': [
        [Log, ['1.6: Pulchra HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['64d75415', 'Pulchra.HairA.MaterialMap.1024']],
    ],
    '64d75415': [
        [Log, ['1.6: Pulchra HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a553df20', 'Pulchra.HairA.MaterialMap.2048']],
    ],

    // Body
    '7fc03353': [
        [Log, ['1.6: Pulchra BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bf7eba0f', 'Pulchra.BodyA.Diffuse.1024']],
    ],
    'bf7eba0f': [
        [Log, ['1.6: Pulchra BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7fc03353', 'Pulchra.BodyA.Diffuse.2048']],
    ],
    'd8462af0': [
        [Log, ['1.6: Pulchra BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['47040200', 'Pulchra.BodyA.LightMap.1024']],
    ],
    '47040200': [
        [Log, ['1.6: Pulchra BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d8462af0', 'Pulchra.BodyA.LightMap.2048']],
    ],
    'd404b789': [
        [Log, ['1.6: Pulchra BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a66a11d0', 'Pulchra.BodyA.MaterialMap.1024']],
    ],
    'a66a11d0': [
        [Log, ['1.6: Pulchra BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['bd385763', 'Pulchra.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d404b789', 'Pulchra.BodyA.MaterialMap.2048']],
    ],



    // MARK: Qingyi
    'f6e96452': [[Log, ['1.1: Qingyi Head IB Hash',]], [AddIbCheckIfMissing,]],
    '3cacba0a': [[Log, ['1.1: Qingyi Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '195857d8': [[Log, ['1.1: Qingyi Body IB Hash',]], [AddIbCheckIfMissing,]],

    '0b75cd32': [
        [Log, ['1.1: Qingyi HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f6e96452', 'Qingyi.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a58b5444', 'Qingyi.HeadA.Diffuse.1024']],
    ],
    'a58b5444': [
        [Log, ['1.1: Qingyi HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f6e96452', 'Qingyi.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0b75cd32', 'Qingyi.HeadA.Diffuse.2048']],
    ],

    '0643440c': [
        [Log, ['1.1 -> 1.2: Qingyi Hair Texcoord Hash',]],
        [UpdateHash, ['53a2b66e',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],

    '3212a0ca': [
        [Log, ['1.1: Qingyi HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a472db9a', 'Qingyi.HairA.Diffuse.1024']],
    ],
    '2910fbd0': [
        [Log, ['1.1: Qingyi HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fc1847a9', 'Qingyi.HairA.NormalMap.1024']],
    ],
    '6e3ac847': [
        [Log, ['1.1: Qingyi HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['683414c1', 'Qingyi.HairA.LightMap.1024']],
    ],
    '4a77fd3b': [
        [Log, ['1.1: Qingyi HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bfefa200', 'Qingyi.HairA.MaterialMap.1024']],
    ],
    'a472db9a': [
        [Log, ['1.1: Qingyi HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3212a0ca', 'Qingyi.HairA.Diffuse.2048']],
    ],
    'fc1847a9': [
        [Log, ['1.1: Qingyi HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2910fbd0', 'Qingyi.HairA.NormalMap.2048']],
    ],
    '683414c1': [
        [Log, ['1.1: Qingyi HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6e3ac847', 'Qingyi.HairA.LightMap.2048']],
    ],
    'bfefa200': [
        [Log, ['1.1: Qingyi HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['3cacba0a', 'Qingyi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4a77fd3b', 'Qingyi.HairA.MaterialMap.2048']],
    ],
    '1fa7e18e': [
        [Log, ['1.1: Qingyi BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['aa3c1147', 'Qingyi.BodyA.Diffuse.1024']],
    ],
    '542c6b04': [
        [Log, ['1.1: Qingyi BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4fbf05be', 'Qingyi.BodyA.NormalMap.1024']],
    ],
    '35c2a022': [
        [Log, ['1.1: Qingyi BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4a484257', 'Qingyi.BodyA.LightMap.1024']],
    ],
    '41054bb6': [
        [Log, ['1.1: Qingyi BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4e561ee5', 'Qingyi.BodyA.MaterialMap.1024']],
    ],
    'aa3c1147': [
        [Log, ['1.1: Qingyi BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1fa7e18e', 'Qingyi.BodyA.Diffuse.2048']],
    ],
    '4fbf05be': [
        [Log, ['1.1: Qingyi BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['542c6b04', 'Qingyi.BodyA.NormalMap.2048']],
    ],
    '4a484257': [
        [Log, ['1.1: Qingyi BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['35c2a022', 'Qingyi.BodyA.LightMap.2048']],
    ],
    '4e561ee5': [
        [Log, ['1.1: Qingyi BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['195857d8', 'Qingyi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['41054bb6', 'Qingyi.BodyA.MaterialMap.2048']],
    ],



    // MARK: Rina
    'cdb2cc7d': [[Log, ['1.0: Rina Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '2825da1e': [[Log, ['1.0: Rina Body IB Hash',]], [AddIbCheckIfMissing,]],
    '9f90cfaa': [[Log, ['1.0: Rina Head IB Hash',]], [AddIbCheckIfMissing,]],


    '7ecc44ce': [
        [Log, ['1.0: Rina HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['9f90cfaa', 'Rina.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['802a3281', 'Rina.HeadA.Diffuse.2048']],
    ],
    '802a3281': [
        [Log, ['1.0: Rina HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['9f90cfaa', 'Rina.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7ecc44ce', 'Rina.HeadA.Diffuse.1024']],
    ],


    'eb5d9d1c': [
        [Log, ['1.0: Rina HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4b005a79', 'Rina.HairA.Diffuse.1024']],
    ],
    '4b005a79': [
        [Log, ['1.0: Rina HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['eb5d9d1c', 'Rina.HairA.Diffuse.2048']],
    ],
    '1145d2b8': [
        [Log, ['1.0: Rina HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fb61499f', 'Rina.HairA.LightMap.1024']],
    ],
    'fb61499f': [
        [Log, ['1.0: Rina HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1145d2b8', 'Rina.HairA.LightMap.2048']],
    ],
    '82153e28': [
        [Log, ['1.0: Rina HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ea08fd96', 'Rina.HairA.MaterialMap.1024']],
    ],
    'ea08fd96': [
        [Log, ['1.0: Rina HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['82153e28', 'Rina.HairA.MaterialMap.2048']],
    ],
    '83ac7993': [
        [Log, ['1.0: Rina HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fa3c40e9', 'Rina.HairA.NormalMap.1024']],
    ],
    'fa3c40e9': [
        [Log, ['1.0: Rina HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['cdb2cc7d', 'Rina.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['83ac7993', 'Rina.HairA.NormalMap.2048']],
    ],


    'bf44bf67': [
        [Log, ['1.0: Rina BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a23e2e14', 'Rina.BodyA.Diffuse.1024']],
    ],
    'a23e2e14': [
        [Log, ['1.0: Rina BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['bf44bf67', 'Rina.BodyA.Diffuse.2048']],
    ],
    '95f4e9c8': [
        [Log, ['1.0: Rina BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fad76987', 'Rina.BodyA.LightMap.1024']],
    ],
    'fad76987': [
        [Log, ['1.0: Rina BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['95f4e9c8', 'Rina.BodyA.LightMap.2048']],
    ],
    'ed47722f': [
        [Log, ['1.0: Rina BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9fa6dfd3', 'Rina.BodyA.MaterialMap.1024']],
    ],
    '9fa6dfd3': [
        [Log, ['1.0: Rina BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ed47722f', 'Rina.BodyA.MaterialMap.2048']],
    ],
    '97637a8f': [
        [Log, ['1.0: Rina BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d6b20159', 'Rina.BodyA.NormalMap.1024']],
    ],
    'd6b20159': [
        [Log, ['1.0: Rina BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['2825da1e', 'Rina.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['97637a8f', 'Rina.BodyA.NormalMap.2048']],
    ],



    // MARK: Seth
    '35cf83ad': [[Log, ['1.1: Seth Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '00172ec3': [[Log, ['1.1: Seth Body IB Hash',]], [AddIbCheckIfMissing,]],
    '52f5aa74': [[Log, ['1.1: Seth Head IB Hash',]], [AddIbCheckIfMissing,]],

    // Reversed in v1.4
    // 'a91eeef2': [
    //     [Log,            ['1.2 -> 1.3: Seth Hair Texcoord Hash',]],
    //     [UpdateHash,    ['a72f760f',]],
    //     [Log,            ['+ Remapping texcoord buffer',]],
    //     [Zzz13RemapTexcoord, [
    //         '13_Seth_Hair',
    //         ['4B','2e','2f','2e'],
    //         ['4f','2e','2f','2e']
    //     ]],
    // ],
    'a72f760f': [
        [Log, ['1.3 -> 1.4: Seth Hair Texcoord Hash',]],
        [UpdateHash, ['a91eeef2',]],
        [Log, ['+ Remapping texcoord buffer',]],
        [Zzz13RemapTexcoord, [
            '14_Seth_Hair',
            ['4f', '2e', '2f', '2e'],
            ['4B', '2e', '2f', '2e']
        ]],
    ],

    'fe5b7534': [
        [Log, ['1.1: Seth HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['52f5aa74', 'Seth.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['09981aff', 'Seth.HeadA.Diffuse.2048']],
    ],
    '09981aff': [
        [Log, ['1.1: Seth HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['52f5aa74', 'Seth.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fe5b7534', 'Seth.HeadA.Diffuse.1024']],
    ],

    'dc8e244d': [
        [Log, ['1.1: Seth HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d3756c37', 'Seth.HairA.Diffuse.1024']],
    ],
    'd3756c37': [
        [Log, ['1.1: Seth HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dc8e244d', 'Seth.HairA.Diffuse.2048']],
    ],
    'd4de9ec1': [
        [Log, ['1.1: Seth HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c01dbf6c', 'Seth.HairA.LightMap.1024']],
    ],
    'c01dbf6c': [
        [Log, ['1.1: Seth HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d4de9ec1', 'Seth.HairA.LightMap.2048']],
    ],
    '3c256565': [
        [Log, ['1.1: Seth HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['833e9405', 'Seth.HairA.MaterialMap.1024']],
    ],
    '833e9405': [
        [Log, ['1.1: Seth HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3c256565', 'Seth.HairA.MaterialMap.2048']],
    ],
    '3376b58c': [
        [Log, ['1.1: Seth HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['24d52dd8', 'Seth.HairA.NormalMap.1024']],
    ],
    '24d52dd8': [
        [Log, ['1.1: Seth HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['35cf83ad', 'Seth.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3376b58c', 'Seth.HairA.NormalMap.2048']],
    ],

    '7f8416ab': [
        [Log, ['1.1: Seth BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dbc90150', 'Seth.BodyA.Diffuse.1024']],
    ],
    'dbc90150': [
        [Log, ['1.1: Seth BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7f8416ab', 'Seth.BodyA.Diffuse.2048']],
    ],
    '3d97c2ef': [
        [Log, ['1.1: Seth BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9436aa83', 'Seth.BodyA.LightMap.1024']],
    ],
    '9436aa83': [
        [Log, ['1.1: Seth BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3d97c2ef', 'Seth.BodyA.LightMap.2048']],
    ],
    '732d3f81': [
        [Log, ['1.1: Seth BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['56775fcb', 'Seth.BodyA.MaterialMap.1024']],
    ],
    '56775fcb': [
        [Log, ['1.1: Seth BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['732d3f81', 'Seth.BodyA.MaterialMap.2048']],
    ],
    'dde45d3d': [
        [Log, ['1.1: Seth BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['62b047c5', 'Seth.BodyA.NormalMap.1024']],
    ],
    '62b047c5': [
        [Log, ['1.1: Seth BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['00172ec3', 'Seth.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dde45d3d', 'Seth.BodyA.NormalMap.2048']],
    ],



    // MARK: Soldier0
    '217ec790': [[Log, ['1.6: Soldier0 Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '53d3f4e5': [[Log, ['1.6: Soldier0 Body IB Hash',]], [AddIbCheckIfMissing,]],
    'f2f539b8': [[Log, ['1.6: Soldier0 Face IB Hash',]], [AddIbCheckIfMissing,]],

    // Face
    '05d7b504': [
        [Log, ['1.6: Soldier0 FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f2f539b8', 'Soldier0.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['692c6d2b', 'Soldier0.FaceA.Diffuse.1024']],
    ],
    '692c6d2b': [
        [Log, ['1.6: Soldier0 FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f2f539b8', 'Soldier0.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['05d7b504', 'Soldier0.FaceA.Diffuse.2048']],
    ],

    // Hair
    'aa3d57ff': [
        [Log, ['1.6: Soldier0 HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8cb4086a', 'Soldier0.HairA.Diffuse.1024']],
    ],
    '8cb4086a': [
        [Log, ['1.6: Soldier0 HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['aa3d57ff', 'Soldier0.HairA.Diffuse.2048']],
    ],
    '8d42a55b': [
        [Log, ['1.6: Soldier0 HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['96a28554', 'Soldier0.HairA.LightMap.1024']],
    ],
    '96a28554': [
        [Log, ['1.6: Soldier0 HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8d42a55b', 'Soldier0.HairA.LightMap.2048']],
    ],
    '464847b3': [
        [Log, ['1.6: Soldier0 HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ce3e73be', 'Soldier0.HairA.MaterialMap.1024']],
    ],
    'ce3e73be': [
        [Log, ['1.6: Soldier0 HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['217ec790', 'Soldier0.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['464847b3', 'Soldier0.HairA.MaterialMap.2048']],
    ],

    // Body
    '627baf3f': [
        [Log, ['1.6: Soldier0 BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0acef326', 'Soldier0.BodyA.Diffuse.1024']],
    ],
    '0acef326': [
        [Log, ['1.6: Soldier0 BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['627baf3f', 'Soldier0.BodyA.Diffuse.2048']],
    ],
    '3a56b70b': [
        [Log, ['1.6: Soldier0 BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['625ad0eb', 'Soldier0.BodyA.LightMap.1024']],
    ],
    '625ad0eb': [
        [Log, ['1.6: Soldier0 BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3a56b70b', 'Soldier0.BodyA.LightMap.2048']],
    ],
    '7cfa12b6': [
        [Log, ['1.6: Soldier0 BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['dea3c5a0', 'Soldier0.BodyA.MaterialMap.1024']],
    ],
    'dea3c5a0': [
        [Log, ['1.6: Soldier0 BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['53d3f4e5', 'Soldier0.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7cfa12b6', 'Soldier0.BodyA.MaterialMap.2048']],
    ],



    // MARK: Soldier11
    '2fa74e2f': [[Log, ['1.0: Soldier11 Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'e3ee72d9': [[Log, ['1.0: Soldier11 Body IB Hash',]], [AddIbCheckIfMissing,]],
    'bb315c43': [[Log, ['1.0: Soldier11 Head IB Hash',]], [AddIbCheckIfMissing,]],


    '3c8697e8': [
        [Log, ['1.0: Soldier11 HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['bb315c43', 'Soldier11.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['67821d9d', 'Soldier11.HeadA.Diffuse.2048']],
    ],
    '67821d9d': [
        [Log, ['1.0: Soldier11 HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['bb315c43', 'Soldier11.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3c8697e8', 'Soldier11.HeadA.Diffuse.1024']],
    ],


    'b41b671a': [
        [Log, ['1.0: Soldier11 HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['15f933dc', 'Soldier11.HairA.Diffuse.1024']],
    ],
    '15f933dc': [
        [Log, ['1.0: Soldier11 HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b41b671a', 'Soldier11.HairA.Diffuse.2048']],
    ],
    '787659b9': [
        [Log, ['1.0: Soldier11 HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['baa3c836', 'Soldier11.HairA.LightMap.1024']],
    ],
    'baa3c836': [
        [Log, ['1.0: Soldier11 HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['787659b9', 'Soldier11.HairA.LightMap.2048']],
    ],
    '68d9644a': [
        [Log, ['1.0: Soldier11 HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4e08e50b', 'Soldier11.HairA.NormalMap.1024']],
    ],
    '4e08e50b': [
        [Log, ['1.0: Soldier11 HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['2fa74e2f', 'Soldier11.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['68d9644a', 'Soldier11.HairA.NormalMap.2048']],
    ],


    '640a8c01': [
        [Log, ['1.0: Soldier11 BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d7f2269b', 'Soldier11.BodyA.Diffuse.1024']],
    ],
    'd7f2269b': [
        [Log, ['1.0: Soldier11 BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['640a8c01', 'Soldier11.BodyA.Diffuse.2048']],
    ],
    '2f88092e': [
        [Log, ['1.0: Soldier11 BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ce581269', 'Soldier11.BodyA.LightMap.1024']],
    ],
    'ce581269': [
        [Log, ['1.0: Soldier11 BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2f88092e', 'Soldier11.BodyA.LightMap.2048']],
    ],
    '81db8cbe': [
        [Log, ['1.0: Soldier11 BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['874f9f68', 'Soldier11.BodyA.MaterialMap.1024']],
    ],
    '874f9f68': [
        [Log, ['1.0: Soldier11 BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['81db8cbe', 'Soldier11.BodyA.MaterialMap.2048']],
    ],
    'c94bb3d6': [
        [Log, ['1.0: Soldier11 BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['eb924a91', 'Soldier11.BodyA.NormalMap.1024']],
    ],
    'eb924a91': [
        [Log, ['1.0: Soldier11 BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['e3ee72d9', 'Soldier11.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c94bb3d6', 'Soldier11.BodyA.NormalMap.2048']],
    ],



    // MARK: Soukaku
    'fe70c7a3': [[Log, ['1.0: Soukaku Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'ced49ff8': [[Log, ['1.0: Soukaku Body IB Hash',]], [AddIbCheckIfMissing,]],
    '1315178e': [[Log, ['1.1: Soukaku Mask IB Hash',]], [AddIbCheckIfMissing,]],
    '020f9ac6': [[Log, ['1.1: Soukaku Head IB Hash',]], [AddIbCheckIfMissing,]],

    '01f7369e': [[Log, ['1.0 - 1.1: Soukaku Head IB Hash',]], [UpdateHash, ['020f9ac6',]]],


    '2ceacde6': [
        [Log, ['1.0: Soukaku HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['020f9ac6', '01f7369e'], 'Soukaku.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['427b39a4', 'Soukaku.HeadA.Diffuse.2048']],
    ],
    'c20a8c82': [
        [Log, ['1.0: Soukaku HeadA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['020f9ac6', '01f7369e'], 'Soukaku.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['17110d01', 'Soukaku.HeadA.Diffuse.2048']],
    ],
    '427b39a4': [
        [Log, ['1.0: Soukaku HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['020f9ac6', '01f7369e'], 'Soukaku.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2ceacde6', 'Soukaku.HeadA.Diffuse.1024']],
    ],
    '17110d01': [
        [Log, ['1.0: Soukaku HeadA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['020f9ac6', '01f7369e'], 'Soukaku.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c20a8c82', 'Soukaku.HeadA.Diffuse.1024']],
    ],


    '32ea0d00': [
        [Log, ['1.0: Soukaku HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['34a3ff5b', 'Soukaku.HairA.Diffuse.1024']],
    ],
    '34a3ff5b': [
        [Log, ['1.0: Soukaku HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['32ea0d00', 'Soukaku.HairA.Diffuse.2048']],
    ],
    '04654e94': [
        [Log, ['1.0: Soukaku HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7bbb3d02', 'Soukaku.HairA.LightMap.1024']],
    ],
    '7bbb3d02': [
        [Log, ['1.0: Soukaku HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['04654e94', 'Soukaku.HairA.LightMap.2048']],
    ],
    'd1444c52': [
        [Log, ['1.0: Soukaku HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['218689cf', 'Soukaku.HairA.MaterialMap.1024']],
    ],
    '218689cf': [
        [Log, ['1.0: Soukaku HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d1444c52', 'Soukaku.HairA.MaterialMap.2048']],
    ],
    '8498ee4d': [
        [Log, ['1.0: Soukaku HairA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0003126a', 'Soukaku.HairA.NormalMap.1024']],
    ],
    '0003126a': [
        [Log, ['1.0: Soukaku HairA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['fe70c7a3', 'Soukaku.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8498ee4d', 'Soukaku.HairA.NormalMap.2048']],
    ],


    'ee31954b': [
        [Log, ['1.0: Soukaku BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6f5d31fc', 'Soukaku.BodyA.Diffuse.1024']],
    ],
    '6f5d31fc': [
        [Log, ['1.0: Soukaku BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ee31954b', 'Soukaku.BodyA.Diffuse.2048']],
    ],
    '112a36a4': [
        [Log, ['1.0: Soukaku BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c0f0bb74', 'Soukaku.BodyA.LightMap.1024']],
    ],
    'c0f0bb74': [
        [Log, ['1.0: Soukaku BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['112a36a4', 'Soukaku.BodyA.LightMap.2048']],
    ],
    'd638ddf9': [
        [Log, ['1.0: Soukaku BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1ec28297', 'Soukaku.BodyA.MaterialMap.1024']],
    ],
    '1ec28297': [
        [Log, ['1.0: Soukaku BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['d638ddf9', 'Soukaku.BodyA.MaterialMap.2048']],
    ],
    '363e3d70': [
        [Log, ['1.0: Soukaku BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['77c48d32', 'Soukaku.BodyA.NormalMap.1024']],
    ],
    '77c48d32': [
        [Log, ['1.0: Soukaku BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['ced49ff8', 'Soukaku.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['363e3d70', 'Soukaku.BodyA.NormalMap.2048']],
    ],



    // MARK: Trigger
    '8e98ef9a': [[Log, ['1.6: Trigger Hair IB Hash',]], [AddIbCheckIfMissing,]],
    '7f32eeae': [[Log, ['1.6: Trigger Body IB Hash',]], [AddIbCheckIfMissing,]],
    '40cd4182': [[Log, ['1.6: Trigger Face IB Hash',]], [AddIbCheckIfMissing,]],

    // Face
    '88728785': [
        [Log, ['1.6: Trigger FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['40cd4182', 'Trigger.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cffc4b09', 'Trigger.FaceA.Diffuse.1024']],
    ],
    'cffc4b09': [
        [Log, ['1.6: Trigger FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['40cd4182', 'Trigger.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['88728785', 'Trigger.FaceA.Diffuse.2048']],
    ],

    // Hair
    'e826a564': [
        [Log, ['1.6: Trigger HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['984e7896', 'Trigger.HairA.Diffuse.1024']],
    ],
    '984e7896': [
        [Log, ['1.6: Trigger HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e826a564', 'Trigger.HairA.Diffuse.2048']],
    ],
    '23f2a4cf': [
        [Log, ['1.6: Trigger HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c321345c', 'Trigger.HairA.LightMap.1024']],
    ],
    'c321345c': [
        [Log, ['1.6: Trigger HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['23f2a4cf', 'Trigger.HairA.LightMap.2048']],
    ],
    'b24f1752': [
        [Log, ['1.6: Trigger HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4ee3c3fe', 'Trigger.HairA.MaterialMap.1024']],
    ],
    '4ee3c3fe': [
        [Log, ['1.6: Trigger HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['8e98ef9a', 'Trigger.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b24f1752', 'Trigger.HairA.MaterialMap.2048']],
    ],

    // Body
    '6631eadc': [
        [Log, ['1.6: Trigger BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8cffa733', 'Trigger.BodyA.Diffuse.1024']],
    ],
    '8cffa733': [
        [Log, ['1.6: Trigger BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6631eadc', 'Trigger.BodyA.Diffuse.2048']],
    ],
    '05250215': [
        [Log, ['1.6: Trigger BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2c72b961', 'Trigger.BodyA.LightMap.1024']],
    ],
    '2c72b961': [
        [Log, ['1.6: Trigger BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['05250215', 'Trigger.BodyA.LightMap.2048']],
    ],
    '985c5f52': [
        [Log, ['1.6: Trigger BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cd507047', 'Trigger.BodyA.MaterialMap.1024']],
    ],
    'cd507047': [
        [Log, ['1.6: Trigger BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['7f32eeae', 'Trigger.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['985c5f52', 'Trigger.BodyA.MaterialMap.2048']],
    ],



    // MARK: Vivian
    'c4eb6168': [[Log, ['1.7: Vivian Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'cd609d98': [[Log, ['1.7: Vivian Body IB Hash',]], [AddIbCheckIfMissing,]],
    '39944f20': [[Log, ['1.7: Vivian Face IB Hash',]], [AddIbCheckIfMissing,]],

    // Face
    '7b262ab6': [
        [Log, ['1.7: Vivian FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['39944f20', 'Vivian.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['66b5da8e', 'Vivian.FaceA.Diffuse.1024']],
    ],
    '66b5da8e': [
        [Log, ['1.7: Vivian FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['39944f20', 'Vivian.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7b262ab6', 'Vivian.FaceA.Diffuse.2048']],
    ],

    // Hair
    'a84d933f': [
        [Log, ['1.7: Vivian HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2df6f7b5', 'Vivian.HairA.Diffuse.1024']],
    ],
    '2df6f7b5': [
        [Log, ['1.7: Vivian HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a84d933f', 'Vivian.HairA.Diffuse.2048']],
    ],
    '8e3a20ea': [
        [Log, ['1.7: Vivian HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['36b80366', 'Vivian.HairA.LightMap.1024']],
    ],
    '36b80366': [
        [Log, ['1.7: Vivian HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['8e3a20ea', 'Vivian.HairA.LightMap.2048']],
    ],
    '2af66072': [
        [Log, ['1.7: Vivian HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2d5b1412', 'Vivian.HairA.MaterialMap.1024']],
    ],
    '2d5b1412': [
        [Log, ['1.7: Vivian HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['c4eb6168', 'Vivian.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['2af66072', 'Vivian.HairA.MaterialMap.2048']],
    ],

    // Body
    '0635e2dd': [
        [Log, ['1.7: Vivian BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['da41fbd6', 'Vivian.BodyA.Diffuse.1024']],
    ],
    'da41fbd6': [
        [Log, ['1.7: Vivian BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['0635e2dd', 'Vivian.BodyA.Diffuse.2048']],
    ],
    'e21c3a6b': [
        [Log, ['1.7: Vivian BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4a86e169', 'Vivian.BodyA.LightMap.1024']],
    ],
    '4a86e169': [
        [Log, ['1.7: Vivian BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['e21c3a6b', 'Vivian.BodyA.LightMap.2048']],
    ],
    '81f7d37c': [
        [Log, ['1.7: Vivian BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['fa650e6c', 'Vivian.BodyA.MaterialMap.1024']],
    ],
    'fa650e6c': [
        [Log, ['1.7: Vivian BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['cd609d98', 'Vivian.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['81f7d37c', 'Vivian.BodyA.MaterialMap.2048']],
    ],



    // MARK: Wise
    'f6cac296': [[Log, ['1.0: Wise Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'b1df5d22': [[Log, ['1.0: Wise Bag IB Hash',]], [AddIbCheckIfMissing,]],
    '8d6acf4e': [[Log, ['1.1: Wise Body IB Hash',]], [AddIbCheckIfMissing,]],
    '1fdaf388': [[Log, ['1.6: Wise Head IB Hash',]], [AddIbCheckIfMissing,]],

    '4894246e': [[Log, ['1.5 -> 1.6: Wise Head IB Hash',]], [UpdateHash, ['1fdaf388',]]],

    '054ea752': [[Log, ['1.0 -> 1.1: Wise Body IB Hash',]], [UpdateHash, ['8d6acf4e',]]],
    '73c48816': [[Log, ['1.0 -> 1.1: Wise Body Draw Hash',]], [UpdateHash, ['b581dc0a',]]],
    '9581de22': [[Log, ['1.0 -> 1.1: Wise Body Position Hash',]], [UpdateHash, ['67f21c9f',]]],
    'a012c752': [[Log, ['1.0 -> 1.1: Wise Body Texcoord Hash',]], [UpdateHash, ['f425bd04',]]],

    // Reversed in v1.6
    // '67f21c9f': [[Log, ['1.2 -> 1.3: Wise Body Position Hash',]], [UpdateHash, ['f6c5b9f3',]]],
    // 'f425bd04': [[Log, ['1.2 -> 1.3: Wise Body Texcoord Hash',]], [UpdateHash, ['a9d5b70d',]]],

    'f6c5b9f3': [[Log, ['1.5 -> 1.6: Wise Body Position Hash',]], [UpdateHash, ['67f21c9f',]]],
    'a9d5b70d': [[Log, ['1.5 -> 1.6: Wise Body Texcoord Hash',]], [UpdateHash, ['f425bd04',]]],

    'cb22cb95': [[Log, ['1.2 -> 1.3: Wise Bag Texcoord Hash',]], [UpdateHash, ['2ae08ae7',]]],

    '6c4ae8ce': [[Log, ['1.0 -> 1.1: Wise HeadA Diffuse 1024p Hash',]], [UpdateHash, ['588d7d2d',]]],

    '588d7d2d': [
        [Log, ['1.1: Wise HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['1fdaf388', '4894246e'], 'Wise.Head.IB', 'match_priority = 0\n']],
    ],
    '8f9d78c1': [
        [Log, ['1.0: Wise HeadA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['1fdaf388', '4894246e'], 'Wise.Head.IB', 'match_priority = 0\n']],
    ],


    '28005a5b': [
        [Log, ['1.0: Wise HairA, BagA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cb0d0c22', 'Wise.HairA.Diffuse.1024']],
    ],
    'cb0d0c22': [
        [Log, ['1.0: Wise HairA, BagA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['28005a5b', 'Wise.HairA.Diffuse.2048']],
    ],
    '1f21c633': [
        [Log, ['1.0: Wise HairA, BagA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6fcc4ad4', 'Wise.HairA.LightMap.1024']],
    ],
    '6fcc4ad4': [
        [Log, ['1.0: Wise HairA, BagA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['1f21c633', 'Wise.HairA.LightMap.2048']],
    ],
    '473f816d': [
        [Log, ['1.0: Wise HairA, BagA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['7c8b0713', 'Wise.HairA.MaterialMap.1024']],
    ],
    '7c8b0713': [
        [Log, ['1.0: Wise HairA, BagA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['473f816d', 'Wise.HairA.MaterialMap.2048']],
    ],
    '3b4f22ad': [
        [Log, ['1.0: Wise HairA, BagA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['db08bb73', 'Wise.HairA.NormalMap.1024']],
    ],
    'db08bb73': [
        [Log, ['1.0: Wise HairA, BagA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, ['f6cac296', 'Wise.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['3b4f22ad', 'Wise.HairA.NormalMap.2048']],
    ],


    '84529dab': [[Log, ['1.0 - 1.1: Wise BodyA Diffuse 2048p Hash',]], [UpdateHash, ['868709f2',]]],
    '868709f2': [
        [Log, ['1.1: Wise BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['3d7a53b0', 'ef76b675'], 'Wise.BodyA.Diffuse.1024']],
    ],

    'ef76b675': [[Log, ['1.0 - 1.1: Wise BodyA Diffuse 1024p Hash',]], [UpdateHash, ['3d7a53b0',]]],
    '3d7a53b0': [
        [Log, ['1.1: Wise BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['868709f2', '84529dab'], 'Wise.BodyA.Diffuse.2048']],
    ],
    '088718a9': [
        [Log, ['1.0: Wise BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['9f46182a', 'Wise.BodyA.LightMap.1024']],
    ],
    '9f46182a': [
        [Log, ['1.0: Wise BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['088718a9', 'Wise.BodyA.LightMap.2048']],
    ],
    'a5fdb5e7': [
        [Log, ['1.0: Wise BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['148283b7', 'Wise.BodyA.MaterialMap.1024']],
    ],
    '148283b7': [
        [Log, ['1.0: Wise BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a5fdb5e7', 'Wise.BodyA.MaterialMap.2048']],
    ],
    'f43c8025': [
        [Log, ['1.0: Wise BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['6807521d', 'Wise.BodyA.NormalMap.1024']],
    ],
    '6807521d': [
        [Log, ['1.0: Wise BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['8d6acf4e', '054ea752'], 'Wise.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f43c8025', 'Wise.BodyA.NormalMap.2048']],
    ],



    // MARK: Yanagi
    '9e12899f': [[Log, ['1.3: Yanagi Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'f478ee4c': [[Log, ['1.3: Yanagi Body IB Hash',]], [AddIbCheckIfMissing,]],
    // '27d49f0b': [[Log, ['1.3: Yanagi Sheathe IB Hash',]], [AddIbCheckIfMissing,]],
    // '2d7f2223': [[Log, ['1.3: Yanagi Weapon IB Hash',]],  [AddIbCheckIfMissing,]],
    '0817204c': [[Log, ['1.3: Yanagi Face IB Hash',]], [AddIbCheckIfMissing,]],


    'cfe7ab46': [
        [Log, ['1.3: Yanagi FaceA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['0817204c', 'Yanagi.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['95d9e92e', 'Yanagi.FaceA.Diffuse.2048']],
    ],
    '95d9e92e': [
        [Log, ['1.3: Yanagi FaceA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['0817204c', 'Yanagi.Face.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['cfe7ab46', 'Yanagi.FaceA.Diffuse.1024']],
    ],

    '4edb5c79': [
        [Log, ['1.3: Yanagi HairA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['ac5f6d76', 'Yanagi.HairA.Diffuse.2048']],
    ],
    '5a43d985': [
        [Log, ['1.3: Yanagi HairA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['99cfa935', 'Yanagi.HairA.LightMap.2048']],
    ],
    '486e3c42': [
        [Log, ['1.3: Yanagi HairA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f80b57f0', 'Yanagi.HairA.MaterialMap.2048']],
    ],
    'ac5f6d76': [
        [Log, ['1.3: Yanagi HairA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['4edb5c79', 'Yanagi.HairA.Diffuse.1024']],
    ],
    '99cfa935': [
        [Log, ['1.3: Yanagi HairA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['5a43d985', 'Yanagi.HairA.LightMap.1024']],
    ],
    'f80b57f0': [
        [Log, ['1.3: Yanagi HairA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['9e12899f', 'Yanagi.Hair.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['486e3c42', 'Yanagi.HairA.MaterialMap.1024']],
    ],


    'c119dbd7': [
        [Log, ['1.3: Yanagi BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c7c4f5c5', 'Yanagi.BodyA.Diffuse.2048']],
    ],
    'f60602ec': [
        [Log, ['1.3: Yanagi BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['08933e28', 'Yanagi.BodyA.LightMap.2048']],
    ],
    'b29f0188': [
        [Log, ['1.3: Yanagi BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c2ae5d2b', 'Yanagi.BodyA.MaterialMap.2048']],
    ],
    'c7c4f5c5': [
        [Log, ['1.3: Yanagi BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['c119dbd7', 'Yanagi.BodyA.Diffuse.1024']],
    ],
    '08933e28': [
        [Log, ['1.3: Yanagi BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['f60602ec', 'Yanagi.BodyA.LightMap.1024']],
    ],
    'c2ae5d2b': [
        [Log, ['1.3: Yanagi BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, ['f478ee4c', 'Yanagi.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['b29f0188', 'Yanagi.BodyA.MaterialMap.1024']],
    ],


    // 'aaccff06': [
    //     [Log,                           ['1.3: Yanagi WeaponA, SheatheA Diffuse 1024p Hash',]],
    //     [AddSectionIfMissing,        ['2d7f2223', 'Yanagi.Weapon.IB', 'match_priority = 0\n']],
    //     [AddSectionIfMissing,        ['27d49f0b', 'Yanagi.Sheathe.IB', 'match_priority = 0\n']],
    //     // [MultiplySectionIfMissing,   ['a1eabb9f', 'Yanagi.WeaponA.Diffuse.2048']],
    // ],
    // '8ef68839': [
    //     [Log,                           ['1.3: Yanagi WeaponA, SheatheA LightMap 1024p Hash',]],
    //     [AddSectionIfMissing,        ['2d7f2223', 'Yanagi.Weapon.IB', 'match_priority = 0\n']],
    //     [AddSectionIfMissing,        ['27d49f0b', 'Yanagi.Sheathe.IB', 'match_priority = 0\n']],
    //     // [MultiplySectionIfMissing,   ['a1eabb9f', 'Yanagi.WeaponA.LightMap.2048']],
    // ],
    // 'ecd8605e': [
    //     [Log,                           ['1.3: Yanagi WeaponA, SheatheA MaterialMap 1024p Hash',]],
    //     [AddSectionIfMissing,        ['2d7f2223', 'Yanagi.Weapon.IB', 'match_priority = 0\n']],
    //     [AddSectionIfMissing,        ['27d49f0b', 'Yanagi.Sheathe.IB', 'match_priority = 0\n']],
    //     // [MultiplySectionIfMissing,   ['a1eabb9f', 'Yanagi.WeaponA.MaterialMap.2048']],
    // ],



    // MARK: ZhuYuan
    '6619364f': [[Log, ['1.1: ZhuYuan Body IB Hash',]], [AddIbCheckIfMissing,]],
    '9821017e': [[Log, ['1.0: ZhuYuan Hair IB Hash',]], [AddIbCheckIfMissing,]],
    'fcac8411': [[Log, ['1.0: ZhuYuan Extras IB Hash',]], [AddIbCheckIfMissing,]],
    '5e717358': [[Log, ['1.0: ZhuYuan ShoulderAmmo IB Hash',]], [AddIbCheckIfMissing,]],
    'a63028ae': [[Log, ['1.0: ZhuYuan HipAmmo IB Hash',]], [AddIbCheckIfMissing,]],
    'f1c241b7': [[Log, ['1.0: ZhuYuan Head IB Hash',]], [AddIbCheckIfMissing,]],

    'a4aeb1d5': [[Log, ['1.0 -> 1.1: ZhuYuan Body IB Hash',]], [UpdateHash, ['6619364f',]]],


    'f3569f8d': [[Log, ['1.0 -> 1.1: ZhuYuan Body Position Hash',]], [UpdateHash, ['f595d24d',]]],
    '160872c0': [[Log, ['1.0 -> 1.1: ZhuYuan Body Texcoord Hash',]], [UpdateHash, ['cb885260',]]],


    // Reverted in 1.2
    // Comment out to prevent infinite loop :/
    // 'f3c092c5': [
    //     [Log, ['1.0 -> 1.1: ZhuYuan Hair Texcoord Hash',]],
    //     [UpdateHash, ['fdc045fc',]],
    //     [Log, ['+ Remapping texcoord buffer from stride 20 to 32',]],
    //     [update_buffer_element_width, [['BBBB', 'ee', 'ff', 'ee'], ['ffff', 'ee', 'ff', 'ee'], '1.1']],
    //     [Log, ['+ Setting texcoord vcolor alpha to 1',]],
    //     [update_buffer_element_value, [['ffff', 'ee', 'ff', 'ee'], ['xxx1', 'xx', 'xx', 'xx'], '1.1']]
    // ],

    'fdc045fc': [
        [Log, ['1.1 -> 1.2: ZhuYuan Hair Texcoord Hash',]],
        [UpdateHash, ['f3c092c5',]],
        [Log, ['+ Reverting texcoord buffer remap',]],
        [Zzz12ShrinkTexcoordColor, ['1.2',]]
    ],

    '138c7d76': [
        [Log, ['1.0: ZhuYuan HeadA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, ['f1c241b7', 'ZhuYuan.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['a1eabb9f', 'ZhuYuan.HeadA.Diffuse.2048']],
    ],
    'a1eabb9f': [
        [Log, ['1.0: ZhuYuan HeadA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, ['f1c241b7', 'ZhuYuan.Head.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, ['138c7d76', 'ZhuYuan.HeadA.Diffuse.1024']],
    ],


    '9b86c2f6': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['7f823598', 'ZhuYuan.HairA.Diffuse.2048']],
    ],
    '6eb346b9': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['4ac1defe', 'ZhuYuan.HairA.NormalMap.2048']],
    ],
    '8955095f': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['d4ee59c7', 'ZhuYuan.HairA.LightMap.2048']],
    ],
    '7d884663': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['12a407b1', 'ZhuYuan.HairA.MaterialMap.2048']],
    ],

    '7f823598': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['9b86c2f6', 'ZhuYuan.HairA.Diffuse.1024']],
    ],
    '4ac1defe': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['6eb346b9', 'ZhuYuan.HairA.NormalMap.1024']],
    ],
    'd4ee59c7': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['8955095f', 'ZhuYuan.HairA.LightMap.1024']],
    ],
    '12a407b1': [
        [Log, ['1.0: ZhuYuan HairA, ExtrasA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['7d884663', 'ZhuYuan.HairA.MaterialMap.1024']],
    ],


    'b57a8744': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA Diffuse 1024p Hash',]], [UpdateHash, ['f6795718',]]],
    '833bafd5': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA NormalMap 1024p Hash',]], [UpdateHash, ['729ea75a',]]],
    '18d00ac6': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA LightMap 1024p Hash',]], [UpdateHash, ['14b638b6',]]],
    '1daa379f': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA MaterialMap 1024p Hash',]], [UpdateHash, ['cd4dee2c',]]],

    'f6795718': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA Diffuse 1024p Hash',]], [UpdateHash, ['46af14f8',]]],
    '729ea75a': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA NormalMap 1024p Hash',]], [UpdateHash, ['d5b175bf',]]],
    '14b638b6': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA LightMap 1024p Hash',]], [UpdateHash, ['fb385169',]]],
    'cd4dee2c': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA MaterialMap 1024p Hash',]], [UpdateHash, ['29e2ebc5',]]],

    '46af14f8': [
        [Log, ['1.2: ZhuYuan BodyA Diffuse 1024p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['a271e894', '3ef82f41', 'c88e7660'], 'ZhuYuan.BodyA.Diffuse.2048']],
    ],
    'd5b175bf': [
        [Log, ['1.2: ZhuYuan BodyA NormalMap 1024p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['d81fb56e', '7195a311', 'a396c53a'], 'ZhuYuan.BodyA.NormalMap.2048']],
    ],
    'fb385169': [
        [Log, ['1.2: ZhuYuan BodyA LightMap 1024p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['d02bc66c', '80ebf536', '13a38449'], 'ZhuYuan.BodyA.LightMap.2048']],
    ],
    '29e2ebc5': [
        [Log, ['1.2: ZhuYuan BodyA MaterialMap 1024p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['3e808ef6', '10415de8', 'b4e20235'], 'ZhuYuan.BodyA.MaterialMap.2048']],
    ],

    'c88e7660': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA Diffuse 2048p Hash',]], [UpdateHash, ['3ef82f41',]]],
    'a396c53a': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA NormalMap 2048p Hash',]], [UpdateHash, ['7195a311',]]],
    '13a38449': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA LightMap 2048p Hash',]], [UpdateHash, ['80ebf536',]]],
    'b4e20235': [[Log, ['1.0 -> 1.1: ZhuYuan BodyA MaterialMap 2048p Hash',]], [UpdateHash, ['10415de8',]]],

    '3ef82f41': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA Diffuse 2048p Hash',]], [UpdateHash, ['a271e894',]]],
    '7195a311': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA NormalMap 2048p Hash',]], [UpdateHash, ['d81fb56e',]]],
    '80ebf536': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA LightMap 2048p Hash',]], [UpdateHash, ['d02bc66c',]]],
    '10415de8': [[Log, ['1.1 -> 1.2: ZhuYuan BodyA MaterialMap 2048p Hash',]], [UpdateHash, ['3e808ef6',]]],

    'a271e894': [
        [Log, ['1.2: ZhuYuan BodyA Diffuse 2048p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['46af14f8', 'f6795718', 'b57a8744'], 'ZhuYuan.BodyA.Diffuse.1024']],
    ],
    'd81fb56e': [
        [Log, ['1.2: ZhuYuan BodyA NormalMap 2048p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['d5b175bf', '729ea75a', '833bafd5'], 'ZhuYuan.BodyA.NormalMap.1024']],
    ],
    'd02bc66c': [
        [Log, ['1.2: ZhuYuan BodyA LightMap 2048p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['fb385169', '14b638b6', '18d00ac6'], 'ZhuYuan.BodyA.LightMap.1024']],
    ],
    '3e808ef6': [
        [Log, ['1.2: ZhuYuan BodyA MaterialMap 2048p Hash',]],
        [AddSectionIfMissing, [['a4aeb1d5', '6619364f'], 'ZhuYuan.Body.IB', 'match_priority = 0\n']],
        [MultiplySectionIfMissing, [['29e2ebc5', 'cd4dee2c', '1daa379f'], 'ZhuYuan.BodyA.MaterialMap.1024']],
    ],


    '222ae5ee': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA Diffuse 1024p Hash',]],
        [MultiplySectionIfMissing, ['6a33b25e', 'ZhuYuan.ExtrasB.Diffuse.2048']],
    ],
    '0fda74c3': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA NormalMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['fb35b7e9', 'ZhuYuan.ExtrasB.NormalMap.2048']],
    ],
    '790183b4': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA LightMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['e30f025b', 'ZhuYuan.ExtrasB.LightMap.2048']],
    ],
    '84842409': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA MaterialMap 1024p Hash',]],
        [MultiplySectionIfMissing, ['58d5c840', 'ZhuYuan.ExtrasB.MaterialMap.2048']],
    ],

    '6a33b25e': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA Diffuse 2048p Hash',]],
        [MultiplySectionIfMissing, ['222ae5ee', 'ZhuYuan.ExtrasB.Diffuse.1024']],
    ],
    'fb35b7e9': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA NormalMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['0fda74c3', 'ZhuYuan.ExtrasB.NormalMap.1024']],
    ],
    'e30f025b': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA LightMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['790183b4', 'ZhuYuan.ExtrasB.LightMap.1024']],
    ],
    '58d5c840': [
        [Log, ['1.0: ZhuYuan ExtrasB, ShoulderAmmoA, HipAmmoA MaterialMap 2048p Hash',]],
        [MultiplySectionIfMissing, ['84842409', 'ZhuYuan.ExtrasB.MaterialMap.1024']],
    ],
};