import fse from "fs-extra";
import type { NahidaDesktop } from "../..";

export class ModIniService {
    constructor(private readonly desktop: NahidaDesktop) {}

    public async updateToggleKey(
        iniPath: string,
        sectionName: string,
        variable: string,
        value: string,
    ): Promise<void> {
        try {
            const content = await fse.readFile(iniPath, "utf-8");
            const lines = content.split("\n");
            const newLines: string[] = [];
            const variableLine = `${variable} = ${value}`;

            let currentSection: string | null = null;
            let updated = false;
            let foundVariableInSection = false;
            let sectionStartIndex = -1;

            const insertIntoCurrentSection = () => {
                if (
                    currentSection?.toLowerCase() !== sectionName.toLowerCase() ||
                    foundVariableInSection ||
                    value === ""
                ) {
                    return;
                }

                let insertIndex = newLines.length;
                while (
                    insertIndex > sectionStartIndex + 1 &&
                    newLines[insertIndex - 1].trim() === ""
                ) {
                    insertIndex -= 1;
                }

                newLines.splice(insertIndex, 0, variableLine);
                updated = true;
                foundVariableInSection = true;
            };

            for (let i = 0; i < lines.length; i++) {
                const line = lines[i];
                const trimmedLine = line.trim();

                if (trimmedLine.startsWith("[") && trimmedLine.endsWith("]")) {
                    insertIntoCurrentSection();

                    currentSection = trimmedLine.slice(1, -1);
                    newLines.push(line);
                    sectionStartIndex = newLines.length - 1;
                    continue;
                }

                const lowerLine = trimmedLine.toLowerCase();
                const lowerVar = variable.toLowerCase();
                const isVariableLine =
                    lowerLine.startsWith(lowerVar + " =") || lowerLine.startsWith(lowerVar + "=");

                if (currentSection?.toLowerCase() === sectionName.toLowerCase() && isVariableLine) {
                    foundVariableInSection = true;
                    if (value === "") {
                        updated = true;
                    } else {
                        newLines.push(variableLine);
                        updated = true;
                    }
                } else {
                    newLines.push(line);
                }
            }

            insertIntoCurrentSection();

            if (updated) {
                const newContent = newLines.join("\n");
                try {
                    await fse.chmod(iniPath, 0o666);
                    await fse.writeFile(iniPath, newContent, "utf-8");
                } catch (error) {
                    if (
                        (error as NodeJS.ErrnoException).code === "EPERM" ||
                        (error as NodeJS.ErrnoException).code === "EACCES"
                    ) {
                        await fse.unlink(iniPath);
                        await fse.writeFile(iniPath, newContent, "utf-8");
                    } else {
                        throw error;
                    }
                }
            }
        } catch (error) {
            this.desktop.logger.error(error, `Mod:updateToggleKey:${iniPath}`);
            throw error;
        }
    }
}
